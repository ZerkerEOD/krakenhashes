package repository

import (
	"context"
	"database/sql"
	"encoding/json"
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

	// priority and max_agents are read live from job_executions in
	// scheduler/cycle.go buildUnitInfos — they are NOT denormalized
	// here. Migration 000153 dropped the columns. See plan
	// drop-denormalized-priority-max-agents.
	const query = `
		INSERT INTO scheduling_units (
			id, parent_job_id, layer_index, status,
			attack_mode, effective_keyspace, base_keyspace, is_accurate_keyspace,
			wordlist_refs, rule_file_refs, mask_string, custom_charsets,
			retry_budget_remaining
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at
	`

	// json.RawMessage's zero value is []byte(nil), which lib/pq tries
	// to serialize as JSON and rejects ("invalid input syntax for
	// type json"). When the unit carries no custom charsets, pass a
	// real nil interface{} so the driver writes SQL NULL.
	var customCharsetsArg interface{}
	if len(unit.CustomCharsets) > 0 {
		customCharsetsArg = []byte(unit.CustomCharsets)
	}

	err := r.db.QueryRowContext(ctx, query,
		unit.ID,
		unit.ParentJobID,
		unit.LayerIndex,
		unit.Status,
		unit.AttackMode,
		unit.EffectiveKeyspace,
		unit.BaseKeyspace, // nullable *int64; nil → SQL NULL
		unit.IsAccurateKeyspace,
		pq.Array(unit.WordlistRefs),
		pq.Array(unit.RuleFileRefs),
		unit.MaskString,
		customCharsetsArg,
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
		SELECT id, parent_job_id, layer_index, status,
		       attack_mode, effective_keyspace, base_keyspace, is_accurate_keyspace,
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
		SELECT id, parent_job_id, layer_index, status,
		       attack_mode, effective_keyspace, base_keyspace, is_accurate_keyspace,
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
// status pending or running, with an accurate keyspace, parent job_execution
// also in a non-terminal state, ordered by priority then created_at. Coverage
// / gap-existence checks are deferred to the dispatcher because they require
// the intervals table — keeping that logic out of this repo avoids cross-table
// coupling at the persistence layer.
//
// The parent-job-status JOIN is the primary stop-the-runaway fix: without it,
// a unit whose parent job has been marked completed (e.g., by
// HandleHashlistFullyCracked when all hashes crack) stays selectable because
// its own status is still 'pending'. The cycle then dispatches against an
// empty hashlist forever, every task failing with exit 255.
func (r *SchedulingUnitRepository) GetSchedulable(ctx context.Context) ([]*models.SchedulingUnit, error) {
	// Ordering by je.priority (not the dropped su.priority column).
	// The allocator re-sorts by priority anyway (allocator.go:170-171)
	// so the ORDER BY here is only for caller convenience and stable
	// test output. If profiling shows the join+sort is slow at scale,
	// materialize a partial index on (created_at) WHERE su.status IN
	// ('pending','running') AND su.is_accurate_keyspace = true.
	const query = `
		SELECT su.id, su.parent_job_id, su.layer_index, su.status,
		       su.attack_mode, su.effective_keyspace, su.base_keyspace, su.is_accurate_keyspace,
		       su.wordlist_refs, su.rule_file_refs, su.mask_string, su.custom_charsets,
		       su.retry_budget_remaining, su.created_at, su.updated_at
		FROM scheduling_units su
		JOIN job_executions je ON je.id = su.parent_job_id
		WHERE su.status IN ('pending', 'running')
		  AND su.is_accurate_keyspace = true
		  AND je.status IN ('pending', 'running')
		ORDER BY je.priority DESC, su.created_at ASC
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
//
// custom_charsets is scanned into a nullable []byte then assigned to
// json.RawMessage. Scanning directly into *json.RawMessage doesn't
// work because the type doesn't implement sql.Scanner; lib/pq raises
// "unsupported Scan, storing driver.Value type <nil> into type
// *json.RawMessage" when the column is NULL.
func scanSchedulingUnit(scanner rowScanner) (*models.SchedulingUnit, error) {
	u := &models.SchedulingUnit{}
	var customCharsetsBytes []byte
	err := scanner.Scan(
		&u.ID,
		&u.ParentJobID,
		&u.LayerIndex,
		&u.Status,
		&u.AttackMode,
		&u.EffectiveKeyspace,
		&u.BaseKeyspace, // nullable *int64
		&u.IsAccurateKeyspace,
		pq.Array(&u.WordlistRefs),
		pq.Array(&u.RuleFileRefs),
		&u.MaskString,
		&customCharsetsBytes,
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
	if len(customCharsetsBytes) > 0 {
		u.CustomCharsets = json.RawMessage(customCharsetsBytes)
	}
	return u, nil
}

// rowScanner is the common interface satisfied by *sql.Row and *sql.Rows
// so we can share scan logic. Kept unexported and local to this package.
type rowScanner interface {
	Scan(dest ...interface{}) error
}
