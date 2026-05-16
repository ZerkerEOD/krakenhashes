package repository

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

// createSchedulerV2Prereqs sets up the minimum chain of rows the
// scheduler-v2 tests need to exist before they can create
// scheduling_units and keyspace intervals: a user, a hashlist, a preset
// job, and a job_execution. Returns the job_execution ID for use as the
// parent_job_id of test scheduling_units.
func createSchedulerV2Prereqs(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	user := testutil.CreateTestUser(t, database, "scheduler-v2-test", "scheduler-v2@test.local", testutil.DefaultTestPassword, "user")

	var hashlistID int64
	err := database.QueryRowContext(ctx, `
		INSERT INTO hashlists (name, user_id, hash_type_id, status)
		VALUES ('scheduler-v2-test', $1, 0, $2)
		RETURNING id
	`, user.ID, models.HashListStatusReady).Scan(&hashlistID)
	if err != nil {
		t.Fatalf("failed to create test hashlist: %v", err)
	}

	presetJobID := uuid.New()
	_, err = database.ExecContext(ctx, `
		INSERT INTO preset_jobs (id, name, attack_mode, priority, chunk_size_seconds)
		VALUES ($1, 'scheduler-v2-test', 0, 0, 60)
	`, presetJobID)
	if err != nil {
		t.Fatalf("failed to create test preset_job: %v", err)
	}

	jobExecutionID := uuid.New()
	_, err = database.ExecContext(ctx, `
		INSERT INTO job_executions (id, preset_job_id, hashlist_id, attack_mode)
		VALUES ($1, $2, $3, 0)
	`, jobExecutionID, presetJobID, hashlistID)
	if err != nil {
		t.Fatalf("failed to create test job_execution: %v", err)
	}

	return jobExecutionID
}

// newTestSchedulingUnit returns a SchedulingUnit struct with safe defaults
// suitable for repository tests. Callers can override fields before
// calling Create.
func newTestSchedulingUnit(parentJobID uuid.UUID) *models.SchedulingUnit {
	return &models.SchedulingUnit{
		ParentJobID:        parentJobID,
		LayerIndex:         0,
		Status:             models.SchedulingUnitStatusPending,
		Priority:           0,
		MaxAgents:          0,
		AttackMode:         0,
		EffectiveKeyspace:  1000,
		IsAccurateKeyspace: true,
	}
}
