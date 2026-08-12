package scheduler

import (
	"context"
	"testing"
)

// TestFirstGap_PicksMidGapBeforeTail is the exact scenario GH #77 was reported
// as: "1-100 is complete, we stop a task at 105, but another task is already
// working on 150-200."
//
// The hole a stop leaves behind sits BEFORE the tail gap, so firstGap has to
// hand it back first. If it preferred the tail, the re-opened range would only
// be re-dispatched after every later range finished — and if the stopped task's
// interval were left 'assigned' instead of truncated (the stranding this fix
// removes), it would never be handed back at all and the unit would sit "fully
// covered but never complete".
func TestFirstGap_PicksMidGapBeforeTail(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)

	// [0,100) already done, [150,200) in flight on another agent. The hole
	// between them is [100,150); the tail gap is [200,1000).
	insertTestInterval(t, database, unitID, nil, 0, 100, "completed")
	insertTestInterval(t, database, unitID, nil, 150, 200, "assigned")

	gap, ok, err := firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap: %v", err)
	}
	if !ok {
		t.Fatal("expected a gap, got none")
	}
	if gap.Start != 100 || gap.End != 150 {
		t.Errorf("expected mid-gap [100,150), got [%d,%d) — the tail gap [200,1000) must not win", gap.Start, gap.End)
	}

	// Now dispatch that gap and stop the task at restore_point 105. Recovery
	// truncates the interval to [100,105) and completes it; the untouched
	// remainder [105,150) re-opens as a gap. Done as raw SQL (mirroring
	// applyRecovery's truncate UPDATE) to keep this a firstGap test.
	midIntervalID := insertTestInterval(t, database, unitID, nil, 100, 150, "assigned")
	if _, err := database.ExecContext(ctx, `
		UPDATE job_keyspace_intervals
		SET range_end = 105, status = 'completed'
		WHERE id = $1
	`, midIntervalID); err != nil {
		t.Fatalf("truncate interval to [100,105): %v", err)
	}

	gap, ok, err = firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap after truncate: %v", err)
	}
	if !ok {
		t.Fatal("expected a gap after truncate, got none")
	}
	if gap.Start != 105 || gap.End != 150 {
		t.Errorf("expected the stopped task's remainder [105,150), got [%d,%d)", gap.Start, gap.End)
	}
}

// TestFirstGap_TailOnly is the no-holes case: coverage runs from 0 and the only
// undispatched range is everything past it, bounded by the unit's
// base_keyspace (NOT effective_keyspace — chunking is in base units).
func TestFirstGap_TailOnly(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)

	insertTestInterval(t, database, unitID, nil, 0, 100, "completed")

	gap, ok, err := firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap: %v", err)
	}
	if !ok {
		t.Fatal("expected a tail gap, got none")
	}
	if gap.Start != 100 || gap.End != 1000 {
		t.Errorf("expected tail gap [100,1000), got [%d,%d)", gap.Start, gap.End)
	}
}

// TestFirstGap_IgnoresFailedIntervals covers the `status <> 'failed'` filter: a
// failed interval is not coverage, so the gap must swallow it whole rather than
// stopping at its start. This is the half of the query that works correctly
// today — the GH #77 bug was that an 'assigned' interval belonging to a parked
// 'pending' task is NOT failed, so it counted as coverage forever.
func TestFirstGap_IgnoresFailedIntervals(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)

	insertTestInterval(t, database, unitID, nil, 0, 100, "completed")
	insertTestInterval(t, database, unitID, nil, 150, 200, "failed")

	gap, ok, err := firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap: %v", err)
	}
	if !ok {
		t.Fatal("expected a gap, got none")
	}
	if gap.Start != 100 || gap.End != 1000 {
		t.Errorf("expected the gap to extend through the failed [150,200) to [100,1000), got [%d,%d)", gap.Start, gap.End)
	}
}
