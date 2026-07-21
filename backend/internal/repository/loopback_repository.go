package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// LoopbackRepository persists loopback sessions (GH #64) and computes each round's
// delta of newly-cracked plaintexts.
type LoopbackRepository struct {
	db *db.DB
}

// NewLoopbackRepository creates a new loopback repository.
func NewLoopbackRepository(database *db.DB) *LoopbackRepository {
	return &LoopbackRepository{db: database}
}

// terminalJobStatuses are the job_execution states that count as "finished" for the
// purpose of deciding whether a loopback round is complete.
const terminalJobStatusesSQL = "('completed', 'failed', 'cancelled')"

// CreateSessionWithJobs inserts a session and links its round-0 jobs atomically, so the
// monitor (which runs on another goroutine) never observes a session before its jobs are
// linked — which would otherwise let it prematurely complete the session as "no jobs".
func (r *LoopbackRepository) CreateSessionWithJobs(ctx context.Context, s *models.LoopbackSession, jobs []models.LoopbackSessionJob) error {
	if s.Status == "" {
		s.Status = models.LoopbackSessionStatusWaiting
	}
	if s.MaxRounds <= 0 {
		s.MaxRounds = 10
	}
	var workflowID interface{}
	if s.SourceWorkflowID != nil {
		workflowID = *s.SourceWorkflowID
	}
	var createdBy interface{}
	if s.CreatedBy != nil {
		createdBy = *s.CreatedBy
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("error starting loopback session transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	err = tx.QueryRowContext(ctx, `
		INSERT INTO loopback_sessions (hashlist_id, source_type, source_workflow_id, name, status, current_round, max_rounds, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at`,
		s.HashlistID, s.SourceType, workflowID, s.Name, s.Status, s.CurrentRound, s.MaxRounds, createdBy,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		debug.Error("Error creating loopback session: %v", err)
		return fmt.Errorf("error creating loopback session: %w", err)
	}

	for i := range jobs {
		var originJob interface{}
		if jobs[i].OriginJobID != nil {
			originJob = *jobs[i].OriginJobID
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO loopback_session_jobs (session_id, job_execution_id, round, role, is_mutatable, origin_job_id)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (session_id, job_execution_id) DO NOTHING`,
			s.ID, jobs[i].JobExecutionID, jobs[i].Round, jobs[i].Role, jobs[i].IsMutatable, originJob,
		); err != nil {
			return fmt.Errorf("error linking round-0 job to loopback session: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("error committing loopback session: %w", err)
	}
	committed = true
	return nil
}

// GetSession loads a single session by ID (without its jobs).
func (r *LoopbackRepository) GetSession(ctx context.Context, id uuid.UUID) (*models.LoopbackSession, error) {
	query := `
		SELECT id, hashlist_id, source_type, source_workflow_id, name, status, current_round, max_rounds, error_message, created_by, created_at, updated_at
		FROM loopback_sessions WHERE id = $1`
	return r.scanSession(r.db.QueryRowContext(ctx, query, id))
}

// GetActiveSessions returns all sessions still being monitored (waiting or active).
func (r *LoopbackRepository) GetActiveSessions(ctx context.Context) ([]*models.LoopbackSession, error) {
	query := `
		SELECT id, hashlist_id, source_type, source_workflow_id, name, status, current_round, max_rounds, error_message, created_by, created_at, updated_at
		FROM loopback_sessions
		WHERE status IN ('waiting', 'active')
		ORDER BY created_at`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error listing active loopback sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.LoopbackSession
	for rows.Next() {
		s, err := r.scanSessionRows(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// HasActiveSessions is the cheap idle-gate for the monitor: it avoids scanning job
// states when there is no loopback work in flight.
func (r *LoopbackRepository) HasActiveSessions(ctx context.Context) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM loopback_sessions WHERE status IN ('waiting', 'active'))`,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("error checking for active loopback sessions: %w", err)
	}
	return exists, nil
}

// ListSessionsByHashlist returns sessions for a hashlist (most recent first) for the UI.
func (r *LoopbackRepository) ListSessionsByHashlist(ctx context.Context, hashlistID int64) ([]*models.LoopbackSession, error) {
	query := `
		SELECT id, hashlist_id, source_type, source_workflow_id, name, status, current_round, max_rounds, error_message, created_by, created_at, updated_at
		FROM loopback_sessions
		WHERE hashlist_id = $1
		ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("error listing loopback sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.LoopbackSession
	for rows.Next() {
		s, err := r.scanSessionRows(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// ListSessions returns loopback sessions, optionally filtered to a single creator
// (nil = all creators, for admins), most recent first, capped at limit.
func (r *LoopbackRepository) ListSessions(ctx context.Context, createdBy *uuid.UUID, limit int) ([]*models.LoopbackSession, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, hashlist_id, source_type, source_workflow_id, name, status, current_round, max_rounds, error_message, created_by, created_at, updated_at
		FROM loopback_sessions`
	var rows *sql.Rows
	var err error
	if createdBy != nil {
		query += ` WHERE created_by = $1 ORDER BY created_at DESC LIMIT $2`
		rows, err = r.db.QueryContext(ctx, query, *createdBy, limit)
	} else {
		query += ` ORDER BY created_at DESC LIMIT $1`
		rows, err = r.db.QueryContext(ctx, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("error listing loopback sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.LoopbackSession
	for rows.Next() {
		s, err := r.scanSessionRows(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// UpdateSessionStatus updates a session's status, current round and error message.
func (r *LoopbackRepository) UpdateSessionStatus(ctx context.Context, id uuid.UUID, status models.LoopbackSessionStatus, currentRound int, errMsg *string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE loopback_sessions SET status = $2, current_round = $3, error_message = $4 WHERE id = $1`,
		id, status, currentRound, errMsg,
	)
	if err != nil {
		return fmt.Errorf("error updating loopback session %s: %w", id, err)
	}
	return nil
}

// AddSessionJob links a job_execution to a session at a specific round.
func (r *LoopbackRepository) AddSessionJob(ctx context.Context, j *models.LoopbackSessionJob) error {
	query := `
		INSERT INTO loopback_session_jobs (session_id, job_execution_id, round, role, is_mutatable, origin_job_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (session_id, job_execution_id) DO NOTHING
		RETURNING id, created_at`
	var originJob interface{}
	if j.OriginJobID != nil {
		originJob = *j.OriginJobID
	}
	err := r.db.QueryRowContext(ctx, query,
		j.SessionID, j.JobExecutionID, j.Round, j.Role, j.IsMutatable, originJob,
	).Scan(&j.ID, &j.CreatedAt)
	if err == sql.ErrNoRows {
		// Row already existed (ON CONFLICT); not an error.
		return nil
	}
	if err != nil {
		return fmt.Errorf("error adding loopback session job: %w", err)
	}
	return nil
}

// GetSessionJobs returns all jobs of a session with job name/status joined, for the UI.
func (r *LoopbackRepository) GetSessionJobs(ctx context.Context, sessionID uuid.UUID) ([]models.LoopbackSessionJob, error) {
	query := `
		SELECT lsj.id, lsj.session_id, lsj.job_execution_id, lsj.round, lsj.role, lsj.is_mutatable, lsj.origin_job_id, lsj.created_at,
		       je.name, je.status
		FROM loopback_session_jobs lsj
		JOIN job_executions je ON je.id = lsj.job_execution_id
		WHERE lsj.session_id = $1
		ORDER BY lsj.round, lsj.created_at`
	rows, err := r.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("error getting loopback session jobs: %w", err)
	}
	defer rows.Close()

	var jobs []models.LoopbackSessionJob
	for rows.Next() {
		var j models.LoopbackSessionJob
		if err := rows.Scan(&j.ID, &j.SessionID, &j.JobExecutionID, &j.Round, &j.Role, &j.IsMutatable, &j.OriginJobID, &j.CreatedAt, &j.JobName, &j.JobStatus); err != nil {
			return nil, fmt.Errorf("error scanning loopback session job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// GetMutatableOriginJobs returns the round-0 jobs whose attack is looped back against
// the delta. Their configs are the templates each round's re-runs are built from.
func (r *LoopbackRepository) GetMutatableOriginJobs(ctx context.Context, sessionID uuid.UUID) ([]models.LoopbackSessionJob, error) {
	query := `
		SELECT id, session_id, job_execution_id, round, role, is_mutatable, origin_job_id, created_at
		FROM loopback_session_jobs
		WHERE session_id = $1 AND round = 0 AND is_mutatable = true
		ORDER BY created_at`
	rows, err := r.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("error getting mutatable origin jobs: %w", err)
	}
	defer rows.Close()

	var jobs []models.LoopbackSessionJob
	for rows.Next() {
		var j models.LoopbackSessionJob
		if err := rows.Scan(&j.ID, &j.SessionID, &j.JobExecutionID, &j.Round, &j.Role, &j.IsMutatable, &j.OriginJobID, &j.CreatedAt); err != nil {
			return nil, fmt.Errorf("error scanning mutatable origin job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// CountNonTerminalRoundJobs returns how many of a round's jobs have not yet reached a
// terminal state. A round is complete when this is zero.
func (r *LoopbackRepository) CountNonTerminalRoundJobs(ctx context.Context, sessionID uuid.UUID, round int) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM loopback_session_jobs lsj
		JOIN job_executions je ON je.id = lsj.job_execution_id
		WHERE lsj.session_id = $1 AND lsj.round = $2
		  AND je.status NOT IN ` + terminalJobStatusesSQL
	var count int
	if err := r.db.QueryRowContext(ctx, query, sessionID, round).Scan(&count); err != nil {
		return 0, fmt.Errorf("error counting non-terminal round jobs: %w", err)
	}
	return count, nil
}

// CountRoundJobs returns how many jobs a round has (used to detect an empty round-0,
// which means the workflow had no jobs to monitor).
func (r *LoopbackRepository) CountRoundJobs(ctx context.Context, sessionID uuid.UUID, round int) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM loopback_session_jobs WHERE session_id = $1 AND round = $2`,
		sessionID, round,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("error counting round jobs: %w", err)
	}
	return count, nil
}

// HashlistHasUncracked reports whether the hashlist still has any uncracked hash. When it
// doesn't, there is nothing left for a further loopback round to crack, so the session can
// complete immediately instead of spawning a wasted round. Backed by the partial index
// idx_hashes_uncracked.
func (r *LoopbackRepository) HashlistHasUncracked(ctx context.Context, hashlistID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM hashlist_hashes hh
			JOIN hashes h ON h.id = hh.hash_id
			WHERE hh.hashlist_id = $1 AND h.is_cracked = false
		)`, hashlistID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("error checking for uncracked hashes: %w", err)
	}
	return exists, nil
}

// GetNewDeltaPlaintexts returns the distinct plaintexts newly cracked by this session's
// own jobs (attributed via hashes.cracked_by_task_id) on the session's hashlist that
// have NOT already been used as loopback input. This is the delta for the next round.
// Scoping to the session's tasks keeps unrelated concurrent jobs on the same hashlist
// from bleeding into the loopback.
func (r *LoopbackRepository) GetNewDeltaPlaintexts(ctx context.Context, sessionID uuid.UUID, hashlistID int64) ([]string, error) {
	query := `
		SELECT DISTINCT h.password
		FROM hashes h
		JOIN hashlist_hashes hh ON hh.hash_id = h.id
		JOIN job_tasks jt ON jt.id = h.cracked_by_task_id
		JOIN loopback_session_jobs lsj ON lsj.job_execution_id = jt.job_execution_id
		WHERE lsj.session_id = $1
		  AND hh.hashlist_id = $2
		  AND h.is_cracked = true
		  AND h.password IS NOT NULL
		  AND h.password <> ''
		  AND NOT EXISTS (
		      SELECT 1 FROM loopback_session_plaintexts lsp
		      WHERE lsp.session_id = $1 AND lsp.plaintext_md5 = md5(h.password)
		  )`
	rows, err := r.db.QueryContext(ctx, query, sessionID, hashlistID)
	if err != nil {
		return nil, fmt.Errorf("error computing loopback delta: %w", err)
	}
	defer rows.Close()

	var plaintexts []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("error scanning delta plaintext: %w", err)
		}
		plaintexts = append(plaintexts, p)
	}
	return plaintexts, rows.Err()
}

// MarkPlaintextsUsed records plaintexts as consumed loopback input for the session so a
// later round never re-runs the same candidate. Idempotent.
func (r *LoopbackRepository) MarkPlaintextsUsed(ctx context.Context, sessionID uuid.UUID, plaintexts []string) error {
	if len(plaintexts) == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO loopback_session_plaintexts (session_id, plaintext_md5)
		 SELECT $1, md5(p) FROM unnest($2::text[]) AS p
		 ON CONFLICT DO NOTHING`,
		sessionID, pq.Array(plaintexts),
	)
	if err != nil {
		return fmt.Errorf("error marking loopback plaintexts used: %w", err)
	}
	return nil
}

// scanSession scans a single session row.
func (r *LoopbackRepository) scanSession(row *sql.Row) (*models.LoopbackSession, error) {
	var s models.LoopbackSession
	err := row.Scan(&s.ID, &s.HashlistID, &s.SourceType, &s.SourceWorkflowID, &s.Name, &s.Status,
		&s.CurrentRound, &s.MaxRounds, &s.ErrorMessage, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("error scanning loopback session: %w", err)
	}
	return &s, nil
}

// scanSessionRows scans a session from a multi-row result.
func (r *LoopbackRepository) scanSessionRows(rows *sql.Rows) (*models.LoopbackSession, error) {
	var s models.LoopbackSession
	err := rows.Scan(&s.ID, &s.HashlistID, &s.SourceType, &s.SourceWorkflowID, &s.Name, &s.Status,
		&s.CurrentRound, &s.MaxRounds, &s.ErrorMessage, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("error scanning loopback session: %w", err)
	}
	return &s, nil
}
