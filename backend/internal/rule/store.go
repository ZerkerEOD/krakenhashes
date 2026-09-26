package rule

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// Store handles database operations for rules
type Store struct {
	db *sql.DB
}

// NewStore creates a new rule store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// ListRules retrieves rules based on the provided filter
func (s *Store) ListRules(ctx context.Context, filter *models.RuleFilter) ([]*models.Rule, error) {
	query := `
		SELECT r.id, r.name, r.description, r.rule_type, r.file_name, 
		       r.md5_hash, r.file_size, r.rule_count, r.created_at, r.created_by, 
		       r.updated_at, r.updated_by, r.last_verified_at, r.verification_status, r.missing_since
		FROM rules r
		WHERE 1=1
	`
	args := []interface{}{}
	argIndex := 1

	// Apply filters
	if filter != nil {
		if filter.Search != "" {
			query += fmt.Sprintf(" AND (r.name ILIKE $%d OR r.description ILIKE $%d)", argIndex, argIndex)
			args = append(args, "%"+filter.Search+"%")
			argIndex++
		}
		if filter.RuleType != "" {
			query += fmt.Sprintf(" AND r.rule_type = $%d", argIndex)
			args = append(args, filter.RuleType)
			argIndex++
		}
		if filter.VerificationStatus != "" {
			query += fmt.Sprintf(" AND r.verification_status = $%d", argIndex)
			args = append(args, filter.VerificationStatus)
			argIndex++
		}
	}

	// Apply sorting
	if filter != nil && filter.SortBy != "" {
		// Validate sort column to prevent SQL injection
		validSortColumns := map[string]string{
			"name":                "r.name",
			"rule_type":           "r.rule_type",
			"file_size":           "r.file_size",
			"rule_count":          "r.rule_count",
			"created_at":          "r.created_at",
			"verification_status": "r.verification_status",
		}

		sortColumn, ok := validSortColumns[filter.SortBy]
		if !ok {
			sortColumn = "r.created_at" // Default sort column
		}

		// Validate sort order
		sortOrder := "DESC"
		if filter.SortOrder == "asc" {
			sortOrder = "ASC"
		}

		query += fmt.Sprintf(" ORDER BY %s %s", sortColumn, sortOrder)
	} else {
		query += " ORDER BY r.created_at DESC" // Default sorting
	}

	// Execute query
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		debug.Error("Failed to list rules: %v", err)
		return nil, err
	}
	defer rows.Close()

	// Parse results
	rules := []*models.Rule{}
	for rows.Next() {
		r := &models.Rule{}
		var lastVerifiedAt sql.NullTime
		var missingSince sql.NullTime

		err := rows.Scan(
			&r.ID, &r.Name, &r.Description, &r.RuleType, &r.FileName,
			&r.MD5Hash, &r.FileSize, &r.RuleCount, &r.CreatedAt, &r.CreatedBy,
			&r.UpdatedAt, &r.UpdatedBy, &lastVerifiedAt, &r.VerificationStatus, &missingSince,
		)
		if err != nil {
			debug.Error("Failed to scan rule: %v", err)
			return nil, err
		}

		// Set LastVerifiedAt if valid
		if lastVerifiedAt.Valid {
			r.LastVerifiedAt = lastVerifiedAt.Time
		}
		if missingSince.Valid {
			t := missingSince.Time
			r.MissingSince = &t
		}

		// Get tags for this rule
		tags, err := s.GetRuleTags(ctx, r.ID)
		if err != nil {
			debug.Error("Failed to get tags for rule %d: %v", r.ID, err)
			return nil, err
		}
		r.Tags = tags

		rules = append(rules, r)
	}

	if err := rows.Err(); err != nil {
		debug.Error("Error iterating rule rows: %v", err)
		return nil, err
	}

	return rules, nil
}

// GetRule retrieves a rule by ID
func (s *Store) GetRule(ctx context.Context, id int) (*models.Rule, error) {
	query := `
		SELECT r.id, r.name, r.description, r.rule_type, r.file_name, 
		       r.md5_hash, r.file_size, r.rule_count, r.created_at, r.created_by, 
		       r.updated_at, r.updated_by, r.last_verified_at, r.verification_status
		FROM rules r
		WHERE r.id = $1
	`

	r := &models.Rule{}
	var lastVerifiedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&r.ID, &r.Name, &r.Description, &r.RuleType, &r.FileName,
		&r.MD5Hash, &r.FileSize, &r.RuleCount, &r.CreatedAt, &r.CreatedBy,
		&r.UpdatedAt, &r.UpdatedBy, &lastVerifiedAt, &r.VerificationStatus,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		debug.Error("Failed to get rule %d: %v", id, err)
		return nil, err
	}

	// Set LastVerifiedAt if valid
	if lastVerifiedAt.Valid {
		r.LastVerifiedAt = lastVerifiedAt.Time
	}

	// Get tags for this rule
	tags, err := s.GetRuleTags(ctx, r.ID)
	if err != nil {
		debug.Error("Failed to get tags for rule %d: %v", r.ID, err)
		return nil, err
	}
	r.Tags = tags

	return r, nil
}

// GetRuleByFilename retrieves a rule by its filename
func (s *Store) GetRuleByFilename(ctx context.Context, filename string) (*models.Rule, error) {
	query := `
		SELECT r.id, r.name, r.description, r.rule_type, r.file_name, 
		       r.md5_hash, r.file_size, r.rule_count, r.created_at, r.created_by, 
		       r.updated_at, r.updated_by, r.last_verified_at, r.verification_status
		FROM rules r
		WHERE r.file_name = $1
	`

	r := &models.Rule{}
	var lastVerifiedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, filename).Scan(
		&r.ID, &r.Name, &r.Description, &r.RuleType, &r.FileName,
		&r.MD5Hash, &r.FileSize, &r.RuleCount, &r.CreatedAt, &r.CreatedBy,
		&r.UpdatedAt, &r.UpdatedBy, &lastVerifiedAt, &r.VerificationStatus,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		debug.Error("Failed to get rule by filename %s: %v", filename, err)
		return nil, err
	}

	// Set LastVerifiedAt if valid
	if lastVerifiedAt.Valid {
		r.LastVerifiedAt = lastVerifiedAt.Time
	}

	// Get tags for this rule
	tags, err := s.GetRuleTags(ctx, r.ID)
	if err != nil {
		debug.Error("Failed to get tags for rule %d: %v", r.ID, err)
		return nil, err
	}
	r.Tags = tags

	return r, nil
}

// GetRuleByMD5Hash retrieves a rule by its MD5 hash
func (s *Store) GetRuleByMD5Hash(ctx context.Context, md5Hash string) (*models.Rule, error) {
	query := `
		SELECT r.id, r.name, r.description, r.rule_type, r.file_name, 
		       r.md5_hash, r.file_size, r.rule_count, r.created_at, r.created_by, 
		       r.updated_at, r.updated_by, r.last_verified_at, r.verification_status
		FROM rules r
		WHERE r.md5_hash = $1
	`

	r := &models.Rule{}
	var lastVerifiedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, md5Hash).Scan(
		&r.ID, &r.Name, &r.Description, &r.RuleType, &r.FileName,
		&r.MD5Hash, &r.FileSize, &r.RuleCount, &r.CreatedAt, &r.CreatedBy,
		&r.UpdatedAt, &r.UpdatedBy, &lastVerifiedAt, &r.VerificationStatus,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		debug.Error("Failed to get rule by MD5 hash %s: %v", md5Hash, err)
		return nil, err
	}

	// Set LastVerifiedAt if valid
	if lastVerifiedAt.Valid {
		r.LastVerifiedAt = lastVerifiedAt.Time
	}

	// Get tags for this rule
	tags, err := s.GetRuleTags(ctx, r.ID)
	if err != nil {
		debug.Error("Failed to get tags for rule %d: %v", r.ID, err)
		return nil, err
	}
	r.Tags = tags

	return r, nil
}

// CreateRule creates a new rule
func (s *Store) CreateRule(ctx context.Context, rule *models.Rule) error {
	query := `
		INSERT INTO rules (
			name, description, rule_type, file_name, 
			md5_hash, file_size, rule_count, created_by, verification_status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at
	`

	err := s.db.QueryRowContext(ctx, query,
		rule.Name, rule.Description, rule.RuleType, rule.FileName,
		rule.MD5Hash, rule.FileSize, rule.RuleCount, rule.CreatedBy, rule.VerificationStatus,
	).Scan(&rule.ID, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		debug.Error("Failed to create rule: %v", err)
		return err
	}

	// Add tags if provided
	if len(rule.Tags) > 0 {
		for _, tag := range rule.Tags {
			err := s.AddRuleTag(ctx, rule.ID, tag, rule.CreatedBy)
			if err != nil {
				debug.Error("Failed to add tag %s to rule %d: %v", tag, rule.ID, err)
				return err
			}
		}
	}

	return nil
}

// UpdateRule updates an existing rule
func (s *Store) UpdateRule(ctx context.Context, rule *models.Rule) error {
	query := `
		UPDATE rules
		SET name = $1, description = $2, rule_type = $3,
		    updated_at = NOW(), updated_by = $4
		WHERE id = $5
		RETURNING updated_at
	`

	err := s.db.QueryRowContext(ctx, query,
		rule.Name, rule.Description, rule.RuleType,
		rule.UpdatedBy, rule.ID,
	).Scan(&rule.UpdatedAt)
	if err != nil {
		debug.Error("Failed to update rule %d: %v", rule.ID, err)
		return err
	}

	return nil
}

// DeleteRule deletes a rule
func (s *Store) DeleteRule(ctx context.Context, id int) error {
	// Delete tags first (foreign key constraint)
	_, err := s.db.ExecContext(ctx, "DELETE FROM rule_tags WHERE rule_id = $1", id)
	if err != nil {
		debug.Error("Failed to delete tags for rule %d: %v", id, err)
		return err
	}

	// Delete rule
	_, err = s.db.ExecContext(ctx, "DELETE FROM rules WHERE id = $1", id)
	if err != nil {
		debug.Error("Failed to delete rule %d: %v", id, err)
		return err
	}

	return nil
}

// UpdateRuleVerification updates a rule's verification status
func (s *Store) UpdateRuleVerification(ctx context.Context, id int, status string, ruleCount *int64) error {
	query := `
		UPDATE rules
		SET verification_status = $1, last_verified_at = NOW()
	`
	args := []interface{}{status, id}
	argPos := 3

	if ruleCount != nil {
		query += ", rule_count = $" + strconv.Itoa(argPos)
		args = append(args, *ruleCount)
		argPos++
	}

	query += " WHERE id = $2"

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		debug.Error("Failed to update verification status for rule %d: %v", id, err)
		return err
	}

	return nil
}

// MarkRuleMissing flags a rule whose file is gone from disk (GH #93): status ->
// 'failed', missing_since -> NOW(). Only flips a currently-'verified' row not
// already flagged, so it never clobbers a genuine content failure. Returns
// whether a row changed.
func (s *Store) MarkRuleMissing(ctx context.Context, id int) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE rules
		SET verification_status = 'failed', missing_since = NOW(), last_verified_at = NOW()
		WHERE id = $1 AND verification_status = 'verified' AND missing_since IS NULL
	`, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark rule %d missing: %w", id, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RestoreRuleOnDisk clears the missing flag once the file is back (GH #93):
// status -> 'verified', missing_since -> NULL. Guarded on missing_since IS NOT
// NULL so it only un-does a missing-file flag. Returns whether a row changed.
func (s *Store) RestoreRuleOnDisk(ctx context.Context, id int) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE rules
		SET verification_status = 'verified', missing_since = NULL, last_verified_at = NOW()
		WHERE id = $1 AND missing_since IS NOT NULL
	`, id)
	if err != nil {
		return false, fmt.Errorf("failed to restore rule %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// UpdateRuleFileInfo updates a rule's file information (MD5 hash and file size)
func (s *Store) UpdateRuleFileInfo(ctx context.Context, id int, md5Hash string, fileSize int64) error {
	query := `
		UPDATE rules
		SET md5_hash = $1, file_size = $2, updated_at = NOW()
		WHERE id = $3
	`

	_, err := s.db.ExecContext(ctx, query, md5Hash, fileSize, id)
	if err != nil {
		debug.Error("Failed to update rule file info for ID %d: %v", id, err)
		return err
	}

	return nil
}

// GetRuleTags gets tags for a rule
func (s *Store) GetRuleTags(ctx context.Context, id int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT tag FROM rule_tags WHERE rule_id = $1", id)
	if err != nil {
		debug.Error("Failed to get tags for rule %d: %v", id, err)
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

// AddRuleTag adds a tag to a rule
func (s *Store) AddRuleTag(ctx context.Context, id int, tag string, userID uuid.UUID) error {
	// Check if tag already exists
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM rule_tags WHERE rule_id = $1 AND tag = $2)", id, tag).Scan(&exists)
	if err != nil {
		debug.Error("Failed to check if tag exists: %v", err)
		return err
	}

	if exists {
		return nil // Tag already exists, nothing to do
	}

	// Add tag
	_, err = s.db.ExecContext(ctx, "INSERT INTO rule_tags (rule_id, tag, created_by) VALUES ($1, $2, $3)", id, tag, userID)
	if err != nil {
		debug.Error("Failed to add tag %s to rule %d: %v", tag, id, err)
		return err
	}

	return nil
}

// DeleteRuleTag deletes a tag from a rule
func (s *Store) DeleteRuleTag(ctx context.Context, id int, tag string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM rule_tags WHERE rule_id = $1 AND tag = $2", id, tag)
	if err != nil {
		debug.Error("Failed to delete tag %s from rule %d: %v", tag, id, err)
		return err
	}

	return nil
}

// GetRuleByName retrieves a rule by its name
func (s *Store) GetRuleByName(ctx context.Context, name string) (*models.Rule, error) {
	query := `
		SELECT r.id, r.name, r.description, r.rule_type, r.file_name, 
		       r.md5_hash, r.file_size, r.rule_count, r.created_at, r.created_by, 
		       r.updated_at, r.updated_by, r.last_verified_at, r.verification_status
		FROM rules r
		WHERE r.name = $1
	`

	r := &models.Rule{}
	var lastVerifiedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, name).Scan(
		&r.ID, &r.Name, &r.Description, &r.RuleType, &r.FileName,
		&r.MD5Hash, &r.FileSize, &r.RuleCount, &r.CreatedAt, &r.CreatedBy,
		&r.UpdatedAt, &r.UpdatedBy, &lastVerifiedAt, &r.VerificationStatus,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		debug.Error("Failed to get rule by name %s: %v", name, err)
		return nil, err
	}

	// Set LastVerifiedAt if valid
	if lastVerifiedAt.Valid {
		r.LastVerifiedAt = lastVerifiedAt.Time
	}

	// Get tags for this rule
	tags, err := s.GetRuleTags(ctx, r.ID)
	if err != nil {
		debug.Error("Failed to get tags for rule %d: %v", r.ID, err)
		return nil, err
	}
	r.Tags = tags

	return r, nil
}
