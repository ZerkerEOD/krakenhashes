package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// PresetJobRepository defines the interface for interacting with preset_jobs.
type PresetJobRepository interface {
	Create(ctx context.Context, params models.PresetJob) (*models.PresetJob, error)
	GetByID(ctx context.Context, id uuid.UUID) (*models.PresetJob, error)
	GetByName(ctx context.Context, name string) (*models.PresetJob, error)
	List(ctx context.Context) ([]models.PresetJob, error)
	Update(ctx context.Context, id uuid.UUID, params models.PresetJob) (*models.PresetJob, error)
	Delete(ctx context.Context, id uuid.UUID) error
	ListFormData(ctx context.Context) (*PresetJobFormData, error)
}

// PresetJobFormData holds lists needed for preset job forms.
type PresetJobFormData struct {
	Wordlists      []models.WordlistBasic      `json:"wordlists"`
	Rules          []models.RuleBasic          `json:"rules"`
	BinaryVersions []models.BinaryVersionBasic `json:"binary_versions"`
}

// presetJobRepository implements PresetJobRepository.
type presetJobRepository struct {
	db *sql.DB
}

// NewPresetJobRepository creates a new repository for preset jobs.
func NewPresetJobRepository(db *sql.DB) PresetJobRepository {
	return &presetJobRepository{db: db}
}

// Create inserts a new preset job into the database.
func (r *presetJobRepository) Create(ctx context.Context, params models.PresetJob) (*models.PresetJob, error) {
	query := `
		INSERT INTO preset_jobs (
			name, wordlist_ids, rule_ids, attack_mode, priority, 
			chunk_size_seconds, status_updates_enabled, is_small_job, 
			allow_high_priority_override, binary_version_id, mask
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, name, wordlist_ids, rule_ids, attack_mode, priority, chunk_size_seconds, 
				  status_updates_enabled, is_small_job, allow_high_priority_override, 
				  binary_version_id, mask, created_at, updated_at`

	row := r.db.QueryRowContext(ctx, query,
		params.Name, params.WordlistIDs, params.RuleIDs, params.AttackMode, params.Priority,
		params.ChunkSizeSeconds, params.StatusUpdatesEnabled, params.IsSmallJob,
		params.AllowHighPriorityOverride, params.BinaryVersionID, params.Mask,
	)

	var created models.PresetJob
	err := row.Scan(
		&created.ID, &created.Name, &created.WordlistIDs, &created.RuleIDs, &created.AttackMode, &created.Priority,
		&created.ChunkSizeSeconds, &created.StatusUpdatesEnabled, &created.IsSmallJob,
		&created.AllowHighPriorityOverride, &created.BinaryVersionID, &created.Mask, &created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		debug.Error("Error creating preset job: %v", err)
		return nil, fmt.Errorf("error creating preset job: %w", err)
	}
	return &created, nil
}

// GetByID retrieves a preset job by its UUID.
func (r *presetJobRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.PresetJob, error) {
	query := `
		SELECT 
			id, name, wordlist_ids, rule_ids, attack_mode, priority, chunk_size_seconds, 
			status_updates_enabled, is_small_job, allow_high_priority_override, 
			binary_version_id, mask, created_at, updated_at 
		FROM preset_jobs WHERE id = $1 LIMIT 1`

	row := r.db.QueryRowContext(ctx, query, id)
	var job models.PresetJob
	err := row.Scan(
		&job.ID, &job.Name, &job.WordlistIDs, &job.RuleIDs, &job.AttackMode, &job.Priority,
		&job.ChunkSizeSeconds, &job.StatusUpdatesEnabled, &job.IsSmallJob,
		&job.AllowHighPriorityOverride, &job.BinaryVersionID, &job.Mask, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("preset job not found: %w", ErrNotFound)
		}
		debug.Error("Error getting preset job by ID %s: %v", id, err)
		return nil, fmt.Errorf("error getting preset job by ID: %w", err)
	}
	return &job, nil
}

// GetByName retrieves a preset job by its name.
func (r *presetJobRepository) GetByName(ctx context.Context, name string) (*models.PresetJob, error) {
	query := `
		SELECT 
			id, name, wordlist_ids, rule_ids, attack_mode, priority, chunk_size_seconds, 
			status_updates_enabled, is_small_job, allow_high_priority_override, 
			binary_version_id, mask, created_at, updated_at 
		FROM preset_jobs WHERE name = $1 LIMIT 1`

	row := r.db.QueryRowContext(ctx, query, name)
	var job models.PresetJob
	err := row.Scan(
		&job.ID, &job.Name, &job.WordlistIDs, &job.RuleIDs, &job.AttackMode, &job.Priority,
		&job.ChunkSizeSeconds, &job.StatusUpdatesEnabled, &job.IsSmallJob,
		&job.AllowHighPriorityOverride, &job.BinaryVersionID, &job.Mask, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("preset job not found: %w", ErrNotFound)
		}
		debug.Error("Error getting preset job by name %s: %v", name, err)
		return nil, fmt.Errorf("error getting preset job by name: %w", err)
	}
	return &job, nil
}

// List retrieves all preset jobs, potentially joining with binary_versions.
func (r *presetJobRepository) List(ctx context.Context) ([]models.PresetJob, error) {
	query := `
		SELECT 
			pj.id, pj.name, pj.wordlist_ids, pj.rule_ids, pj.attack_mode, pj.priority, 
			pj.chunk_size_seconds, pj.status_updates_enabled, pj.is_small_job, 
			pj.allow_high_priority_override, pj.binary_version_id, pj.mask, pj.created_at, pj.updated_at,
			bv.file_name as binary_version_name
		FROM preset_jobs pj
		LEFT JOIN binary_versions bv ON pj.binary_version_id = bv.id
		ORDER BY pj.name` // TODO: Add pagination/sorting

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		debug.Error("Error listing preset jobs: %v", err)
		return nil, fmt.Errorf("error listing preset jobs: %w", err)
	}
	defer rows.Close()

	jobs := []models.PresetJob{}
	for rows.Next() {
		var job models.PresetJob
		var binaryVersionName sql.NullString
		if err := rows.Scan(
			&job.ID, &job.Name, &job.WordlistIDs, &job.RuleIDs, &job.AttackMode, &job.Priority,
			&job.ChunkSizeSeconds, &job.StatusUpdatesEnabled, &job.IsSmallJob,
			&job.AllowHighPriorityOverride, &job.BinaryVersionID, &job.Mask, &job.CreatedAt, &job.UpdatedAt,
			&binaryVersionName,
		); err != nil {
			debug.Error("Error scanning preset job row: %v", err)
			return nil, fmt.Errorf("error scanning preset job row: %w", err)
		}
		if binaryVersionName.Valid {
			job.BinaryVersionName = binaryVersionName.String
		}
		jobs = append(jobs, job)
	}

	if err = rows.Err(); err != nil {
		debug.Error("Error iterating preset job rows: %v", err)
		return nil, fmt.Errorf("error iterating preset job rows: %w", err)
	}

	return jobs, nil
}

// Update modifies an existing preset job.
func (r *presetJobRepository) Update(ctx context.Context, id uuid.UUID, params models.PresetJob) (*models.PresetJob, error) {
	query := `
		UPDATE preset_jobs
		SET 
			name = $2,
			wordlist_ids = $3,
			rule_ids = $4,
			attack_mode = $5,
			priority = $6,
			chunk_size_seconds = $7,
			status_updates_enabled = $8,
			is_small_job = $9,
			allow_high_priority_override = $10,
			binary_version_id = $11,
			mask = $12,
			updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, wordlist_ids, rule_ids, attack_mode, priority, chunk_size_seconds, 
				  status_updates_enabled, is_small_job, allow_high_priority_override, 
				  binary_version_id, mask, created_at, updated_at`

	row := r.db.QueryRowContext(ctx, query,
		id, params.Name, params.WordlistIDs, params.RuleIDs, params.AttackMode, params.Priority,
		params.ChunkSizeSeconds, params.StatusUpdatesEnabled, params.IsSmallJob,
		params.AllowHighPriorityOverride, params.BinaryVersionID, params.Mask,
	)

	var updated models.PresetJob
	err := row.Scan(
		&updated.ID, &updated.Name, &updated.WordlistIDs, &updated.RuleIDs, &updated.AttackMode, &updated.Priority,
		&updated.ChunkSizeSeconds, &updated.StatusUpdatesEnabled, &updated.IsSmallJob,
		&updated.AllowHighPriorityOverride, &updated.BinaryVersionID, &updated.Mask, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("preset job not found for update: %w", ErrNotFound)
		}
		debug.Error("Error updating preset job %s: %v", id, err)
		return nil, fmt.Errorf("error updating preset job: %w", err)
	}
	return &updated, nil
}

// Delete removes a preset job from the database.
func (r *presetJobRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM preset_jobs WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		debug.Error("Error deleting preset job %s: %v", id, err)
		return fmt.Errorf("error deleting preset job: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		debug.Warning("Could not get rows affected after deleting preset job %s: %v", id, err)
	} else if rowsAffected == 0 {
		return fmt.Errorf("preset job not found for deletion: %w", ErrNotFound)
	}
	return nil
}

// ListFormData retrieves necessary lists for creating/editing preset jobs.
func (r *presetJobRepository) ListFormData(ctx context.Context) (*PresetJobFormData, error) {
	formData := &PresetJobFormData{}
	var err error
	var rows *sql.Rows

	// Fetch Wordlists
	wordlistQuery := `SELECT id, name FROM wordlists ORDER BY name`
	rows, err = r.db.QueryContext(ctx, wordlistQuery)
	if err != nil {
		debug.Error("Error fetching wordlists for form data: %v", err)
		return nil, fmt.Errorf("error fetching wordlists: %w", err)
	}
	for rows.Next() {
		var w models.WordlistBasic
		if scanErr := rows.Scan(&w.ID, &w.Name); scanErr != nil {
			rows.Close()
			debug.Error("Error scanning wordlist row: %v", scanErr)
			return nil, fmt.Errorf("error scanning wordlist: %w", scanErr)
		}
		formData.Wordlists = append(formData.Wordlists, w)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		debug.Error("Error iterating wordlist rows: %v", err)
		return nil, fmt.Errorf("error iterating wordlists: %w", err)
	}
	rows.Close()

	// Fetch Rules
	ruleQuery := `SELECT id, name FROM rules ORDER BY name`
	rows, err = r.db.QueryContext(ctx, ruleQuery)
	if err != nil {
		debug.Error("Error fetching rules for form data: %v", err)
		return nil, fmt.Errorf("error fetching rules: %w", err)
	}
	for rows.Next() {
		var rule models.RuleBasic
		if scanErr := rows.Scan(&rule.ID, &rule.Name); scanErr != nil {
			rows.Close()
			debug.Error("Error scanning rule row: %v", scanErr)
			return nil, fmt.Errorf("error scanning rule: %w", scanErr)
		}
		formData.Rules = append(formData.Rules, rule)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		debug.Error("Error iterating rule rows: %v", err)
		return nil, fmt.Errorf("error iterating rules: %w", err)
	}
	rows.Close()

	// Fetch Binary Versions
	binaryQuery := `SELECT id, file_name as name FROM binary_versions WHERE is_active = true AND verification_status = 'verified' ORDER BY file_name`
	rows, err = r.db.QueryContext(ctx, binaryQuery)
	if err != nil {
		debug.Error("Error fetching binary versions for form data: %v", err)
		return nil, fmt.Errorf("error fetching binary versions: %w", err)
	}
	for rows.Next() {
		var bv models.BinaryVersionBasic
		if scanErr := rows.Scan(&bv.ID, &bv.Name); scanErr != nil {
			rows.Close()
			debug.Error("Error scanning binary version row: %v", scanErr)
			return nil, fmt.Errorf("error scanning binary version: %w", scanErr)
		}
		formData.BinaryVersions = append(formData.BinaryVersions, bv)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		debug.Error("Error iterating binary version rows: %v", err)
		return nil, fmt.Errorf("error iterating binary versions: %w", err)
	}
	rows.Close()

	return formData, nil
}
