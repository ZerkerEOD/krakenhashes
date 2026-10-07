package scheduler

import (
	"context"
	"testing"
	"time"
)

// The sweeper split (WS1, network-share feature): a still-'assigned' task —
// dispatched but not yet reporting progress because the agent is downloading
// a large wordlist/rule/binary — is held to the LONG startup grace, while a
// 'running' task (first progress arrived) keeps the SHORT heartbeat timeout.
//
// Before the split, EvictTimedOutTasks applied the 120s heartbeat to
// 'assigned' tasks too, so a multi-minute file pull tripped it and the chunk
// was evicted and reissued mid-download. These tests pin the corrected
// behavior. They use a real test DB and skip when none is reachable.

const (
	graceHeartbeatSeconds = 120
	graceStartupSeconds   = 600
)

// TestEvictTimedOutTasks_AssignedWithinStartupGraceNotEvicted is the core
// regression: an assigned task older than the heartbeat but within the
// startup grace must survive.
func TestEvictTimedOutTasks_AssignedWithinStartupGraceNotEvicted(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	// 3 minutes silent: past the 120s heartbeat, comfortably inside the 600s
	// startup grace — the shape of an agent mid-download.
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:         "assigned",
		RangeStart:     0,
		RangeEnd:       500,
		LastActivityAt: time.Now().Add(-3 * time.Minute),
	})
	insertTestInterval(t, database, unitID, &taskID, 0, 500, "assigned")

	evicted, errs := EvictTimedOutTasks(ctx, database, graceHeartbeatSeconds, graceStartupSeconds)
	if len(errs) != 0 {
		t.Fatalf("EvictTimedOutTasks returned errors: %v", errs)
	}
	if len(evicted) != 0 {
		t.Fatalf("evicted %d tasks, want 0 — an assigned task within the startup grace must survive", len(evicted))
	}
	if n := taskRowCount(t, database, taskID); n != 1 {
		t.Fatalf("task row count = %d, want 1 (task must not be deleted)", n)
	}
	if st := readTaskState(t, database, taskID); st.Status != "assigned" {
		t.Errorf("task status = %q, want assigned (untouched)", st.Status)
	}
}

// TestEvictTimedOutTasks_AssignedPastStartupGraceEvicted: once an assigned
// task has been silent past the startup grace (a genuinely stalled prep, no
// task_loading pings), it is evicted. With no restore point that is a discard
// — the task + interval rows are deleted so the range re-opens.
func TestEvictTimedOutTasks_AssignedPastStartupGraceEvicted(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:         "assigned",
		RangeStart:     0,
		RangeEnd:       500,
		LastActivityAt: time.Now().Add(-11 * time.Minute), // past the 600s grace
	})
	insertTestInterval(t, database, unitID, &taskID, 0, 500, "assigned")

	evicted, errs := EvictTimedOutTasks(ctx, database, graceHeartbeatSeconds, graceStartupSeconds)
	if len(errs) != 0 {
		t.Fatalf("EvictTimedOutTasks returned errors: %v", errs)
	}
	if len(evicted) != 1 {
		t.Fatalf("evicted %d tasks, want 1 (assigned task past the startup grace)", len(evicted))
	}
	if !evicted[0].Discarded {
		t.Errorf("eviction outcome = %+v, want Discarded (no restore point)", evicted[0])
	}
	if n := taskRowCount(t, database, taskID); n != 0 {
		t.Errorf("task row count = %d, want 0 (discarded task is deleted)", n)
	}
}

// TestEvictTimedOutTasks_RunningPastHeartbeatEvicted confirms the split keeps
// the short heartbeat for running tasks: once first progress arrived the
// startup window is over, so a running task silent past the heartbeat is
// evicted even though it is well within the startup grace.
func TestEvictTimedOutTasks_RunningPastHeartbeatEvicted(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:         "running",
		RangeStart:     0,
		RangeEnd:       500,
		LastActivityAt: time.Now().Add(-3 * time.Minute), // > 120s heartbeat, < 600s grace
	})
	insertTestInterval(t, database, unitID, &taskID, 0, 500, "running")

	evicted, errs := EvictTimedOutTasks(ctx, database, graceHeartbeatSeconds, graceStartupSeconds)
	if len(errs) != 0 {
		t.Fatalf("EvictTimedOutTasks returned errors: %v", errs)
	}
	if len(evicted) != 1 {
		t.Fatalf("evicted %d tasks, want 1 — a running task past the heartbeat must be evicted", len(evicted))
	}
	if n := taskRowCount(t, database, taskID); n != 0 {
		t.Errorf("task row count = %d, want 0 (discarded)", n)
	}
}

// TestEvictTimedOutTasks_RunningWithinHeartbeatNotEvicted: a healthy running
// task that reported progress recently is left alone.
func TestEvictTimedOutTasks_RunningWithinHeartbeatNotEvicted(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)
	unitID := createTestUnit(t, database, jobID, 1000)
	taskID := insertTestTask(t, database, jobID, unitID, testTask{
		Status:         "running",
		RangeStart:     0,
		RangeEnd:       500,
		LastActivityAt: time.Now().Add(-30 * time.Second), // well within 120s
	})
	insertTestInterval(t, database, unitID, &taskID, 0, 500, "running")

	evicted, errs := EvictTimedOutTasks(ctx, database, graceHeartbeatSeconds, graceStartupSeconds)
	if len(errs) != 0 {
		t.Fatalf("EvictTimedOutTasks returned errors: %v", errs)
	}
	if len(evicted) != 0 {
		t.Fatalf("evicted %d tasks, want 0", len(evicted))
	}
	if n := taskRowCount(t, database, taskID); n != 1 {
		t.Errorf("task row count = %d, want 1 (healthy running task untouched)", n)
	}
}
