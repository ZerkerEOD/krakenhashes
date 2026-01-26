package processor

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/hashutils"
	"github.com/google/uuid"
)

// Add a new constant for the status
const HashListStatusReadyWithErrors = "ready_with_errors"

// HashlistDBProcessor handles the asynchronous processing of uploaded hashlists, focusing on DB interactions.
type HashlistDBProcessor struct {
	hashlistRepo       *repository.HashListRepository
	hashTypeRepo       *repository.HashTypeRepository
	hashRepo           *repository.HashRepository
	systemSettingsRepo *repository.SystemSettingsRepository
	config             *config.Config
	progressService    *services.ProcessingProgressService
}

// NewHashlistDBProcessor creates a new instance of HashlistDBProcessor.
func NewHashlistDBProcessor(
	hashlistRepo *repository.HashListRepository,
	hashTypeRepo *repository.HashTypeRepository,
	hashRepo *repository.HashRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
	config *config.Config,
	progressService *services.ProcessingProgressService,
) *HashlistDBProcessor {
	return &HashlistDBProcessor{
		hashlistRepo:       hashlistRepo,
		hashTypeRepo:       hashTypeRepo,
		hashRepo:           hashRepo,
		systemSettingsRepo: systemSettingsRepo,
		config:             config,
		progressService:    progressService,
	}
}

// SubmitHashlistForProcessing initiates the background processing for a given hashlist ID.
func (p *HashlistDBProcessor) SubmitHashlistForProcessing(hashlistID int64, filePath string) {
	// Launch the actual processing in a goroutine
	go p.processHashlist(hashlistID, filePath)
}

// processHashlist contains the main logic for reading, processing, and storing hashes from a list.
func (p *HashlistDBProcessor) processHashlist(hashlistID int64, filePath string) {
	ctx := context.Background() // Use background context for async task
	debug.Info("Starting background processing for hashlist %d from file: %s", hashlistID, filePath)

	// Get hashlist details
	hashlist, err := p.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil || hashlist == nil {
		debug.Error("Background task: Failed to get hashlist %d: %v", hashlistID, err)
		if p.progressService != nil {
			p.progressService.FailProcessing(hashlistID, "Failed to get hashlist details")
		}
		return
	}

	if hashlist.Status != models.HashListStatusProcessing {
		debug.Warning("Background task: Hashlist %d status is %s, expected 'processing'. Aborting.", hashlistID, hashlist.Status)
		return
	}
	if filePath == "" {
		p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "File path is missing")
		if p.progressService != nil {
			p.progressService.FailProcessing(hashlistID, "File path is missing")
		}
		return
	}

	// Get hash type info
	hashType, err := p.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
	if err != nil || hashType == nil {
		debug.Error("Background task: Failed to get hash type %d for hashlist %d: %v", hashlist.HashTypeID, hashlistID, err)
		p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Invalid hash type")
		if p.progressService != nil {
			p.progressService.FailProcessing(hashlistID, "Invalid hash type")
		}
		return
	}

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		debug.Error("Background task: Failed to open file %s for hashlist %d: %v", filePath, hashlistID, err)
		p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Failed to open hashlist file")
		if p.progressService != nil {
			p.progressService.FailProcessing(hashlistID, "Failed to open hashlist file")
		}
		return
	}
	defer file.Close()

	// Count actual lines for accurate progress tracking
	debug.Info("[Processor:%d] Counting lines in file...", hashlistID)
	lineCount, err := countFileLines(filePath)
	if err != nil {
		debug.Warning("[Processor:%d] Failed to count lines, using estimate: %v", hashlistID, err)
		// Fallback to file size estimation
		if fileInfo, statErr := file.Stat(); statErr == nil {
			lineCount = fileInfo.Size() / 50
		}
	}
	if lineCount < 100 {
		lineCount = 100 // Minimum for progress display
	}
	debug.Info("[Processor:%d] File has %d lines", hashlistID, lineCount)

	// Start progress tracking
	if p.progressService != nil {
		p.progressService.StartProcessing(hashlistID, lineCount)
	}

	// --- Process the file line by line ---
	scanner := bufio.NewScanner(file)
	var totalHashes, crackedHashes int64

	// Get configurable batch size from system settings
	bulkBatchSize := 100000 // Default
	if p.systemSettingsRepo != nil {
		if size, err := p.systemSettingsRepo.GetHashlistBulkBatchSize(ctx); err == nil {
			bulkBatchSize = size
			debug.Info("Using hashlist bulk batch size from settings: %d", bulkBatchSize)
		}
	}

	hashesToProcess := make([]*models.Hash, 0, bulkBatchSize)
	lineNumber := 0
	firstLineErrorMsg := ""     // Store the first line processing error
	lineErrorsOccurred := false // Track if any line errors happened

	// Work factor tracking for association attack validation
	var detectedWorkFactors []string
	hasMixedWorkFactors := false

	// valueProcessor, processorFound := p.valueProcessors[hashType.ID] // Removed unused variables

	// Get the needs_processing flag from the fetched hashType
	needsProcessing := hashType.NeedsProcessing

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}

		totalHashes++

		// --- New Processing Logic ---
		originalHash := line // Store the raw line
		usernameAndDomain := hashutils.ExtractUsernameAndDomain(originalHash, hashType.ID)
		hashValue := hashutils.ProcessHashIfNeeded(originalHash, hashType.ID, needsProcessing)

		// --- Work Factor Detection for Association Attack Validation ---
		// Check for variable work factor hash types (bcrypt 3200, etc.)
		if hashType.ID == 3200 { // bcrypt
			workFactor := extractBcryptWorkFactor(hashValue)
			if workFactor != "" {
				if len(detectedWorkFactors) == 0 {
					detectedWorkFactors = append(detectedWorkFactors, workFactor)
				} else if detectedWorkFactors[0] != workFactor && !hasMixedWorkFactors {
					hasMixedWorkFactors = true
					debug.Warning("[Processor:%d] Mixed bcrypt work factors detected: first=%s, current=%s at line %d",
						hashlistID, detectedWorkFactors[0], workFactor, lineNumber)
				}
			}
		}

		// Skip blank LM hashes for LM hash type (3000)
		if hashType.ID == 3000 {
			upperHashValue := strings.ToUpper(hashValue)
			if upperHashValue == "AAD3B435B51404EEAAD3B435B51404EE" {
				debug.Debug("[Processor:%d] Line %d: Skipping blank LM hash", hashlistID, lineNumber)
				totalHashes-- // Don't count blank LM hashes
				continue
			}
		}

		// Extract username and domain from result
		var username *string
		var domain *string
		if usernameAndDomain != nil {
			username = usernameAndDomain.Username
			domain = usernameAndDomain.Domain
		}

		// --- End New Processing Logic ---

		// Determine if cracked (e.g., from input format like hash:pass)
		// Note: ProcessHashIfNeeded doesn't handle cracking detection currently.
		// We might need a separate mechanism or refine processing rules.
		// For now, let's assume a simple heuristic for :password suffix if no specific processor modified it.
		var password *string
		isCracked := false
		if hashValue == originalHash { // Only apply suffix check if ProcessHashIfNeeded didn't modify it
			parts := strings.SplitN(originalHash, ":", 2)
			if len(parts) > 1 {
				// Basic check: is the first part potentially the hashValue we expect?
				// This is weak. A better approach might involve hash length/format checks.
				if parts[0] == hashValue { // Check if splitting by ':' gives back the expected hash
					pwd := parts[1]
					password = &pwd
					isCracked = true
				}
				// Else: it might be user:hash or some other format, don't assume crack.
			}
		}

		if isCracked {
			crackedHashes++
		}

		// Create hash model
		hash := &models.Hash{
			ID:           uuid.New(),   // Generate new UUID for potential insert
			HashValue:    hashValue,    // The value to crack (potentially processed)
			OriginalHash: originalHash, // Always store the original line
			Username:     username,     // Store the extracted username (or nil)
			Domain:       domain,       // Store the extracted domain (or nil)
			HashTypeID:   hashlist.HashTypeID,
			IsCracked:    isCracked,  // Mark cracked based on heuristic above
			Password:     password,   // Store potential password from heuristic
			LastUpdated:  time.Now(), // Set initial time
		}
		hashesToProcess = append(hashesToProcess, hash)

		// Log progress every 100K lines to avoid log overhead
		if lineNumber%100000 == 0 {
			debug.Info("[Processor:%d] Parsed %d lines...", hashlistID, lineNumber)
		}

		// Process in batches using bulk import (PostgreSQL COPY)
		if len(hashesToProcess) >= bulkBatchSize {
			debug.Debug("[Processor:%d] Bulk importing batch of %d hashes (Lines up to %d)", hashlistID, len(hashesToProcess), lineNumber)
			result, err := p.hashRepo.BulkImportHashes(ctx, hashesToProcess, hashlist.ID)
			if err != nil {
				debug.Error("Background task: Error bulk importing hash batch for hashlist %d: %v", hashlistID, err)
				p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Error bulk importing hash batch")
				if p.progressService != nil {
					p.progressService.FailProcessing(hashlistID, "Error bulk importing hash batch")
				}
				return
			}
			crackedHashes += result.CrackedInBatch
			debug.Info("[Processor:%d] Bulk import batch complete: New=%d, Updated=%d, Associations=%d",
				hashlistID, result.NewHashes, result.UpdatedHashes, result.Associations)
			hashesToProcess = hashesToProcess[:0] // Clear batch

			// Update progress after each batch
			if p.progressService != nil {
				p.progressService.UpdateProgress(hashlistID, int64(lineNumber), totalHashes)
			}
		}
	}

	// Process any remaining hashes
	if len(hashesToProcess) > 0 {
		debug.Debug("[Processor:%d] Bulk importing final batch of %d hashes (Lines up to %d)", hashlistID, len(hashesToProcess), lineNumber)
		result, err := p.hashRepo.BulkImportHashes(ctx, hashesToProcess, hashlist.ID)
		if err != nil {
			debug.Error("Background task: Error bulk importing final hash batch for hashlist %d: %v", hashlistID, err)
			p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Error bulk importing final hash batch")
			if p.progressService != nil {
				p.progressService.FailProcessing(hashlistID, "Error bulk importing final hash batch")
			}
			return
		}
		crackedHashes += result.CrackedInBatch
		debug.Info("[Processor:%d] Bulk import final batch complete: New=%d, Updated=%d, Associations=%d",
			hashlistID, result.NewHashes, result.UpdatedHashes, result.Associations)
	}

	// Check for scanner errors after loop
	if err := scanner.Err(); err != nil {
		debug.Error("Background task: Error reading file %s for hashlist %d: %v", filePath, hashlistID, err)
		p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Error reading hashlist file")
		if p.progressService != nil {
			p.progressService.FailProcessing(hashlistID, "Error reading hashlist file")
		}
		return
	}

	debug.Info("Successfully created all hashlist associations for %d", hashlistID)

	// Hashlists are now generated on-demand from database when agents request them
	// No static files are created during processing

	// --- Copy original upload file to standardized location for association attacks ---
	// Association attack mode (-a 9) requires the original hashlist with preserved line order
	// Copy to hashlists/{id}_original.hash for predictable path resolution
	originalHashlistPath := filepath.Join(p.config.DataDir, "hashlists", fmt.Sprintf("%d_original.hash", hashlistID))
	if err := copyFile(filePath, originalHashlistPath); err != nil {
		debug.Error("Failed to copy original hashlist file for association attacks: %v", err)
		// Don't fail processing - association attacks just won't work for this hashlist
	} else {
		debug.Info("Copied original hashlist to %s for association attacks", originalHashlistPath)
		// Clean up the temporary upload file since we've copied it
		if err := os.Remove(filePath); err != nil {
			debug.Warning("Failed to remove temporary upload file %s: %v", filePath, err)
		}
		// Update filePath to the new standardized location
		filePath = originalHashlistPath
	}

	// Determine final status
	finalStatus := models.HashListStatusReady
	if lineErrorsOccurred {
		finalStatus = HashListStatusReadyWithErrors // Use the new status constant
	}

	// Update final hashlist status, counts, file path (for association attacks), and work factor flag
	hashlist.TotalHashes = int(totalHashes)
	hashlist.CrackedHashes = int(crackedHashes) // Note: This counts cracks found *during* ingest heuristic, not pre-cracked ones
	hashlist.Status = finalStatus
	hashlist.ErrorMessage = sql.NullString{String: firstLineErrorMsg, Valid: firstLineErrorMsg != ""}
	hashlist.OriginalFilePath = &filePath // Store for association attacks
	hashlist.HasMixedWorkFactors = hasMixedWorkFactors
	hashlist.UpdatedAt = time.Now()

	// Log work factor warning if detected
	if hasMixedWorkFactors {
		debug.Warning("[Processor:%d] Hashlist has mixed work factors - association attacks will be blocked for this hashlist", hashlistID)
	}

	err = p.hashlistRepo.UpdateStatsStatusAndAssociationFields(ctx, hashlist.ID, int(totalHashes), int(crackedHashes), hashlist.Status, hashlist.ErrorMessage.String, filePath, hasMixedWorkFactors)
	if err != nil {
		debug.Error("Background task: Failed to update final stats/status/path for hashlist %d: %v", hashlistID, err)
		// Status is likely 'processing' still, but processing technically finished.
		// Might need manual intervention or retry logic.
		return
	}

	debug.Info("Successfully processed hashlist %d. Total: %d hashes", hashlistID, totalHashes)

	// Mark processing as complete
	if p.progressService != nil {
		p.progressService.CompleteProcessing(hashlistID, totalHashes)
	}

	// Sync the cracked count to reflect actual state (including pre-cracked hashes)
	if err := p.hashlistRepo.SyncCrackedCount(ctx, hashlistID); err != nil {
		debug.Error("Failed to sync cracked count for hashlist %d: %v", hashlistID, err)
		// Don't fail the entire process, just log the error
	} else {
		debug.Info("Successfully synced cracked count for hashlist %d", hashlistID)
	}

	// Check if this hashlist is linked to another hashlist for LM/NTLM pairing
	linkedHashlist, err := p.hashlistRepo.GetLinkedHashlist(ctx, hashlistID, "lm_ntlm")
	if err != nil {
		debug.Warning("Failed to check for linked hashlist: %v", err)
	} else if linkedHashlist != nil {
		debug.Info("Hashlist %d is linked to hashlist %d (link_type=lm_ntlm), creating individual hash links", hashlistID, linkedHashlist.ID)

		// Determine which is LM (3000) and which is NTLM (1000)
		var lmHashlistID, ntlmHashlistID int64
		if hashlist.HashTypeID == 3000 {
			lmHashlistID = hashlistID
			ntlmHashlistID = linkedHashlist.ID
		} else if hashlist.HashTypeID == 1000 {
			ntlmHashlistID = hashlistID
			lmHashlistID = linkedHashlist.ID
		} else {
			debug.Warning("Linked hashlists are not LM/NTLM (hash_type %d and %d)", hashlist.HashTypeID, linkedHashlist.HashTypeID)
			goto skipLinking
		}

		// Call helper to create hash links
		if err := p.createHashLinksForLMNTLM(ctx, lmHashlistID, ntlmHashlistID); err != nil {
			debug.Error("Failed to create hash links between hashlists %d and %d: %v", lmHashlistID, ntlmHashlistID, err)
		} else {
			debug.Info("Successfully created hash links between LM hashlist %d and NTLM hashlist %d", lmHashlistID, ntlmHashlistID)
		}
	}
skipLinking:
}

// createHashLinksForLMNTLM creates individual hash-to-hash links between LM and NTLM hashlists
func (p *HashlistDBProcessor) createHashLinksForLMNTLM(ctx context.Context, lmHashlistID, ntlmHashlistID int64) error {
	// Get all hashes from both hashlists with username/domain
	lmHashes, err := p.hashRepo.GetAllHashesByHashlistID(ctx, lmHashlistID)
	if err != nil {
		return fmt.Errorf("failed to get LM hashes: %w", err)
	}

	ntlmHashes, err := p.hashRepo.GetAllHashesByHashlistID(ctx, ntlmHashlistID)
	if err != nil {
		return fmt.Errorf("failed to get NTLM hashes: %w", err)
	}

	// Create map: username+domain -> NTLM hash
	ntlmMap := make(map[string]*models.Hash)
	for _, h := range ntlmHashes {
		key := makeUserDomainKey(h.Username, h.Domain)
		ntlmMap[key] = h
	}

	// Match LM hashes to NTLM hashes and build link array
	var hashLinks []struct {
		HashID1  uuid.UUID
		HashID2  uuid.UUID
		LinkType string
	}

	for _, lmHash := range lmHashes {
		key := makeUserDomainKey(lmHash.Username, lmHash.Domain)
		if ntlmHash, found := ntlmMap[key]; found {
			hashLinks = append(hashLinks, struct {
				HashID1  uuid.UUID
				HashID2  uuid.UUID
				LinkType string
			}{
				HashID1:  lmHash.ID,
				HashID2:  ntlmHash.ID,
				LinkType: "lm_ntlm",
			})
		}
	}

	if len(hashLinks) == 0 {
		debug.Warning("No matching username/domain pairs found between LM and NTLM hashlists")
		return nil
	}

	debug.Info("Creating %d hash links between LM hashlist %d and NTLM hashlist %d", len(hashLinks), lmHashlistID, ntlmHashlistID)

	// Call BatchLinkHashes to create links
	return p.hashRepo.BatchLinkHashes(ctx, hashLinks)
}

// makeUserDomainKey creates a consistent key for matching hashes by username and domain
func makeUserDomainKey(username, domain *string) string {
	user := ""
	if username != nil {
		user = *username
	}

	dom := ""
	if domain != nil {
		dom = *domain
	}

	if dom != "" {
		return fmt.Sprintf("%s\\%s", dom, user)
	}
	return user
}

// Helper to update hashlist status (avoids direct repo access from other funcs if needed)
func (p *HashlistDBProcessor) updateHashlistStatus(ctx context.Context, id int64, status string, errMsg string) {
	err := p.hashlistRepo.UpdateStatus(ctx, id, status, errMsg)
	if err != nil {
		debug.Error("Failed to update hashlist %d status to %s: %v", id, status, err)
	}
}

// countFileLines efficiently counts newlines in a file using buffered reading.
// This is much more accurate than file-size estimation for progress tracking.
func countFileLines(filePath string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var count int64
	buf := make([]byte, 64*1024) // 64KB buffer for efficient reading
	lineSep := []byte{'\n'}

	for {
		n, err := file.Read(buf)
		count += int64(bytes.Count(buf[:n], lineSep))
		if err == io.EOF {
			return count, nil
		}
		if err != nil {
			return count, err
		}
	}
}

// extractBcryptWorkFactor extracts the cost parameter from a bcrypt hash.
// Bcrypt format: $2a$XX$... or $2b$XX$... or $2y$XX$... where XX is the cost (04-31).
// Returns the cost as a string (e.g., "10", "12") or empty string if not found.
func extractBcryptWorkFactor(hashValue string) string {
	// bcrypt hashes start with $2a$, $2b$, or $2y$ followed by cost
	if len(hashValue) < 7 {
		return ""
	}

	// Check for valid bcrypt prefix
	if hashValue[0] != '$' || hashValue[1] != '2' {
		return ""
	}

	// Find the second and third '$' to extract cost
	// Format: $2X$CC$hash... where X is a/b/y and CC is cost
	if len(hashValue) > 4 && hashValue[3] == '$' {
		// Extract the two-digit cost between positions 4 and 6
		if len(hashValue) > 6 && hashValue[6] == '$' {
			return hashValue[4:6]
		}
	}

	return ""
}

// copyFile copies a file from src to dst.
// If dst already exists, it will be overwritten.
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// Create destination file
	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	// Copy contents
	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := destFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync destination file: %w", err)
	}

	return nil
}
