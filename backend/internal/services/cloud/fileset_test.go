package cloud

import (
	"context"
	"strings"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// pqArray adapts a string slice for a TEXT[] column.
func pqArray(v []string) interface{} { return pq.Array(v) }

/*
 * The job-scoped file set is the other half of the cloud security story.
 *
 * Dispatch isolation keeps a rented agent from being offered another client's
 * WORK; the file set keeps it from being handed another client's DATA. Client
 * potfiles are cracked plaintexts, so a ref naming the wrong client is the
 * difference between "10GB of the job's wordlists" and "every plaintext this
 * deployment has ever recovered, on a stranger's machine".
 */

// seedClientPotfile gives a client a potfile row so refs to it can resolve.
func seedClientPotfile(t *testing.T, database *db.DB, clientID uuid.UUID, size int64) {
	t.Helper()
	_, err := database.Exec(`
		INSERT INTO client_potfiles (client_id, file_path, file_size, md5_hash, line_count)
		VALUES ($1, $2, $3, $4, 0)`,
		clientID,
		"wordlists/clients/"+clientID.String()+"/potfile.txt",
		size, "deadbeef"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("seed client potfile: %v", err)
	}
}

// setUnitWordlistRefs replaces a job's scheduling-unit wordlist refs.
func setUnitWordlistRefs(t *testing.T, database *db.DB, jobID uuid.UUID, refs []string) {
	t.Helper()

	unitID := uuid.New()
	_, err := database.Exec(`
		INSERT INTO scheduling_units (
			id, parent_job_id, layer_index, status, attack_mode,
			effective_keyspace, base_keyspace, is_accurate_keyspace, wordlist_refs
		) VALUES ($1,$2,0,'pending',0,1000::numeric,1000::bigint,true,$3)`,
		unitID, jobID, pqArray(refs))
	if err != nil {
		t.Fatalf("create scheduling unit with refs: %v", err)
	}
}

/*
 * TestFileSet_RejectsForeignClientPotfile is the leak guard.
 *
 * A job belonging to client A must never resolve a file scoped to client B,
 * however the ref got there — a bug in unit construction, a copied preset, or
 * a deliberately crafted ref. The resolver is the last place that can tell,
 * because everything downstream just downloads what it is given.
 */
func TestFileSet_RejectsForeignClientPotfile(t *testing.T) {
	database := requireCloudTestDB(t)
	ctx := context.Background()

	clientA := testutil.CreateTestClient(t, database, "owner-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true, BudgetCents: testutil.Int64Ptr(10_000)})
	clientB := testutil.CreateTestClient(t, database, "victim-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true, BudgetCents: testutil.Int64Ptr(10_000)})

	seedClientPotfile(t, database, clientA, 1024)
	seedClientPotfile(t, database, clientB, 4096)

	job := testutil.CreateCloudJob(t, database, clientA, true)

	// The job legitimately references its OWN client's potfile, and — the leak —
	// also references the other client's.
	setUnitWordlistRefs(t, database, job.JobID, []string{
		"wordlists/clients/" + clientA.String() + "/potfile.txt",
		"wordlists/clients/" + clientB.String() + "/potfile.txt",
	})

	resolver := NewFileSetResolver(database)
	set, err := resolver.Resolve(ctx, job.JobID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	var sawOwn, sawForeign bool
	for _, f := range set.Files {
		if strings.Contains(f.Name, clientA.String()) {
			sawOwn = true
		}
		if strings.Contains(f.Name, clientB.String()) {
			sawForeign = true
		}
	}

	if !sawOwn {
		t.Error("the job's own client potfile must still resolve")
	}
	if sawForeign {
		t.Fatalf("LEAK: client %s's potfile was included in a file set for client %s's job. "+
			"This ships cracked plaintexts to a rented third-party machine.", clientB, clientA)
	}
}

// TestFileSet_RejectsForeignClientWordlist covers the same guard for
// client-scoped wordlists, which are engagement-specific corpora.
func TestFileSet_RejectsForeignClientWordlist(t *testing.T) {
	database := requireCloudTestDB(t)
	ctx := context.Background()

	clientA := testutil.CreateTestClient(t, database, "owner-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true, BudgetCents: testutil.Int64Ptr(10_000)})
	clientB := testutil.CreateTestClient(t, database, "victim-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true, BudgetCents: testutil.Int64Ptr(10_000)})

	for _, c := range []uuid.UUID{clientA, clientB} {
		_, err := database.Exec(`
			INSERT INTO client_wordlists (client_id, file_path, file_name, file_size, md5_hash, line_count)
			VALUES ($1, $2, 'corp.txt', 2048, $3, 0)`,
			c, "wordlists/clients/"+c.String()+"/corp.txt", "cafe"+uuid.NewString()[:8])
		if err != nil {
			t.Fatalf("seed client wordlist: %v", err)
		}
	}

	job := testutil.CreateCloudJob(t, database, clientA, true)
	setUnitWordlistRefs(t, database, job.JobID, []string{
		"wordlists/clients/" + clientB.String() + "/corp.txt",
	})

	set, err := NewFileSetResolver(database).Resolve(ctx, job.JobID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, f := range set.Files {
		if strings.Contains(f.Name, clientB.String()) {
			t.Fatalf("LEAK: client %s's wordlist reached a file set for client %s's job",
				clientB, clientA)
		}
	}
}

// TestFileSet_ResolvesOwnClientFiles is the counterpart: the guard must not be
// so strict that a job cannot use its own client's data.
func TestFileSet_ResolvesOwnClientFiles(t *testing.T) {
	database := requireCloudTestDB(t)
	ctx := context.Background()

	clientA := testutil.CreateTestClient(t, database, "owner-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{CloudEnabled: true, BudgetCents: testutil.Int64Ptr(10_000)})
	seedClientPotfile(t, database, clientA, 512)

	job := testutil.CreateCloudJob(t, database, clientA, true)
	setUnitWordlistRefs(t, database, job.JobID, []string{
		"wordlists/clients/" + clientA.String() + "/potfile.txt",
	})

	set, err := NewFileSetResolver(database).Resolve(ctx, job.JobID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	var found bool
	for _, f := range set.Files {
		if strings.Contains(f.Name, clientA.String()) {
			found = true
			// Size and MD5 must be populated or the agent silently skips both
			// its disk pre-check and its download verification.
			if f.Size <= 0 {
				t.Errorf("file %s has Size %d; the agent's disk pre-check only "+
					"fires when Size > 0", f.Name, f.Size)
			}
			if f.MD5Hash == "" {
				t.Errorf("file %s has no MD5; the agent only verifies downloads "+
					"when MD5Hash is set — on an untrusted host that matters", f.Name)
			}
		}
	}
	if !found {
		t.Error("a job must be able to use its own client's potfile")
	}
}
