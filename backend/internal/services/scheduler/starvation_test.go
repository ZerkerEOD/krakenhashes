package scheduler

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

var errTestLockLoad = errors.New("database unavailable")

/*
 * publishStarvation is the only input to the cloud autoscaler, which is the one
 * component that spends money unattended. A false positive here does not
 * produce a wrong number on a dashboard — it rents a GPU.
 */

// recordingPublisher captures what the cycle published.
type recordingPublisher struct {
	starving   map[uuid.UUID]bool
	idleOnPrem int
	calls      int
}

func (r *recordingPublisher) Publish(starving map[uuid.UUID]bool, idleOnPrem int) {
	r.starving = starving
	r.idleOnPrem = idleOnPrem
	r.calls++
}

// TestPublishStarvation_UnallocatedUnitStarves is the base case: work exists,
// no agent took it.
func TestPublishStarvation_UnallocatedUnitStarves(t *testing.T) {
	job := uuid.New()
	unit := uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	c.publishStarvation(
		[]UnitInfo{{ID: unit, ParentJobID: job}},
		[]AgentInfo{},
		nil,
	)

	if !rec.starving[job] {
		t.Fatal("a unit with no allocation and no cap must report its job as starving")
	}
}

// TestPublishStarvation_AllocatedUnitDoesNotStarve: the agent it needed arrived.
func TestPublishStarvation_AllocatedUnitDoesNotStarve(t *testing.T) {
	job := uuid.New()
	unit := uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	c.publishStarvation(
		[]UnitInfo{{ID: unit, ParentJobID: job}},
		[]AgentInfo{{ID: 1}},
		[]Allocation{{UnitID: unit, AgentID: 1}},
	)

	if rec.starving[job] {
		t.Fatal("a unit that received an agent is not starving")
	}
}

/*
 * TestPublishStarvation_JobAtMaxAgentsIsNotStarving is the money guard.
 *
 * A job pinned at its max_agents cap has unallocated units on EVERY cycle, by
 * design. Reporting that as starvation would make the autoscaler rent an
 * instance the allocator is then forbidden to use: a GPU that bills by the
 * second and never receives a chunk, repeating up to cloud_max_instances.
 */
func TestPublishStarvation_JobAtMaxAgentsIsNotStarving(t *testing.T) {
	job := uuid.New()
	u1, u2 := uuid.New(), uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	// Parent cap 2, both slots already held by running tasks. Neither unit can
	// be allocated this cycle and neither should ask for paid capacity.
	units := []UnitInfo{
		{ID: u1, ParentJobID: job, MaxAgents: 2, ActiveAgentCount: 2},
		{ID: u2, ParentJobID: job, MaxAgents: 2, ActiveAgentCount: 2},
	}

	c.publishStarvation(units, nil, nil)

	if rec.starving[job] {
		t.Fatal("a job already at max_agents was reported as starving; the autoscaler " +
			"would rent an instance the allocator cannot give work to")
	}
}

// TestPublishStarvation_JobBelowMaxAgentsStarves: the cap only suppresses
// starvation when it is actually reached.
func TestPublishStarvation_JobBelowMaxAgentsStarves(t *testing.T) {
	job := uuid.New()
	unit := uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	c.publishStarvation(
		[]UnitInfo{{ID: unit, ParentJobID: job, MaxAgents: 4, ActiveAgentCount: 1}},
		nil, nil,
	)

	if !rec.starving[job] {
		t.Fatal("a job with 1 of 4 agent slots used has room and is starving")
	}
}

/*
 * TestPublishStarvation_CapCountsThisCycleAllocations.
 *
 * ActiveAgentCount is a snapshot from the start of the cycle. A job one slot
 * below its cap that just had that slot filled is full, and must not also be
 * reported as starving on the strength of a sibling unit.
 */
func TestPublishStarvation_CapCountsThisCycleAllocations(t *testing.T) {
	job := uuid.New()
	u1, u2 := uuid.New(), uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	units := []UnitInfo{
		{ID: u1, ParentJobID: job, MaxAgents: 2, ActiveAgentCount: 1},
		{ID: u2, ParentJobID: job, MaxAgents: 2, ActiveAgentCount: 1},
	}
	// u1 takes the last free slot this cycle; u2 is now capped, not starving.
	c.publishStarvation(units, []AgentInfo{{ID: 7}}, []Allocation{{UnitID: u1, AgentID: 7}})

	if rec.starving[job] {
		t.Fatal("the job hit its cap with this cycle's allocation; counting only " +
			"ActiveAgentCount would rent an instance for the slot just filled")
	}
}

// TestPublishStarvation_UnlimitedMaxAgentsAlwaysHasRoom: 0 means unlimited, and
// must not be read as "cap of zero, therefore always full".
func TestPublishStarvation_UnlimitedMaxAgentsAlwaysHasRoom(t *testing.T) {
	job := uuid.New()
	unit := uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	c.publishStarvation(
		[]UnitInfo{{ID: unit, ParentJobID: job, MaxAgents: 0, ActiveAgentCount: 12}},
		nil, nil,
	)

	if !rec.starving[job] {
		t.Fatal("max_agents=0 means unlimited; a job with 12 running agents and " +
			"unallocated work still wants more")
	}
}

/*
 * TestPublishStarvation_IdleOnPremExcludesAllocatedAgents.
 *
 * The autoscaler refuses to rent when idleOnPrem > 0. If that count included
 * agents that were just given work, then every cycle where all free capacity
 * got used would report "capacity is free" and the autoscaler would never fire.
 */
func TestPublishStarvation_IdleOnPremExcludesAllocatedAgents(t *testing.T) {
	job := uuid.New()
	u1, u2 := uuid.New(), uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	units := []UnitInfo{{ID: u1, ParentJobID: job}, {ID: u2, ParentJobID: job}}
	agents := []AgentInfo{{ID: 1}, {ID: 2}}
	// Both on-prem agents were used; u2 still has nothing.
	allocs := []Allocation{{UnitID: u1, AgentID: 1}, {UnitID: u1, AgentID: 2}}

	c.publishStarvation(units, agents, allocs)

	if rec.idleOnPrem != 0 {
		t.Fatalf("idleOnPrem = %d after every on-prem agent was allocated; the "+
			"autoscaler would read this as spare capacity and never rent", rec.idleOnPrem)
	}
	if !rec.starving[job] {
		t.Error("a unit with no allocation is still starving")
	}
}

// TestPublishStarvation_IdleOnPremCountsLeftovers: genuinely spare on-prem
// capacity must be reported, because free beats paid.
func TestPublishStarvation_IdleOnPremCountsLeftovers(t *testing.T) {
	job := uuid.New()
	unit := uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	c.publishStarvation(
		[]UnitInfo{{ID: unit, ParentJobID: job}},
		[]AgentInfo{{ID: 1}, {ID: 2}, {ID: 3}},
		[]Allocation{{UnitID: unit, AgentID: 1}},
	)

	if rec.idleOnPrem != 2 {
		t.Fatalf("idleOnPrem = %d, want 2 (three idle agents, one allocated)", rec.idleOnPrem)
	}
}

/*
 * TestPublishStarvation_CloudAgentsAreNotSpareCapacity.
 *
 * A rented instance between chunks is idle, but it is not free capacity — it is
 * capacity the client is already paying for. Counting it would let one
 * momentarily-idle instance suppress all further scaling for the very job that
 * bought it.
 */
func TestPublishStarvation_CloudAgentsAreNotSpareCapacity(t *testing.T) {
	job := uuid.New()
	unit := uuid.New()
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}

	c.publishStarvation(
		[]UnitInfo{{ID: unit, ParentJobID: job}},
		[]AgentInfo{{ID: 1, IsCloud: true}, {ID: 2, IsCloud: true}},
		nil,
	)

	if rec.idleOnPrem != 0 {
		t.Fatalf("idleOnPrem = %d; rented instances are not spare on-prem capacity", rec.idleOnPrem)
	}
}

// TestPublishStarvation_NoPublisherIsSafe: deployments without cloud
// provisioning leave this nil, and the cycle must not care.
func TestPublishStarvation_NoPublisherIsSafe(t *testing.T) {
	c := &Cycle{}
	unit := uuid.New()
	c.publishStarvation([]UnitInfo{{ID: unit, ParentJobID: uuid.New()}}, nil, nil)
}

/*
 * TestRunOnce_ErrorExitPublishesNothing.
 *
 * The autoscaler treats a fresh snapshot as "the scheduler ran and this is what
 * it saw". An aborted cycle saw nothing, so publishing an empty set from one
 * would assert that no job is starving on the strength of a failed database
 * call — and would keep the snapshot looking fresh for as long as the failure
 * persisted, hiding the outage from the one component built to notice it.
 *
 * Uses the cloud-lock load failure because it aborts before any repository is
 * touched, which is the only error exit a bare Cycle can reach.
 */
func TestRunOnce_ErrorExitPublishesNothing(t *testing.T) {
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}
	c.SetCloudLocks(failingLocks{err: errTestLockLoad})

	if _, err := c.RunOnce(t.Context()); err == nil {
		t.Fatal("a failed cloud lock load must abort the cycle")
	}
	if rec.calls != 0 {
		t.Fatalf("published %d time(s) from an error exit; a partial view of the "+
			"cycle must not be reported as a complete one", rec.calls)
	}
}

/*
 * TestRunOnce_SingleFlightSkipPublishesNothing.
 *
 * An overlapping RunOnce returns immediately having examined nothing. Reporting
 * from it would overwrite a real snapshot with an empty one and refresh the
 * freshness clock, so the autoscaler would act on a cycle that never happened.
 */
func TestRunOnce_SingleFlightSkipPublishesNothing(t *testing.T) {
	rec := &recordingPublisher{}
	c := &Cycle{starvation: rec}
	c.running.Store(true) // pretend a cycle is already in flight

	res, err := c.RunOnce(t.Context())
	if err != nil {
		t.Fatalf("a skipped overlapping cycle is not an error: %v", err)
	}
	if res.UnitsSchedulable != 0 || res.Allocations != 0 {
		t.Errorf("a skipped cycle must report nothing, got %+v", res)
	}
	if rec.calls != 0 {
		t.Fatalf("published %d time(s) from a skipped cycle", rec.calls)
	}
}
