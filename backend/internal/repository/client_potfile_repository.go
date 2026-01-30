package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ClientPotfileRepository handles database operations for client-specific potfiles.
type ClientPotfileRepository struct {
	db *db.DB
}

// NewClientPotfileRepository creates a new instance of ClientPotfileRepository.
func NewClientPotfileRepository(database *db.DB) *ClientPotfileRepository {
	return &ClientPotfileRepository{db: database}
}

// Create inserts a new client potfile record into the database.
func (r *ClientPotfileRepository) Create(ctx context.Context, potfile *models.ClientPotfile) error {
	potfile.CreatedAt = time.Now()
	potfile.UpdatedAt = time.Now()

	query := `
		INSERT INTO client_potfiles (client_id, file_path, file_size, line_count, md5_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	err := r.db.QueryRowContext(ctx, query,
		potfile.ClientID,
		potfile.FilePath,
		potfile.FileSize,
		potfile.LineCount,
		potfile.MD5Hash,
		potfile.CreatedAt,
		potfile.UpdatedAt,
	).Scan(&potfile.ID)

	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" { // unique_violation
			return fmt.Errorf("client potfile already exists for client %s: %w", potfile.ClientID, ErrDuplicateRecord)
		}
		return fmt.Errorf("failed to create client potfile: %w", err)
	}

	debug.Info("Created client potfile %d for client %s", potfile.ID, potfile.ClientID)
	return nil
}

// GetByClientID retrieves a client potfile by the client's ID.
func (r *ClientPotfileRepository) GetByClientID(ctx context.Context, clientID uuid.UUID) (*models.ClientPotfile, error) {
	query := `
		SELECT id, client_id, file_path, file_size, line_count, md5_hash, created_at, updated_at
		FROM client_potfiles
		WHERE client_id = $1
	`

	var potfile models.ClientPotfile
	err := r.db.QueryRowContext(ctx, query, clientID).Scan(
		&potfile.ID,
		&potfile.ClientID,
		&potfile.FilePath,
		&potfile.FileSize,
		&potfile.LineCount,
		&potfile.MD5Hash,
		&potfile.CreatedAt,
		&potfile.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Return nil, nil when not found (no error, just doesn't exist yet)
		}
		return nil, fmt.Errorf("failed to get client potfile for client %s: %w", clientID, err)
	}

	return &potfile, nil
}

// GetByID retrieves a client potfile by its ID.
func (r *ClientPotfileRepository) GetByID(ctx context.Context, id int) (*models.ClientPotfile, error) {
	query := `
		SELECT id, client_id, file_path, file_size, line_count, md5_hash, created_at, updated_at
		FROM client_potfiles
		WHERE id = $1
	`

	var potfile models.ClientPotfile
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&potfile.ID,
		&potfile.ClientID,
		&potfile.FilePath,
		&potfile.FileSize,
		&potfile.LineCount,
		&potfile.MD5Hash,
		&potfile.CreatedAt,
		&potfile.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("client potfile with ID %d not found: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get client potfile by ID %d: %w", id, err)
	}

	return &potfile, nil
}

// UpdateMetadata updates the file metadata for a client potfile.
func (r *ClientPotfileRepository) UpdateMetadata(ctx context.Context, clientID uuid.UUID, fileSize int64, lineCount int64, md5Hash string) error {
	query := `
		UPDATE client_potfiles
		SET file_size = $1, line_count = $2, md5_hash = $3, updated_at = $4
		WHERE client_id = $5
	`

	result, err := r.db.ExecContext(ctx, query, fileSize, lineCount, md5Hash, time.Now(), clientID)
	if err != nil {
		return fmt.Errorf("failed to update client potfile metadata for client %s: %w", clientID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after updating client potfile for %s: %v", clientID, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("client potfile for client %s not found for update: %w", clientID, ErrNotFound)
	}

	return nil
}

// Delete removes a client potfile record from the database.
func (r *ClientPotfileRepository) Delete(ctx context.Context, clientID uuid.UUID) error {
	query := `DELETE FROM client_potfiles WHERE client_id = $1`

	result, err := r.db.ExecContext(ctx, query, clientID)
	if err != nil {
		return fmt.Errorf("failed to delete client potfile for client %s: %w", clientID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting client potfile for %s: %v", clientID, err)
	} else if rowsAffected == 0 {
		// Not an error - potfile might not exist
		debug.Debug("No client potfile found to delete for client %s", clientID)
	}

	return nil
}

// ListActiveClientIDs returns all client IDs that have client potfiles enabled.
func (r *ClientPotfileRepository) ListActiveClientIDs(ctx context.Context) ([]uuid.UUID, error) {
	query := `
		SELECT c.id
		FROM clients c
		WHERE c.enable_client_potfile = true
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list active client IDs: %w", err)
	}
	defer rows.Close()

	var clientIDs []uuid.UUID
	for rows.Next() {
		var clientID uuid.UUID
		if err := rows.Scan(&clientID); err != nil {
			return nil, fmt.Errorf("failed to scan client ID: %w", err)
		}
		clientIDs = append(clientIDs, clientID)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client IDs: %w", err)
	}

	return clientIDs, nil
}

// NOTE: Staging operations have been moved to the unified PotfileService.
// The potfile_staging table now has a client_id column for unified staging.
// Client potfile processing is handled by PotfileService.ProcessStagedEntries().

// GetUniquePlaintextsForClient retrieves all unique cracked plaintexts for a client's remaining hashlists.
// This is used when regenerating a client potfile after hashlist deletion.
func (r *ClientPotfileRepository) GetUniquePlaintextsForClient(ctx context.Context, clientID uuid.UUID) ([]string, error) {
	query := `
		SELECT DISTINCT h.plaintext
		FROM hashes h
		JOIN hashlist_hashes hh ON h.id = hh.hash_id
		JOIN hashlists hl ON hh.hashlist_id = hl.id
		WHERE hl.client_id = $1
		  AND h.is_cracked = true
		  AND h.plaintext IS NOT NULL
		  AND h.plaintext != ''
		ORDER BY h.plaintext
	`

	rows, err := r.db.QueryContext(ctx, query, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to get unique plaintexts for client %s: %w", clientID, err)
	}
	defer rows.Close()

	var plaintexts []string
	for rows.Next() {
		var plaintext string
		if err := rows.Scan(&plaintext); err != nil {
			return nil, fmt.Errorf("failed to scan plaintext: %w", err)
		}
		plaintexts = append(plaintexts, plaintext)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating plaintexts: %w", err)
	}

	debug.Info("Retrieved %d unique plaintexts for client %s", len(plaintexts), clientID)
	return plaintexts, nil
}
