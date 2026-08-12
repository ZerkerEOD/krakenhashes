package scheduler

import (
	"context"
	"errors"
	"testing"
)

// TestRecoverTaskByID_TruncatesOnProgress is the truncate-and-complete branch:
// the agent got as far as restore_point before it was stopped, so that work is
// kept. The interval shrinks to [range_start, restore_point) and the task is
// completed at 100% OF THE WORK IT ACTUALLY DID — not 100% of the range it was
// originally handed. The untouched remainder re-opens as a gap.
func TestRecoverTaskByID_TruncatesOnProgress(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		RangeStart:   100,
		RangeEnd:     200,
		RestorePoint: int64Ptr(150),
	})
	intervalID := insertTestInterval(t, database, unitID, &taskID, 100, 200, "assigned")
	// Neighbours on both sides, so the remainder has to come back as a MID gap
	// rather than merging into the tail.
	insertTestInterval(t, database, unitID, nil, 0, 100, "completed")
	insertTestInterval(t, database, unitID, nil, 200, 300, "assigned")

	res, err := RecoverTaskByID(ctx, database, taskID, "chunk time limit exceeded")
	if err != nil {
		t.Fatalf("RecoverTaskByID: %v", err)
	}
	if !res.Handled {
		t.Fatal("scheduler-v2 task must be Handled")
	}
	if !res.Truncated {
		t.Error("progress was made, so the interval should have been truncated")
	}

	task := readTaskState(t, database, taskID)
	if task.Status != "completed" {
		t.Errorf("task status = %q, want completed", task.Status)
	}
	if task.DetailedStatus != "completed_no_cracks" {
		t.Errorf("detailed_status = %q, want completed_no_cracks (crack_count was 0)", task.DetailedStatus)
	}
	if !task.RangeEnd.Valid || task.RangeEnd.Int64 != 150 {
		t.Errorf("range_end = %v, want 150 (truncated to restore_point)", task.RangeEnd)
	}
	if task.KeyspaceEnd != 150 {
		t.Errorf("keyspace_end = %d, want 150", task.KeyspaceEnd)
	}
	if !task.RestorePoint.Valid || task.RestorePoint.Int64 != 150 {
		t.Errorf("restore_point = %v, want 150", task.RestorePoint)
	}
	if !task.KeyspaceProcessed.Valid || task.KeyspaceProcessed.Int64 != 50 {
		t.Errorf("keyspace_processed = %v, want 50 (restore_point - range_start)", task.KeyspaceProcessed)
	}
	if task.ProgressPercent != 100 {
		t.Errorf("progress_percent = %v, want 100 (the task fully covered its new smaller range)", task.ProgressPercent)
	}
	if !task.FailureReason.Valid || task.FailureReason.String != "chunk time limit exceeded" {
		t.Errorf("failure_reason = %v, want the caller's reason", task.FailureReason)
	}

	iv := readIntervalState(t, database, intervalID)
	if iv.RangeStart != 100 || iv.RangeEnd != 150 {
		t.Errorf("interval = [%d,%d), want [100,150)", iv.RangeStart, iv.RangeEnd)
	}
	if iv.Status != "completed" {
		t.Errorf("interval status = %q, want completed", iv.Status)
	}

	// The remainder must come back as a gap, ahead of the tail.
	gap, ok, err := firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap: %v", err)
	}
	if !ok || gap.Start != 150 || gap.End != 200 {
		t.Errorf("expected the unprocessed remainder [150,200) to re-open as the first gap, got [%d,%d) ok=%v", gap.Start, gap.End, ok)
	}

	assertNoStrandedIntervals(t, database)
}

// TestRecoverTaskByID_TruncateWithCracksSetsDetailedStatus is the other half of
// the truncate branch's detailed_status CASE. SendJobStop stamps 'stopping' on
// the row when it issues the stop, so recovery has to overwrite it or the
// finished task displays "stopping" forever.
func TestRecoverTaskByID_TruncateWithCracksSetsDetailedStatus(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		RangeStart:   0,
		RangeEnd:     500,
		RestorePoint: int64Ptr(200),
		CrackCount:   3,
	})
	insertTestInterval(t, database, unitID, &taskID, 0, 500, "running")

	if _, err := RecoverTaskByID(ctx, database, taskID, "preempted by higher priority"); err != nil {
		t.Fatalf("RecoverTaskByID: %v", err)
	}

	task := readTaskState(t, database, taskID)
	if task.Status != "completed" {
		t.Errorf("task status = %q, want completed", task.Status)
	}
	if task.DetailedStatus != "completed_with_cracks" {
		t.Errorf("detailed_status = %q, want completed_with_cracks (crack_count was 3)", task.DetailedStatus)
	}

	assertNoStrandedIntervals(t, database)
}

// TestRecoverTaskByID_FailsWithoutProgress is the no-progress branch: the agent
// never advanced past its own range_start, so there is nothing to keep. Task
// and interval both go 'failed', which re-opens the WHOLE range (the exclusion
// constraint and firstGap both ignore failed intervals).
func TestRecoverTaskByID_FailsWithoutProgress(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	cases := []struct {
		name         string
		restorePoint *int64
	}{
		{name: "restore_point never reported", restorePoint: nil},
		{name: "restore_point equals range_start", restorePoint: int64Ptr(100)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobID := createTestJobExecution(t, database)
			unitID := createTestUnit(t, database, jobID, 1000)
			taskID := insertTestTask(t, database, jobID, unitID, testTask{
				RangeStart:   100,
				RangeEnd:     200,
				RestorePoint: tc.restorePoint,
			})
			intervalID := insertTestInterval(t, database, unitID, &taskID, 100, 200, "assigned")

			res, err := RecoverTaskByID(ctx, database, taskID, "heartbeat timeout")
			if err != nil {
				t.Fatalf("RecoverTaskByID: %v", err)
			}
			if !res.Handled {
				t.Fatal("scheduler-v2 task must be Handled")
			}
			if res.Truncated {
				t.Error("no progress was made, so nothing should have been truncated")
			}

			task := readTaskState(t, database, taskID)
			if task.Status != "failed" {
				t.Errorf("task status = %q, want failed", task.Status)
			}
			if task.DetailedStatus != "failed" {
				t.Errorf("detailed_status = %q, want failed", task.DetailedStatus)
			}
			if !task.FailureReason.Valid || task.FailureReason.String != "heartbeat timeout" {
				t.Errorf("failure_reason = %v, want the caller's reason", task.FailureReason)
			}

			iv := readIntervalState(t, database, intervalID)
			if iv.Status != "failed" {
				t.Errorf("interval status = %q, want failed", iv.Status)
			}

			// The full range re-opens, not just a remainder.
			gap, ok, err := firstGap(ctx, database, unitID)
			if err != nil {
				t.Fatalf("firstGap: %v", err)
			}
			if !ok || gap.Start != 0 || gap.End != 1000 {
				t.Errorf("expected the whole unit to be a gap again, got [%d,%d) ok=%v", gap.Start, gap.End, ok)
			}

			assertNoStrandedIntervals(t, database)
		})
	}
}

// TestRecoverTaskByID_IsIdempotent covers both branches being re-run. The stop
// paths race by design — the agent sends BOTH a final job_progress
// {status:"stopped"} and a task_stop_ack, and the heartbeat sweeper is a third
// caller — so a second recovery must not undo the first one's outcome.
func TestRecoverTaskByID_IsIdempotent(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	t.Run("truncate branch", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 1000)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			RangeStart:   100,
			RangeEnd:     200,
			RestorePoint: int64Ptr(150),
		})
		intervalID := insertTestInterval(t, database, unitID, &taskID, 100, 200, "assigned")

		if _, err := RecoverTaskByID(ctx, database, taskID, "agent stopped by operator"); err != nil {
			t.Fatalf("first RecoverTaskByID: %v", err)
		}
		if _, err := RecoverTaskByID(ctx, database, taskID, "heartbeat timeout"); err != nil {
			t.Fatalf("second RecoverTaskByID: %v", err)
		}

		task := readTaskState(t, database, taskID)
		if task.Status != "completed" {
			t.Errorf("task status = %q after second recovery, want completed — a completed task must never be flipped to failed", task.Status)
		}
		if task.DetailedStatus != "completed_no_cracks" {
			t.Errorf("detailed_status = %q after second recovery, want completed_no_cracks", task.DetailedStatus)
		}
		if !task.RangeEnd.Valid || task.RangeEnd.Int64 != 150 {
			t.Errorf("range_end = %v after second recovery, want 150 (unchanged)", task.RangeEnd)
		}

		iv := readIntervalState(t, database, intervalID)
		if iv.RangeEnd != 150 || iv.Status != "completed" {
			t.Errorf("interval = [%d,%d) %s after second recovery, want [100,150) completed", iv.RangeStart, iv.RangeEnd, iv.Status)
		}

		assertNoStrandedIntervals(t, database)
	})

	t.Run("fail branch", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 1000)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			RangeStart: 0,
			RangeEnd:   300,
		})
		intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 300, "assigned")

		if _, err := RecoverTaskByID(ctx, database, taskID, "heartbeat timeout"); err != nil {
			t.Fatalf("first RecoverTaskByID: %v", err)
		}
		if _, err := RecoverTaskByID(ctx, database, taskID, "heartbeat timeout"); err != nil {
			t.Fatalf("second RecoverTaskByID: %v", err)
		}

		task := readTaskState(t, database, taskID)
		if task.Status != "failed" {
			t.Errorf("task status = %q after second recovery, want failed", task.Status)
		}
		if task.DetailedStatus != "failed" {
			t.Errorf("detailed_status = %q after second recovery, want failed", task.DetailedStatus)
		}

		iv := readIntervalState(t, database, intervalID)
		if iv.RangeStart != 0 || iv.RangeEnd != 300 || iv.Status != "failed" {
			t.Errorf("interval = [%d,%d) %s after second recovery, want [0,300) failed", iv.RangeStart, iv.RangeEnd, iv.Status)
		}

		assertNoStrandedIntervals(t, database)
	})
}

// TestRecoverTaskByID_DoesNotResurrectCancelledTask covers the terminal guard
// added with GH #77. The operator-stop path CANCELS the task first
// (JobSchedulingService.StopJob) and only then sends job_stop, so the agent's
// stop-ack arrives against an already-cancelled row. Without the
// `status NOT IN ('completed','cancelled')` guard on the truncate UPDATE, that
// ack would resurrect the cancelled task as 'completed'.
//
// The interval is still truncated — it is guarded on the INTERVAL's status, not
// the task's — so the unprocessed remainder correctly re-opens as a gap.
func TestRecoverTaskByID_DoesNotResurrectCancelledTask(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:       "cancelled",
		RangeStart:   100,
		RangeEnd:     200,
		RestorePoint: int64Ptr(150),
	})
	intervalID := insertTestInterval(t, database, unitID, &taskID, 100, 200, "assigned")

	if _, err := RecoverTaskByID(ctx, database, taskID, "agent stopped by operator"); err != nil {
		t.Fatalf("RecoverTaskByID: %v", err)
	}

	task := readTaskState(t, database, taskID)
	if task.Status != "cancelled" {
		t.Errorf("task status = %q, want cancelled — recovery must not resurrect a terminal task", task.Status)
	}

	iv := readIntervalState(t, database, intervalID)
	if iv.RangeEnd != 150 || iv.Status != "completed" {
		t.Errorf("interval = [%d,%d) %s, want [100,150) completed — the work done still counts", iv.RangeStart, iv.RangeEnd, iv.Status)
	}

	assertNoStrandedIntervals(t, database)
}

// assertIntervalUntouched fails unless every column readIntervalState selects
// is exactly what it was before the recovery ran.
//
// updated_at is the load-bearing one: job_keyspace_intervals carries a BEFORE
// UPDATE trigger that stamps NOW(), so an unmoved updated_at proves no
// statement wrote the row at all — a strictly stronger claim than "the values
// happen to still be the same".
func assertIntervalUntouched(t *testing.T, before, after intervalState) {
	t.Helper()

	if after.RangeStart != before.RangeStart ||
		after.RangeEnd != before.RangeEnd ||
		after.Status != before.Status ||
		!after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("interval was written to: before [%d,%d) %s updated_at=%s; after [%d,%d) %s updated_at=%s",
			before.RangeStart, before.RangeEnd, before.Status, before.UpdatedAt,
			after.RangeStart, after.RangeEnd, after.Status, after.UpdatedAt)
	}
}

// TestRecoverStoppedTaskByID_CompletesFullyProcessedTaskWithBookedCoverage is
// the GH #79 incident, replayed row for row.
//
// The live row: range [0,3), restore_point 3 (the WHOLE range processed),
// crack_count 1, the crack handshake counters all satisfied, status still
// 'processing' — and its interval already flipped to 'completed' by
// HashlistCompletionService.stopJobTasks' job-wide, task-status-blind cascade.
// Then the agent disconnected.
//
// Before the fix, applyRecovery took its truncate branch, the interval UPDATE
// matched 0 rows because of `AND status IN ('assigned','running')`, and that
// "0 rows" was read as "no progress" — so a task that had searched its entire
// range and cracked the last hash in the list was written off. (In the
// incident it then went 'failed', poisoning the job via HasFailedTasks.)
//
// The correct answer is the covered branch: the coverage is booked, so the
// interval must not be touched at all, and the task that produced that
// coverage must say 'completed'.
func TestRecoverStoppedTaskByID_CompletesFullyProcessedTaskWithBookedCoverage(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 3)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:                  "processing",
		RangeStart:              0,
		RangeEnd:                3,
		RestorePoint:            int64Ptr(3),
		CrackCount:              1,
		ExpectedCrackCount:      1,
		ReceivedCrackCount:      1,
		BatchesCompleteSignaled: true,
	})
	intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 3, "completed")

	before := readIntervalState(t, database, intervalID)

	res, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected")
	if err != nil {
		t.Fatalf("RecoverStoppedTaskByID: %v", err)
	}

	want := RecoverResult{Handled: true, Completed: true}
	if res != want {
		t.Errorf("RecoverResult = %+v, want %+v — the range was already accounted for, so nothing was truncated and nothing was discarded", res, want)
	}

	task := readTaskState(t, database, taskID)
	if task.Status != "completed" {
		t.Errorf("task status = %q, want completed — it processed its whole range and cracked the last hash", task.Status)
	}
	if task.DetailedStatus != "completed_with_cracks" {
		t.Errorf("detailed_status = %q, want completed_with_cracks (crack_count was 1)", task.DetailedStatus)
	}
	if task.ProgressPercent != 100 {
		t.Errorf("progress_percent = %v, want 100", task.ProgressPercent)
	}
	if !task.RestorePoint.Valid || task.RestorePoint.Int64 != 3 {
		t.Errorf("restore_point = %v, want 3", task.RestorePoint)
	}
	if !task.RangeEnd.Valid || task.RangeEnd.Int64 != 3 {
		t.Errorf("range_end = %v, want 3 — the full range was processed, so nothing should have been clipped off it", task.RangeEnd)
	}
	if !task.KeyspaceProcessed.Valid || task.KeyspaceProcessed.Int64 != 3 {
		t.Errorf("keyspace_processed = %v, want 3 (range_end - range_start)", task.KeyspaceProcessed)
	}

	// The task row must survive: it is the only thing left that says which
	// task produced this coverage, and hashes.cracked_by_task_id points at it.
	if n := taskRowCount(t, database, taskID); n != 1 {
		t.Errorf("task row count = %d, want 1 — recovery must never delete a task whose crack attribution matters", n)
	}

	// The interval is the coverage ledger and it already reads 'completed'.
	// Touching it either way is a bug: shrinking it un-covers searched
	// keyspace, deleting it re-issues finished work.
	assertIntervalUntouched(t, before, readIntervalState(t, database, intervalID))

	// ...and therefore the searched range must not re-open as a gap.
	gap, ok, err := firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap: %v", err)
	}
	if ok {
		t.Errorf("unit re-opened gap [%d,%d) — the whole keyspace was searched and booked, so there must be no gap", gap.Start, gap.End)
	}

	assertNoStrandedIntervals(t, database)
}

// TestRecoverStoppedTaskByID_CancelsNoProgressTaskWithBookedCoverage is the
// other half of the covered branch: same booked coverage, but this task never
// advanced past its own range_start, so it did not produce that coverage and
// must not claim 'completed'.
//
// crack_count is 0 on purpose. Under PolicyDiscardOnNoProgress the no-progress
// branch would DELETE a crack-free task outright, so "the row survived" is the
// discriminator that proves the covered branch ran — and it must survive,
// because deleting it would orphan a 'completed' interval with nothing left to
// say which task owns it.
func TestRecoverStoppedTaskByID_CancelsNoProgressTaskWithBookedCoverage(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 3)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:       "processing",
		RangeStart:   0,
		RangeEnd:     3,
		RestorePoint: int64Ptr(0), // never moved off the start line
	})
	intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 3, "completed")

	before := readIntervalState(t, database, intervalID)

	res, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected")
	if err != nil {
		t.Fatalf("RecoverStoppedTaskByID: %v", err)
	}

	want := RecoverResult{Handled: true}
	if res != want {
		t.Errorf("RecoverResult = %+v, want %+v — no progress to complete, nothing truncated, nothing discarded", res, want)
	}

	task := readTaskState(t, database, taskID)
	if task.Status != "cancelled" {
		t.Errorf("task status = %q, want cancelled — it made no progress, but the range IS covered so it must not be 'failed' either", task.Status)
	}
	if task.DetailedStatus != "cancelled" {
		t.Errorf("detailed_status = %q, want cancelled", task.DetailedStatus)
	}
	if n := taskRowCount(t, database, taskID); n != 1 {
		t.Errorf("task row count = %d, want 1 — deleting it would orphan the 'completed' interval it is the only owner of", n)
	}

	assertIntervalUntouched(t, before, readIntervalState(t, database, intervalID))
	assertNoStrandedIntervals(t, database)
}

// TestRecoverStoppedTaskByID_ClampsRestorePointToTruncatedInterval pins the
// endPoint clamp. An earlier recovery already shortened this interval to
// [0,50) and the range beyond it has since been handed to somebody else, so
// the honest thing this task can claim is 50 — not the 80 hashcat reported,
// which would double-count [50,80) against whoever owns it now.
func TestRecoverStoppedTaskByID_ClampsRestorePointToTruncatedInterval(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		RangeStart:   0,
		RangeEnd:     100,
		RestorePoint: int64Ptr(80),
	})
	intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 50, "assigned")

	res, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected")
	if err != nil {
		t.Fatalf("RecoverStoppedTaskByID: %v", err)
	}

	want := RecoverResult{Handled: true, Truncated: true}
	if res != want {
		t.Errorf("RecoverResult = %+v, want %+v", res, want)
	}

	iv := readIntervalState(t, database, intervalID)
	if iv.RangeStart != 0 || iv.RangeEnd != 50 || iv.Status != "completed" {
		t.Errorf("interval = [%d,%d) %s, want [0,50) completed — the clamp must not grow an already-truncated interval back out", iv.RangeStart, iv.RangeEnd, iv.Status)
	}

	task := readTaskState(t, database, taskID)
	if task.Status != "completed" {
		t.Errorf("task status = %q, want completed", task.Status)
	}
	if !task.RangeEnd.Valid || task.RangeEnd.Int64 != 50 {
		t.Errorf("range_end = %v, want 50 (clamped to the interval's end), NOT 80 (the raw restore_point)", task.RangeEnd)
	}
	if !task.RestorePoint.Valid || task.RestorePoint.Int64 != 50 {
		t.Errorf("restore_point = %v, want 50 (clamped)", task.RestorePoint)
	}
	if !task.KeyspaceProcessed.Valid || task.KeyspaceProcessed.Int64 != 50 {
		t.Errorf("keyspace_processed = %v, want 50 — claiming 80 would extrapolate the task past keyspace it no longer owns", task.KeyspaceProcessed)
	}
	if task.ProgressPercent != 100 {
		t.Errorf("progress_percent = %v, want 100 (fully done for its new smaller range)", task.ProgressPercent)
	}

	gap, ok, err := firstGap(ctx, database, unitID)
	if err != nil {
		t.Fatalf("firstGap: %v", err)
	}
	if !ok || gap.Start != 50 || gap.End != 1000 {
		t.Errorf("first gap = [%d,%d) ok=%v, want [50,1000) — everything past the clamp re-opens", gap.Start, gap.End, ok)
	}

	assertNoStrandedIntervals(t, database)
}

// TestRecoverStoppedTaskByID_FailedIntervalKeepsDiscardBehaviour is the
// regression guard for PR #80. The covered branch added by GH #79 must not
// have changed anything about the case where the interval says NOT covered.
//
// The third subtest pins the decision applyRecovery documents as deliberately
// NOT taken: even when restore_point says the agent processed the whole range,
// a 'failed' interval means that range is going to be redone by another task,
// so completing this one would double-count it in every coverage / progress /
// AreAllTasksComplete query — the exact mirror image of the GH #79 bug.
func TestRecoverStoppedTaskByID_FailedIntervalKeepsDiscardBehaviour(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	t.Run("no restore point and no cracks discards the row", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 1000)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			RangeStart: 0,
			RangeEnd:   100,
		})
		intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 100, "failed")

		res, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected")
		if err != nil {
			t.Fatalf("RecoverStoppedTaskByID: %v", err)
		}

		want := RecoverResult{Handled: true, Discarded: true}
		if res != want {
			t.Errorf("RecoverResult = %+v, want %+v", res, want)
		}
		if n := taskRowCount(t, database, taskID); n != 0 {
			t.Errorf("task row count = %d, want 0 — a benign stop with nothing to preserve leaves no 'failed' row to poison the job", n)
		}

		// The interval DELETE is scoped to assigned/running, so the failed
		// interval stays as evidence; it re-opens the range either way
		// because coverage ignores 'failed'.
		iv := readIntervalState(t, database, intervalID)
		if iv.Status != "failed" {
			t.Errorf("interval status = %q, want failed", iv.Status)
		}

		gap, ok, err := firstGap(ctx, database, unitID)
		if err != nil {
			t.Fatalf("firstGap: %v", err)
		}
		if !ok || gap.Start != 0 || gap.End != 1000 {
			t.Errorf("first gap = [%d,%d) ok=%v, want the whole unit [0,1000) back", gap.Start, gap.End, ok)
		}

		assertNoStrandedIntervals(t, database)
	})

	t.Run("cracks present cancel instead of deleting", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 1000)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			RangeStart: 0,
			RangeEnd:   100,
			CrackCount: 2,
		})
		insertTestInterval(t, database, unitID, &taskID, 0, 100, "failed")

		res, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected")
		if err != nil {
			t.Fatalf("RecoverStoppedTaskByID: %v", err)
		}

		want := RecoverResult{Handled: true}
		if res != want {
			t.Errorf("RecoverResult = %+v, want %+v — the delete guard blocked the discard", res, want)
		}
		if n := taskRowCount(t, database, taskID); n != 1 {
			t.Errorf("task row count = %d, want 1 — deleting it would NULL hashes.cracked_by_task_id and drop the plaintexts from the loopback delta", n)
		}

		task := readTaskState(t, database, taskID)
		if task.Status != "cancelled" {
			t.Errorf("task status = %q, want cancelled (never 'failed' — HasFailedTasks is a COUNT(*) > 0)", task.Status)
		}
		if task.DetailedStatus != "cancelled" {
			t.Errorf("detailed_status = %q, want cancelled", task.DetailedStatus)
		}

		assertNoStrandedIntervals(t, database)
	})

	t.Run("full restore point over a failed interval still does not complete", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 1000)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			RangeStart:   0,
			RangeEnd:     100,
			RestorePoint: int64Ptr(100),
		})
		insertTestInterval(t, database, unitID, &taskID, 0, 100, "failed")

		res, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected")
		if err != nil {
			t.Fatalf("RecoverStoppedTaskByID: %v", err)
		}

		if res.Completed || res.Truncated {
			t.Errorf("RecoverResult = %+v, want neither Completed nor Truncated — the coverage ledger says this range is NOT covered, so the task must not claim it", res)
		}
		if n := taskRowCount(t, database, taskID); n != 0 {
			t.Errorf("task row count = %d, want 0 (discarded); it must never be resurrected as 'completed' on top of a range that is about to be redone", n)
		}

		assertNoStrandedIntervals(t, database)
	})
}

// TestRecoverStoppedTaskByID_NewBranchesAreIdempotent re-runs each of the
// branches above. The stop paths race by design — the agent sends both a final
// job_progress {status:"stopped"} and a task_stop_ack, and the heartbeat
// sweeper is a third caller — so a second recovery must never move the row
// further, and must never come back with an error the caller treats as fatal.
func TestRecoverStoppedTaskByID_NewBranchesAreIdempotent(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	t.Run("covered branch, completed", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 3)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			Status:       "processing",
			RangeStart:   0,
			RangeEnd:     3,
			RestorePoint: int64Ptr(3),
			CrackCount:   1,
		})
		intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 3, "completed")

		if _, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected"); err != nil {
			t.Fatalf("first RecoverStoppedTaskByID: %v", err)
		}
		first := readTaskState(t, database, taskID)
		firstInterval := readIntervalState(t, database, intervalID)

		if _, err := RecoverStoppedTaskByID(ctx, database, taskID, "heartbeat timeout"); err != nil {
			t.Fatalf("second RecoverStoppedTaskByID: %v", err)
		}

		second := readTaskState(t, database, taskID)
		if second != first {
			t.Errorf("task changed on the second recovery: %+v -> %+v", first, second)
		}
		assertIntervalUntouched(t, firstInterval, readIntervalState(t, database, intervalID))
	})

	t.Run("covered branch, cancelled", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 3)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			Status:       "processing",
			RangeStart:   0,
			RangeEnd:     3,
			RestorePoint: int64Ptr(0),
		})
		intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 3, "completed")

		if _, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected"); err != nil {
			t.Fatalf("first RecoverStoppedTaskByID: %v", err)
		}
		first := readTaskState(t, database, taskID)
		firstInterval := readIntervalState(t, database, intervalID)

		if _, err := RecoverStoppedTaskByID(ctx, database, taskID, "heartbeat timeout"); err != nil {
			t.Fatalf("second RecoverStoppedTaskByID: %v", err)
		}

		second := readTaskState(t, database, taskID)
		if second != first {
			t.Errorf("task changed on the second recovery: %+v -> %+v", first, second)
		}
		if second.Status != "cancelled" {
			t.Errorf("task status = %q after the second recovery, want cancelled", second.Status)
		}
		assertIntervalUntouched(t, firstInterval, readIntervalState(t, database, intervalID))
	})

	t.Run("clamped truncate branch", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 1000)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			RangeStart:   0,
			RangeEnd:     100,
			RestorePoint: int64Ptr(80),
		})
		intervalID := insertTestInterval(t, database, unitID, &taskID, 0, 50, "assigned")

		if _, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected"); err != nil {
			t.Fatalf("first RecoverStoppedTaskByID: %v", err)
		}
		first := readTaskState(t, database, taskID)
		firstInterval := readIntervalState(t, database, intervalID)

		if _, err := RecoverStoppedTaskByID(ctx, database, taskID, "heartbeat timeout"); err != nil {
			t.Fatalf("second RecoverStoppedTaskByID: %v", err)
		}

		second := readTaskState(t, database, taskID)
		if second != first {
			t.Errorf("task changed on the second recovery: %+v -> %+v", first, second)
		}
		assertIntervalUntouched(t, firstInterval, readIntervalState(t, database, intervalID))
	})

	t.Run("discard branch reports the row is gone", func(t *testing.T) {
		jobID := createTestJobExecution(t, database)
		unitID := createTestUnit(t, database, jobID, 1000)
		taskID := insertTestTask(t, database, jobID, unitID, testTask{
			RangeStart: 0,
			RangeEnd:   100,
		})
		insertTestInterval(t, database, unitID, &taskID, 0, 100, "failed")

		if _, err := RecoverStoppedTaskByID(ctx, database, taskID, "agent disconnected"); err != nil {
			t.Fatalf("first RecoverStoppedTaskByID: %v", err)
		}

		// The row is gone, so the second caller gets ErrTaskGone. That is the
		// documented benign outcome of losing this race — callers log it at
		// Info — not a failure to be retried.
		_, err := RecoverStoppedTaskByID(ctx, database, taskID, "heartbeat timeout")
		if !errors.Is(err, ErrTaskGone) {
			t.Errorf("second RecoverStoppedTaskByID error = %v, want one satisfying errors.Is(err, ErrTaskGone)", err)
		}
		if n := taskRowCount(t, database, taskID); n != 0 {
			t.Errorf("task row count = %d, want 0 — the second recovery must not recreate anything", n)
		}
	})
}
