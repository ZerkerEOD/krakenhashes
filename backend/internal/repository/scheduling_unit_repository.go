package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// SchedulingUnitRepository handles persistence for scheduling_units, the new
// "atom of scheduling" introduced in the rewrite. See migration 000146 and
// scheduler-rewrite/plan.md §4.2.
type SchedulingUnitRepository struct {
	db *db.DB
}

func NewSchedulingUnitRepository(database *db.DB) *SchedulingUnitRepository {
	return &SchedulingUnitRepository{db: database}
}

// Create inserts a new scheduling_unit. If unit.ID is uuid.Nil, a new UUID
// is generated.
func (r *SchedulingUnitRepository) Create(ctx context.Context, unit *models.SchedulingUnit) error {
	if unit.ID == uuid.Nil {
		unit.ID = uuid.New()
	}
	if unit.Status == "" {
		unit.Status = models.SchedulingUnitStatusPending
	}
	if unit.RetryBudgetRemaining == 0 {
		// The DB default is 5; only honor it if the caller didn't
		// explicitly set a different value. Zero is reserved here as
		// "use the default" because the column is NOT NULL.
		unit.RetryBudgetRemaining = 5
	}

	const query = `
		INSERT INTO scheduling_units (
			id, parent_job_id, layer_index, status, priority, max_agents,
			attack_mode, effective_keyspace, is_accurate_keyspace,
			wordlist_refs, rule_file_refs, mask_string, custom_charsets,
			retry_budget_remaining
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRowContext(ctx, query,
		unit.ID,
		unit.ParentJobID,
		unit.LayerIndex,
		unit.Status,
		unit.Priority,
		unit.MaxAgents,
		unit.AttackMode,
		unit.EffectiveKeyspace,
		unit.IsAccurateKeyspace,
		pq.Array(unit.WordlistRefs),
		pq.Array(unit.RuleFileRefs),
		unit.MaskString,
		unit.CustomCharsets,
		unit.RetryBudgetRemaining,
	).Scan(&unit.CreatedAt, &unit.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create scheduling_unit: %w", err)
	}
	return nil
}

// GetByID returns a single scheduling_unit by ID. Returns sql.ErrNoRows if
// not found.
func (r *SchedulingUnitRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.SchedulingUnit, error) {
	const query = `
		SELECT id, parent_job_id, layer_index, status, priority, max_agents,
		       attack_mode, effective_keyspace, is_accurate_keyspace,
		       wordlist_refs, rule_file_refs, mask_string, custom_charsets,
		       retry_budget_remaining, created_at, updated_at
		FROM scheduling_units
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	return scanSchedulingUnit(row)
}

// GetByParentJobID returns all scheduling_units for a parent job, ordered by
// layer_index. A non-increment job returns exactly one row; an --increment
// 1-4 job returns four.
func (r *SchedulingUnitRepository) GetByParentJobID(ctx context.Context, parentJobID uuid.UUID) ([]*models.SchedulingUnit, error) {
	const query = `
		SELECT id, parent_job_id, layer_index, status, priority, max_agents,
		       attack_mode, effective_keyspace, is_accurate_keyspace,
		       wordlist_refs, rule_file_refs, mask_string, custom_charsets,
		       retry_budget_remaining, created_at, updated_at
		FROM scheduling_units
		WHERE parent_job_id = $1
		ORDER BY layer_index ASC
	`
	rows, err := r.db.QueryContext(ctx, query, parentJobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query scheduling_units for job %s: %w", parentJobID, err)
	}
	defer rows.Close()

	var units []*models.SchedulingUnit
	for rows.Next() {
		u, err := scanSchedulingUnit(rows)
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return units, nil
}

// GetSchedulable returns scheduling_units eligible for dispatch this cycle:
// status pending or running, with an accurate keyspace, ordered by priority
// then created_at. Coverage / gap-existence checks are deferred to the
// dispatcher because they require the intervals table — keeping that logic
// out of this repo avoids cross-table coupling at the persistence layer.
func (r *SchedulingUnitRepository) GetSchedulable(ctx context.Context) ([]*models.SchedulingUnit, error) {
	const query = `
		SELECT id, parent_job_id, layer_index, status, priority, max_agents,
		       attack_mode, effective_keyspace, is_accurate_keyspace,
		       wordlist_refs, rule_file_refs, mask_string, custom_charsets,
		       retry_budget_remaining, created_at, updated_at
		FROM scheduling_units
		WHERE status IN ('pending', 'running')
		  AND is_accurate_keyspace = true
		ORDER BY priority DESC, created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query schedulable units: %w", err)
	}
	defer rows.Close()

	var units []*models.SchedulingUnit
	for rows.Next() {
		u, err := scanSchedulingUnit(rows)
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return units, nil
}

// UpdateStatus moves a scheduling_unit between lifecycle states. The CHECK
// constraint in the DB enforces the value is one of the documented statuses.
func (r *SchedulingUnitRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	const query = `UPDATE scheduling_units SET status = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to update scheduling_unit status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateEffectiveKeyspace records a refined effective_keyspace value. Used
// by the agent-progress path when hashcat's first chunk's progress[1]
// reveals the actual keyspace and we want to upgrade is_accurate_keyspace
// to true.
func (r *SchedulingUnitRepository) UpdateEffectiveKeyspace(ctx context.Context, id uuid.UUID, effective int64, isAccurate bool) error {
	if effective < 0 {
		return errors.New("effective keyspace must be non-negative")
	}
	const query = `
		UPDATE scheduling_units
		SET effective_keyspace = $1, is_accurate_keyspace = $2
		WHERE id = $3
	`
	res, err := r.db.ExecContext(ctx, query, effective, isAccurate, id)
	if err != nil {
		return fmt.Errorf("failed to update effective keyspace: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DecrementRetryBudget removes one from retry_budget_remaining and returns
// the new value. Callers use the returned value to decide whether to give
// up on the unit (per plan §8.5). Atomic to avoid races between concurrent
// recovery flows on the same unit.
func (r *SchedulingUnitRepository) DecrementRetryBudget(ctx context.Context, id uuid.UUID) (int, error) {
	const query = `
		UPDATE scheduling_units
		SET retry_budget_remaining = GREATEST(retry_budget_remaining - 1, 0)
		WHERE id = $1
		RETURNING retry_budget_remaining
	`
	var remaining int
	err := r.db.QueryRowContext(ctx, query, id).Scan(&remaining)
	if err != nil {
		return 0, fmt.Errorf("failed to decrement retry budget: %w", err)
	}
	return remaining, nil
}

// scanSchedulingUnit reads a row into a SchedulingUnit. Works against
// either *sql.Row or *sql.Rows because both implement the Scan method we
// need.
func scanSchedulingUnit(scanner rowScanner) (*models.SchedulingUnit, error) {
	u := &models.SchedulingUnit{}
	err := scanner.Scan(
		&u.ID,
		&u.ParentJobID,
		&u.LayerIndex,
		&u.Status,
		&u.Priority,
		&u.MaxAgents,
		&u.AttackMode,
		&u.EffectiveKeyspace,
		&u.IsAccurateKeyspace,
		pq.Array(&u.WordlistRefs),
		pq.Array(&u.RuleFileRefs),
		&u.MaskString,
		&u.CustomCharsets,
		&u.RetryBudgetRemaining,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to scan scheduling_unit: %w", err)
	}
	return u, nil
}

// rowScanner is the common interface satisfied by *sql.Row and *sql.Rows
// so we can share scan logic. Kept unexported and local to this package.
type rowScanner interface {
	Scan(dest ...interface{}) error
}
