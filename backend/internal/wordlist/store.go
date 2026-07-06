package wordlist

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// Store handles database operations for wordlists
type Store struct {
	db *sql.DB
}

// NewStore creates a new wordlist store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// wordlistColumns is the canonical column list (including the filtering columns
// from GH #40) used by every wordlist SELECT so reads stay consistent.
const wordlistColumns = `w.id, w.name, w.description, w.wordlist_type, w.format, w.file_name,
	w.md5_hash, w.file_size, w.word_count, w.created_at, w.created_by,
	w.updated_at, w.updated_by, w.last_verified_at, w.verification_status,
	w.is_potfile, w.parent_wordlist_id, w.filter_spec, w.parent_md5,
	w.is_ephemeral, w.owner_job_id, w.is_stale, w.parent_offset, w.parent_anchor_md5`

// rowScanner abstracts *sql.Row and *sql.Rows for the shared scan helper.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanWordlist scans a row selected with wordlistColumns into a Wordlist.
func scanWordlist(row rowScanner, w *models.Wordlist) error {
	var (
		lastVerifiedAt sql.NullTime
		parentID       sql.NullInt64
		filterSpec     []byte
		parentMD5      sql.NullString
		ownerJob       uuid.NullUUID
		parentOffset   sql.NullInt64
		parentAnchor   sql.NullString
	)

	if err := row.Scan(
		&w.ID, &w.Name, &w.Description, &w.WordlistType, &w.Format, &w.FileName,
		&w.MD5Hash, &w.FileSize, &w.WordCount, &w.CreatedAt, &w.CreatedBy,
		&w.UpdatedAt, &w.UpdatedBy, &lastVerifiedAt, &w.VerificationStatus,
		&w.IsPotfile, &parentID, &filterSpec, &parentMD5,
		&w.IsEphemeral, &ownerJob, &w.IsStale, &parentOffset, &parentAnchor,
	); err != nil {
		return err
	}

	if lastVerifiedAt.Valid {
		w.LastVerifiedAt = lastVerifiedAt.Time
	}
	if parentID.Valid {
		id := int(parentID.Int64)
		w.ParentWordlistID = &id
	}
	if len(filterSpec) > 0 {
		var f models.WordlistFilter
		if err := json.Unmarshal(filterSpec, &f); err == nil {
			w.FilterSpec = &f
		}
	}
	if parentMD5.Valid {
		w.ParentMD5 = parentMD5.String
	}
	if ownerJob.Valid {
		id := ownerJob.UUID
		w.OwnerJobID = &id
	}
	if parentOffset.Valid {
		off := parentOffset.Int64
		w.ParentOffset = &off
	}
	if parentAnchor.Valid {
		anchor := parentAnchor.String
		w.ParentAnchorMD5 = &anchor
	}
	return nil
}

// ListWordlists retrieves all wordlists with optional filtering
func (s *Store) ListWordlists(ctx context.Context, filters map[string]interface{}) ([]*models.Wordlist, error) {
	query := `SELECT ` + wordlistColumns + ` FROM wordlists w WHERE 1=1`
	args := []interface{}{}
	argPos := 1

	// Apply filters
	if wordlistType, ok := filters["wordlist_type"]; ok {
		query += " AND w.wordlist_type = $" + strconv.Itoa(argPos)
		args = append(args, wordlistType)
		argPos++
	}

	if format, ok := filters["format"]; ok {
		query += " AND w.format = $" + strconv.Itoa(argPos)
		args = append(args, format)
		argPos++
	}

	if tag, ok := filters["tag"]; ok {
		query += ` AND w.id IN (
			SELECT wordlist_id FROM wordlist_tags WHERE tag = $` + strconv.Itoa(argPos) + `
		)`
		args = append(args, tag)
		argPos++
	}

	// By default, hide ephemeral (job-scoped) filtered wordlists from listings.
	if includeEphemeral, ok := filters["include_ephemeral"].(bool); !ok || !includeEphemeral {
		query += " AND w.is_ephemeral = false"
	}

	query += " ORDER BY w.name ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		debug.Error("Failed to list wordlists: %v", err)
		return nil, err
	}
	defer rows.Close()

	wordlists := []*models.Wordlist{}
	for rows.Next() {
		w := &models.Wordlist{}
		if err := scanWordlist(rows, w); err != nil {
			debug.Error("Failed to scan wordlist row: %v", err)
			return nil, err
		}

		tags, err := s.GetWordlistTags(ctx, w.ID)
		if err != nil {
			debug.Error("Failed to get tags for wordlist %d: %v", w.ID, err)
			return nil, err
		}
		w.Tags = tags

		wordlists = append(wordlists, w)
	}

	if err := rows.Err(); err != nil {
		debug.Error("Error iterating wordlist rows: %v", err)
		return nil, err
	}

	return wordlists, nil
}

// GetWordlist retrieves a wordlist by ID
func (s *Store) GetWordlist(ctx context.Context, id int) (*models.Wordlist, error) {
	query := `SELECT ` + wordlistColumns + ` FROM wordlists w WHERE w.id = $1`

	w := &models.Wordlist{}
	err := scanWordlist(s.db.QueryRowContext(ctx, query, id), w)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		debug.Error("Failed to get wordlist %d: %v", id, err)
		return nil, err
	}

	tags, err := s.GetWordlistTags(ctx, w.ID)
	if err != nil {
		debug.Error("Failed to get tags for wordlist %d: %v", w.ID, err)
		return nil, err
	}
	w.Tags = tags

	return w, nil
}

// GetWordlistByFilename retrieves a wordlist by filename
func (s *Store) GetWordlistByFilename(ctx context.Context, filename string) (*models.Wordlist, error) {
	query := `SELECT ` + wordlistColumns + ` FROM wordlists w WHERE w.file_name = $1`

	w := &models.Wordlist{}
	err := scanWordlist(s.db.QueryRowContext(ctx, query, filename), w)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		debug.Error("Failed to get wordlist by filename %s: %v", filename, err)
		return nil, err
	}

	tags, err := s.GetWordlistTags(ctx, w.ID)
	if err != nil {
		debug.Error("Failed to get tags for wordlist %d: %v", w.ID, err)
		return nil, err
	}
	w.Tags = tags

	return w, nil
}

// GetWordlistByMD5Hash retrieves a wordlist by MD5 hash
func (s *Store) GetWordlistByMD5Hash(ctx context.Context, md5Hash string) (*models.Wordlist, error) {
	query := `SELECT ` + wordlistColumns + ` FROM wordlists w WHERE w.md5_hash = $1`

	w := &models.Wordlist{}
	err := scanWordlist(s.db.QueryRowContext(ctx, query, md5Hash), w)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		debug.Error("Failed to get wordlist by MD5 hash %s: %v", md5Hash, err)
		return nil, err
	}

	tags, err := s.GetWordlistTags(ctx, w.ID)
	if err != nil {
		debug.Error("Failed to get tags for wordlist %d: %v", w.ID, err)
		return nil, err
	}
	w.Tags = tags

	return w, nil
}

// GetFilteredChildren returns all filtered wordlists derived from a parent.
func (s *Store) GetFilteredChildren(ctx context.Context, parentID int) ([]*models.Wordlist, error) {
	query := `SELECT ` + wordlistColumns + ` FROM wordlists w WHERE w.parent_wordlist_id = $1 ORDER BY w.name ASC`

	rows, err := s.db.QueryContext(ctx, query, parentID)
	if err != nil {
		debug.Error("Failed to get filtered children for wordlist %d: %v", parentID, err)
		return nil, err
	}
	defer rows.Close()

	children := []*models.Wordlist{}
	for rows.Next() {
		w := &models.Wordlist{}
		if err := scanWordlist(rows, w); err != nil {
			debug.Error("Failed to scan filtered child row: %v", err)
			return nil, err
		}
		children = append(children, w)
	}
	return children, rows.Err()
}

// GetEphemeralByJob returns ephemeral filtered wordlists owned by a job execution.
func (s *Store) GetEphemeralByJob(ctx context.Context, jobID uuid.UUID) ([]*models.Wordlist, error) {
	query := `SELECT ` + wordlistColumns + ` FROM wordlists w WHERE w.owner_job_id = $1`

	rows, err := s.db.QueryContext(ctx, query, jobID)
	if err != nil {
		debug.Error("Failed to get ephemeral wordlists for job %s: %v", jobID, err)
		return nil, err
	}
	defer rows.Close()

	list := []*models.Wordlist{}
	for rows.Next() {
		w := &models.Wordlist{}
		if err := scanWordlist(rows, w); err != nil {
			debug.Error("Failed to scan ephemeral wordlist row: %v", err)
			return nil, err
		}
		list = append(list, w)
	}
	return list, rows.Err()
}

// MarkChildrenStale flags every permanent filtered child of a parent as stale
// when the parent's MD5 differs from the MD5 captured at the child's generation.
func (s *Store) MarkChildrenStale(ctx context.Context, parentID int, currentParentMD5 string) error {
	query := `UPDATE wordlists
		SET is_stale = true, updated_at = NOW()
		WHERE parent_wordlist_id = $1
		  AND is_ephemeral = false
		  AND (parent_md5 IS NULL OR parent_md5 <> $2)`
	_, err := s.db.ExecContext(ctx, query, parentID, currentParentMD5)
	if err != nil {
		debug.Error("Failed to mark children of wordlist %d stale: %v", parentID, err)
	}
	return err
}

// CreateWordlist creates a new wordlist
func (s *Store) CreateWordlist(ctx context.Context, wordlist *models.Wordlist) error {
	query := `
		INSERT INTO wordlists (
			name, description, wordlist_type, format, file_name,
			md5_hash, file_size, word_count, created_by, verification_status, is_potfile,
			parent_wordlist_id, filter_spec, parent_md5, is_ephemeral, owner_job_id, is_stale
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id, created_at, updated_at
	`

	var filterSpec interface{}
	if wordlist.FilterSpec != nil {
		b, err := json.Marshal(wordlist.FilterSpec)
		if err != nil {
			return err
		}
		filterSpec = string(b)
	}
	var parentID interface{}
	if wordlist.ParentWordlistID != nil {
		parentID = *wordlist.ParentWordlistID
	}
	var parentMD5 interface{}
	if wordlist.ParentMD5 != "" {
		parentMD5 = wordlist.ParentMD5
	}
	var ownerJob interface{}
	if wordlist.OwnerJobID != nil {
		ownerJob = *wordlist.OwnerJobID
	}

	err := s.db.QueryRowContext(ctx, query,
		wordlist.Name, wordlist.Description, wordlist.WordlistType, wordlist.Format, wordlist.FileName,
		wordlist.MD5Hash, wordlist.FileSize, wordlist.WordCount, wordlist.CreatedBy, wordlist.VerificationStatus,
		wordlist.IsPotfile, parentID, filterSpec, parentMD5, wordlist.IsEphemeral, ownerJob, wordlist.IsStale,
	).Scan(&wordlist.ID, &wordlist.CreatedAt, &wordlist.UpdatedAt)
	if err != nil {
		debug.Error("Failed to create wordlist: %v", err)
		return err
	}

	if len(wordlist.Tags) > 0 {
		for _, tag := range wordlist.Tags {
			err := s.AddWordlistTag(ctx, wordlist.ID, tag, wordlist.CreatedBy)
			if err != nil {
				debug.Error("Failed to add tag %s to wordlist %d: %v", tag, wordlist.ID, err)
				return err
			}
		}
	}

	return nil
}

// UpdateWordlist updates an existing wordlist
func (s *Store) UpdateWordlist(ctx context.Context, wordlist *models.Wordlist) error {
	query := `
		UPDATE wordlists
		SET name = $1, description = $2, wordlist_type = $3, format = $4,
		    updated_at = NOW(), updated_by = $5
		WHERE id = $6
		RETURNING updated_at
	`

	err := s.db.QueryRowContext(ctx, query,
		wordlist.Name, wordlist.Description, wordlist.WordlistType, wordlist.Format,
		wordlist.UpdatedBy, wordlist.ID,
	).Scan(&wordlist.UpdatedAt)
	if err != nil {
		debug.Error("Failed to update wordlist %d: %v", wordlist.ID, err)
		return err
	}

	return nil
}

// DeleteWordlist deletes a wordlist
func (s *Store) DeleteWordlist(ctx context.Context, id int) error {
	// Delete tags first (foreign key constraint)
	_, err := s.db.ExecContext(ctx, "DELETE FROM wordlist_tags WHERE wordlist_id = $1", id)
	if err != nil {
		debug.Error("Failed to delete tags for wordlist %d: %v", id, err)
		return err
	}

	// Delete wordlist
	_, err = s.db.ExecContext(ctx, "DELETE FROM wordlists WHERE id = $1", id)
	if err != nil {
		debug.Error("Failed to delete wordlist %d: %v", id, err)
		return err
	}

	return nil
}

// UpdateWordlistVerification updates a wordlist's verification status
func (s *Store) UpdateWordlistVerification(ctx context.Context, id int, status string, wordCount *int64) error {
	query := `
		UPDATE wordlists
		SET verification_status = $1, last_verified_at = NOW()
	`
	args := []interface{}{status, id}
	argPos := 3

	if wordCount != nil {
		query += ", word_count = $" + strconv.Itoa(argPos)
		args = append(args, *wordCount)
		argPos++
	}

	query += " WHERE id = $2"

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		debug.Error("Failed to update verification status for wordlist %d: %v", id, err)
		return err
	}

	return nil
}

// UpdateWordlistFileInfo updates a wordlist's file information (MD5 hash and file size)
func (s *Store) UpdateWordlistFileInfo(ctx context.Context, id int, md5Hash string, fileSize int64) error {
	query := `
		UPDATE wordlists
		SET md5_hash = $1, file_size = $2, updated_at = NOW()
		WHERE id = $3
	`

	_, err := s.db.ExecContext(ctx, query, md5Hash, fileSize, id)
	if err != nil {
		debug.Error("Failed to update wordlist file info for ID %d: %v", id, err)
		return err
	}

	return nil
}

// UpdateWordlistComplete updates a wordlist's complete file information (MD5 hash, file size, and word count)
func (s *Store) UpdateWordlistComplete(ctx context.Context, id int, md5Hash string, fileSize int64, wordCount int64) error {
	query := `
		UPDATE wordlists
		SET md5_hash = $1, file_size = $2, word_count = $3, updated_at = NOW()
		WHERE id = $4
	`

	_, err := s.db.ExecContext(ctx, query, md5Hash, fileSize, wordCount, id)
	if err != nil {
		debug.Error("Failed to update wordlist complete info for ID %d: %v", id, err)
		return err
	}

	return nil
}

// ClearStale marks a filtered wordlist as fresh again (used after regeneration).
func (s *Store) ClearStale(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "UPDATE wordlists SET is_stale = false, updated_at = NOW() WHERE id = $1", id)
	if err != nil {
		debug.Error("Failed to clear stale flag for wordlist %d: %v", id, err)
	}
	return err
}

// ClearFilteredIndex drops the incremental-regeneration index (parent_offset /
// parent_anchor_md5) for a filtered wordlist, forcing its next generation to be a
// full rebuild (GH #40 follow-up — used by the manual "force full regenerate" path).
func (s *Store) ClearFilteredIndex(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, "UPDATE wordlists SET parent_offset = NULL, parent_anchor_md5 = NULL, updated_at = NOW() WHERE id = $1", id)
	if err != nil {
		debug.Error("Failed to clear filtered index for wordlist %d: %v", id, err)
	}
	return err
}

// UpdateFilteredParentMD5 records the parent MD5 captured for a filtered wordlist.
func (s *Store) UpdateFilteredParentMD5(ctx context.Context, id int, parentMD5 string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE wordlists SET parent_md5 = $1, updated_at = NOW() WHERE id = $2", parentMD5, id)
	if err != nil {
		debug.Error("Failed to update parent MD5 for wordlist %d: %v", id, err)
	}
	return err
}

// UpdateFilteredIndex records the full incremental-regeneration index for a
// filtered wordlist (GH #40 follow-up): the parent's full MD5 at generation time
// plus the byte offset / anchor hash that let a later append-only parent change be
// regenerated incrementally. parentOffset/anchorMD5 are nil for compressed parents
// (no seekable offset), which stores NULL and forces a full rebuild next time.
func (s *Store) UpdateFilteredIndex(ctx context.Context, id int, parentMD5 string, parentOffset *int64, anchorMD5 *string) error {
	var offsetArg interface{}
	if parentOffset != nil {
		offsetArg = *parentOffset
	}
	var anchorArg interface{}
	if anchorMD5 != nil {
		anchorArg = *anchorMD5
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE wordlists SET parent_md5 = $1, parent_offset = $2, parent_anchor_md5 = $3, updated_at = NOW() WHERE id = $4",
		parentMD5, offsetArg, anchorArg, id)
	if err != nil {
		debug.Error("Failed to update filtered index for wordlist %d: %v", id, err)
	}
	return err
}

// SetWordlistOwnerJob attaches an ephemeral filtered wordlist to its owning job.
func (s *Store) SetWordlistOwnerJob(ctx context.Context, wordlistID int, jobID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "UPDATE wordlists SET owner_job_id = $1, updated_at = NOW() WHERE id = $2", jobID, wordlistID)
	if err != nil {
		debug.Error("Failed to set owner job for wordlist %d: %v", wordlistID, err)
	}
	return err
}

// GetWordlistTags gets tags for a wordlist
func (s *Store) GetWordlistTags(ctx context.Context, id int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT tag FROM wordlist_tags WHERE wordlist_id = $1", id)
	if err != nil {
		debug.Error("Failed to get tags for wordlist %d: %v", id, err)
		return nil, err
	}
	defer rows.Close()

	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			debug.Error("Failed to scan tag: %v", err)
			return nil, err
		}
		tags = append(tags, tag)
	}

	if err := rows.Err(); err != nil {
		debug.Error("Error iterating tag rows: %v", err)
		return nil, err
	}

	return tags, nil
}

// AddWordlistTag adds a tag to a wordlist
func (s *Store) AddWordlistTag(ctx context.Context, id int, tag string, userID uuid.UUID) error {
	// Check if tag already exists
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM wordlist_tags WHERE wordlist_id = $1 AND tag = $2)", id, tag).Scan(&exists)
	if err != nil {
		debug.Error("Failed to check if tag exists: %v", err)
		return err
	}

	if exists {
		return nil // Tag already exists, nothing to do
	}

	// Add tag
	_, err = s.db.ExecContext(ctx, "INSERT INTO wordlist_tags (wordlist_id, tag, created_by) VALUES ($1, $2, $3)", id, tag, userID)
	if err != nil {
		debug.Error("Failed to add tag %s to wordlist %d: %v", tag, id, err)
		return err
	}

	return nil
}

// DeleteWordlistTag deletes a tag from a wordlist
func (s *Store) DeleteWordlistTag(ctx context.Context, id int, tag string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM wordlist_tags WHERE wordlist_id = $1 AND tag = $2", id, tag)
	if err != nil {
		debug.Error("Failed to delete tag %s from wordlist %d: %v", tag, id, err)
		return err
	}

	return nil
}
