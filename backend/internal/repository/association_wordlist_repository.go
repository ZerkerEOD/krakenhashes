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

// AssociationWordlistRepository handles database operations for association wordlists.
type AssociationWordlistRepository struct {
	db *db.DB
}

// NewAssociationWordlistRepository creates a new instance of AssociationWordlistRepository.
func NewAssociationWordlistRepository(database *db.DB) *AssociationWordlistRepository {
	return &AssociationWordlistRepository{db: database}
}

// Create inserts a new association wordlist record into the database.
func (r *AssociationWordlistRepository) Create(ctx context.Context, wordlist *models.AssociationWordlist) error {
	query := `
		INSERT INTO association_wordlists (hashlist_id, file_path, file_name, file_size, line_count, md5_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`
	err := r.db.QueryRowContext(ctx, query,
		wordlist.HashlistID,
		wordlist.FilePath,
		wordlist.FileName,
		wordlist.FileSize,
		wordlist.LineCount,
		wordlist.MD5Hash,
		time.Now(),
	).Scan(&wordlist.ID)
	if err != nil {
		return fmt.Errorf("failed to create association wordlist: %w", err)
	}
	debug.Info("Created association wordlist %s for hashlist %d", wordlist.ID, wordlist.HashlistID)
	return nil
}

// GetByID retrieves an association wordlist by its ID.
func (r *AssociationWordlistRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.AssociationWordlist, error) {
	query := `
		SELECT id, hashlist_id, file_path, file_name, file_size, line_count, md5_hash, created_at
		FROM association_wordlists
		WHERE id = $1
	`
	var wordlist models.AssociationWordlist
	var fileSize sql.NullInt64
	var md5Hash sql.NullString
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&wordlist.ID,
		&wordlist.HashlistID,
		&wordlist.FilePath,
		&wordlist.FileName,
		&fileSize,
		&wordlist.LineCount,
		&md5Hash,
		&wordlist.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("association wordlist with ID %s not found: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get association wordlist by ID %s: %w", id, err)
	}
	if fileSize.Valid {
		wordlist.FileSize = fileSize.Int64
	}
	if md5Hash.Valid {
		wordlist.MD5Hash = md5Hash.String
	}
	return &wordlist, nil
}

// ListByHashlistID retrieves all association wordlists for a specific hashlist.
func (r *AssociationWordlistRepository) ListByHashlistID(ctx context.Context, hashlistID int64) ([]models.AssociationWordlist, error) {
	query := `
		SELECT id, hashlist_id, file_path, file_name, file_size, line_count, md5_hash, created_at
		FROM association_wordlists
		WHERE hashlist_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to list association wordlists for hashlist %d: %w", hashlistID, err)
	}
	defer rows.Close()

	var wordlists []models.AssociationWordlist
	for rows.Next() {
		var wordlist models.AssociationWordlist
		var fileSize sql.NullInt64
		var md5Hash sql.NullString
		if err := rows.Scan(
			&wordlist.ID,
			&wordlist.HashlistID,
			&wordlist.FilePath,
			&wordlist.FileName,
			&fileSize,
			&wordlist.LineCount,
			&md5Hash,
			&wordlist.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan association wordlist row: %w", err)
		}
		if fileSize.Valid {
			wordlist.FileSize = fileSize.Int64
		}
		if md5Hash.Valid {
			wordlist.MD5Hash = md5Hash.String
		}
		wordlists = append(wordlists, wordlist)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating association wordlist rows: %w", err)
	}
	return wordlists, nil
}

// Delete removes an association wordlist record from the database by its ID.
// Note: This does NOT delete the file from disk - that must be handled separately.
func (r *AssociationWordlistRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM association_wordlists WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete association wordlist %s: %w", id, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting association wordlist %s: %v", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("association wordlist %s not found for deletion: %w", id, ErrNotFound)
	}
	debug.Info("Deleted association wordlist %s", id)
	return nil
}

// DeleteByHashlistID removes all association wordlists for a specific hashlist.
// Returns the list of file paths that were deleted so they can be removed from disk.
func (r *AssociationWordlistRepository) DeleteByHashlistID(ctx context.Context, hashlistID int64) ([]string, error) {
	// First get all file paths
	selectQuery := `SELECT file_path FROM association_wordlists WHERE hashlist_id = $1`
	rows, err := r.db.QueryContext(ctx, selectQuery, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get association wordlist paths for hashlist %d: %w", hashlistID, err)
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
		return nil, fmt.Errorf("error iterating file path rows: %w", err)
	}

	// Then delete all records
	deleteQuery := `DELETE FROM association_wordlists WHERE hashlist_id = $1`
	result, err := r.db.ExecContext(ctx, deleteQuery, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete association wordlists for hashlist %d: %w", hashlistID, err)
	}
	rowsDeleted, _ := result.RowsAffected()
	debug.Info("Deleted %d association wordlists for hashlist %d", rowsDeleted, hashlistID)

	return filePaths, nil
}

// GetFilePath retrieves just the file path for an association wordlist by its ID.
// Used when serving the file to agents.
func (r *AssociationWordlistRepository) GetFilePath(ctx context.Context, id uuid.UUID) (string, error) {
	query := `SELECT file_path FROM association_wordlists WHERE id = $1`
	var filePath string
	err := r.db.QueryRowContext(ctx, query, id).Scan(&filePath)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("association wordlist with ID %s not found: %w", id, ErrNotFound)
		}
		return "", fmt.Errorf("failed to get file path for association wordlist %s: %w", id, err)
	}
	return filePath, nil
}

// UpdateMD5Hash updates the MD5 hash for an association wordlist after calculation.
func (r *AssociationWordlistRepository) UpdateMD5Hash(ctx context.Context, id uuid.UUID, md5Hash string) error {
	query := `UPDATE association_wordlists SET md5_hash = $1 WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, md5Hash, id)
	if err != nil {
		return fmt.Errorf("failed to update MD5 hash for association wordlist %s: %w", id, err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("association wordlist %s not found for MD5 update: %w", id, ErrNotFound)
	}
	return nil
}
