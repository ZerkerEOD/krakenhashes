package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// CloudInstanceRepository owns the cloud_instances table.
type CloudInstanceRepository struct {
	db *db.DB
}

// NewCloudInstanceRepository creates a new cloud instance repository.
func NewCloudInstanceRepository(database *db.DB) *CloudInstanceRepository {
	return &CloudInstanceRepository{db: database}
}

const cloudInstanceColumns = `
	id, provider_config_id, label, idempotency_key, provider_instance_id,
	agent_id, job_execution_id, client_id, client_name_snapshot, state,
	gpu_model, gpu_count, hourly_rate_cents, disk_gb, fileset_bytes,
	reserved_cents, estimated_cost_cents, actual_cost_cents,
	launch_deadline_at, ready_deadline_at, ttl_epoch,
	launched_at, ready_at, drain_started_at, terminated_at, termination_reason,
	terminate_attempts, last_terminate_error, vpn_credential_ref,
	provider_raw, created_at, updated_at`

func scanCloudInstance(s interface{ Scan(...interface{}) error }) (*models.CloudInstance, error) {
	var c models.CloudInstance
	var providerInstanceID, clientName, terminationReason, lastTerminateErr, vpnRef sql.NullString
	var gpuModel sql.NullString
	var agentID sql.NullInt64
	var gpuCount, diskGB sql.NullInt32
	var filesetBytes, actualCost sql.NullInt64
	var jobID, clientID uuid.NullUUID

	err := s.Scan(
		&c.ID, &c.ProviderConfigID, &c.Label, &c.IdempotencyKey, &providerInstanceID,
		&agentID, &jobID, &clientID, &clientName, &c.State,
		&gpuModel, &gpuCount, &c.HourlyRateCents, &diskGB, &filesetBytes,
		&c.ReservedCents, &c.EstimatedCostCents, &actualCost,
		&c.LaunchDeadlineAt, &c.ReadyDeadlineAt, &c.TTLEpoch,
		&c.LaunchedAt, &c.ReadyAt, &c.DrainStartedAt, &c.TerminatedAt, &terminationReason,
		&c.TerminateAttempts, &lastTerminateErr, &vpnRef,
		&c.ProviderRaw, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	c.ProviderInstanceID = providerInstanceID.String
	c.ClientNameSnapshot = clientName.String
	c.TerminationReason = terminationReason.String
	c.LastTerminateError = lastTerminateErr.String
	c.VPNCredentialRef = vpnRef.String
	c.GPUModel = gpuModel.String
	c.GPUCount = int(gpuCount.Int32)
	c.DiskGB = int(diskGB.Int32)
	c.FilesetBytes = filesetBytes.Int64
	if agentID.Valid {
		v := int(agentID.Int64)
		c.AgentID = &v
	}
	if actualCost.Valid {
		c.ActualCostCents = &actualCost.Int64
	}
	if jobID.Valid {
		c.JobExecutionID = &jobID.UUID
	}
	if clientID.Valid {
		c.ClientID = &clientID.UUID
	}
	return &c, nil
}

/*
 * Create writes the instance row BEFORE the provider is called.
 *
 * This ordering is the whole recovery story. If the process dies between this
 * INSERT and the provider call, we have a row with a label and no
 * provider_instance_id, and the reaper reconciles it by label. If we wrote the
 * row after the provider responded, a lost response would leave a running,
 * billing instance that nothing in the system knows about.
 */
func (r *CloudInstanceRepository) Create(ctx context.Context, c *models.CloudInstance) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	raw := c.ProviderRaw
	if raw == nil {
		raw = models.JSONMap{}
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO cloud_instances (
			id, provider_config_id, label, idempotency_key, agent_id,
			job_execution_id, client_id, client_name_snapshot, state,
			gpu_model, gpu_count, hourly_rate_cents, disk_gb, fileset_bytes,
			reserved_cents, launch_deadline_at, ready_deadline_at, ttl_epoch,
			vpn_credential_ref, provider_raw
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		RETURNING created_at, updated_at`,
		c.ID, c.ProviderConfigID, c.Label, c.IdempotencyKey, c.AgentID,
		c.JobExecutionID, c.ClientID, nullString(c.ClientNameSnapshot), c.State,
		nullString(c.GPUModel), nullInt32(c.GPUCount), c.HourlyRateCents, nullInt32(c.DiskGB), nullInt64(c.FilesetBytes),
		c.ReservedCents, c.LaunchDeadlineAt, c.ReadyDeadlineAt, c.TTLEpoch,
		nullString(c.VPNCredentialRef), raw,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create cloud instance: %w", err)
	}
	return nil
}

// GetByID retrieves one instance.
func (r *CloudInstanceRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.CloudInstance, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+cloudInstanceColumns+` FROM cloud_instances WHERE id = $1`, id)
	c, err := scanCloudInstance(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("cloud instance %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud instance: %w", err)
	}
	return c, nil
}

// GetByLabel retrieves an instance by its provider-side label. This is the
// reconciliation path for a launch whose response was lost.
func (r *CloudInstanceRepository) GetByLabel(ctx context.Context, label string) (*models.CloudInstance, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+cloudInstanceColumns+` FROM cloud_instances WHERE label = $1`, label)
	c, err := scanCloudInstance(row)
	if err == sql.ErrNoRows {
		return nil, nil // not an error: an unknown label means an orphan
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud instance by label: %w", err)
	}
	return c, nil
}

// ListLive returns every instance that could still be costing money.
func (r *CloudInstanceRepository) ListLive(ctx context.Context) ([]*models.CloudInstance, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+cloudInstanceColumns+`
		FROM cloud_instances
		WHERE state NOT IN ('terminated','failed')
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list live cloud instances: %w", err)
	}
	defer rows.Close()

	var out []*models.CloudInstance
	for rows.Next() {
		c, err := scanCloudInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cloud instance: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListLiveForJob returns live instances provisioned for a specific job.
func (r *CloudInstanceRepository) ListLiveForJob(ctx context.Context, jobID uuid.UUID) ([]*models.CloudInstance, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+cloudInstanceColumns+`
		FROM cloud_instances
		WHERE job_execution_id = $1 AND state NOT IN ('terminated','failed')
		ORDER BY created_at ASC`, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to list cloud instances for job: %w", err)
	}
	defer rows.Close()

	var out []*models.CloudInstance
	for rows.Next() {
		c, err := scanCloudInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cloud instance: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountLive returns how many instances are live, for the global concurrency cap.
func (r *CloudInstanceRepository) CountLive(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cloud_instances WHERE state NOT IN ('terminated','failed')`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("failed to count live cloud instances: %w", err)
	}
	return n, nil
}

/*
 * CountLiveForConfig counts live instances belonging to one provider config.
 *
 * Backs cloud_provider_configs.max_concurrent_instances, which was stored,
 * validated by the admin handler, round-tripped through the API and enforced by
 * nothing at all — an operator who capped a provider at 3 could get any number.
 *
 * Per CONFIG rather than per provider kind: two configs of the same kind are
 * usually two accounts or two regions, and a cap is a statement about the one
 * account it was set on.
 */
func (r *CloudInstanceRepository) CountLiveForConfig(ctx context.Context, configID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cloud_instances
		WHERE provider_config_id = $1 AND state NOT IN ('terminated','failed')`, configID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("failed to count live instances for provider config %s: %w", configID, err)
	}
	return n, nil
}

// SetState transitions an instance, optionally recording why.
func (r *CloudInstanceRepository) SetState(ctx context.Context, id uuid.UUID, state models.CloudInstanceState, reason string) error {
	// $2 must be cast explicitly at BOTH use sites. Without the casts Postgres
	// tries to deduce one type for a parameter used as a VARCHAR assignment
	// target and as an IN operand, and rejects the statement with
	// "inconsistent types deduced for parameter $2" — which the reaper logged
	// and continued past, leaving every instance stuck in its pre-teardown
	// state while being re-destroyed on every sweep.
	_, err := r.db.ExecContext(ctx, `
		UPDATE cloud_instances
		SET state = $2::varchar,
		    termination_reason = COALESCE(NULLIF($3::text, ''), termination_reason),
		    terminated_at = CASE WHEN $2::varchar IN ('terminated','failed')
		                         THEN NOW() ELSE terminated_at END,
		    updated_at = NOW()
		WHERE id = $1`, id, string(state), reason)
	if err != nil {
		return fmt.Errorf("failed to set cloud instance state: %w", err)
	}
	return nil
}

// MarkLaunched records the provider's identifier and the TTL clock.
func (r *CloudInstanceRepository) MarkLaunched(ctx context.Context, id uuid.UUID, providerInstanceID string, launchedAt time.Time, ttlEpoch time.Time, raw models.JSONMap) error {
	if raw == nil {
		raw = models.JSONMap{}
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE cloud_instances
		SET provider_instance_id = $2, state = 'provisioning',
		    launched_at = $3, ttl_epoch = $4, provider_raw = $5, updated_at = NOW()
		WHERE id = $1`, id, providerInstanceID, launchedAt, ttlEpoch, raw)
	if err != nil {
		return fmt.Errorf("failed to mark cloud instance launched: %w", err)
	}
	return nil
}

/*
 * NOTE: there is deliberately no AttachAgent method here.
 *
 * Linking an agent to its instance is not a standalone operation. Both
 * directions of the link — agents.cloud_instance_id and
 * cloud_instances.agent_id — have to become visible at the same moment, and
 * the forward one has to be part of the registration INSERT rather than a
 * follow-up write. Between an INSERT and a later UPDATE the row is a
 * fully-registered on-prem agent, and the scheduler's next cycle (3 seconds)
 * could hand it any client's job.
 *
 * Both writes therefore live in one transaction in
 * AgentRepository.Create. A method here would only be useful for doing it the
 * unsafe way.
 */

/*
 * InstanceWorkStatus answers "does this rented machine still have anything to
 * do?", which is the question no other teardown tier asks.
 *
 * Every other tier fires on something being wrong. This one fires on the
 * ordinary happy ending — a job that finished early on an instance rented for
 * hours longer.
 */
type InstanceWorkStatus struct {
	// JobFinished is true once the parent job reaches a terminal state. There
	// is then nothing that could ever need this instance again.
	JobFinished bool
	// JobExists is false when the job row is gone, which is also a reason to
	// stop paying for the machine that was serving it.
	JobExists bool
	// LastActivityAt is the most recent moment this instance's agent had a task
	// assigned, started, reported progress on, or completed. Invalid when it has
	// never had one — a distinct case from "idle since X", because an instance
	// that has never worked is measured from when it became ready.
	LastActivityAt sql.NullTime
	/*
	 * InFlightActivityAt is the last moment any of this agent's NON-TERMINAL
	 * tasks was written to. It is the freshness half of InFlight, and the two
	 * are deliberately returned together.
	 *
	 * InFlight alone is not safe to gate a teardown on. "The agent owns a
	 * non-terminal row" and "the agent is doing something" are different facts,
	 * and only the second is worth billing for: a wedged 'processing' row, or a
	 * stale row belonging to a DIFFERENT job on the same agent (this aggregate
	 * is agent-scoped on purpose — see WorkStatus), would otherwise pin InFlight
	 * true and suppress teardown until TTL.
	 *
	 * updated_at rather than last_activity_at is what makes this work during the
	 * crack-drain tail: once hashcat exits there are no more progress messages,
	 * so last_activity_at freezes, but every crack batch still bumps updated_at
	 * through IncrementReceivedCrackCount. It is the same signal
	 * checkForStaleProcessingTasks already trusts.
	 */
	InFlightActivityAt sql.NullTime
	// InFlight is true when this instance's agent holds a task that has not
	// reached a terminal state.
	//
	// This is what the drain rung waits on, and it is deliberately a boolean
	// rather than a reuse of LastActivityAt. The drain promise is "finish what
	// is running, then go"; measuring it with the idle-drain grace would make a
	// budget-drained instance sit for another five minutes AT >=99% OF CAP
	// after its last chunk ended.
	InFlight bool
}

/*
 * WorkStatus reports whether an instance's job still needs it.
 *
 * Activity is measured from job_tasks rather than from scheduler state on
 * purpose: this must not reimplement "is there dispatchable keyspace", which
 * would drift from the allocator and start destroying instances the scheduler
 * was about to use. Whether a task was recently handed to THIS agent is a fact,
 * and a stale one only ever delays teardown.
 *
 * THE TASK AGGREGATES ARE AGENT-SCOPED, NOT JOB-SCOPED, AND MUST STAY THAT WAY.
 * Scoping them to jobID looks like a tightening and is a regression:
 *
 *   - What dies with a destroyed VM is the agent's crack buffer and its local
 *     outfile. Which job owns the task is irrelevant to whether destroying the
 *     disk loses data.
 *   - Retarget (below) repoints an instance at a different job of the same
 *     client while the previous job's task may still be 'processing' on that
 *     same agent. A job-scoped query would report InFlight=false and destroy
 *     mid-handshake — on the one code path that makes it likely.
 *   - After a retarget there is no history for the new job, so a job-scoped
 *     LastActivityAt would be NULL, dropping a healthy instance into the
 *     reaper's commissioning branch to be measured from ready_at (hours old)
 *     and destroyed immediately.
 *
 * The risk agent-scoping carries — a stale foreign row pinning InFlight — is
 * neutralised by InFlightActivityAt rather than by narrowing the query: a row
 * that stopped being written stops suppressing teardown.
 */
func (r *CloudInstanceRepository) WorkStatus(ctx context.Context, jobID uuid.UUID, agentID *int) (*InstanceWorkStatus, error) {
	out := &InstanceWorkStatus{}

	var status string
	err := r.db.QueryRowContext(ctx,
		`SELECT status FROM job_executions WHERE id = $1`, jobID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil // JobExists stays false
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read job status for cloud instance: %w", err)
	}
	out.JobExists = true
	switch status {
	case "completed", "failed", "cancelled":
		out.JobFinished = true
	}

	if agentID == nil {
		return out, nil
	}
	/*
	 * GREATEST over the lifecycle stamps: a task can be assigned and then sit
	 * for a while before it starts, and either is proof the instance is in use.
	 * COALESCE to assigned_at so a NULL stamp cannot null the whole expression
	 * and make a busy agent look idle.
	 *
	 * last_activity_at is in this list because WITHOUT IT THIS EXPRESSION DOES
	 * NOT MOVE DURING A CHUNK. completed_at is NULL while a task runs, so the
	 * whole GREATEST collapsed to started_at — and cloud_chunk_duration_seconds
	 * is a FLOOR of 3600s for rented agents (scheduler/dispatcher.go) while
	 * cloud_idle_drain_minutes defaults to 5. The idle rung therefore destroyed
	 * every rented instance exactly five minutes into its first chunk, whatever
	 * the agent was doing. Observed twice: once on real AWS hardware
	 * ("no work for 5m41s (idle drain)") and once on the mock provider.
	 *
	 * last_activity_at is written ONLY by IngestProgressV2
	 * (scheduler/progress.go), reached unconditionally from HandleJobProgress
	 * before every early return, so it means precisely "the agent last told us
	 * something about this task" — and nothing but an agent message moves it.
	 * That is the semantics this field always claimed to have.
	 *
	 * updated_at is deliberately NOT in this list. It is trigger-driven
	 * (update_job_tasks_updated_at, migration 000026) and fires on any write,
	 * including backend bookkeeping, so folding it in would soften the idle
	 * signal for a genuinely dead agent. It is used only inside the in-flight
	 * FILTER below, where a write IS the evidence wanted.
	 *
	 * Postgres GREATEST ignores NULL operands, so a legacy row with no
	 * last_activity_at cannot null the expression.
	 *
	 * InFlight and its freshness stamp ride along in the same round trip.
	 * 'processing' counts as in flight on purpose: it means the agent is still
	 * uploading crack batches (models/jobs.go), and destroying the VM there
	 * loses cracks it has already found — permanently, because RetransmitOutfile
	 * reads a file on the disk being destroyed and applyRecovery books the range
	 * as covered so it is never re-run. 'reconnect_pending' is excluded: the
	 * agent is not connected, so there is no work to preserve.
	 *
	 * THE TWO STATUS LISTS BELOW MUST STAY IDENTICAL. TestWorkStatus_InFlight
	 * asserts both columns from one table so they cannot drift.
	 *
	 * All three aggregates are total, so an agent with no rows at all yields
	 * (NULL, false, NULL) rather than no row, and the ErrNoRows handling below
	 * stays correct.
	 */
	err = r.db.QueryRowContext(ctx, `
		SELECT MAX(GREATEST(
			COALESCE(completed_at,     assigned_at),
			COALESCE(started_at,       assigned_at),
			COALESCE(last_activity_at, assigned_at),
			assigned_at)),
		       COUNT(*) FILTER (WHERE status IN ('assigned','running','processing')) > 0,
		       MAX(GREATEST(
			COALESCE(last_activity_at, assigned_at),
			COALESCE(updated_at,       assigned_at),
			assigned_at))
		         FILTER (WHERE status IN ('assigned','running','processing'))
		FROM job_tasks
		WHERE agent_id = $1`, *agentID).
		Scan(&out.LastActivityAt, &out.InFlight, &out.InFlightActivityAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to read task activity for agent %d: %w", *agentID, err)
	}
	return out, nil
}

/*
 * BeginDrain puts an instance on the drain rung and starts its clock.
 *
 * COALESCE rather than an unconditional NOW(): the reaper re-evaluates the
 * ladder every sweep, so an instance whose client stays over the threshold
 * would restart its own timeout every 60 seconds and never expire.
 *
 * The state guard stops a sweep that raced a teardown from pulling a row back
 * out of 'terminating' and making a dying instance look live again.
 */
func (r *CloudInstanceRepository) BeginDrain(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cloud_instances
		SET state = 'draining',
		    drain_started_at = COALESCE(drain_started_at, NOW()),
		    termination_reason = COALESCE(NULLIF($2::text, ''), termination_reason),
		    updated_at = NOW()
		WHERE id = $1 AND state NOT IN ('terminating','terminated','failed')`, id, reason)
	if err != nil {
		return fmt.Errorf("failed to begin drain: %w", err)
	}
	return nil
}

/*
 * EndDrain returns a drained instance to service.
 *
 * Reached when spend falls back below drain_pct — a raised cap, a new budget
 * period, or a released reservation. Without it a transient spike would exclude
 * the instance from dispatch until the timeout killed it, which is a rental
 * paid for and then thrown away.
 *
 * Resumes to 'running' even if the instance was 'syncing' when it drained.
 * Deliberate: file-sync progress lives on agents.sync_status, and nothing reads
 * cloud_instances.state to decide sync behaviour, so storing and restoring a
 * pre-drain state would be more machinery than the fact is worth.
 */
func (r *CloudInstanceRepository) EndDrain(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cloud_instances
		SET state = 'running',
		    drain_started_at = NULL,
		    termination_reason = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND state = 'draining'`, id)
	if err != nil {
		return fmt.Errorf("failed to end drain: %w", err)
	}
	return nil
}

/*
 * RetireAgent marks a cloud agent's row dead alongside its instance.
 *
 * agents.retired_at was created with the scheduler already filtering on it
 * (getIdleAgents) and nothing ever writing it. Until now the only thing keeping
 * a terminated instance's agent out of the idle pool was the WebSocket
 * dropping — so between provider.Destroy returning and the socket closing, the
 * scheduler could hand a chunk to an agent on a machine that no longer exists.
 *
 * Soft retirement rather than DELETE: deleting the row NULLs job_tasks.agent_id
 * and destroys cost attribution, which is the whole reason this column exists.
 *
 * The cloud_instance_id guard is a safety rail. This must never retire an
 * on-prem agent, whatever a corrupted cloud_instances.agent_id might point at.
 */
func (r *CloudInstanceRepository) RetireAgent(ctx context.Context, agentID int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE agents
		SET retired_at = COALESCE(retired_at, NOW()), updated_at = NOW()
		WHERE id = $1 AND cloud_instance_id IS NOT NULL`, agentID)
	if err != nil {
		return fmt.Errorf("failed to retire agent %d: %w", agentID, err)
	}
	return nil
}

// AddIncurredCost advances the running cost estimate.
func (r *CloudInstanceRepository) AddIncurredCost(ctx context.Context, id uuid.UUID, deltaCents int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cloud_instances
		SET estimated_cost_cents = estimated_cost_cents + $2, updated_at = NOW()
		WHERE id = $1`, id, deltaCents)
	if err != nil {
		return fmt.Errorf("failed to add incurred cost: %w", err)
	}
	return nil
}

// RecordTerminateFailure counts a failed teardown. Past a threshold this
// escalates to admins: an instance we cannot kill is money actively burning.
func (r *CloudInstanceRepository) RecordTerminateFailure(ctx context.Context, id uuid.UUID, errMsg string) (int, error) {
	var attempts int
	err := r.db.QueryRowContext(ctx, `
		UPDATE cloud_instances
		SET terminate_attempts = terminate_attempts + 1,
		    last_terminate_error = $2,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING terminate_attempts`, id, errMsg).Scan(&attempts)
	if err != nil {
		return 0, fmt.Errorf("failed to record terminate failure: %w", err)
	}
	return attempts, nil
}

// ExtendTTL pushes an instance's deadline out, for the case where a modest
// extension completes the job and avoids paying a whole fresh boot elsewhere.
func (r *CloudInstanceRepository) ExtendTTL(ctx context.Context, id uuid.UUID, newTTL time.Time, extraReservedCents int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cloud_instances
		SET ttl_epoch = $2, reserved_cents = reserved_cents + $3, updated_at = NOW()
		WHERE id = $1`, id, newTTL, extraReservedCents)
	if err != nil {
		return fmt.Errorf("failed to extend cloud instance TTL: %w", err)
	}
	return nil
}

// Retarget points a still-useful instance at a different job of the same
// client, so its already-paid-for boot and file sync are not wasted.
func (r *CloudInstanceRepository) Retarget(ctx context.Context, id uuid.UUID, jobID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cloud_instances SET job_execution_id = $2, updated_at = NOW() WHERE id = $1`, id, jobID)
	if err != nil {
		return fmt.Errorf("failed to retarget cloud instance: %w", err)
	}
	return nil
}

/*
 * LoadAgentJobLocks returns agent_id -> job_execution_id for every cloud agent
 * whose instance is still live.
 *
 * The scheduler calls this once per cycle to build its dispatch-isolation
 * predicate. It deliberately covers ALL cloud agents, not just idle ones,
 * because preemption asks whether a BUSY agent is compatible with a starving
 * unit — and a paid instance must never be preempted away from the job that
 * bought it.
 */
func (r *CloudInstanceRepository) LoadAgentJobLocks(ctx context.Context) (map[int]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT agent_id, job_execution_id
		FROM cloud_instances
		WHERE agent_id IS NOT NULL
		  AND job_execution_id IS NOT NULL
		  AND state NOT IN ('terminated','failed')`)
	if err != nil {
		return nil, fmt.Errorf("failed to load cloud agent job locks: %w", err)
	}
	defer rows.Close()

	locks := make(map[int]uuid.UUID)
	for rows.Next() {
		var agentID int
		var jobID uuid.UUID
		if err := rows.Scan(&agentID, &jobID); err != nil {
			return nil, fmt.Errorf("failed to scan cloud agent job lock: %w", err)
		}
		locks[agentID] = jobID
	}
	return locks, rows.Err()
}

// nullString is defined in sso_repository.go (same package).

func nullInt32(i int) sql.NullInt32 {
	return sql.NullInt32{Int32: int32(i), Valid: i != 0}
}

func nullInt64(i int64) sql.NullInt64 {
	return sql.NullInt64{Int64: i, Valid: i != 0}
}
