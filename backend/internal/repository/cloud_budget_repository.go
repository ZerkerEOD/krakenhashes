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

// ErrNoBudget means the client has no funded cloud budget, so no provisioning
// is permitted at all. Distinct from ErrInsufficientBudget, which means there
// is a budget but not enough headroom right now.
var ErrNoBudget = errors.New("client has no cloud budget configured")

// ErrInsufficientBudget means the requested reservation would exceed the cap.
var ErrInsufficientBudget = errors.New("insufficient cloud budget for reservation")

/*
 * CloudBudgetRepository owns the spend ledger and the reservation transaction.
 *
 * LEDGER SEMANTICS
 *
 * The ledger tracks one authoritative number, "committed spend":
 *
 *     committed = SUM(reservation) + SUM(release) + SUM(reconciliation)
 *     available = cap - committed
 *
 *   reservation    +R, written at launch for the instance's whole TTL. This is
 *                  what makes the cap real: the money is spoken for before the
 *                  instance boots, not measured after it has overspent.
 *   release        negative, written at teardown to give back the unused
 *                  remainder of a reservation.
 *   reconciliation signed correction when the provider's own billing disagrees
 *                  with our estimate (AWS Cost Explorer lags ~24h).
 *   incurred       positive, how much of the committed envelope has actually
 *                  been consumed so far.
 *
 * `incurred` deliberately does NOT reduce availability: the reservation that
 * covers it already did. Subtracting both — as "cap - incurred - reservations"
 * suggests — would double-count every running instance and refuse launches
 * while the budget was in fact half free.
 */
type CloudBudgetRepository struct {
	db *db.DB
}

// NewCloudBudgetRepository creates a new cloud budget repository.
func NewCloudBudgetRepository(database *db.DB) *CloudBudgetRepository {
	return &CloudBudgetRepository{db: database}
}

// budgetWindow restricts the ledger to the current billing period. Computed on
// read rather than rolled over by a scheduled job, because there is no cron
// infrastructure in this codebase to own a period boundary.
const budgetWindow = `recorded_at >= date_trunc('month', NOW())`

/*
 * GlobalCommittedThisMonth is committed spend across EVERY client in the
 * current billing period.
 *
 * Per-client caps bound one engagement; this bounds the deployment. Without it,
 * twenty funded clients each inside their own budget can still produce a bill
 * nobody authorised, because no single check ever sees the total.
 *
 * Same arithmetic as the per-client state on purpose — reservation + release +
 * reconciliation, with `incurred` excluded so a running instance is not counted
 * twice. Any divergence between the two would show up as a system cap that
 * disagrees with the sum of the client caps it is supposed to bound.
 */
func (r *CloudBudgetRepository) GlobalCommittedThisMonth(ctx context.Context) (int64, error) {
	var committed int64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(cents), 0)
		FROM cloud_spend_ledger
		WHERE kind IN ('reservation','release','reconciliation')
		  AND `+budgetWindow).Scan(&committed)
	if err != nil {
		return 0, fmt.Errorf("failed to compute global committed cloud spend: %w", err)
	}
	return committed, nil
}

// GetPolicy returns the client's threshold ladder, falling back to the system
// default row (client_id IS NULL) when the client has no override.
func (r *CloudBudgetRepository) GetPolicy(ctx context.Context, clientID uuid.UUID) (*models.CloudBudgetPolicy, error) {
	var p models.CloudBudgetPolicy
	var notifyPct sql.NullInt32
	var cid uuid.NullUUID

	// ORDER BY puts the client-specific row first when it exists.
	err := r.db.QueryRowContext(ctx, `
		SELECT id, client_id, notify_pct, stop_provision_pct, drain_pct,
		       hard_stop_pct, allow_overage, drain_timeout_seconds,
		       created_at, updated_at
		FROM cloud_budget_policies
		WHERE client_id = $1 OR client_id IS NULL
		ORDER BY client_id NULLS LAST
		LIMIT 1`, clientID).Scan(
		&p.ID, &cid, &notifyPct, &p.StopProvisionPct, &p.DrainPct,
		&p.HardStopPct, &p.AllowOverage, &p.DrainTimeoutSeconds,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no cloud budget policy found (the system default row is missing)")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud budget policy: %w", err)
	}

	if cid.Valid {
		p.ClientID = &cid.UUID
	}
	if notifyPct.Valid {
		v := int(notifyPct.Int32)
		p.NotifyPct = &v
	}
	return &p, nil
}

/*
 * UpsertPolicy writes a client's threshold ladder, or the system default when
 * clientID is nil.
 *
 * The DB CHECK enforces notify <= stop_provision <= drain <= hard_stop; this
 * validates the same ordering first so the admin gets a readable message rather
 * than a raw constraint-violation string.
 */
func (r *CloudBudgetRepository) UpsertPolicy(ctx context.Context, p *models.CloudBudgetPolicy) error {
	if p.NotifyPct != nil && (*p.NotifyPct <= 0 || *p.NotifyPct > p.StopProvisionPct) {
		return fmt.Errorf("notify threshold must be between 1 and the stop-provisioning threshold (%d%%)", p.StopProvisionPct)
	}
	if p.StopProvisionPct > p.DrainPct {
		return fmt.Errorf("stop-provisioning threshold (%d%%) must not exceed the drain threshold (%d%%)", p.StopProvisionPct, p.DrainPct)
	}
	if p.DrainPct > p.HardStopPct {
		return fmt.Errorf("drain threshold (%d%%) must not exceed the hard-stop threshold (%d%%)", p.DrainPct, p.HardStopPct)
	}
	if p.DrainTimeoutSeconds < 0 {
		return fmt.Errorf("drain timeout must not be negative")
	}

	var notify sql.NullInt32
	if p.NotifyPct != nil {
		notify = sql.NullInt32{Int32: int32(*p.NotifyPct), Valid: true}
	}

	// Two statements rather than one: the system default row has client_id
	// NULL, which the UNIQUE constraint does not cover, so ON CONFLICT
	// (client_id) cannot target it. The partial unique index does.
	conflictTarget := "(client_id)"
	if p.ClientID == nil {
		conflictTarget = "((client_id IS NULL)) WHERE client_id IS NULL"
	}

	err := r.db.QueryRowContext(ctx, `
		INSERT INTO cloud_budget_policies (
			client_id, notify_pct, stop_provision_pct, drain_pct,
			hard_stop_pct, allow_overage, drain_timeout_seconds
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT `+conflictTarget+` DO UPDATE SET
			notify_pct            = EXCLUDED.notify_pct,
			stop_provision_pct    = EXCLUDED.stop_provision_pct,
			drain_pct             = EXCLUDED.drain_pct,
			hard_stop_pct         = EXCLUDED.hard_stop_pct,
			allow_overage         = EXCLUDED.allow_overage,
			drain_timeout_seconds = EXCLUDED.drain_timeout_seconds,
			updated_at            = NOW()
		RETURNING id, created_at, updated_at`,
		p.ClientID, notify, p.StopProvisionPct, p.DrainPct,
		p.HardStopPct, p.AllowOverage, p.DrainTimeoutSeconds,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to save cloud budget policy: %w", err)
	}
	return nil
}

// DeletePolicy removes a client override so the client falls back to the
// system default. Deleting the system default is refused: GetPolicy has no
// further fallback and every budget decision would fail.
func (r *CloudBudgetRepository) DeletePolicy(ctx context.Context, clientID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM cloud_budget_policies WHERE client_id = $1`, clientID)
	if err != nil {
		return fmt.Errorf("failed to delete cloud budget policy override: %w", err)
	}
	return nil
}

// ClientForJob resolves which client's budget a job spends against, via its
// hashlist. Used so a projection does not have to be told the client: passing
// no client would otherwise report zero available budget, which renders as
// "0% covered" and looks like a real shortfall rather than a missing argument.
func (r *CloudBudgetRepository) ClientForJob(ctx context.Context, jobID uuid.UUID) (uuid.UUID, error) {
	var clientID uuid.UUID
	err := r.db.QueryRowContext(ctx, `
		SELECT h.client_id
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.id = $1`, jobID).Scan(&clientID)
	if err == sql.ErrNoRows {
		return uuid.Nil, fmt.Errorf("job execution %s not found", jobID)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to resolve client for job: %w", err)
	}
	return clientID, nil
}

// GetClientCloudSettings returns one client's cloud burst configuration.
func (r *CloudBudgetRepository) GetClientCloudSettings(ctx context.Context, clientID uuid.UUID) (*models.ClientCloudSettings, error) {
	s := &models.ClientCloudSettings{ClientID: clientID}
	var budget sql.NullInt64
	var ttl sql.NullInt32

	err := r.db.QueryRowContext(ctx, `
		SELECT name, cloud_enabled, cloud_provider_allowlist,
		       cloud_budget_cents, max_instance_ttl_minutes, provider_ack
		FROM clients WHERE id = $1`, clientID).Scan(
		&s.ClientName, &s.Enabled, pq.Array(&s.ProviderAllowlist),
		&budget, &ttl, &s.ProviderAck,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("client %s not found", clientID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get client cloud settings: %w", err)
	}

	if budget.Valid {
		v := budget.Int64
		s.BudgetCents = &v
	}
	if ttl.Valid {
		v := int(ttl.Int32)
		s.MaxInstanceTTLMinutes = &v
	}
	if s.ProviderAllowlist == nil {
		s.ProviderAllowlist = []string{}
	}
	return s, nil
}

// ListClientCloudSettings returns the cloud configuration for every client that
// has cloud burst turned on OR a funded budget, so the admin sees anything that
// could spend money — including a client left funded but disabled.
func (r *CloudBudgetRepository) ListClientCloudSettings(ctx context.Context) ([]*models.ClientCloudSettings, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, cloud_enabled, cloud_provider_allowlist,
		       cloud_budget_cents, max_instance_ttl_minutes, provider_ack
		FROM clients
		WHERE cloud_enabled = true OR cloud_budget_cents IS NOT NULL
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list client cloud settings: %w", err)
	}
	defer rows.Close()

	var out []*models.ClientCloudSettings
	for rows.Next() {
		s := &models.ClientCloudSettings{}
		var budget sql.NullInt64
		var ttl sql.NullInt32

		if err := rows.Scan(&s.ClientID, &s.ClientName, &s.Enabled,
			pq.Array(&s.ProviderAllowlist), &budget, &ttl, &s.ProviderAck); err != nil {
			return nil, fmt.Errorf("failed to scan client cloud settings: %w", err)
		}
		if budget.Valid {
			v := budget.Int64
			s.BudgetCents = &v
		}
		if ttl.Valid {
			v := int(ttl.Int32)
			s.MaxInstanceTTLMinutes = &v
		}
		if s.ProviderAllowlist == nil {
			s.ProviderAllowlist = []string{}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpdateClientCloudSettings writes the admin-editable cloud fields.
//
// provider_ack is deliberately untouched: acknowledgements are recorded through
// RecordClientProviderAck so the stored attribution is always the authenticated
// admin, never whatever the request body claimed.
func (r *CloudBudgetRepository) UpdateClientCloudSettings(ctx context.Context, clientID uuid.UUID, in *models.ClientCloudSettingsInput) error {
	if in.BudgetCents != nil && *in.BudgetCents < 0 {
		return fmt.Errorf("cloud budget must not be negative")
	}
	if in.MaxInstanceTTLMinutes != nil && *in.MaxInstanceTTLMinutes <= 0 {
		return fmt.Errorf("maximum instance TTL must be positive")
	}
	allowlist := in.ProviderAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	for _, p := range allowlist {
		if !models.CloudProvider(p).IsValid() {
			return fmt.Errorf("unknown cloud provider %q in allowlist", p)
		}
	}

	res, err := r.db.ExecContext(ctx, `
		UPDATE clients
		SET cloud_enabled = $2,
		    cloud_provider_allowlist = $3,
		    cloud_budget_cents = $4,
		    max_instance_ttl_minutes = $5,
		    updated_at = NOW()
		WHERE id = $1`,
		clientID, in.Enabled, pq.Array(allowlist), in.BudgetCents, in.MaxInstanceTTLMinutes)
	if err != nil {
		return fmt.Errorf("failed to update client cloud settings: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("client %s not found", clientID)
	}
	return nil
}

// RecordClientProviderAck stores this client's acknowledgement that a given
// provider exposes their data, attributed to the acting admin.
//
// jsonb_set with create_if_missing merges into whatever is already there, so
// acknowledging AWS does not erase an earlier Vast.ai acknowledgement.
func (r *CloudBudgetRepository) RecordClientProviderAck(ctx context.Context, clientID uuid.UUID, provider string, userID uuid.UUID) error {
	if !models.CloudProvider(provider).IsValid() {
		return fmt.Errorf("unknown cloud provider %q", provider)
	}

	_, err := r.db.ExecContext(ctx, `
		UPDATE clients
		SET provider_ack = jsonb_set(
			COALESCE(provider_ack, '{}'::jsonb),
			ARRAY[$2::text],
			jsonb_build_object('at', to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), 'by', $3::text),
			true
		),
		updated_at = NOW()
		WHERE id = $1`, clientID, provider, userID.String())
	if err != nil {
		return fmt.Errorf("failed to record provider acknowledgement: %w", err)
	}
	return nil
}

// GetBudgetState computes the current budget picture for a client. Read-only;
// use Reserve when the result will drive a launch decision, so the check and
// the commitment are atomic.
func (r *CloudBudgetRepository) GetBudgetState(ctx context.Context, clientID uuid.UUID) (*models.CloudBudgetState, error) {
	return r.budgetState(ctx, r.db, clientID)
}

// queryRower lets budgetState run against either the pool or an open tx.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

func (r *CloudBudgetRepository) budgetState(ctx context.Context, q queryRower, clientID uuid.UUID) (*models.CloudBudgetState, error) {
	state := &models.CloudBudgetState{ClientID: clientID}

	var capCol sql.NullInt64
	var committed, incurred sql.NullInt64

	err := q.QueryRowContext(ctx, `
		SELECT
			c.cloud_budget_cents,
			COALESCE(SUM(l.cents) FILTER (WHERE l.kind IN ('reservation','release','reconciliation')), 0) AS committed,
			COALESCE(SUM(l.cents) FILTER (WHERE l.kind = 'incurred'), 0)                                  AS incurred
		FROM clients c
		LEFT JOIN cloud_spend_ledger l
		       ON l.client_id = c.id AND l.`+budgetWindow+`
		WHERE c.id = $1
		GROUP BY c.cloud_budget_cents`, clientID).Scan(&capCol, &committed, &incurred)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("client %s not found", clientID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to compute cloud budget state: %w", err)
	}

	state.IncurredCents = incurred.Int64
	// Outstanding = committed but not yet consumed. Reconciliation can push
	// this slightly negative; report zero rather than a confusing negative.
	if outstanding := committed.Int64 - incurred.Int64; outstanding > 0 {
		state.ReservedCents = outstanding
	}

	if !capCol.Valid {
		// No funded budget. AvailableCents stays zero and UsedPct stays zero;
		// callers must check CapCents == nil, not AvailableCents == 0.
		return state, nil
	}

	capCents := capCol.Int64
	state.CapCents = &capCents

	if available := capCents - committed.Int64; available > 0 {
		state.AvailableCents = available
	}
	if capCents > 0 {
		state.UsedPct = (float64(committed.Int64) / float64(capCents)) * 100
	}
	return state, nil
}

/*
 * Reserve atomically verifies headroom and commits `cents` against the
 * client's budget.
 *
 * The SELECT ... FOR UPDATE on the client row is the whole point: without it,
 * two concurrent provisioning decisions both read the same headroom, both
 * decide they fit, and both launch — which for this feature means two rented
 * GPUs charged against a budget that only covered one.
 *
 * Returns ErrNoBudget when the client is unfunded and ErrInsufficientBudget
 * when the reservation does not fit. When the policy allows overage the fit
 * check is skipped, but the reservation is still recorded so spend stays
 * attributable.
 */
func (r *CloudBudgetRepository) Reserve(
	ctx context.Context,
	clientID uuid.UUID,
	instanceID uuid.UUID,
	jobExecutionID *uuid.UUID,
	cents int64,
	allowOverage bool,
	note string,
) (*models.CloudBudgetState, error) {
	if cents < 0 {
		return nil, fmt.Errorf("reservation must not be negative, got %d", cents)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to start reservation transaction: %w", err)
	}
	defer tx.Rollback()

	// Serialise every budget decision for this client behind its own row.
	var locked uuid.UUID
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM clients WHERE id = $1 FOR UPDATE`, clientID).Scan(&locked); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("client %s not found", clientID)
		}
		return nil, fmt.Errorf("failed to lock client for reservation: %w", err)
	}

	state, err := r.budgetState(ctx, tx, clientID)
	if err != nil {
		return nil, err
	}
	if state.CapCents == nil {
		return nil, ErrNoBudget
	}
	if !allowOverage && cents > state.AvailableCents {
		return nil, fmt.Errorf("%w: need %d cents, %d available",
			ErrInsufficientBudget, cents, state.AvailableCents)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO cloud_spend_ledger (client_id, job_execution_id, cloud_instance_id, cents, kind, note)
		VALUES ($1, $2, $3, $4, 'reservation', $5)`,
		clientID, jobExecutionID, instanceID, cents, note); err != nil {
		return nil, fmt.Errorf("failed to write reservation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit reservation: %w", err)
	}

	// Recompute so the caller sees post-reservation headroom.
	state.ReservedCents += cents
	if state.AvailableCents >= cents {
		state.AvailableCents -= cents
	} else {
		state.AvailableCents = 0
	}
	if *state.CapCents > 0 {
		state.UsedPct += (float64(cents) / float64(*state.CapCents)) * 100
	}
	return state, nil
}

// RecordIncurred appends consumed spend for an instance. It does not change
// availability — the reservation already accounted for it — so it needs no
// client lock.
func (r *CloudBudgetRepository) RecordIncurred(ctx context.Context, clientID *uuid.UUID, instanceID uuid.UUID, cents int64, note string) error {
	if cents == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO cloud_spend_ledger (client_id, cloud_instance_id, cents, kind, note)
		VALUES ($1, $2, $3, 'incurred', $4)`, clientID, instanceID, cents, note)
	if err != nil {
		return fmt.Errorf("failed to record incurred spend: %w", err)
	}
	return nil
}

// ReleaseUnused gives back the unspent remainder of an instance's reservation.
// `cents` is the positive amount to return; it is stored negative so that
// summing the ledger yields committed spend directly.
func (r *CloudBudgetRepository) ReleaseUnused(ctx context.Context, clientID *uuid.UUID, instanceID uuid.UUID, cents int64, note string) error {
	if cents <= 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO cloud_spend_ledger (client_id, cloud_instance_id, cents, kind, note)
		VALUES ($1, $2, $3, 'release', $4)`, clientID, instanceID, -cents, note)
	if err != nil {
		return fmt.Errorf("failed to release reservation: %w", err)
	}
	return nil
}

// RecordReconciliation applies a signed provider-authoritative correction.
//
// Callers must not treat a negative correction as freshly spendable budget for
// a decision already made: it lands up to 24h late, long after the launches it
// would have influenced.
func (r *CloudBudgetRepository) RecordReconciliation(ctx context.Context, clientID *uuid.UUID, instanceID uuid.UUID, cents int64, note string) error {
	if cents == 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO cloud_spend_ledger (client_id, cloud_instance_id, cents, kind, note)
		VALUES ($1, $2, $3, 'reconciliation', $4)`, clientID, instanceID, cents, note)
	if err != nil {
		return fmt.Errorf("failed to record reconciliation: %w", err)
	}
	return nil
}
