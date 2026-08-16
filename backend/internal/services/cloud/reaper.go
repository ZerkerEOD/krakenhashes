package cloud

import (
	"context"
	"fmt"
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
}

// NewReaper creates a reaper.
func NewReaper(
	instances *repository.CloudInstanceRepository,
	budget *BudgetEngine,
	providers func(ctx context.Context, providerConfigID uuid.UUID) (Provider, error),
	notifier Notifier,
) *Reaper {
	return &Reaper{
		instances:   instances,
		budget:      budget,
		providers:   providers,
		notifier:    notifier,
		OrphanGrace: 10 * time.Minute,
		IdleDrain:   5 * time.Minute,
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

	// Orphans: present at the provider, absent from our database. This is the
	// only recovery for a launch that applied but whose label never landed in
	// a row, and it is why a dedicated provider account is recommended.
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
			debug.Warning("Cloud reaper: destroying ORPHAN instance %s (present at provider, absent from database)", label)
			if err := provider.Destroy(ctx, status.ProviderInstanceID); err != nil {
				debug.Error("Cloud reaper: failed to destroy orphan %s: %v", label, err)
			}
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
