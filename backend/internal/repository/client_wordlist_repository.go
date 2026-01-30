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
)

// ClientWordlistRepository handles database operations for client-specific wordlists.
type ClientWordlistRepository struct {
	db *db.DB
}

// NewClientWordlistRepository creates a new instance of ClientWordlistRepository.
func NewClientWordlistRepository(database *db.DB) *ClientWordlistRepository {
	return &ClientWordlistRepository{db: database}
}

// Create inserts a new client wordlist record into the database.
func (r *ClientWordlistRepository) Create(ctx context.Context, wordlist *models.ClientWordlist) error {
	if wordlist.ID == uuid.Nil {
		wordlist.ID = uuid.New()
	}
	wordlist.CreatedAt = time.Now()

	query := `
		INSERT INTO client_wordlists (id, client_id, file_path, file_name, file_size, line_count, md5_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := r.db.ExecContext(ctx, query,
		wordlist.ID,
		wordlist.ClientID,
		wordlist.FilePath,
		wordlist.FileName,
		wordlist.FileSize,
		wordlist.LineCount,
		wordlist.MD5Hash,
		wordlist.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create client wordlist: %w", err)
	}

	debug.Info("Created client wordlist %s for client %s", wordlist.ID, wordlist.ClientID)
	return nil
}

// GetByID retrieves a client wordlist by its ID.
func (r *ClientWordlistRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.ClientWordlist, error) {
	query := `
		SELECT id, client_id, file_path, file_name, file_size, line_count, md5_hash, created_at
		FROM client_wordlists
		WHERE id = $1
	`

	var wordlist models.ClientWordlist
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&wordlist.ID,
		&wordlist.ClientID,
		&wordlist.FilePath,
		&wordlist.FileName,
		&wordlist.FileSize,
		&wordlist.LineCount,
		&wordlist.MD5Hash,
		&wordlist.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("client wordlist with ID %s not found: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get client wordlist by ID %s: %w", id, err)
	}

	return &wordlist, nil
}

// ListByClientID retrieves all wordlists for a specific client.
func (r *ClientWordlistRepository) ListByClientID(ctx context.Context, clientID uuid.UUID) ([]models.ClientWordlist, error) {
	query := `
		SELECT id, client_id, file_path, file_name, file_size, line_count, md5_hash, created_at
		FROM client_wordlists
		WHERE client_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to list client wordlists for client %s: %w", clientID, err)
	}
	defer rows.Close()

	var wordlists []models.ClientWordlist
	for rows.Next() {
		var wordlist models.ClientWordlist
		if err := rows.Scan(
			&wordlist.ID,
			&wordlist.ClientID,
			&wordlist.FilePath,
			&wordlist.FileName,
			&wordlist.FileSize,
			&wordlist.LineCount,
			&wordlist.MD5Hash,
			&wordlist.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan client wordlist row: %w", err)
		}
		wordlists = append(wordlists, wordlist)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client wordlist rows: %w", err)
	}

	return wordlists, nil
}

// Delete removes a client wordlist record from the database.
// Returns the file path so the caller can delete the file.
func (r *ClientWordlistRepository) Delete(ctx context.Context, id uuid.UUID) (string, error) {
	// First get the file path
	var filePath string
	getQuery := `SELECT file_path FROM client_wordlists WHERE id = $1`
	err := r.db.QueryRowContext(ctx, getQuery, id).Scan(&filePath)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("client wordlist with ID %s not found: %w", id, ErrNotFound)
		}
		return "", fmt.Errorf("failed to get client wordlist file path: %w", err)
	}

	// Delete the record
	deleteQuery := `DELETE FROM client_wordlists WHERE id = $1`
	result, err := r.db.ExecContext(ctx, deleteQuery, id)
	if err != nil {
		return "", fmt.Errorf("failed to delete client wordlist %s: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting client wordlist %s: %v", id, err)
	} else if rowsAffected == 0 {
		return "", fmt.Errorf("client wordlist with ID %s not found for deletion: %w", id, ErrNotFound)
	}

	debug.Info("Deleted client wordlist %s", id)
	return filePath, nil
}

// DeleteByClientID removes all wordlists for a client.
// Returns the file paths so the caller can delete the files.
func (r *ClientWordlistRepository) DeleteByClientID(ctx context.Context, clientID uuid.UUID) ([]string, error) {
	// First get all file paths
	getQuery := `SELECT file_path FROM client_wordlists WHERE client_id = $1`
	rows, err := r.db.QueryContext(ctx, getQuery, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client wordlist file paths: %w", err)
	}
	defer rows.Close()

	var filePaths []string
	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err != nil {
			return nil, fmt.Errorf("failed to scan file path: %w", err)
		}
		filePaths = append(filePaths, filePath)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating file paths: %w", err)
	}

	// Delete all records
	deleteQuery := `DELETE FROM client_wordlists WHERE client_id = $1`
	_, err = r.db.ExecContext(ctx, deleteQuery, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete client wordlists for client %s: %w", clientID, err)
	}

	debug.Info("Deleted %d client wordlists for client %s", len(filePaths), clientID)
	return filePaths, nil
}

// GetFilePath returns the file path for a client wordlist.
func (r *ClientWordlistRepository) GetFilePath(ctx context.Context, id uuid.UUID) (string, error) {
	query := `SELECT file_path FROM client_wordlists WHERE id = $1`
	var filePath string
	err := r.db.QueryRowContext(ctx, query, id).Scan(&filePath)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("client wordlist with ID %s not found: %w", id, ErrNotFound)
		}
		return "", fmt.Errorf("failed to get client wordlist file path: %w", err)
	}
	return filePath, nil
}

// UpdateMD5Hash updates the MD5 hash for a client wordlist.
func (r *ClientWordlistRepository) UpdateMD5Hash(ctx context.Context, id uuid.UUID, md5Hash string) error {
	query := `UPDATE client_wordlists SET md5_hash = $1 WHERE id = $2`
	_, err := r.db.ExecContext(ctx, query, md5Hash, id)
	if err != nil {
		return fmt.Errorf("failed to update MD5 hash for client wordlist %s: %w", id, err)
	}
	return nil
}

// Exists checks if a client wordlist with the given ID exists.
func (r *ClientWordlistRepository) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM client_wordlists WHERE id = $1)`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, id).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check client wordlist existence: %w", err)
	}
	return exists, nil
}

// GetByClientIDAndFileName checks if a wordlist with the same filename already exists for the client.
func (r *ClientWordlistRepository) GetByClientIDAndFileName(ctx context.Context, clientID uuid.UUID, fileName string) (*models.ClientWordlist, error) {
	query := `
		SELECT id, client_id, file_path, file_name, file_size, line_count, md5_hash, created_at
		FROM client_wordlists
		WHERE client_id = $1 AND file_name = $2
	`

	var wordlist models.ClientWordlist
	err := r.db.QueryRowContext(ctx, query, clientID, fileName).Scan(
		&wordlist.ID,
		&wordlist.ClientID,
		&wordlist.FilePath,
		&wordlist.FileName,
		&wordlist.FileSize,
		&wordlist.LineCount,
		&wordlist.MD5Hash,
		&wordlist.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found, no error
		}
		return nil, fmt.Errorf("failed to get client wordlist by filename: %w", err)
	}

	return &wordlist, nil
}
