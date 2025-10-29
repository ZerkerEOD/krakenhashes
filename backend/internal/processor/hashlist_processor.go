package processor

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/hashutils"
	"github.com/google/uuid"
)

// Add a new constant for the status
const HashListStatusReadyWithErrors = "ready_with_errors"

// HashlistDBProcessor handles the asynchronous processing of uploaded hashlists, focusing on DB interactions.
type HashlistDBProcessor struct {
	hashlistRepo *repository.HashListRepository
	hashTypeRepo *repository.HashTypeRepository
	hashRepo     *repository.HashRepository
	config       *config.Config
	// valueProcessors map[int]HashValueProcessor // REMOVED: Replaced by hashutils
}

// NewHashlistDBProcessor creates a new instance of HashlistDBProcessor.
func NewHashlistDBProcessor(
	hashlistRepo *repository.HashListRepository,
	hashTypeRepo *repository.HashTypeRepository,
	hashRepo *repository.HashRepository,
	config *config.Config,
) *HashlistDBProcessor {
	// REMOVED: Initialization of valueProcessors map
	/*
		valueProcessors := make(map[int]HashValueProcessor)
		valueProcessors[1000] = &NTLMProcessor{} // Register NTLM processor
		// Register other processors here...
	*/

	return &HashlistDBProcessor{
		hashlistRepo: hashlistRepo,
		hashTypeRepo: hashTypeRepo,
		hashRepo:     hashRepo,
		config:       config,
		// valueProcessors: valueProcessors, // REMOVED
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
		return
	}

	if hashlist.Status != models.HashListStatusProcessing {
		debug.Warning("Background task: Hashlist %d status is %s, expected 'processing'. Aborting.", hashlistID, hashlist.Status)
		return
	}
	if filePath == "" {
		p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "File path is missing")
		return
	}

	// Get hash type info
	hashType, err := p.hashTypeRepo.GetByID(ctx, hashlist.HashTypeID)
	if err != nil || hashType == nil {
		debug.Error("Background task: Failed to get hash type %d for hashlist %d: %v", hashlist.HashTypeID, hashlistID, err)
		p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Invalid hash type")
		return
	}

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		debug.Error("Background task: Failed to open file %s for hashlist %d: %v", filePath, hashlistID, err)
		p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Failed to open hashlist file")
		return
	}
	defer file.Close()

	// --- Process the file line by line ---
	scanner := bufio.NewScanner(file)
	var totalHashes, crackedHashes int64
	batchSize := p.config.HashlistBatchSize
	hashesToProcess := make([]*models.Hash, 0, batchSize)
	lineNumber := 0
	firstLineErrorMsg := ""     // Store the first line processing error
	lineErrorsOccurred := false // Track if any line errors happened

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

		// Extract username and domain from result
		var username *string
		var domain *string
		if usernameAndDomain != nil {
			username = usernameAndDomain.Username
			domain = usernameAndDomain.Domain
		}

		usernameStr := ""
		domainStr := ""
		if username != nil {
			usernameStr = *username
		}
		if domain != nil {
			domainStr = *domain
		}
		debug.Debug("[Processor:%d] Line %d: Original='%s', ProcessedValue='%s', User='%s', Domain='%s'", hashlistID, lineNumber, originalHash, hashValue, usernameStr, domainStr)
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
		debug.Debug("[Processor:%d] Line %d: Created Hash struct with ID: %s", hashlistID, lineNumber, hash.ID)
		hashesToProcess = append(hashesToProcess, hash)

		// Process in batches
		if len(hashesToProcess) >= batchSize {
			debug.Debug("[Processor:%d] Processing batch of %d hashes (Lines up to %d)", hashlistID, len(hashesToProcess), lineNumber)
			newAssociations, err := p.batchProcessHashes(ctx, hashesToProcess, hashlist.ID)
			if err != nil {
				debug.Error("Background task: Error processing hash batch for hashlist %d: %v", hashlistID, err)
				p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Error processing hash batch")
				return // Stop processing on batch error
			}
			// Create associations immediately for this batch instead of accumulating
			if len(newAssociations) > 0 {
				err = p.hashRepo.AddBatchToHashList(ctx, newAssociations)
				if err != nil {
					debug.Error("Background task: Error creating hashlist associations for batch (hashlist %d): %v", hashlistID, err)
					p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Error saving hash associations")
					return
				}
			}
			hashesToProcess = hashesToProcess[:0] // Clear batch
		}
	}

	// Process any remaining hashes
	if len(hashesToProcess) > 0 {
		debug.Debug("[Processor:%d] Processing final batch of %d hashes (Lines up to %d)", hashlistID, len(hashesToProcess), lineNumber)
		newAssociations, err := p.batchProcessHashes(ctx, hashesToProcess, hashlist.ID)
		if err != nil {
			debug.Error("Background task: Error processing final hash batch for hashlist %d: %v", hashlistID, err)
			p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Error processing final hash batch")
			return
		}
		// Create associations for final batch
		if len(newAssociations) > 0 {
			err = p.hashRepo.AddBatchToHashList(ctx, newAssociations)
			if err != nil {
				debug.Error("Background task: Error creating hashlist associations for final batch (hashlist %d): %v", hashlistID, err)
				p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Error saving final hash associations")
				return
			}
		}
	}

	// Check for scanner errors after loop
	if err := scanner.Err(); err != nil {
		debug.Error("Background task: Error reading file %s for hashlist %d: %v", filePath, hashlistID, err)
		p.updateHashlistStatus(ctx, hashlistID, models.HashListStatusError, "Error reading hashlist file")
		return
	}

	debug.Info("Successfully created all hashlist associations for %d", hashlistID)

	// Hashlists are now generated on-demand from database when agents request them
	// No static files are created during processing

	// --- Delete temporary upload file after processing ---
	if filePath != "" {
		if err := os.Remove(filePath); err != nil {
			debug.Warning("Failed to delete temporary upload file %s for hashlist %d: %v", filePath, hashlistID, err)
		} else {
			debug.Info("Deleted temporary upload file %s for hashlist %d", filePath, hashlistID)
		}
	}

	// Determine final status
	finalStatus := models.HashListStatusReady
	if lineErrorsOccurred {
		finalStatus = HashListStatusReadyWithErrors // Use the new status constant
	}

	// Update final hashlist status, counts, AND the file path
	hashlist.TotalHashes = int(totalHashes)
	hashlist.CrackedHashes = int(crackedHashes) // Note: This counts cracks found *during* ingest heuristic, not pre-cracked ones
	hashlist.Status = finalStatus
	hashlist.ErrorMessage = sql.NullString{String: firstLineErrorMsg, Valid: firstLineErrorMsg != ""}
	// FilePath no longer needed - hashlists generated on-demand from database
	hashlist.UpdatedAt = time.Now()

	err = p.hashlistRepo.UpdateStatsAndStatus(ctx, hashlist.ID, int(totalHashes), int(crackedHashes), hashlist.Status, hashlist.ErrorMessage.String)
	if err != nil {
		debug.Error("Background task: Failed to update final stats/status/path for hashlist %d: %v", hashlistID, err)
		// Status is likely 'processing' still, but processing technically finished.
		// Might need manual intervention or retry logic.
		return
	}

	debug.Info("Successfully processed hashlist %d. Total: %d hashes", hashlistID, totalHashes)

	// Sync the cracked count to reflect actual state (including pre-cracked hashes)
	if err := p.hashlistRepo.SyncCrackedCount(ctx, hashlistID); err != nil {
		debug.Error("Failed to sync cracked count for hashlist %d: %v", hashlistID, err)
		// Don't fail the entire process, just log the error
	} else {
		debug.Info("Successfully synced cracked count for hashlist %d", hashlistID)
	}
}

// batchProcessHashes handles creating/updating hashes and preparing associations.
// It deduplicates by original_hash (full input line) to preserve unique entries
// like different users with the same password hash. Each unique original_hash
// gets its own hash record and association to the hashlist.
func (p *HashlistDBProcessor) batchProcessHashes(ctx context.Context, hashes []*models.Hash, hashlistID int64) ([]*models.HashListHash, error) {
	debug.Debug("[Processor:%d] batchProcessHashes received %d hashes", hashlistID, len(hashes))
	if len(hashes) == 0 {
		return nil, nil
	}

	// Deduplicate by original_hash (exact duplicate input lines)
	// This preserves all unique entries (e.g., different users with same password)
	uniqueHashesByOriginal := make(map[string]*models.Hash) // Key: original_hash
	uniqueHashValues := make([]string, 0)
	uniqueHashValueSet := make(map[string]struct{})

	for _, h := range hashes {
		// Skip exact duplicate lines (same original_hash)
		if _, alreadySeen := uniqueHashesByOriginal[h.OriginalHash]; alreadySeen {
			debug.Debug("[Processor:%d] Skipping duplicate original_hash: %s", hashlistID, h.OriginalHash)
			continue
		}

		// Store this unique original line
		uniqueHashesByOriginal[h.OriginalHash] = h

		// Collect unique hash_values for DB existence check
		if _, exists := uniqueHashValueSet[h.HashValue]; !exists {
			uniqueHashValues = append(uniqueHashValues, h.HashValue)
			uniqueHashValueSet[h.HashValue] = struct{}{}
		}
	}
	debug.Debug("[Processor:%d] After deduplication: %d unique original hashes from %d inputs, %d unique hash values",
		hashlistID, len(uniqueHashesByOriginal), len(hashes), len(uniqueHashValues))

	// Find existing hashes in DB by hash_values, then filter by original_hash
	existingHashesFromDB, err := p.hashRepo.GetByHashValues(ctx, uniqueHashValues)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing hashes: %w", err)
	}

	// Create map keyed by ORIGINAL_HASH for exact matching
	existingHashMap := make(map[string]*models.Hash)
	for _, eh := range existingHashesFromDB {
		existingHashMap[eh.OriginalHash] = eh
	}
	debug.Debug("[Processor:%d] Found %d existing hash records in DB", hashlistID, len(existingHashMap))

	// Prepare lists for creation, update, and final associations
	newHashesToCreate := make([]*models.Hash, 0)
	hashesToUpdate := make([]*models.Hash, 0)
	finalAssociations := make([]*models.HashListHash, 0, len(uniqueHashesByOriginal))
	newlyCrackedInBatch := 0

	// Iterate through each unique original_hash
	for originalHash, inputHash := range uniqueHashesByOriginal {
		existingDBHash, existsInDB := existingHashMap[originalHash]

		if existsInDB {
			// This exact original_hash already exists in DB
			// Just create association, and potentially update crack status
			debug.Debug("[Processor:%d] Original hash '%s' exists in DB (ID: %s)",
				hashlistID, originalHash, existingDBHash.ID)

			finalAssociations = append(finalAssociations, &models.HashListHash{
				HashlistID: hashlistID,
				HashID:     existingDBHash.ID,
			})

			// Check if we need to update crack status
			needsUpdate := false
			if !existingDBHash.IsCracked && inputHash.IsCracked {
				existingDBHash.IsCracked = true
				existingDBHash.Password = inputHash.Password
				needsUpdate = true
				newlyCrackedInBatch++
			}
			// Update username if existing is null and new one is provided
			if existingDBHash.Username == nil && inputHash.Username != nil {
				existingDBHash.Username = inputHash.Username
				needsUpdate = true
			}
			// Update domain if existing is null and new one is provided
			if existingDBHash.Domain == nil && inputHash.Domain != nil {
				existingDBHash.Domain = inputHash.Domain
				needsUpdate = true
			}

			if needsUpdate {
				existingDBHash.LastUpdated = time.Now()
				hashesToUpdate = append(hashesToUpdate, existingDBHash)
			}

		} else {
			// This is a NEW unique original_hash - create a new record
			inputHash.ID = uuid.New()
			newHashesToCreate = append(newHashesToCreate, inputHash)

			debug.Debug("[Processor:%d] Creating new hash record for original: '%s' (ID: %s)",
				hashlistID, originalHash, inputHash.ID)

			finalAssociations = append(finalAssociations, &models.HashListHash{
				HashlistID: hashlistID,
				HashID:     inputHash.ID,
			})

			if inputHash.IsCracked {
				newlyCrackedInBatch++
			}
		}
	}

	debug.Debug("[Processor:%d] Will create %d new hashes, update %d existing hashes, create %d associations",
		hashlistID, len(newHashesToCreate), len(hashesToUpdate), len(finalAssociations))

	// Create new hashes
	if len(newHashesToCreate) > 0 {
		// CreateBatch needs to handle potential duplicates if run concurrently,
		// but here we assume it attempts to insert all. It should return only successfully created ones.
		// We already assigned UUIDs, so we expect CreateBatch to use those.
		_, err := p.hashRepo.CreateBatch(ctx, newHashesToCreate) // Don't need returned hashes if IDs are pre-set
		if err != nil {
			// If CreateBatch fails, we cannot reliably create associations for the new hashes.
			return nil, fmt.Errorf("failed to create new hash batch: %w", err)
		}
	}

	// Update existing hashes
	if len(hashesToUpdate) > 0 {
		err = p.hashRepo.UpdateBatch(ctx, hashesToUpdate)
		if err != nil {
			// Log the error but potentially continue to create associations?
			// For now, return error to prevent potentially inconsistent state.
			debug.Error("[Processor:%d] Failed to update hash batch: %v. Associations might be incomplete.", hashlistID, err)
			return nil, fmt.Errorf("failed to update hash batch: %w", err)
		}
	}

	// Increment cracked count in hashlists table
	if newlyCrackedInBatch > 0 {
		err = p.hashlistRepo.IncrementCrackedCount(ctx, hashlistID, newlyCrackedInBatch)
		if err != nil {
			// Log error but don't fail the whole process for this.
			debug.Error("[Processor:%d] Failed to increment cracked count by %d: %v", hashlistID, newlyCrackedInBatch, err)
		}
	}

	// Return the prepared list of all associations to be created in the next step
	return finalAssociations, nil
}

// Helper to update hashlist status (avoids direct repo access from other funcs if needed)
func (p *HashlistDBProcessor) updateHashlistStatus(ctx context.Context, id int64, status string, errMsg string) {
	err := p.hashlistRepo.UpdateStatus(ctx, id, status, errMsg)
	if err != nil {
		debug.Error("Failed to update hashlist %d status to %s: %v", id, status, err)
	}
}
