package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/cache/filehash"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/fsutil"
	"github.com/google/uuid"
)

// JobUpdateHandler defines the interface for handling job updates when files change
type JobUpdateHandler interface {
	StartUpdate(ctx context.Context)
	FinishUpdate(ctx context.Context)
	IsUpdating() bool
	HandleWordlistUpdate(ctx context.Context, wordlistID int, oldLines, newLines int64) error
	HandleRuleUpdate(ctx context.Context, ruleID int, oldCount, newCount int) error
}

// DirectoryMonitor watches directories for file changes
type DirectoryMonitor struct {
	wordlistManager  wordlist.Manager
	ruleManager      rule.Manager
	jobUpdateHandler JobUpdateHandler
	hashCache        *filehash.Cache
	wordlistDir      string
	ruleDir          string
	interval         time.Duration
	systemUserID     uuid.UUID
	stopChan         chan struct{}
	wg               sync.WaitGroup

	// Worker pool control
	maxWorkers int
	workerSem  chan struct{}

	// Track files being processed
	processingFiles sync.Map
	fileStatuses    sync.Map
}

// NewDirectoryMonitor creates a new directory monitor
func NewDirectoryMonitor(
	wordlistManager wordlist.Manager,
	ruleManager rule.Manager,
	wordlistDir, ruleDir string,
	interval time.Duration,
	systemUserID uuid.UUID,
	jobUpdateHandler JobUpdateHandler,
	hashCache *filehash.Cache,
) *DirectoryMonitor {
	// Default to 4 concurrent workers
	maxWorkers := 4

	return &DirectoryMonitor{
		wordlistManager:  wordlistManager,
		ruleManager:      ruleManager,
		jobUpdateHandler: jobUpdateHandler,
		hashCache:        hashCache,
		wordlistDir:      wordlistDir,
		ruleDir:          ruleDir,
		interval:         interval,
		systemUserID:     systemUserID,
		stopChan:         make(chan struct{}),
		maxWorkers:       maxWorkers,
		workerSem:        make(chan struct{}, maxWorkers),
	}
}

// Start begins monitoring directories
func (m *DirectoryMonitor) Start() {
	debug.Info("Starting directory monitor")
	m.wg.Add(2)

	// Perform initial checks immediately
	debug.Info("Performing initial directory checks")
	m.checkWordlistDirectory()
	m.checkRuleDirectory()

	// Monitor wordlist directory
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.checkWordlistDirectory()
			case <-m.stopChan:
				debug.Info("Stopping wordlist directory monitor")
				return
			}
		}
	}()

	// Monitor rule directory
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.checkRuleDirectory()
			case <-m.stopChan:
				debug.Info("Stopping rule directory monitor")
				return
			}
		}
	}()
}

// Stop stops monitoring directories
func (m *DirectoryMonitor) Stop() {
	debug.Info("Stopping directory monitor")
	close(m.stopChan)
	m.wg.Wait()
}

// isFileStable checks if a file has not been modified for the given duration
// or if its size hasn't changed for large files
func (m *DirectoryMonitor) isFileStable(path string, waitDuration time.Duration) (bool, error) {
	// Get initial file info
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	// For small files (< 100MB), just check modification time
	if fileInfo.Size() < 100*1024*1024 {
		return time.Since(fileInfo.ModTime()) > waitDuration, nil
	}

	// For larger files, check if size has changed
	initialSize := fileInfo.Size()
	initialModTime := fileInfo.ModTime()

	// Wait a short time and check again
	checkInterval := 5 * time.Second
	if checkInterval > waitDuration {
		checkInterval = waitDuration / 2
	}

	time.Sleep(checkInterval)

	// Get updated file info
	updatedInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	// If size or mod time changed, file is not stable
	if updatedInfo.Size() != initialSize || updatedInfo.ModTime() != initialModTime {
		return false, nil
	}

	return true, nil
}

// checkWordlistDirectory checks for new or modified wordlist files
func (m *DirectoryMonitor) checkWordlistDirectory() {
	debug.Debug("Checking wordlist directory: %s", m.wordlistDir)

	// Ensure directory exists
	if !fsutil.DirectoryExists(m.wordlistDir) {
		debug.Warning("Wordlist directory does not exist: %s", m.wordlistDir)
		return
	}

	// Walk directory recursively
	err := filepath.Walk(m.wordlistDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			debug.Error("Error accessing path %s: %v", path, err)
			return nil // Continue walking
		}

		// Skip directories and hidden files
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(m.wordlistDir, path)
		if err != nil {
			debug.Error("Failed to get relative path for %s: %v", path, err)
			return nil
		}

		// Skip potfile explicitly
		if info.Name() == "potfile.txt" || strings.Contains(relPath, "custom/potfile.txt") {
			debug.Debug("Skipping pot-file from directory monitoring: %s", relPath)
			return nil
		}

		// Skip association wordlists - they are managed separately in the association_wordlists table
		if strings.HasPrefix(relPath, "association/") || strings.HasPrefix(relPath, "association"+string(filepath.Separator)) {
			debug.Debug("Skipping association wordlist from directory monitoring: %s", relPath)
			return nil
		}

		// Skip if already being processed
		if _, isProcessing := m.processingFiles.Load(relPath); isProcessing {
			debug.Debug("Skipping file that is already being processed: %s", relPath)
			return nil
		}

		// Check if file is stable (not being written)
		isStable, err := m.isFileStable(path, 30*time.Second)
		if err != nil {
			debug.Error("Error checking if file is stable: %s: %v", path, err)
			return nil
		}

		if !isStable {
			debug.Debug("Skipping file that appears to be still transferring: %s", path)
			return nil
		}

		// Mark file as being processed
		m.processingFiles.Store(relPath, true)
		m.fileStatuses.Store(relPath, "queued")

		// Process file in a worker goroutine
		go func(fullPath, relPath string) {
			// Acquire worker semaphore slot
			m.workerSem <- struct{}{}
			defer func() {
				<-m.workerSem
				m.processingFiles.Delete(relPath)
				m.fileStatuses.Delete(relPath)
			}()

			m.fileStatuses.Store(relPath, "processing")
			debug.Info("Processing wordlist file: %s", relPath)

			ctx := context.Background()

			// Calculate MD5 hash first (faster than counting lines)
			// Uses cache to avoid recalculating unchanged files
			m.fileStatuses.Store(relPath, "calculating hash")
			md5Hash, err := m.hashCache.GetOrCalculate(fullPath)
			if err != nil {
				debug.Error("Failed to calculate MD5 hash for %s: %v", fullPath, err)
				m.fileStatuses.Store(relPath, "error: "+err.Error())
				return
			}

			// Check if file exists in database
			existingWordlist, err := m.wordlistManager.GetWordlistByFilename(ctx, relPath)
			if err != nil {
				debug.Error("Error checking if wordlist exists: %v", err)
				m.fileStatuses.Store(relPath, "error: "+err.Error())
				return
			}

			// If file exists with same MD5, skip it
			if existingWordlist != nil && existingWordlist.MD5Hash == md5Hash {
				debug.Debug("Skipping wordlist with unchanged hash: %s", relPath)
				m.fileStatuses.Store(relPath, "unchanged")
				return
			}
			
			// Skip pot-file from monitoring
			if existingWordlist != nil && existingWordlist.IsPotfile {
				debug.Info("Skipping pot-file from monitoring due to is_potfile flag: %s (ID: %d)", relPath, existingWordlist.ID)
				m.fileStatuses.Store(relPath, "potfile-excluded")
				return
			}

			// If file exists but MD5 is different, update it
			if existingWordlist != nil {
				debug.Info("Found modified wordlist file: %s", relPath)
				m.fileStatuses.Store(relPath, "updating")
				m.updateExistingWordlist(ctx, fullPath, relPath, existingWordlist.ID, md5Hash)
			} else {
				// Process new file
				debug.Info("Found new wordlist file: %s", relPath)
				m.fileStatuses.Store(relPath, "adding")
				m.processNewWordlistFile(ctx, fullPath, relPath, md5Hash)
			}
		}(path, relPath)

		return nil
	})

	if err != nil {
		debug.Error("Error walking wordlist directory: %v", err)
	}
}

// processNewWordlistFile processes a new wordlist file
func (m *DirectoryMonitor) processNewWordlistFile(ctx context.Context, fullPath, relPath string, md5Hash string) {
	// Get file info
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		debug.Error("Failed to get file info for %s: %v", fullPath, err)
		return
	}

	// Determine wordlist type based on directory structure
	wordlistType := determineWordlistType(relPath)

	// Create tags based on directory structure
	tags := generateTagsFromPath(relPath)
	tags = append(tags, "auto-imported") // Always add auto-imported tag

	// Create wordlist request with pending status
	name := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	req := &models.WordlistAddRequest{
		Name:         name,
		Description:  "Auto-imported wordlist",
		WordlistType: wordlistType,
		Format:       determineFormat(relPath),
		FileName:     relPath,
		MD5Hash:      md5Hash,
		FileSize:     fileInfo.Size(),
		WordCount:    0, // Will be updated after counting
		Tags:         tags,
	}

	// Add wordlist to database
	wordlist, err := m.wordlistManager.AddWordlist(ctx, req, m.systemUserID)
	if err != nil {
		debug.Error("Failed to add wordlist %s: %v", relPath, err)
		return
	}

	// Count words in a separate goroutine
	go func() {
		debug.Info("Counting words in new wordlist: %s", relPath)
		m.fileStatuses.Store(relPath, "counting words")

		// Use the wordlist manager's CountWordsInFile method instead of fsutil.CountLinesInFile
		wordCount, err := m.wordlistManager.CountWordsInFile(fullPath)
		if err != nil {
			debug.Error("Failed to count words in %s: %v", fullPath, err)
			m.fileStatuses.Store(relPath, "error counting: "+err.Error())
			return
		}

		// Verify wordlist
		verifyReq := &models.WordlistVerifyRequest{
			Status:    "verified",
			WordCount: &wordCount,
		}
		if err := m.wordlistManager.VerifyWordlist(ctx, wordlist.ID, verifyReq); err != nil {
			debug.Error("Failed to verify wordlist %s: %v", relPath, err)
			m.fileStatuses.Store(relPath, "error verifying: "+err.Error())
			return
		}

		m.fileStatuses.Store(relPath, "completed")
		debug.Info("Successfully imported wordlist: %s (ID: %d)", relPath, wordlist.ID)
	}()
}

// determineWordlistType determines the wordlist type based on the directory structure
func determineWordlistType(relPath string) string {
	// Default type
	wordlistType := "general"

	// Split the path to get directories
	dirs := strings.Split(filepath.Dir(relPath), string(filepath.Separator))

	// Check if the path contains specific directory names
	for _, dir := range dirs {
		dirLower := strings.ToLower(dir)
		switch dirLower {
		case "specialized":
			wordlistType = "specialized"
		case "targeted":
			wordlistType = "targeted"
		case "custom":
			wordlistType = "custom"
		}
	}

	debug.Debug("Determined wordlist type '%s' for path: %s", wordlistType, relPath)
	return wordlistType
}

// generateTagsFromPath generates tags based on the directory structure
func generateTagsFromPath(relPath string) []string {
	tags := []string{}

	// Split the path to get directories
	dirs := strings.Split(filepath.Dir(relPath), string(filepath.Separator))

	// Add each directory as a tag, excluding empty strings
	for _, dir := range dirs {
		if dir != "" {
			// Clean the tag (lowercase, replace spaces with hyphens)
			tag := strings.ToLower(dir)
			tag = strings.ReplaceAll(tag, " ", "-")
			tags = append(tags, tag)
		}
	}

	debug.Debug("Generated tags from path: %v for %s", tags, relPath)
	return tags
}

// updateExistingWordlist updates an existing wordlist in the database
func (m *DirectoryMonitor) updateExistingWordlist(ctx context.Context, fullPath, relPath string, wordlistID int, md5Hash string) {
	// Get file info
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		debug.Error("Failed to get file info for %s: %v", fullPath, err)
		return
	}

	// Update the MD5 hash and file size in the database
	debug.Info("Updating MD5 hash for wordlist %d: %s", wordlistID, md5Hash)
	if err := m.wordlistManager.UpdateWordlistFileInfo(ctx, wordlistID, md5Hash, fileInfo.Size()); err != nil {
		debug.Error("Failed to update wordlist file info: %v", err)
		return
	}

	// Determine wordlist type based on directory structure
	wordlistType := determineWordlistType(relPath)

	// Create tags based on directory structure
	tags := generateTagsFromPath(relPath)
	tags = append(tags, "auto-imported", "updated") // Add standard tags

	// Update metadata
	updateReq := &models.WordlistUpdateRequest{
		Name:         strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath)),
		Description:  "Auto-imported wordlist (updated)",
		WordlistType: wordlistType,
		Format:       determineFormat(relPath),
		Tags:         tags,
	}

	if _, err := m.wordlistManager.UpdateWordlist(ctx, wordlistID, updateReq, m.systemUserID); err != nil {
		debug.Error("Failed to update wordlist metadata: %v", err)
		return
	}

	// Set to pending status
	verifyReq := &models.WordlistVerifyRequest{
		Status: "pending",
	}
	if err := m.wordlistManager.VerifyWordlist(ctx, wordlistID, verifyReq); err != nil {
		debug.Error("Failed to set wordlist to pending status: %v", err)
		return
	}

	// Count words in a separate goroutine
	go func() {
		debug.Info("Counting words in updated wordlist: %s", relPath)
		m.fileStatuses.Store(relPath, "counting words")

		// Use the wordlist manager's CountWordsInFile method instead of fsutil.CountLinesInFile
		wordCount, err := m.wordlistManager.CountWordsInFile(fullPath)
		if err != nil {
			debug.Error("Failed to count words in %s: %v", fullPath, err)
			m.fileStatuses.Store(relPath, "error counting: "+err.Error())
			return
		}

		// Get the old word count before updating
		oldWordlist, err := m.wordlistManager.GetWordlist(ctx, wordlistID)
		oldLines := int64(0)
		if err == nil && oldWordlist != nil {
			oldLines = oldWordlist.WordCount
		}

		// Verify wordlist with updated count
		verifyReq := &models.WordlistVerifyRequest{
			Status:    "verified",
			WordCount: &wordCount,
		}
		if err := m.wordlistManager.VerifyWordlist(ctx, wordlistID, verifyReq); err != nil {
			debug.Error("Failed to verify updated wordlist %s: %v", relPath, err)
			m.fileStatuses.Store(relPath, "error verifying: "+err.Error())
			return
		}

		// Notify job update handler about the wordlist change
		if m.jobUpdateHandler != nil && !m.jobUpdateHandler.IsUpdating() {
			m.jobUpdateHandler.StartUpdate(ctx)
			defer m.jobUpdateHandler.FinishUpdate(ctx)

			newLines := int64(wordCount)
			if err := m.jobUpdateHandler.HandleWordlistUpdate(ctx, wordlistID, oldLines, newLines); err != nil {
				debug.Error("Failed to update jobs for wordlist change: %v", err)
			}
		}

		m.fileStatuses.Store(relPath, "completed")
		debug.Info("Successfully updated wordlist: %s (ID: %d) with MD5: %s", relPath, wordlistID, md5Hash)
	}()
}

// determineFormat determines the format of a wordlist file
func determineFormat(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".gz" || ext == ".zip" {
		return "compressed"
	}
	return "plaintext"
}

// checkRuleDirectory checks for new or modified rule files
func (m *DirectoryMonitor) checkRuleDirectory() {
	debug.Debug("Checking rule directory: %s", m.ruleDir)

	// Ensure directory exists
	if !fsutil.DirectoryExists(m.ruleDir) {
		debug.Warning("Rule directory does not exist: %s", m.ruleDir)
		return
	}

	// Walk directory recursively
	err := filepath.Walk(m.ruleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			debug.Error("Error accessing path %s: %v", path, err)
			return nil // Continue walking
		}

		// Skip directories and hidden files
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(m.ruleDir, path)
		if err != nil {
			debug.Error("Failed to get relative path for %s: %v", path, err)
			return nil
		}

		// Skip if already being processed
		if _, isProcessing := m.processingFiles.Load(relPath); isProcessing {
			debug.Debug("Skipping file that is already being processed: %s", relPath)
			return nil
		}

		// Check if file is stable (not being written)
		isStable, err := m.isFileStable(path, 30*time.Second)
		if err != nil {
			debug.Error("Error checking if file is stable: %s: %v", path, err)
			return nil
		}

		if !isStable {
			debug.Debug("Skipping file that appears to be still transferring: %s", path)
			return nil
		}

		// Mark file as being processed
		m.processingFiles.Store(relPath, true)
		m.fileStatuses.Store(relPath, "queued")

		// Process file in a worker goroutine
		go func(fullPath, relPath string) {
			// Acquire worker semaphore slot
			m.workerSem <- struct{}{}
			defer func() {
				<-m.workerSem
				m.processingFiles.Delete(relPath)
				m.fileStatuses.Delete(relPath)
			}()

			m.fileStatuses.Store(relPath, "processing")
			debug.Info("Processing rule file: %s", relPath)

			ctx := context.Background()

			// Calculate MD5 hash first (faster than counting lines)
			// Uses cache to avoid recalculating unchanged files
			m.fileStatuses.Store(relPath, "calculating hash")
			md5Hash, err := m.hashCache.GetOrCalculate(fullPath)
			if err != nil {
				debug.Error("Failed to calculate MD5 hash for %s: %v", fullPath, err)
				m.fileStatuses.Store(relPath, "error: "+err.Error())
				return
			}

			// Check if file exists in database
			existingRule, err := m.ruleManager.GetRuleByFilename(ctx, relPath)
			if err != nil {
				debug.Error("Error checking if rule exists: %v", err)
				m.fileStatuses.Store(relPath, "error: "+err.Error())
				return
			}

			// If file exists with same MD5, skip it
			if existingRule != nil && existingRule.MD5Hash == md5Hash {
				debug.Debug("Skipping rule with unchanged hash: %s", relPath)
				m.fileStatuses.Store(relPath, "unchanged")
				return
			}

			// If file exists but MD5 is different, update it
			if existingRule != nil {
				debug.Info("Found modified rule file: %s", relPath)
				m.fileStatuses.Store(relPath, "updating")
				m.updateExistingRule(ctx, fullPath, relPath, existingRule.ID, md5Hash)
			} else {
				// Process new file
				debug.Info("Found new rule file: %s", relPath)
				m.fileStatuses.Store(relPath, "adding")
				m.processNewRuleFile(ctx, fullPath, relPath, md5Hash)
			}
		}(path, relPath)

		return nil
	})

	if err != nil {
		debug.Error("Error walking rule directory: %v", err)
	}
}

// processNewRuleFile processes a new rule file
func (m *DirectoryMonitor) processNewRuleFile(ctx context.Context, fullPath, relPath string, md5Hash string) {
	// Get file info
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		debug.Error("Failed to get file info for %s: %v", fullPath, err)
		return
	}

	// Determine rule type based on directory structure and path
	ruleType := determineRuleType(relPath)

	// Create tags based on directory structure
	tags := generateTagsFromPath(relPath)
	tags = append(tags, "auto-imported") // Always add auto-imported tag

	// Create rule request with pending status
	name := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	req := &models.RuleAddRequest{
		Name:        name,
		Description: "Auto-imported rule",
		RuleType:    ruleType,
		FileName:    relPath,
		MD5Hash:     md5Hash,
		FileSize:    fileInfo.Size(),
		RuleCount:   0, // Will be updated after counting
		Tags:        tags,
	}

	// Add rule to database
	rule, err := m.ruleManager.AddRule(ctx, req, m.systemUserID)
	if err != nil {
		debug.Error("Failed to add rule %s: %v", relPath, err)
		return
	}

	// Count rules in a separate goroutine
	go func() {
		debug.Info("Counting rules in new rule file: %s", relPath)
		m.fileStatuses.Store(relPath, "counting rules")

		// Use the rule manager's CountRulesInFile method instead of fsutil.CountLinesInFile
		ruleCount, err := m.ruleManager.CountRulesInFile(fullPath)
		if err != nil {
			debug.Error("Failed to count rules in %s: %v", fullPath, err)
			m.fileStatuses.Store(relPath, "error counting: "+err.Error())
			return
		}

		// Verify rule
		verifyReq := &models.RuleVerifyRequest{
			Status:    "verified",
			RuleCount: &ruleCount,
		}
		if err := m.ruleManager.VerifyRule(ctx, rule.ID, verifyReq); err != nil {
			debug.Error("Failed to verify rule %s: %v", relPath, err)
			m.fileStatuses.Store(relPath, "error verifying: "+err.Error())
			return
		}

		m.fileStatuses.Store(relPath, "completed")
		debug.Info("Successfully imported rule: %s (ID: %d)", relPath, rule.ID)
	}()
}

// determineRuleType determines the rule type based on the directory structure and path
func determineRuleType(relPath string) string {
	// Default type
	ruleType := "hashcat"

	// Check if the path contains "john" (case-insensitive)
	if strings.Contains(strings.ToLower(relPath), "john") {
		ruleType = "john"
		return ruleType
	}

	// Split the path to get directories
	dirs := strings.Split(filepath.Dir(relPath), string(filepath.Separator))

	// Check if any directory indicates a specific rule type
	for _, dir := range dirs {
		dirLower := strings.ToLower(dir)
		switch dirLower {
		case "john":
			ruleType = "john"
		case "custom":
			ruleType = "custom"
		}
	}

	debug.Debug("Determined rule type '%s' for path: %s", ruleType, relPath)
	return ruleType
}

// updateExistingRule updates an existing rule in the database
func (m *DirectoryMonitor) updateExistingRule(ctx context.Context, fullPath, relPath string, ruleID int, md5Hash string) {
	// Get file info
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		debug.Error("Failed to get file info for %s: %v", fullPath, err)
		return
	}

	// Update the MD5 hash and file size in the database
	debug.Info("Updating MD5 hash for rule %d: %s", ruleID, md5Hash)
	if err := m.ruleManager.UpdateRuleFileInfo(ctx, ruleID, md5Hash, fileInfo.Size()); err != nil {
		debug.Error("Failed to update rule file info: %v", err)
		return
	}

	// Determine rule type based on directory structure and path
	ruleType := determineRuleType(relPath)

	// Create tags based on directory structure
	tags := generateTagsFromPath(relPath)
	tags = append(tags, "auto-imported", "updated") // Add standard tags

	// Update metadata
	updateReq := &models.RuleUpdateRequest{
		Name:        strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath)),
		Description: "Auto-imported rule (updated)",
		RuleType:    ruleType,
		Tags:        tags,
	}

	if _, err := m.ruleManager.UpdateRule(ctx, ruleID, updateReq, m.systemUserID); err != nil {
		debug.Error("Failed to update rule metadata: %v", err)
		return
	}

	// Set to pending status
	verifyReq := &models.RuleVerifyRequest{
		Status: "pending",
	}
	if err := m.ruleManager.VerifyRule(ctx, ruleID, verifyReq); err != nil {
		debug.Error("Failed to set rule to pending status: %v", err)
		return
	}

	// Count rules in a separate goroutine
	go func() {
		debug.Info("Counting rules in updated rule file: %s", relPath)
		m.fileStatuses.Store(relPath, "counting rules")

		// Use the rule manager's CountRulesInFile method instead of fsutil.CountLinesInFile
		ruleCount, err := m.ruleManager.CountRulesInFile(fullPath)
		if err != nil {
			debug.Error("Failed to count rules in %s: %v", fullPath, err)
			m.fileStatuses.Store(relPath, "error counting: "+err.Error())
			return
		}

		// Get the old rule count before updating
		oldRule, err := m.ruleManager.GetRule(ctx, ruleID)
		oldCount := 0
		if err == nil && oldRule != nil {
			oldCount = int(oldRule.RuleCount)
		}

		// Verify rule with updated count
		verifyReq := &models.RuleVerifyRequest{
			Status:    "verified",
			RuleCount: &ruleCount,
		}
		if err := m.ruleManager.VerifyRule(ctx, ruleID, verifyReq); err != nil {
			debug.Error("Failed to verify updated rule %s: %v", relPath, err)
			m.fileStatuses.Store(relPath, "error verifying: "+err.Error())
			return
		}

		// Notify job update handler about the rule change
		if m.jobUpdateHandler != nil && !m.jobUpdateHandler.IsUpdating() {
			m.jobUpdateHandler.StartUpdate(ctx)
			defer m.jobUpdateHandler.FinishUpdate(ctx)

			newCount := int(ruleCount)
			if err := m.jobUpdateHandler.HandleRuleUpdate(ctx, ruleID, oldCount, newCount); err != nil {
				debug.Error("Failed to update jobs for rule change: %v", err)
			}
		}

		m.fileStatuses.Store(relPath, "completed")
		debug.Info("Successfully updated rule: %s (ID: %d) with MD5: %s", relPath, ruleID, md5Hash)
	}()
}

// GetProcessingStatus returns the status of all files currently being processed
func (m *DirectoryMonitor) GetProcessingStatus() map[string]string {
	status := make(map[string]string)

	m.fileStatuses.Range(func(key, value interface{}) bool {
		if keyStr, ok := key.(string); ok {
			if valueStr, ok := value.(string); ok {
				status[keyStr] = valueStr
			}
		}
		return true
	})

	return status
}

// GetProcessingCount returns the number of files currently being processed
func (m *DirectoryMonitor) GetProcessingCount() int {
	count := 0

	m.processingFiles.Range(func(_, _ interface{}) bool {
		count++
		return true
	})

	return count
}
