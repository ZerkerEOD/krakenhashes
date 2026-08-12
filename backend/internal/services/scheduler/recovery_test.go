package scheduler

import (
	"context"
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
