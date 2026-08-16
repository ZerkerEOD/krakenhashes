package cloud

import (
	"context"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

/*
 * StarvationSnapshot is what the scheduler publishes each cycle so the
 * autoscaler can make provisioning decisions without duplicating
 * computeStarvingUnits or re-querying.
 *
 * Deliberately a snapshot rather than a callback: the scheduler cycle runs
 * every 3 seconds and must never block on network I/O or on money-moving
 * database writes, both of which provisioning involves.
 */
type StarvationSnapshot struct {
	mu sync.RWMutex
	// starvingJobs is parent_job_id -> true for units that got zero
	// allocations this cycle.
	starvingJobs map[uuid.UUID]bool
	// idleOnPrem counts free on-prem agents. Non-zero means free capacity
	// exists and renting would be wasteful.
	idleOnPrem int
	updatedAt  time.Time
}

// NewStarvationSnapshot creates an empty snapshot.
func NewStarvationSnapshot() *StarvationSnapshot {
	return &StarvationSnapshot{starvingJobs: make(map[uuid.UUID]bool)}
}

// Publish replaces the snapshot. Called by the scheduler cycle.
func (s *StarvationSnapshot) Publish(starving map[uuid.UUID]bool, idleOnPrem int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starvingJobs = starving
	s.idleOnPrem = idleOnPrem
	s.updatedAt = time.Now()
}

// Read returns the current snapshot and whether it is fresh enough to act on.
func (s *StarvationSnapshot) Read(maxAge time.Duration) (map[uuid.UUID]bool, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.updatedAt.IsZero() || time.Since(s.updatedAt) > maxAge {
		return nil, 0, false
	}
	out := make(map[uuid.UUID]bool, len(s.starvingJobs))
	for k, v := range s.starvingJobs {
		out[k] = v
	}
	return out, s.idleOnPrem, true
}

// Provisioner is the subset of the cloud service the autoscaler needs.
type Provisioner interface {
	// ProvisionForJob rents one instance for a job. Implementations must be
	// safe to call concurrently and must fail closed on any missing
	// precondition (no budget, no VPN credential, no capacity).
	ProvisionForJob(ctx context.Context, jobID uuid.UUID) error
	// LiveInstanceCountForJob reports how many instances already serve a job.
	LiveInstanceCountForJob(ctx context.Context, jobID uuid.UUID) (int, error)
	// CloudEligibleJobs filters a candidate set down to jobs that opted in,
	// whose client has funded budget and has allowed this provider.
	CloudEligibleJobs(ctx context.Context, candidates []uuid.UUID) ([]EligibleJob, error)
}

// EligibleJob is a job that may burst, with the caps that apply.
type EligibleJob struct {
	JobExecutionID uuid.UUID
	ClientID       uuid.UUID
	// MaxInstances is job_executions.cloud_max_instances, or a budget-derived
	// value when unset.
	MaxInstances int
	// Priority mirrors job_executions.priority, so the autoscaler spends a
	// constrained budget in the same order the scheduler would dispatch.
	Priority int
	// CreatedAtNanos breaks priority ties oldest-first, matching
	// GetSchedulable's ORDER BY.
	CreatedAtNanos int64
}

/*
 * Autoscaler provisions rented capacity for starving, cloud-eligible jobs.
 *
 * Runs on its own timer, NOT inside the scheduler's 3-second cycle: it does
 * network I/O against provider APIs and writes budget reservations, neither of
 * which belongs on the dispatch hot path.
 *
 * On-prem capacity is always preferred because it is free. The autoscaler only
 * acts when a job is starving AND no idle on-prem agent could have taken the
 * work.
 */
type Autoscaler struct {
	snapshot    *StarvationSnapshot
	provisioner Provisioner

	// GlobalInstanceCap bounds live rented instances across all clients.
	// Zero means unlimited (budget still applies).
	GlobalInstanceCap int
	// LiveInstanceCount reports the current global total.
	LiveInstanceCount func(ctx context.Context) (int, error)
}

// NewAutoscaler creates an autoscaler.
func NewAutoscaler(snapshot *StarvationSnapshot, provisioner Provisioner) *Autoscaler {
	return &Autoscaler{snapshot: snapshot, provisioner: provisioner}
}

// Run drives the autoscaler until the context is cancelled.
func (a *Autoscaler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			debug.Info("Cloud autoscaler stopping")
			return
		case <-ticker.C:
			a.ScaleOnce(ctx)
		}
	}
}

// ScaleOnce performs one provisioning pass.
func (a *Autoscaler) ScaleOnce(ctx context.Context) {
	// A stale snapshot means the scheduler is not running or is wedged.
	// Provisioning on stale starvation data could rent GPUs for work that is
	// already finished, so do nothing.
	starving, idleOnPrem, fresh := a.snapshot.Read(30 * time.Second)
	if !fresh {
		debug.Info("Cloud autoscaler: scheduler snapshot is stale; skipping this pass")
		return
	}
	if len(starving) == 0 {
		return
	}
	if idleOnPrem > 0 {
		// Free capacity exists. If jobs are still starving with idle on-prem
		// agents, the blocker is compatibility or max_agents, and renting
		// would not help.
		debug.Info("Cloud autoscaler: %d on-prem agent(s) idle; not renting", idleOnPrem)
		return
	}

	candidates := make([]uuid.UUID, 0, len(starving))
	for jobID := range starving {
		candidates = append(candidates, jobID)
	}

	eligible, err := a.provisioner.CloudEligibleJobs(ctx, candidates)
	if err != nil {
		debug.Error("Cloud autoscaler: could not resolve eligible jobs: %v", err)
		return
	}
	if len(eligible) == 0 {
		return
	}

	// Spend a constrained budget in the scheduler's own order: priority
	// DESC, then oldest first. One ordering across the system means an
	// operator's priority setting means the same thing everywhere.
	for i := 0; i < len(eligible); i++ {
		for j := i + 1; j < len(eligible); j++ {
			a1, a2 := eligible[i], eligible[j]
			if a2.Priority > a1.Priority ||
				(a2.Priority == a1.Priority && a2.CreatedAtNanos < a1.CreatedAtNanos) {
				eligible[i], eligible[j] = eligible[j], eligible[i]
			}
		}
	}

	for _, job := range eligible {
		if a.GlobalInstanceCap > 0 && a.LiveInstanceCount != nil {
			live, err := a.LiveInstanceCount(ctx)
			if err != nil {
				debug.Error("Cloud autoscaler: could not count live instances: %v", err)
				return
			}
			if live >= a.GlobalInstanceCap {
				debug.Info("Cloud autoscaler: global instance cap %d reached", a.GlobalInstanceCap)
				return
			}
		}

		have, err := a.provisioner.LiveInstanceCountForJob(ctx, job.JobExecutionID)
		if err != nil {
			debug.Error("Cloud autoscaler: could not count instances for job %s: %v", job.JobExecutionID, err)
			continue
		}
		if job.MaxInstances > 0 && have >= job.MaxInstances {
			continue
		}

		// One instance per pass per job. Deliberately incremental: each
		// launch consumes budget and takes minutes to become useful, so
		// provisioning a burst all at once would commit money before the
		// first instance had proven it can even register.
		if err := a.provisioner.ProvisionForJob(ctx, job.JobExecutionID); err != nil {
			debug.Error("Cloud autoscaler: provisioning failed for job %s: %v", job.JobExecutionID, err)
			continue
		}
		debug.Info("Cloud autoscaler: provisioned an instance for job %s", job.JobExecutionID)
	}
}
