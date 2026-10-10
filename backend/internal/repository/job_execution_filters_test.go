package repository

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * JobFilter additions for the redesign: hashlist_id, client_id and multi-status
 * filtering, plus the linkable client/workflow/creator columns on
 * JobExecutionWithUser and GetByIDWithUser.
 *
 * Shares the test database: run under `go test -p 1`, no t.Parallel().
 */
func TestJobExecutionRepository_EntityFilters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	database := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, database)
	testutil.SeedDefaults(t, database)
	testutil.SetTeamsEnabled(t, database, false)
	ctx := context.Background()

	user := testutil.CreateTestUser(t, database, "filt-user", "filt-user@test.local", testutil.DefaultTestPassword, "user")
	clientA := testutil.CreateTestClient(t, database, "Filter Client A", testutil.ClientCloudOpts{})
	clientB := testutil.CreateTestClient(t, database, "Filter Client B", testutil.ClientCloudOpts{})
	hlA := testutil.CreateTestHashlist(t, database, user.ID, clientA, "filt-hl-a")
	hlB := testutil.CreateTestHashlist(t, database, user.ID, clientB, "filt-hl-b")
	jobA1 := testutil.CreateTestJobExecution(t, database, hlA, user.ID, "running")
	testutil.CreateTestJobExecution(t, database, hlA, user.ID, "completed")
	testutil.CreateTestJobExecution(t, database, hlB, user.ID, "pending")

	repo := NewJobExecutionRepository(database)

	// hashlist_id
	rows, err := repo.ListWithFiltersAndUser(ctx, 50, 0, JobFilter{HashlistID: &hlA})
	if err != nil {
		t.Fatalf("list by hashlist: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("hashlist filter: want 2 jobs, got %d", len(rows))
	}
	for _, r := range rows {
		if r.ClientID == nil || *r.ClientID != clientA || r.ClientName == nil || *r.ClientName != "Filter Client A" {
			t.Fatalf("expected client A reference on job %s, got id=%v name=%v", r.ID, r.ClientID, r.ClientName)
		}
		if r.CreatedBy == nil || *r.CreatedBy != user.ID || r.CreatedByUsername == nil || *r.CreatedByUsername != "filt-user" {
			t.Fatalf("expected creator reference on job %s", r.ID)
		}
	}
	if n, err := repo.GetFilteredCount(ctx, JobFilter{HashlistID: &hlA}); err != nil || n != 2 {
		t.Fatalf("count by hashlist: want 2, got %d (%v)", n, err)
	}

	// client_id
	rows, err = repo.ListWithFiltersAndUser(ctx, 50, 0, JobFilter{ClientID: &clientB})
	if err != nil || len(rows) != 1 {
		t.Fatalf("client filter: want 1 job, got %d (%v)", len(rows), err)
	}

	// multi-status
	rows, err = repo.ListWithFiltersAndUser(ctx, 50, 0, JobFilter{Statuses: []string{"running", "pending"}})
	if err != nil || len(rows) != 2 {
		t.Fatalf("multi-status filter: want 2 jobs, got %d (%v)", len(rows), err)
	}
	counts, err := repo.GetStatusCountsFiltered(ctx, JobFilter{ClientID: &clientA})
	if err != nil || counts["running"] != 1 || counts["completed"] != 1 || counts["pending"] != 0 {
		t.Fatalf("status counts by client: got %v (%v)", counts, err)
	}

	// workflow link + GetByIDWithUser
	var wfID uuid.UUID
	if err := database.QueryRow(`INSERT INTO job_workflows (name) VALUES ('filt-wf') RETURNING id`).Scan(&wfID); err != nil {
		t.Fatalf("insert workflow: %v", err)
	}
	if err := repo.SetWorkflowID(ctx, jobA1, wfID); err != nil {
		t.Fatalf("set workflow id: %v", err)
	}
	one, err := repo.GetByIDWithUser(ctx, jobA1)
	if err != nil {
		t.Fatalf("GetByIDWithUser: %v", err)
	}
	if one.WorkflowID == nil || *one.WorkflowID != wfID || one.WorkflowName == nil || *one.WorkflowName != "filt-wf" {
		t.Fatalf("expected workflow reference, got id=%v name=%v", one.WorkflowID, one.WorkflowName)
	}
	if _, err := repo.GetByIDWithUser(ctx, uuid.New()); err != ErrNotFound {
		t.Fatalf("missing job: want ErrNotFound, got %v", err)
	}
}
