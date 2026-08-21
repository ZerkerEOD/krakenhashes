package cloud

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// terminateFailureAlertThreshold is how many consecutive failed teardowns
// escalate to admins. An instance we cannot kill is money actively burning, so
// this is deliberately small.
const terminateFailureAlertThreshold = 3

// Notifier raises operator-visible alerts. Kept as a narrow interface so the
// reaper does not depend on the notification package's construction order.
type Notifier interface {
	CloudTeardownFailed(ctx context.Context, inst *models.CloudInstance, attempts int, cause error)
	CloudBudgetThreshold(ctx context.Context, clientID uuid.UUID, action BudgetAction, reason string)
}

/*
 * Reaper reconciles the database against provider inventory and destroys
 * anything that should not exist.
 *
 * It is the third tier of the teardown ladder, below the in-guest absolute
 * deadline and the agent's heartbeat-loss self-destruct. Those two survive the
 * backend disappearing entirely; the reaper does not. It exists to catch what
 * they cannot:
 *
 *   - instances whose launch response was lost, so we never recorded an ID
 *   - instances that exist on the account with no database row at all
 *   - instances stuck in a state that will never become useful (Vast.ai's
 *     exited/unknown/offline never recover — polling them burns money)
 *   - instances past TTL whose in-guest timer failed to arm
 *   - instances idle with no work left
 *   - instances whose client has exhausted its budget
 */
type Reaper struct {
	instances *repository.CloudInstanceRepository
	budget    *BudgetEngine
	providers func(ctx context.Context, providerConfigID uuid.UUID) (Provider, error)
	notifier  Notifier

	// OrphanGrace is how long an unknown provider-side instance is tolerated
	// before destruction. Non-zero so a launch in flight is not reaped by the
	// reconciliation pass racing it.
	OrphanGrace time.Duration
	// IdleDrain is how long an instance may sit with no work before teardown.
	IdleDrain time.Duration

	// orphanFirstSeen is when each currently-unrecognised provider-side label
	// was first observed, so OrphanGrace can be applied. Guarded by orphanMu
	// because Run and a manually triggered SweepOnce could overlap.
	//
	// In memory rather than in the database, deliberately. A restart forgets
	// and re-starts the clock, which delays a destruction by one grace period
	// — the SAFE direction. Persisting it would mean a crash-looping backend
	// accumulated grace it never actually observed and destroyed a live
	// instance the moment it came up.
	orphanMu        sync.Mutex
	orphanFirstSeen map[string]time.Time
}

// NewReaper creates a reaper.
func NewReaper(
	instances *repository.CloudInstanceRepository,
	budget *BudgetEngine,
	providers func(ctx context.Context, providerConfigID uuid.UUID) (Provider, error),
	notifier Notifier,
) *Reaper {
	return &Reaper{
		instances:       instances,
		budget:          budget,
		providers:       providers,
		notifier:        notifier,
		OrphanGrace:     10 * time.Minute,
		IdleDrain:       5 * time.Minute,
		orphanFirstSeen: make(map[string]time.Time),
	}
}

// Run drives the reaper until the context is cancelled. Modelled on
// AgentUpdateSweeper: a ticker plus an extracted SweepOnce so the logic stays
// directly testable.
func (r *Reaper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately: on backend startup this is the reconciliation pass that
	// reclaims anything orphaned by the previous process dying.
	r.SweepOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			debug.Info("Cloud reaper stopping")
			return
		case <-ticker.C:
			r.SweepOnce(ctx)
		}
	}
}

// SweepOnce performs one reconciliation pass. It never returns an error:
// a failure on one instance must not stop the others from being reaped.
func (r *Reaper) SweepOnce(ctx context.Context) {
	now := time.Now()

	live, err := r.instances.ListLive(ctx)
	if err != nil {
		debug.Error("Cloud reaper: failed to list live instances: %v", err)
		return
	}

	// Provider inventory, fetched once per provider config, keyed by label.
	inventories := make(map[uuid.UUID]map[string]InstanceStatus)
	known := make(map[string]bool)

	for _, inst := range live {
		known[inst.Label] = true

		provider, err := r.providers(ctx, inst.ProviderConfigID)
		if err != nil {
			debug.Error("Cloud reaper: no provider for instance %s: %v", inst.Label, err)
			continue
		}

		if _, ok := inventories[inst.ProviderConfigID]; !ok {
			inv, err := provider.ListOwned(ctx)
			if err != nil {
				debug.Error("Cloud reaper: failed to list owned instances: %v", err)
				// Without inventory we cannot safely judge orphans this pass,
				// but per-instance deadline checks below still apply.
				inventories[inst.ProviderConfigID] = nil
			} else {
				inventories[inst.ProviderConfigID] = inv
			}
		}

		r.reconcileInstance(ctx, provider, inst, inventories[inst.ProviderConfigID], now)
	}

	/*
	 * Orphans: present at the provider, absent from our database. This is the
	 * only recovery for a launch that applied but whose label never landed in
	 * a row, and it is why a dedicated provider account is recommended.
	 *
	 * Gated on OrphanGrace, which until now was loaded from settings, assigned
	 * in main.go and never read — orphans were destroyed on first sight.
	 *
	 * The race it exists for is real and this pass creates it: `live` is read
	 * once at the top, but each provider's inventory is fetched later in the
	 * loop below it. An instance provisioned in that window is in the inventory
	 * and NOT in `known`, so first-sight destruction would tear down an
	 * instance whose row was written seconds earlier — and the operator would
	 * see a launch that "failed" for no visible reason.
	 */
	r.forgetVanishedOrphans(inventories)

	for cfgID, inv := range inventories {
		if inv == nil {
			continue
		}
		provider, err := r.providers(ctx, cfgID)
		if err != nil {
			continue
		}
		for label, status := range inv {
			if known[label] {
				continue
			}
			if waited, ready := r.orphanAge(label, now); !ready {
				debug.Info("Cloud reaper: label %s is unrecognised but only %s old; "+
					"holding for the %s orphan grace in case its launch is still in flight",
					label, waited.Round(time.Second), r.OrphanGrace)
				continue
			}
			debug.Warning("Cloud reaper: destroying ORPHAN instance %s (present at provider, absent from database)", label)
			if err := provider.Destroy(ctx, status.ProviderInstanceID); err != nil {
				debug.Error("Cloud reaper: failed to destroy orphan %s: %v", label, err)
			}
		}
	}
}

/*
 * orphanAge reports how long a label has been unrecognised, and whether that is
 * long enough to destroy it.
 *
 * First sight records the time and returns not-ready, so an orphan always
 * survives at least one sweep. With OrphanGrace <= 0 the caller gets the old
 * first-sight behaviour, which keeps the setting's "0 disables it" reading
 * consistent with the other cloud knobs.
 */
func (r *Reaper) orphanAge(label string, now time.Time) (time.Duration, bool) {
	if r.OrphanGrace <= 0 {
		return 0, true
	}

	r.orphanMu.Lock()
	defer r.orphanMu.Unlock()

	first, seen := r.orphanFirstSeen[label]
	if !seen {
		r.orphanFirstSeen[label] = now
		return 0, false
	}
	waited := now.Sub(first)
	return waited, waited >= r.OrphanGrace
}

/*
 * forgetVanishedOrphans drops labels that are no longer in any inventory.
 *
 * Without this the map grows for the life of the process, and — worse — a label
 * that was briefly unrecognised, then adopted into a row, then legitimately
 * reused would inherit its old first-seen time and skip its grace period.
 *
 * A nil inventory means that provider could not be listed this pass. Its labels
 * are left untouched rather than forgotten, so one failed list call does not
 * reset the clock on every orphan it owns.
 */
func (r *Reaper) forgetVanishedOrphans(inventories map[uuid.UUID]map[string]InstanceStatus) {
	present := make(map[string]bool)
	for _, inv := range inventories {
		if inv == nil {
			return // at least one provider is unreadable; do not prune on partial data
		}
		for label := range inv {
			present[label] = true
		}
	}

	r.orphanMu.Lock()
	defer r.orphanMu.Unlock()
	for label := range r.orphanFirstSeen {
		if !present[label] {
			delete(r.orphanFirstSeen, label)
		}
	}
}

// reconcileInstance decides the fate of one tracked instance.
func (r *Reaper) reconcileInstance(ctx context.Context, provider Provider, inst *models.CloudInstance, inventory map[string]InstanceStatus, now time.Time) {
	// 1. Launch never confirmed. Reconcile by label: if the provider has it,
	//    adopt the ID; if the deadline passed and it does not, give up.
	if inst.ProviderInstanceID == "" {
		if inventory != nil {
			if status, ok := inventory[inst.Label]; ok {
				debug.Warning("Cloud reaper: adopting instance %s from provider (launch response was lost)", inst.Label)
				if err := r.instances.MarkLaunched(ctx, inst.ID, status.ProviderInstanceID, now,
					now.Add(r.remainingTTL(inst, now)), status.Raw); err != nil {
					debug.Error("Cloud reaper: failed to adopt %s: %v", inst.Label, err)
				}
				return
			}
		}
		if inst.LaunchDeadlineAt.Valid && now.After(inst.LaunchDeadlineAt.Time) {
			debug.Warning("Cloud reaper: instance %s never launched, marking failed", inst.Label)
			r.finalize(ctx, inst, models.CloudInstanceFailed, "launch deadline exceeded")
		}
		return
	}

	// 2. Provider says it is terminal. Vast.ai's exited/unknown/offline never
	//    recover, so polling them further is pure spend.
	status, err := provider.Status(ctx, inst.ProviderInstanceID)
	if err == nil && status.Terminal {
		debug.Info("Cloud reaper: instance %s is terminal at provider (%s); tearing down", inst.Label, status.Message)
		r.destroy(ctx, provider, inst, fmt.Sprintf("provider terminal state: %s", status.Message))
		return
	}

	// 3. TTL expired. The in-guest watchdog should already have fired; if we
	//    are here it did not, which is exactly why this tier exists.
	if inst.TTLEpoch.Valid && now.After(inst.TTLEpoch.Time) {
		r.destroy(ctx, provider, inst, "TTL expired")
		return
	}

	// 4. Agent never registered within its readiness window.
	if inst.AgentID == nil && inst.ReadyDeadlineAt.Valid && now.After(inst.ReadyDeadlineAt.Time) {
		r.destroy(ctx, provider, inst, "agent did not register before ready deadline")
		return
	}

	/*
	 * 4.5. The work is gone.
	 *
	 * This is the largest avoidable waste in the whole feature and the one no
	 * other tier catches. Every tier above fires on something being WRONG — a
	 * failed launch, an expired TTL, an exhausted budget. Nothing fires on the
	 * ordinary happy ending: the job finishes at 14:02 on an instance rented
	 * until 18:00, and a GPU bills for four hours with nothing to do.
	 *
	 * Two cases, deliberately separated:
	 *
	 *   - the job reached a terminal state: destroy now, no grace. There is
	 *     nothing left that could ever need this instance.
	 *   - the job is alive but this instance has had no task for IdleDrain:
	 *     destroy after that grace, because a gap between chunks is normal and
	 *     tearing down during one would waste the launch we just paid for.
	 */
	if r.instanceHasNoWork(ctx, inst, now) {
		return
	}

	// 5. Budget. Assessed per client, so one client exhausting its budget
	//    never tears down another's instances.
	if inst.ClientID != nil {
		assessment, err := r.budget.Assess(ctx, *inst.ClientID)
		if err != nil {
			debug.Error("Cloud reaper: budget assessment failed for %s: %v", inst.Label, err)
		} else {
			switch assessment.Action {
			case BudgetActionHardStop:
				r.destroy(ctx, provider, inst, "budget hard stop: "+assessment.Reason)
				return
			case BudgetActionDrain:
				if inst.State != models.CloudInstanceDraining {
					debug.Info("Cloud reaper: draining %s (%s)", inst.Label, assessment.Reason)
					if err := r.instances.SetState(ctx, inst.ID, models.CloudInstanceDraining, assessment.Reason); err != nil {
						debug.Error("Cloud reaper: failed to mark draining: %v", err)
					}
				}
			}
		}
	}

	// 6. Accrue spend so the budget picture stays current between launches.
	billedFrom := inst.LaunchedAt.Time
	if delta, err := r.budget.AccrueInstance(ctx, inst, billedFrom, now); err != nil {
		debug.Error("Cloud reaper: failed to accrue for %s: %v", inst.Label, err)
	} else if delta > 0 {
		if err := r.instances.AddIncurredCost(ctx, inst.ID, delta); err != nil {
			debug.Error("Cloud reaper: failed to record incurred cost: %v", err)
		}
	}
}

/*
 * instanceHasNoWork destroys an instance whose job no longer needs it, and
 * reports whether it did.
 *
 * Fails toward KEEPING the instance: a query error, an unknown activity time,
 * or a job that is merely between chunks all leave it running. Destroying a
 * busy instance throws away the launch that was just paid for and the work in
 * flight on it, so the grace period is spent deliberately rather than saved.
 */
func (r *Reaper) instanceHasNoWork(ctx context.Context, inst *models.CloudInstance, now time.Time) bool {
	/*
	 * A null job on a launched instance means the job was DELETED, not that the
	 * instance never had one. cloud_instances.job_execution_id is ON DELETE SET
	 * NULL and ProvisionForJob always sets it, so the only way to arrive here
	 * with nil is that the row it pointed at is gone.
	 *
	 * Skipping this case leaves a GPU running until its TTL with no job, no
	 * work, and nothing else in the ladder that would notice. If a warm pool of
	 * job-less instances is ever added, this is the branch it has to change.
	 */
	if inst.JobExecutionID == nil {
		r.destroyByLookup(ctx, inst, "the job this instance was rented for was deleted")
		return true
	}

	work, err := r.instances.WorkStatus(ctx, *inst.JobExecutionID, inst.AgentID)
	if err != nil {
		debug.Error("Cloud reaper: could not determine whether %s still has work: %v", inst.Label, err)
		return false
	}

	if !work.JobExists {
		r.destroyByLookup(ctx, inst, "the job this instance was rented for no longer exists")
		return true
	}
	if work.JobFinished {
		r.destroyByLookup(ctx, inst, "job finished; instance is no longer needed")
		return true
	}

	// Idle drain disabled.
	if r.IdleDrain <= 0 {
		return false
	}

	// An instance that has never run a task is measured from when it became
	// ready, not from epoch — otherwise every instance would look infinitely
	// idle the moment it registered and be destroyed before its first chunk.
	idleSince := work.LastActivityAt.Time
	if !work.LastActivityAt.Valid {
		if !inst.ReadyAt.Valid {
			return false // not ready yet; nothing to measure from
		}
		idleSince = inst.ReadyAt.Time
	}

	if now.Sub(idleSince) < r.IdleDrain {
		return false
	}
	r.destroyByLookup(ctx, inst,
		fmt.Sprintf("no work for %s (idle drain)", now.Sub(idleSince).Round(time.Second)))
	return true
}

// destroyByLookup resolves the instance's provider before tearing down. The
// idle path is reached from reconcileInstance, which already holds a provider,
// but the settle-and-destroy sequence is identical and worth not duplicating.
func (r *Reaper) destroyByLookup(ctx context.Context, inst *models.CloudInstance, reason string) {
	provider, err := r.providers(ctx, inst.ProviderConfigID)
	if err != nil {
		debug.Error("Cloud reaper: cannot resolve provider to destroy idle instance %s: %v", inst.Label, err)
		return
	}
	debug.Info("Cloud reaper: destroying %s - %s", inst.Label, reason)
	r.destroy(ctx, provider, inst, reason)
}

// remainingTTL returns how much of an instance's intended life is left.
func (r *Reaper) remainingTTL(inst *models.CloudInstance, now time.Time) time.Duration {
	if inst.TTLEpoch.Valid {
		if d := inst.TTLEpoch.Time.Sub(now); d > 0 {
			return d
		}
		return 0
	}
	return time.Hour
}

/*
 * destroy tears an instance down and settles its budget.
 *
 * Failures are counted rather than swallowed. After
 * terminateFailureAlertThreshold consecutive failures an admin is paged with
 * the raw provider ID, because at that point automation has demonstrably lost
 * control of something that is still billing. The instance deliberately stays
 * in a live state so it keeps accruing and keeps blocking new provisioning for
 * that client — reporting it as terminated would hide real spend.
 */
func (r *Reaper) destroy(ctx context.Context, provider Provider, inst *models.CloudInstance, reason string) {
	if err := r.instances.SetState(ctx, inst.ID, models.CloudInstanceTerminating, reason); err != nil {
		debug.Error("Cloud reaper: failed to mark terminating: %v", err)
	}

	if err := provider.Destroy(ctx, inst.ProviderInstanceID); err != nil {
		attempts, recErr := r.instances.RecordTerminateFailure(ctx, inst.ID, err.Error())
		if recErr != nil {
			debug.Error("Cloud reaper: failed to record terminate failure: %v", recErr)
		}
		debug.Error("Cloud reaper: destroy failed for %s (attempt %d): %v", inst.Label, attempts, err)

		if attempts >= terminateFailureAlertThreshold && r.notifier != nil {
			r.notifier.CloudTeardownFailed(ctx, inst, attempts, err)
		}
		return
	}

	r.finalize(ctx, inst, models.CloudInstanceTerminated, reason)
}

// finalize records the end state and releases unused budget.
func (r *Reaper) finalize(ctx context.Context, inst *models.CloudInstance, state models.CloudInstanceState, reason string) {
	if err := r.budget.SettleInstance(ctx, inst); err != nil {
		debug.Error("Cloud reaper: failed to settle budget for %s: %v", inst.Label, err)
	}
	if err := r.instances.SetState(ctx, inst.ID, state, reason); err != nil {
		debug.Error("Cloud reaper: failed to finalize %s: %v", inst.Label, err)
	}
	debug.Info("Cloud reaper: instance %s finalized as %s (%s)", inst.Label, state, reason)
}

// DrainAll tears down every live instance. Called from the ordered SIGTERM
// path, before the HTTP servers shut down.
//
// This is best-effort by nature: SIGKILL and OOM bypass it entirely, which is
// precisely why the in-guest absolute deadline is the real guarantee and this
// is only an optimization to stop billing sooner.
func (r *Reaper) DrainAll(ctx context.Context) {
	live, err := r.instances.ListLive(ctx)
	if err != nil {
		debug.Error("Cloud drain: failed to list live instances: %v", err)
		return
	}
	if len(live) == 0 {
		return
	}
	debug.Info("Cloud drain: destroying %d live instance(s) before shutdown", len(live))

	for _, inst := range live {
		provider, err := r.providers(ctx, inst.ProviderConfigID)
		if err != nil {
			debug.Error("Cloud drain: no provider for %s: %v", inst.Label, err)
			continue
		}
		r.destroy(ctx, provider, inst, "backend shutting down")
	}
}
