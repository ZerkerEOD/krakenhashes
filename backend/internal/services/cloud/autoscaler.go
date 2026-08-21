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
 *
 * It is also the only place that knows HOW LONG each job has been starving,
 * which is what min_starvation_seconds is evaluated against. The edges have to
 * be observed here rather than inferred by the autoscaler: the scheduler
 * publishes every 3 seconds and the autoscaler wakes every 60, so an autoscaler
 * comparing its own consecutive wake-ups samples the truth at a twentieth of
 * the rate — a job that starved for 59 seconds, got an agent, and starved again
 * would look continuously starving to it. Tracking here also keeps working
 * across the passes ScaleOnce abandons early (stale snapshot, idle on-prem
 * capacity), which never reach any edge detection the autoscaler could do.
 *
 * The ages are DELIBERATELY NOT PERSISTED. A crash-looping backend that
 * reloaded them would accumulate starvation age it never observed and rent the
 * instant it came back up, which is the money-losing direction. Losing them
 * costs one extra min_starvation_seconds of delay — the safe direction, and the
 * same delay the rule asks for anyway. There is not even a window in which
 * persisted ages would be read: after a restart ReadAges reports fresh=false
 * until the scheduler publishes, and ScaleOnce already no-ops on that.
 */
type StarvationSnapshot struct {
	mu sync.RWMutex
	// startedAt is parent_job_id -> when that job was FIRST seen starving in
	// its current continuous run. Key presence is the starving set itself:
	// a job the scheduler stops reporting is deleted on that same Publish, so
	// the map is bounded by how many jobs are concurrently starving rather than
	// by everything that has ever starved.
	startedAt map[uuid.UUID]time.Time
	// idleOnPrem counts free on-prem agents. Non-zero means free capacity
	// exists and renting would be wasteful.
	idleOnPrem int
	updatedAt  time.Time
}

// NewStarvationSnapshot creates an empty snapshot.
func NewStarvationSnapshot() *StarvationSnapshot {
	return &StarvationSnapshot{startedAt: make(map[uuid.UUID]time.Time)}
}

/*
 * Publish replaces the starving set, carrying each surviving job's first-seen
 * time forward. Called by the scheduler cycle.
 *
 * A job that leaves the set loses its accumulated age, and re-entering starts
 * its clock from zero. That reset is the point of the rule rather than an
 * oversight: the question being asked is "has this job been unable to make ANY
 * progress for N seconds", and a job that just got an agent has made progress.
 * A high-water mark would let a job that starves for 30 seconds twice an hour
 * eventually qualify for paid capacity, which is the opposite of what the rule
 * is for.
 */
func (s *StarvationSnapshot) Publish(starving map[uuid.UUID]bool, idleOnPrem int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stamped INSIDE the lock. Reading the clock first lets two concurrent
	// publishes commit out of order, moving updatedAt backwards and restamping
	// startedAt from the older view — which would reset starvation ages that
	// had legitimately accumulated. Only the single-flight scheduler cycle
	// calls this today, but StarvationPublisher is an exported interface whose
	// documented contract is merely "must not block".
	now := time.Now()

	started := make(map[uuid.UUID]time.Time, len(starving))
	for jobID, isStarving := range starving {
		// The value is honoured, not just the key: publishStarvation only ever
		// sets true, but a caller that maps a recovered job to false must not
		// leave that job's clock running.
		if !isStarving {
			continue
		}
		if first, ok := s.startedAt[jobID]; ok {
			started[jobID] = first
			continue
		}
		started[jobID] = now
	}
	s.startedAt = started
	s.idleOnPrem = idleOnPrem
	s.updatedAt = now
}

/*
 * ReadAges returns how long each currently starving job has been continuously
 * starving, the leftover idle on-prem count, and whether the snapshot is fresh
 * enough to act on.
 *
 * now is a parameter so that every rule evaluated during one provisioning pass
 * is measured against the same instant, and so tests can drive the clock
 * instead of sleeping through a 15-minute threshold.
 *
 * The map is a copy. ScaleOnce iterates it while the scheduler keeps publishing
 * every 3 seconds, and handing back the live map would be a concurrent map read
 * and write — a hard crash, not a wrong number.
 */
func (s *StarvationSnapshot) ReadAges(maxAge time.Duration, now time.Time) (
	ages map[uuid.UUID]time.Duration, idleOnPrem int, fresh bool,
) {
	if now.IsZero() {
		/*
		 * A zero now breaks both halves of this function, in opposite
		 * directions, so it is replaced rather than defended against.
		 *
		 * time.Time{}.Sub(anything recent) saturates to the MINIMUM int64
		 * duration, not a large positive one. The staleness check at line 120
		 * therefore passes trivially — every snapshot looks fresh forever — and
		 * every age below saturates negative and is clamped to 0, so the
		 * starvation rule would refuse every job forever instead of renting for
		 * any of them. Stale data treated as fresh, and a rail that never opens.
		 *
		 * The clamp at 127 is what stands between a caller's mistake and a
		 * minInt64 age reaching an evaluator; it is not redundant with this.
		 */
		now = time.Now()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.updatedAt.IsZero() || now.Sub(s.updatedAt) > maxAge {
		return nil, 0, false
	}

	ages = make(map[uuid.UUID]time.Duration, len(s.startedAt))
	for jobID, first := range s.startedAt {
		age := now.Sub(first)
		if age < 0 {
			// Only reachable when a caller passes a now from before the last
			// Publish. A negative age would fail every threshold anyway, but
			// clamping keeps "age" a quantity an evaluator can compare or log
			// without special-casing.
			age = 0
		}
		ages[jobID] = age
	}
	return ages, s.idleOnPrem, true
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
	/*
	 * MaxInstances is job_executions.cloud_max_instances, COALESCEd to 0.
	 *
	 * Zero means no per-job ceiling, and there is no budget-derived fallback:
	 * the column is nullable with no default and the job dialog sends undefined
	 * when the field is left blank, so "cloud burst ticked, max instances
	 * blank" is both the easy path and the uncapped one. What actually stops it
	 * is downstream — the client budget reservation and the global monthly cap
	 * — which bound spend but not instance count, so the autoscaler will add
	 * one instance per 60-second tick until money runs out.
	 *
	 * Callers must not read a zero here as "a cap is in force".
	 */
	MaxInstances int
	// Priority mirrors job_executions.priority, so the autoscaler spends a
	// constrained budget in the same order the scheduler would dispatch.
	Priority int

	/*
	 * MinStarvationSeconds is the resolved rule for this job's client, carried
	 * here rather than looked up in the autoscaler.
	 *
	 * The comparison has to happen where the observation lives — the starvation
	 * snapshot the autoscaler owns — but the THRESHOLD comes from a per-client
	 * merge of two database rows. Passing it along the candidate list keeps the
	 * autoscaler free of a rules repository and keeps CloudEligibleJobs from
	 * having to sample an age the scheduler rewrites every 3 seconds.
	 *
	 * Zero is the in-band OFF value: rent on the first starving tick, which is
	 * the behaviour that shipped before this rule existed.
	 */
	MinStarvationSeconds int
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
	//
	// ages carries how long each job has been continuously starving. Nothing
	// filters on it yet — min_starvation_seconds is evaluated by a separate
	// rules evaluator that is wired in later — so today it only reaches the
	// provisioning log line.
	ages, idleOnPrem, fresh := a.snapshot.ReadAges(30*time.Second, time.Now())
	if !fresh {
		debug.Info("Cloud autoscaler: scheduler snapshot is stale; skipping this pass")
		return
	}
	if len(ages) == 0 {
		return
	}
	if idleOnPrem > 0 {
		// Free capacity exists. If jobs are still starving with idle on-prem
		// agents, the blocker is compatibility or max_agents, and renting
		// would not help.
		debug.Info("Cloud autoscaler: %d on-prem agent(s) idle; not renting", idleOnPrem)
		return
	}

	candidates := make([]uuid.UUID, 0, len(ages))
	for jobID := range ages {
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
		if a.GlobalInstanceCap > 0 {
			/*
			 * A cap with no counter is a cap that enforces nothing, so it
			 * refuses rather than passing.
			 *
			 * These two fields are set separately by whoever builds the
			 * autoscaler, and skipping the check when the counter is nil makes
			 * the omission invisible: the operator's "never more than N rented
			 * boxes" silently becomes unlimited, and nothing in the logs, the
			 * settings UI or the tests says so. Refusing turns that same
			 * mistake into a message naming the missing wiring.
			 */
			if a.LiveInstanceCount == nil {
				debug.Error("Cloud autoscaler: global instance cap %d is configured but no live-instance "+
					"counter was wired in; refusing to provision rather than treating the cap as unlimited",
					a.GlobalInstanceCap)
				return
			}
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

		/*
		 * The starvation rail, checked here because this is the only place the
		 * age exists.
		 *
		 * THIS CHANGES BEHAVIOUR that shipped before the rule: one starving
		 * tick used to be enough, so a transient gap between chunks could cost
		 * a full instance launch. The seeded default of 180s is three ticks of
		 * the scheduler's publish interval.
		 *
		 * A job missing from `ages` cannot happen — CloudEligibleJobs is fed
		 * from the starving set itself — but if it ever did, treating the
		 * absence as age zero refuses rather than renting, which is the right
		 * direction for a missing observation.
		 */
		if job.MinStarvationSeconds > 0 {
			required := time.Duration(job.MinStarvationSeconds) * time.Second
			if age := ages[job.JobExecutionID]; age < required {
				debug.Debug("Cloud autoscaler: job %s has been starving for %s, under the "+
					"%s minimum; not provisioning yet", job.JobExecutionID, age.Round(time.Second), required)
				continue
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
		debug.Info("Cloud autoscaler: provisioned an instance for job %s (starving for %s)",
			job.JobExecutionID, ages[job.JobExecutionID].Round(time.Second))
	}
}
