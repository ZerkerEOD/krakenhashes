package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

/*
 * Dispatch isolation is THE security property of cloud provisioning: a rented
 * instance runs on hardware the operator does not control, so it must only ever
 * receive work from the job — and therefore the client — that paid for it.
 *
 * These are pure tests over the extracted predicate. The DB-backed counterpart
 * (a full cycle with two clients) lives below.
 */

// alwaysTrue is a permissive base predicate, so any refusal comes from the
// isolation wrapper rather than from binary compatibility.
func alwaysTrue(uuid.UUID, int) bool { return true }

func TestWithCloudIsolation(t *testing.T) {
	jobA, jobB := uuid.New(), uuid.New()
	unitA, unitB, unitOrphan := uuid.New(), uuid.New(), uuid.New()

	units := map[uuid.UUID]*models.SchedulingUnit{
		unitA: {ID: unitA, ParentJobID: jobA},
		unitB: {ID: unitB, ParentJobID: jobB},
		// unitOrphan is deliberately absent from the map.
	}

	const cloudAgent = 1
	const onPremAgent = 2
	locks := map[int]uuid.UUID{cloudAgent: jobA}

	fn := withCloudIsolation(alwaysTrue, locks, units)

	tests := []struct {
		name    string
		unitID  uuid.UUID
		agentID int
		want    bool
	}{
		{"cloud agent gets its own job's unit", unitA, cloudAgent, true},
		{"cloud agent refused another job's unit", unitB, cloudAgent, false},
		// An unknown unit cannot be shown to belong to the right job. Guessing
		// costs one client's hashes on another client's rented hardware, so the
		// unknown case must fail closed.
		{"cloud agent refused a unit missing from the snapshot", unitOrphan, cloudAgent, false},
		{"on-prem agent unaffected on job A", unitA, onPremAgent, true},
		{"on-prem agent unaffected on job B", unitB, onPremAgent, true},
		{"on-prem agent unaffected on an unknown unit", unitOrphan, onPremAgent, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fn(tc.unitID, tc.agentID); got != tc.want {
				t.Errorf("compat(%s, %d) = %v, want %v", tc.unitID, tc.agentID, got, tc.want)
			}
		})
	}
}

// TestWithCloudIsolation_NoLocksIsPassThrough: with no cloud agents the wrapper
// must be a no-op, so on-prem-only deployments behave exactly as before.
func TestWithCloudIsolation_NoLocksIsPassThrough(t *testing.T) {
	unitID := uuid.New()
	units := map[uuid.UUID]*models.SchedulingUnit{unitID: {ID: unitID, ParentJobID: uuid.New()}}

	calls := 0
	base := func(uuid.UUID, int) bool { calls++; return true }

	for _, locks := range []map[int]uuid.UUID{nil, {}} {
		fn := withCloudIsolation(base, locks, units)
		if !fn(unitID, 7) {
			t.Error("with no locks the base predicate must decide")
		}
	}
	if calls != 2 {
		t.Errorf("base predicate called %d times, want 2 — the wrapper must delegate", calls)
	}
}

// TestWithCloudIsolation_DelegatesRefusal: the wrapper must not turn a base
// refusal (e.g. incompatible hashcat version) into an approval.
func TestWithCloudIsolation_DelegatesRefusal(t *testing.T) {
	jobA := uuid.New()
	unitA := uuid.New()
	units := map[uuid.UUID]*models.SchedulingUnit{unitA: {ID: unitA, ParentJobID: jobA}}

	fn := withCloudIsolation(
		func(uuid.UUID, int) bool { return false },
		map[int]uuid.UUID{1: jobA},
		units,
	)
	if fn(unitA, 1) {
		t.Error("a base refusal must survive the isolation wrapper")
	}
}

/*
 * TestWithCloudIsolation_SurvivesStaleBaseApproval is the regression guard for
 * the comment at the RunOnce call site.
 *
 * CompatCache's invalidation is unreliable: OnUnitChanged has no production
 * callers, OnAgentChanged only fires on connect/disconnect, and WarmAll
 * re-warms every 30 seconds. So a cached `true` for (foreign unit, cloud agent)
 * can persist for many cycles. Because IsFileMapReady fails open and a client
 * is registered before its first agent_status arrives, that stale entry could
 * hand a freshly-registered cloud agent another client's job on its very first
 * cycle.
 *
 * This test simulates exactly that: a base predicate that wrongly approves
 * everything, standing in for a stale cache. If someone "optimises" the wrapper
 * back inside CompatCache, this test fails.
 */
func TestWithCloudIsolation_SurvivesStaleBaseApproval(t *testing.T) {
	jobMine, jobTheirs := uuid.New(), uuid.New()
	unitMine, unitTheirs := uuid.New(), uuid.New()

	units := map[uuid.UUID]*models.SchedulingUnit{
		unitMine:   {ID: unitMine, ParentJobID: jobMine},
		unitTheirs: {ID: unitTheirs, ParentJobID: jobTheirs},
	}

	// A stale cache that approves every pair.
	staleCache := func(uuid.UUID, int) bool { return true }

	fn := withCloudIsolation(staleCache, map[int]uuid.UUID{99: jobMine}, units)

	if fn(unitTheirs, 99) {
		t.Fatal("a stale cached approval must NOT let a rented agent take another " +
			"client's job — this is why the wrapper lives outside CompatCache")
	}
	if !fn(unitMine, 99) {
		t.Error("the agent must still receive work from its own job")
	}
}

// TestWithCloudIsolation_MultipleCloudAgents: each rented agent is pinned to
// its own job independently.
func TestWithCloudIsolation_MultipleCloudAgents(t *testing.T) {
	jobA, jobB := uuid.New(), uuid.New()
	unitA, unitB := uuid.New(), uuid.New()

	units := map[uuid.UUID]*models.SchedulingUnit{
		unitA: {ID: unitA, ParentJobID: jobA},
		unitB: {ID: unitB, ParentJobID: jobB},
	}
	fn := withCloudIsolation(alwaysTrue, map[int]uuid.UUID{10: jobA, 20: jobB}, units)

	if !fn(unitA, 10) || !fn(unitB, 20) {
		t.Error("each cloud agent must receive its own job's work")
	}
	if fn(unitB, 10) || fn(unitA, 20) {
		t.Error("cloud agents must never cross over to each other's jobs")
	}
}

// failingLocks stands in for a database that cannot answer.
type failingLocks struct{ err error }

func (f failingLocks) LoadAgentJobLocks(context.Context) (map[int]uuid.UUID, error) {
	return nil, f.err
}

/*
 * TestRunOnce_FailsClosedWhenLocksUnavailable: if the lock snapshot cannot be
 * loaded, the cycle must abort rather than dispatch.
 *
 * Dispatching without the map would treat every rented agent as unrestricted,
 * which is precisely the leak the wrapper exists to prevent. Skipping a
 * three-second cycle costs nothing by comparison.
 */
func TestRunOnce_FailsClosedWhenLocksUnavailable(t *testing.T) {
	c := &Cycle{}
	c.SetCloudLocks(failingLocks{err: errors.New("database unavailable")})

	res, err := c.RunOnce(context.Background())
	if err == nil {
		t.Fatal("a failed cloud lock load must abort the cycle, not proceed unrestricted")
	}
	if !strings.Contains(err.Error(), "cloud agent job locks") {
		t.Errorf("error %q should name the cloud lock load as the cause", err)
	}
	if res.Allocations != 0 || res.Dispatched != 0 {
		t.Errorf("nothing may be allocated or dispatched when isolation cannot be enforced, got %+v", res)
	}
}
