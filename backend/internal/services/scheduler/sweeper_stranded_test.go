package scheduler

import (
	"context"
	"testing"
	"time"
)

// strandedTaskAge is comfortably past the 120s age guard the tests pass to
// RecoverStrandedPendingTasks, so the row looks abandoned rather than in-flight.
const strandedTaskAge = -10 * time.Minute

// TestRecoverStrandedPendingTasks_ReclaimsStrandedRange is the GH #77
// regression. The stranded shape is a task parked 'pending' with agent_id NULL
// (what ClearTaskAgentAndSetPending left behind when the stop-ack beat the
// stopped-progress) while its keyspace interval stayed 'assigned'.
//
// That combination is invisible to every other recovery path: the dispatcher
// never reclaims a 'pending' task, and firstGap counts any non-failed interval
// as coverage — so the unit reads as fully tiled while nobody is working the
// range, and the job hangs at "covered but never complete" forever.
func TestRecoverStrandedPendingTasks_ReclaimsStrandedRange(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:       "pending",
		RangeStart:   0,
		RangeEnd:     500,
		RestorePoint: int64Ptr(200),
		UpdatedAt:    time.Now().Add(strandedTaskAge),
	})
	intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 500, "assigned")
	// A second, genuinely in-flight interval past the stranded one, so the
	// reclaimed range comes back as a MID gap rather than just a longer tail.
	insertTestInterval(t, database, unitID, nil, 500, 700, "assigned")

	// Before the sweep: [0,500) reads as covered, so the only gap is the tail.
	// This is the symptom — no agent is on [0,500) and nothing will re-issue it.
	gap, ok, err := firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap before sweep: %v", err)
	}
	if !ok || gap.Start != 700 || gap.End != 1000 {
		t.Fatalf("test setup: expected the stranded range to look covered (only the tail gap [700,1000) left), got [%d,%d) ok=%v", gap.Start, gap.End, ok)
	}

	recovered, errs := RecoverStrandedPendingTasks(ctx, database, 120)
	if len(errs) != 0 {
		t.Fatalf("RecoverStrandedPendingTasks returned errors: %v", errs)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}

	task := readTaskState(t, database, taskID)
	if task.Status != "completed" {
		t.Errorf("task status = %q, want completed (restore_point advanced past range_start)", task.Status)
	}

	iv := readIntervalState(t, database, intervalID)
	if iv.RangeEnd != 200 || iv.Status != "completed" {
		t.Errorf("interval = [%d,%d) %s, want [0,200) completed", iv.RangeStart, iv.RangeEnd, iv.Status)
	}

	// And the unworked remainder is a real gap again, ahead of the tail.
	gap, ok, err = firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap after sweep: %v", err)
	}
	if !ok || gap.Start != 200 || gap.End != 500 {
		t.Errorf("expected the reclaimed remainder [200,500) to be the first gap, got [%d,%d) ok=%v", gap.Start, gap.End, ok)
	}

	assertNoStrandedIntervals(t, database)
}

// TestRecoverStrandedPendingTasks_IsIdempotent verifies the sweep is safe to run
// on every tick: a recovered row is terminal and its interval is
// completed/failed, so it stops matching the predicate and there is nothing
// left to re-recover.
func TestRecoverStrandedPendingTasks_IsIdempotent(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:       "pending",
		RangeStart:   0,
		RangeEnd:     500,
		RestorePoint: int64Ptr(200),
		UpdatedAt:    time.Now().Add(strandedTaskAge),
	})
	insertTestInterval(t, database, unitID, &taskID, 0, 500, "assigned")

	if recovered, errs := RecoverStrandedPendingTasks(ctx, database, 120); recovered != 1 || len(errs) != 0 {
		t.Fatalf("first sweep: recovered = %d errs = %v, want 1 and none", recovered, errs)
	}

	recovered, errs := RecoverStrandedPendingTasks(ctx, database, 120)
	if len(errs) != 0 {
		t.Fatalf("second sweep returned errors: %v", errs)
	}
	if recovered != 0 {
		t.Errorf("second sweep recovered = %d, want 0", recovered)
	}

	assertNoStrandedIntervals(t, database)
}

// TestRecoverStrandedPendingTasks_LeavesRecentTasksAlone covers the age guard.
// A stop that is still in flight briefly looks exactly like a stranded row —
// the ack has cleared agent_id but the stopped-progress hasn't landed yet — so
// the sweep must not touch anything younger than the heartbeat timeout.
func TestRecoverStrandedPendingTasks_LeavesRecentTasksAlone(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:       "pending",
		RangeStart:   0,
		RangeEnd:     500,
		RestorePoint: int64Ptr(200),
	})
	intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 500, "assigned")

	recovered, errs := RecoverStrandedPendingTasks(ctx, database, 120)
	if len(errs) != 0 {
		t.Fatalf("RecoverStrandedPendingTasks returned errors: %v", errs)
	}
	if recovered != 0 {
		t.Errorf("recovered = %d, want 0 — a just-updated row may still be an in-flight stop", recovered)
	}

	if task := readTaskState(t, database, taskID); task.Status != "pending" {
		t.Errorf("task status = %q, want pending (untouched)", task.Status)
	}
	if iv := readIntervalState(t, database, intervalID); iv.Status != "assigned" {
		t.Errorf("interval status = %q, want assigned (untouched)", iv.Status)
	}
}

// TestRecoverStrandedPendingTasks_IgnoresTasksWithoutInterval covers the
// deliberate INNER JOIN. A v2 task sitting 'pending' with NO live interval is
// not stranded — its range is already a gap the dispatcher will re-issue, and
// failing the task would only add noise to the job's task list.
func TestRecoverStrandedPendingTasks_IgnoresTasksWithoutInterval(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:       "pending",
		RangeStart:   0,
		RangeEnd:     500,
		RestorePoint: int64Ptr(200),
		UpdatedAt:    time.Now().Add(strandedTaskAge),
	})

	recovered, errs := RecoverStrandedPendingTasks(ctx, database, 120)
	if len(errs) != 0 {
		t.Fatalf("RecoverStrandedPendingTasks returned errors: %v", errs)
	}
	if recovered != 0 {
		t.Errorf("recovered = %d, want 0 — a task with no interval row is not stranded", recovered)
	}

	if task := readTaskState(t, database, taskID); task.Status != "pending" {
		t.Errorf("task status = %q, want pending (untouched)", task.Status)
	}
}
