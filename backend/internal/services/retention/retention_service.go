package retention

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// RetentionService handles the automatic purging of old hashlists and analytics reports based on retention policies.
type RetentionService struct {
	db                    *db.DB // Needed for transactions
	hashlistRepo          *repository.HashListRepository
	hashRepo              *repository.HashRepository
	clientRepo            *repository.ClientRepository
	clientSettingsRepo    *repository.ClientSettingsRepository
	analyticsRepo         *repository.AnalyticsRepository
	assocWordlistRepo     *repository.AssociationWordlistRepository
}

// NewRetentionService creates a new RetentionService.
func NewRetentionService(database *db.DB, hr *repository.HashListRepository, hshr *repository.HashRepository, cr *repository.ClientRepository, sr *repository.ClientSettingsRepository, ar *repository.AnalyticsRepository) *RetentionService {
	return &RetentionService{
		db:                 database,
		hashlistRepo:       hr,
		hashRepo:           hshr,
		clientRepo:         cr,
		clientSettingsRepo: sr,
		analyticsRepo:      ar,
		assocWordlistRepo:  repository.NewAssociationWordlistRepository(database),
	}
}

// PurgeOldHashlists finds and deletes hashlists that have exceeded their retention period.
func (s *RetentionService) PurgeOldHashlists(ctx context.Context) error {
	debug.Info("Starting data retention purge process...")

	// 1. Get default retention policy
	defaultRetentionSetting, err := s.clientSettingsRepo.GetSetting(ctx, "default_data_retention_months")
	if err != nil || defaultRetentionSetting.Value == nil {
		debug.Error("Failed to get default client retention setting during purge: %v", err)
		return fmt.Errorf("purge failed: could not retrieve default client retention setting")
	}
	defaultRetentionMonths, err := strconv.Atoi(*defaultRetentionSetting.Value)
	if err != nil {
		debug.Error("Invalid default client retention setting value '%s': %v", *defaultRetentionSetting.Value, err)
		return fmt.Errorf("purge failed: invalid default client retention setting value")
	}
	debug.Debug("Purge: Default retention is %d months.", defaultRetentionMonths)

	// 2. Get all clients to check their specific policies
	clients, err := s.clientRepo.List(ctx)
	if err != nil {
		debug.Error("Failed to list clients during purge: %v", err)
		return fmt.Errorf("purge failed: could not list clients")
	}
	clientRetentionMap := make(map[string]int)
	for _, client := range clients {
		if client.DataRetentionMonths != nil {
			clientRetentionMap[client.ID.String()] = *client.DataRetentionMonths
		} // Clients with NULL will use the default later
	}

	// 3. Find and process hashlists eligible for purging
	// Need a method in HashListRepository to get *all* hashlists, perhaps with pagination if very large
	// For now, assume List can be used without filters, or create a new GetAll method.
	// Let's use List with a large limit for simplicity, add pagination later if needed.
	limit := 1000 // Process in batches
	offset := 0
	processedCount := 0
	deletedCount := 0

	for {
		hashlists, total, err := s.hashlistRepo.List(ctx, repository.ListHashlistsParams{Limit: limit, Offset: offset})
		if err != nil {
			debug.Error("Failed to list hashlists batch (offset %d) during purge: %v", offset, err)
			return fmt.Errorf("purge failed: could not list hashlists batch")
		}
		if len(hashlists) == 0 {
			debug.Debug("Purge: No more hashlists found.")
			break // Exit loop when no more hashlists are found
		}
		offset += len(hashlists)

		for _, hl := range hashlists {
			processedCount++
			retentionMonths := defaultRetentionMonths
			clientIsSet := hl.ClientID != uuid.Nil
			if clientIsSet {
				if specificRetention, ok := clientRetentionMap[hl.ClientID.String()]; ok {
					retentionMonths = specificRetention
				}
			}

			// Skip if retention is set to 0 (keep forever)
			if retentionMonths == 0 {
				debug.Debug("Purge: Skipping hashlist %d (Client: %s) - Retention is 0 (Keep Forever)", hl.ID, hl.ClientID)
				continue
			}

			// Calculate expiration date
			retentionDuration := time.Duration(retentionMonths) * 30 * 24 * time.Hour // Approx. months
			expirationDate := hl.CreatedAt.Add(retentionDuration)

			// Check if expired
			if time.Now().After(expirationDate) {
				debug.Info("Purge: Hashlist %d (Created: %s, Client: %s, Retention: %d months) has expired (Expiry: %s). Deleting...", hl.ID, hl.CreatedAt, hl.ClientID, retentionMonths, expirationDate)
				err := s.DeleteHashlistAndOrphanedHashes(ctx, hl.ID)
				if err != nil {
					debug.Error("Purge: Failed to delete expired hashlist %d: %v", hl.ID, err)
					// Continue processing other hashlists even if one fails?
					// For now, let's log the error and continue.
					continue
				}
				deletedCount++
			} else {
				debug.Debug("Purge: Hashlist %d (Client: %s) has not expired yet (Expiry: %s)", hl.ID, hl.ClientID, expirationDate)
			}
		}

		// Safety break if List doesn't behave as expected or total is weird
		if offset >= total && total > 0 {
			break
		}
	}

	// 4. Run VACUUM on affected tables if any hashlists were deleted
	if deletedCount > 0 {
		debug.Info("Running VACUUM after deleting %d hashlists...", deletedCount)
		if err := s.VacuumTables(ctx); err != nil {
			debug.Error("Purge: Failed to run VACUUM after deletion: %v", err)
			// Continue - VACUUM failure shouldn't fail the whole operation
		}
	}

	// 5. Update last purge run timestamp
	nowStr := time.Now().Format(time.RFC3339Nano)
	err = s.clientSettingsRepo.SetSetting(ctx, "last_purge_run", &nowStr)
	if err != nil {
		debug.Error("Purge: Failed to update last_purge_run timestamp: %v", err)
		// Log error but don't fail the whole operation
	}

	debug.Info("Data retention purge completed. Processed: %d, Deleted: %d", processedCount, deletedCount)
	return nil
}

// PurgeOldAnalyticsReports finds and deletes analytics reports that have exceeded their retention period.
func (s *RetentionService) PurgeOldAnalyticsReports(ctx context.Context) error {
	debug.Info("Starting analytics report retention purge process...")

	// 1. Get default retention policy
	defaultRetentionSetting, err := s.clientSettingsRepo.GetSetting(ctx, "default_data_retention_months")
	if err != nil || defaultRetentionSetting.Value == nil {
		debug.Error("Failed to get default client retention setting during analytics purge: %v", err)
		return fmt.Errorf("analytics purge failed: could not retrieve default client retention setting")
	}
	defaultRetentionMonths, err := strconv.Atoi(*defaultRetentionSetting.Value)
	if err != nil {
		debug.Error("Invalid default client retention setting value '%s': %v", *defaultRetentionSetting.Value, err)
		return fmt.Errorf("analytics purge failed: invalid default client retention setting value")
	}
	debug.Debug("Analytics Purge: Default retention is %d months.", defaultRetentionMonths)

	// 2. Get all clients to check their specific policies
	clients, err := s.clientRepo.List(ctx)
	if err != nil {
		debug.Error("Failed to list clients during analytics purge: %v", err)
		return fmt.Errorf("analytics purge failed: could not list clients")
	}
	clientRetentionMap := make(map[string]int)
	for _, client := range clients {
		if client.DataRetentionMonths != nil {
			clientRetentionMap[client.ID.String()] = *client.DataRetentionMonths
		} // Clients with NULL will use the default later
	}

	// 3. Find and process analytics reports eligible for purging
	limit := 1000 // Process in batches
	offset := 0
	processedCount := 0
	deletedCount := 0

	for {
		reports, total, err := s.analyticsRepo.List(ctx, limit, offset)
		if err != nil {
			debug.Error("Failed to list analytics reports batch (offset %d) during purge: %v", offset, err)
			return fmt.Errorf("analytics purge failed: could not list analytics reports batch")
		}
		if len(reports) == 0 {
			debug.Debug("Analytics Purge: No more reports found.")
			break // Exit loop when no more reports are found
		}
		offset += len(reports)

		for _, report := range reports {
			processedCount++
			retentionMonths := defaultRetentionMonths
			clientIsSet := report.ClientID != uuid.Nil
			if clientIsSet {
				if specificRetention, ok := clientRetentionMap[report.ClientID.String()]; ok {
					retentionMonths = specificRetention
				}
			}

			// Skip if retention is set to 0 (keep forever)
			if retentionMonths == 0 {
				debug.Debug("Analytics Purge: Skipping report %s (Client: %s) - Retention is 0 (Keep Forever)", report.ID, report.ClientID)
				continue
			}

			// Calculate expiration date
			retentionDuration := time.Duration(retentionMonths) * 30 * 24 * time.Hour // Approx. months
			expirationDate := report.CreatedAt.Add(retentionDuration)

			// Check if expired
			if time.Now().After(expirationDate) {
				debug.Info("Analytics Purge: Report %s (Created: %s, Client: %s, Retention: %d months) has expired (Expiry: %s). Deleting...", report.ID, report.CreatedAt, report.ClientID, retentionMonths, expirationDate)
				err := s.analyticsRepo.Delete(ctx, report.ID)
				if err != nil {
					debug.Error("Analytics Purge: Failed to delete expired report %s: %v", report.ID, err)
					// Continue processing other reports even if one fails
					continue
				}
				deletedCount++
			} else {
				debug.Debug("Analytics Purge: Report %s (Client: %s) has not expired yet (Expiry: %s)", report.ID, report.ClientID, expirationDate)
			}
		}

		// Safety break if List doesn't behave as expected or total is weird
		if offset >= total && total > 0 {
			break
		}
	}

	// 4. Run VACUUM on affected tables if any reports were deleted
	if deletedCount > 0 {
		debug.Info("Running VACUUM after deleting %d analytics reports...", deletedCount)
		if err := s.VacuumTables(ctx); err != nil {
			debug.Error("Analytics Purge: Failed to run VACUUM after deletion: %v", err)
			// Continue - VACUUM failure shouldn't fail the whole operation
		}
	}

	// 5. Update last purge run timestamp
	nowStr := time.Now().Format(time.RFC3339Nano)
	err = s.clientSettingsRepo.SetSetting(ctx, "last_purge_run", &nowStr)
	if err != nil {
		debug.Error("Analytics Purge: Failed to update last_purge_run timestamp: %v", err)
		// Log error but don't fail the whole operation
	}

	debug.Info("Analytics report retention purge completed. Processed: %d, Deleted: %d", processedCount, deletedCount)
	return nil
}

// DeleteHashlistAndOrphanedHashes deletes a hashlist and any hashes that become orphaned as a result.
// Uses the optimized batch delete method in hashlistRepo.Delete() which:
// - Checks orphaned hashes in batches of 50,000 using NOT EXISTS subquery
// - Deletes orphaned hashes in batches of 10,000
// This reduces O(n) individual queries to O(n/50000) batch operations.
// Also securely deletes the original hashlist file if it exists (used for association attacks).
func (s *RetentionService) DeleteHashlistAndOrphanedHashes(ctx context.Context, hashlistID int64) error {
	debug.Info("Purge: Deleting hashlist %d using optimized batch delete...", hashlistID)

	// Get the hashlist to retrieve the original file path before deletion
	hashlist, err := s.hashlistRepo.GetByID(ctx, hashlistID)
	if err != nil {
		debug.Warning("Purge: Could not retrieve hashlist %d before deletion (may already be deleted): %v", hashlistID, err)
		// Continue with deletion attempt anyway
	}

	// Securely delete the original hashlist file if it exists (used for association attacks)
	if hashlist != nil && hashlist.OriginalFilePath != nil && *hashlist.OriginalFilePath != "" {
		debug.Info("Purge: Securely deleting original hashlist file: %s", *hashlist.OriginalFilePath)
		if err := s.secureDeleteFile(*hashlist.OriginalFilePath); err != nil {
			debug.Error("Purge: Failed to securely delete original hashlist file %s: %v", *hashlist.OriginalFilePath, err)
			// Continue with hashlist deletion even if file deletion fails
		}
	}

	// Delete all association wordlists and their files for this hashlist
	if s.assocWordlistRepo != nil {
		assocWordlistPaths, err := s.assocWordlistRepo.DeleteByHashlistID(ctx, hashlistID)
		if err != nil {
			debug.Warning("Purge: Failed to delete association wordlist records for hashlist %d: %v", hashlistID, err)
			// Continue with hashlist deletion even if this fails
		} else {
			for _, filePath := range assocWordlistPaths {
				if err := s.secureDeleteFile(filePath); err != nil {
					debug.Warning("Purge: Failed to securely delete association wordlist file %s: %v", filePath, err)
				}
			}
			if len(assocWordlistPaths) > 0 {
				debug.Info("Purge: Deleted %d association wordlists for hashlist %d", len(assocWordlistPaths), hashlistID)
			}
		}
	}

	// Use the optimized Delete method from hashlistRepo which handles:
	// 1. Getting all associated hash IDs
	// 2. Deleting the hashlist (cascades to hashlist_hashes)
	// 3. Finding orphaned hashes using NOT EXISTS subquery in batches
	// 4. Deleting orphaned hashes in batches
	// All within a single transaction
	if err := s.hashlistRepo.Delete(ctx, hashlistID); err != nil {
		return fmt.Errorf("failed to delete hashlist %d: %w", hashlistID, err)
	}

	debug.Info("Purge: Successfully deleted hashlist %d using optimized batch delete", hashlistID)
	return nil
}

// secureDeleteFile overwrites a file with random data before deleting it
func (s *RetentionService) secureDeleteFile(filePath string) error {
	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			debug.Warning("File %s does not exist, skipping deletion", filePath)
			return nil
		}
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	// Open file for writing
	file, err := os.OpenFile(filePath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open file %s for secure deletion: %w", filePath, err)
	}
	defer file.Close()

	// Overwrite file with random data
	fileSize := fileInfo.Size()
	randomData := make([]byte, 4096) // Use 4KB buffer for efficiency

	for written := int64(0); written < fileSize; {
		// Generate random data for this chunk
		if _, err := rand.Read(randomData); err != nil {
			return fmt.Errorf("failed to generate random data: %w", err)
		}

		// Calculate how much to write
		toWrite := fileSize - written
		if toWrite > int64(len(randomData)) {
			toWrite = int64(len(randomData))
		}

		// Write the random data
		n, err := file.Write(randomData[:toWrite])
		if err != nil {
			return fmt.Errorf("failed to overwrite file %s: %w", filePath, err)
		}
		written += int64(n)
	}

	// Sync to ensure data is written to disk
	if err := file.Sync(); err != nil {
		debug.Warning("Failed to sync file %s after overwrite: %v", filePath, err)
	}

	// Close file before deletion
	file.Close()

	// Now delete the file
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete file %s after overwrite: %w", filePath, err)
	}

	debug.Info("Securely deleted file: %s (overwritten %d bytes)", filePath, fileSize)
	return nil
}

// VacuumTables runs VACUUM on the affected tables to reclaim space and remove dead tuples
func (s *RetentionService) VacuumTables(ctx context.Context) error {
	debug.Info("Running VACUUM on retention-affected tables...")

	// List of tables to vacuum
	tables := []string{"hashlists", "hashlist_hashes", "hashes", "agent_hashlists", "job_executions", "analytics_reports"}

	for _, table := range tables {
		// VACUUM cannot run inside a transaction, so we execute directly
		query := fmt.Sprintf("VACUUM ANALYZE %s", table)

		// Execute VACUUM
		_, err := s.db.ExecContext(ctx, query)
		if err != nil {
			debug.Error("Failed to VACUUM table %s: %v", table, err)
			// Continue with other tables even if one fails
			continue
		}

		debug.Debug("Successfully ran VACUUM on table: %s", table)
	}

	debug.Info("Completed VACUUM operation on retention-affected tables")
	return nil
}
