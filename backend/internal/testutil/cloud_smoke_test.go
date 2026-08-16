package testutil

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// requireTestDB skips when no test database is reachable, matching the
// ping-probe pattern used by the scheduler and completion-service tests. That
// is the only one of the repo's four gating styles that behaves correctly both
// locally and in CI.
func requireTestDB(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	if os.Getenv("TEST_DATABASE_URL") == "" {
		// Fall through to the default DSN; SetupTestDB fails loudly if it is
		// unreachable, which is what we want locally.
		return
	}
}

// TestHarnessTruncatesCloudTables is the regression guard for the bug this
// harness change fixes: the previous hardcoded truncate list omitted every
// cloud table, so rows survived cleanup and broke later tests in the package.
func TestHarnessTruncatesCloudTables(t *testing.T) {
	requireTestDB(t)
	database := SetupTestDB(t)

	clientID := CreateTestClient(t, database, "truncate-check", ClientCloudOpts{
		CloudEnabled:      true,
		ProviderAllowlist: []string{"mock"},
		BudgetCents:       Int64Ptr(1000),
	})
	providerID := CreateTestCloudProviderConfig(t, database, "mock", "truncate-mock", ProviderConfigOpts{})
	instanceID, label := CreateTestCloudInstance(t, database, providerID, InstanceOpts{
		ClientID:      &clientID,
		ReservedCents: 500,
	})
	InsertLedgerEntry(t, database, clientID, &instanceID, 500, "reservation", time.Now())

	if label == "" {
		t.Fatal("expected a generated instance label")
	}

	for _, table := range []string{"clients", "cloud_provider_configs", "cloud_instances", "cloud_spend_ledger"} {
		var n int
		if err := database.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n == 0 {
			t.Fatalf("expected rows in %s before truncation", table)
		}
	}

	TruncateAll(t, database)

	for _, table := range []string{"clients", "cloud_provider_configs", "cloud_instances", "cloud_spend_ledger"} {
		var n int
		if err := database.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatalf("count %s after truncate: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d row(s) after TruncateAll", table, n)
		}
	}
}

// TestSeedDefaultsRestoresBudgetPolicy guards the trap that makes truncation
// dangerous: the system-default cloud budget policy comes from a migration, so
// truncating without re-seeding leaves GetPolicy with no fallback and every
// budget decision fails.
func TestSeedDefaultsRestoresBudgetPolicy(t *testing.T) {
	requireTestDB(t)
	database := SetupTestDB(t)

	var n int
	if err := database.QueryRow(
		"SELECT count(*) FROM cloud_budget_policies WHERE client_id IS NULL").Scan(&n); err != nil {
		t.Fatalf("count default policy: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 system-default budget policy after setup, got %d", n)
	}

	TruncateAll(t, database)
	SeedDefaults(t, database)

	if err := database.QueryRow(
		"SELECT count(*) FROM cloud_budget_policies WHERE client_id IS NULL").Scan(&n); err != nil {
		t.Fatalf("count default policy after reseed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the default budget policy to survive truncate+reseed, got %d", n)
	}
}

// TestCreateCloudJobLinksClient guards the reason a cloud-specific job fixture
// exists: every cloud query resolves the paying client through
// hashlists.client_id, which the scheduler's own prereq helper never sets.
func TestCreateCloudJobLinksClient(t *testing.T) {
	requireTestDB(t)
	database := SetupTestDB(t)

	clientID := CreateTestClient(t, database, "job-client", ClientCloudOpts{
		CloudEnabled:      true,
		ProviderAllowlist: []string{"mock"},
		BudgetCents:       Int64Ptr(5000),
	})
	job := CreateCloudJob(t, database, clientID, true)

	var resolved uuid.UUID
	err := database.QueryRow(`
		SELECT h.client_id
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.id = $1`, job.JobID).Scan(&resolved)
	if err != nil {
		t.Fatalf("resolve client for job: %v", err)
	}
	if resolved != clientID {
		t.Fatalf("job resolved to client %s, want %s", resolved, clientID)
	}

	var burst bool
	if err := database.QueryRow(
		"SELECT cloud_burst_enabled FROM job_executions WHERE id = $1", job.JobID).Scan(&burst); err != nil {
		t.Fatalf("read cloud_burst_enabled: %v", err)
	}
	if !burst {
		t.Fatal("expected cloud_burst_enabled=true on the created job")
	}
}

// TestProviderAckFixture proves the acknowledgement fixture writes the shape
// the handler's gate reads, so client-level Vast.ai opt-in can be tested.
func TestProviderAckFixture(t *testing.T) {
	requireTestDB(t)
	database := SetupTestDB(t)

	clientID := CreateTestClient(t, database, "ack-client", ClientCloudOpts{
		AckProviders: []string{"vastai", "aws"},
	})

	var hasVast, hasAWS bool
	err := database.QueryRow(`
		SELECT provider_ack ? 'vastai', provider_ack ? 'aws'
		FROM clients WHERE id = $1`, clientID).Scan(&hasVast, &hasAWS)
	if err != nil {
		t.Fatalf("read provider_ack: %v", err)
	}
	if !hasVast || !hasAWS {
		t.Fatalf("expected both acknowledgements to merge, got vastai=%v aws=%v", hasVast, hasAWS)
	}
}
