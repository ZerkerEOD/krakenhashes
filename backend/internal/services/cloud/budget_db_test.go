package cloud

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

// defaultTestDatabaseURL mirrors testutil's fallback so the probe below targets
// the same server the harness will use.
const defaultTestDatabaseURL = "postgres://krakenhashes:krakenhashes@localhost:5432/krakenhashes_test?sslmode=disable"

// requireCloudTestDB returns a migrated test database or skips.
//
// Skipping rather than failing keeps the package's pure tests (budget_test.go,
// vpn_test.go) usable on a machine with no Postgres, which is most of what this
// package's coverage is.
func requireCloudTestDB(t *testing.T) *db.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("DB-backed cloud test skipped in -short mode")
	}

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = defaultTestDatabaseURL
	}
	probe, err := sql.Open("postgres", url)
	if err != nil {
		t.Skipf("no test database available (%v); set TEST_DATABASE_URL to run", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if perr := probe.PingContext(ctx); perr != nil {
		probe.Close()
		t.Skipf("no test database reachable at %s (%v)", url, perr)
	}
	probe.Close()

	return testutil.SetupTestDB(t)
}

func newEngine(t *testing.T) (*BudgetEngine, *db.DB) {
	t.Helper()
	database := requireCloudTestDB(t)
	return NewBudgetEngine(repository.NewCloudBudgetRepository(database)), database
}

func fundedTestClient(t *testing.T, database *db.DB, capCents int64) uuid.UUID {
	t.Helper()
	return testutil.CreateTestClient(t, database, "plan-"+uuid.NewString()[:8], testutil.ClientCloudOpts{
		CloudEnabled:      true,
		ProviderAllowlist: []string{"mock"},
		BudgetCents:       testutil.Int64Ptr(capCents),
	})
}

func newTestInstance(t *testing.T, database *db.DB, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	providerID := testutil.CreateTestCloudProviderConfig(t, database, "mock",
		"m-"+uuid.NewString()[:8], testutil.ProviderConfigOpts{})
	id, _ := testutil.CreateTestCloudInstance(t, database, providerID,
		testutil.InstanceOpts{ClientID: &clientID})
	return id
}

// TestPlanLaunch_TTLBoundedByBudget: the TTL is min(maxTTL, what the budget
// buys). This is NPK's campaign_max_price / spotPrice idea.
func TestPlanLaunch_TTLBoundedByBudget(t *testing.T) {
	engine, database := newEngine(t)
	ctx := context.Background()

	t.Run("budget is the binding constraint", func(t *testing.T) {
		// 200c at 100c/hr buys 2 hours; maxTTL of 8h does not bind.
		clientID := fundedTestClient(t, database, 200)
		plan, err := engine.PlanLaunch(ctx, clientID, 100, 8*time.Hour, 0)
		if err != nil {
			t.Fatalf("PlanLaunch: %v", err)
		}
		if plan.TTL != 2*time.Hour {
			t.Errorf("TTL = %s, want 2h (budget-bound)", plan.TTL)
		}
		if plan.ReserveCents != 200 {
			t.Errorf("ReserveCents = %d, want 200", plan.ReserveCents)
		}
	})

	t.Run("maxTTL is the binding constraint", func(t *testing.T) {
		// 10000c at 100c/hr buys 100 hours; maxTTL of 1h binds.
		clientID := fundedTestClient(t, database, 10_000)
		plan, err := engine.PlanLaunch(ctx, clientID, 100, time.Hour, 0)
		if err != nil {
			t.Fatalf("PlanLaunch: %v", err)
		}
		if plan.TTL != time.Hour {
			t.Errorf("TTL = %s, want 1h (maxTTL-bound)", plan.TTL)
		}
	})
}

// TestPlanLaunch_ExtraCentsSubtractedBeforeDividing: storage and bandwidth come
// off the top. Dividing first and subtracting after would buy a TTL the budget
// cannot actually cover once the non-compute charges land.
func TestPlanLaunch_ExtraCentsSubtractedBeforeDividing(t *testing.T) {
	engine, database := newEngine(t)
	ctx := context.Background()
	clientID := fundedTestClient(t, database, 1000)

	// 1000c cap, 400c of storage/bandwidth => 600c spendable => 6h at 100c/hr.
	plan, err := engine.PlanLaunch(ctx, clientID, 100, 24*time.Hour, 400)
	if err != nil {
		t.Fatalf("PlanLaunch: %v", err)
	}
	if plan.TTL != 6*time.Hour {
		t.Errorf("TTL = %s, want 6h ((1000-400)/100)", plan.TTL)
	}
	if plan.ReserveCents != 1000 {
		t.Errorf("ReserveCents = %d, want 1000 (600 compute + 400 extra)", plan.ReserveCents)
	}
}

func TestPlanLaunch_Refusals(t *testing.T) {
	engine, database := newEngine(t)
	ctx := context.Background()

	t.Run("extras alone exceed the budget", func(t *testing.T) {
		clientID := fundedTestClient(t, database, 100)
		_, err := engine.PlanLaunch(ctx, clientID, 100, time.Hour, 500)
		if !errors.Is(err, repository.ErrInsufficientBudget) {
			t.Errorf("err = %v, want ErrInsufficientBudget", err)
		}
	})

	t.Run("budget buys less than the minimum useful rental", func(t *testing.T) {
		// 4c at 100c/hr buys ~2.4 minutes; renting a GPU for that is pure waste.
		clientID := fundedTestClient(t, database, 4)
		_, err := engine.PlanLaunch(ctx, clientID, 100, time.Hour, 0)
		if !errors.Is(err, repository.ErrInsufficientBudget) {
			t.Errorf("err = %v, want ErrInsufficientBudget", err)
		}
	})

	t.Run("unfunded client", func(t *testing.T) {
		clientID := testutil.CreateTestClient(t, database, "unfunded-"+uuid.NewString()[:8],
			testutil.ClientCloudOpts{CloudEnabled: true})
		if _, err := engine.PlanLaunch(ctx, clientID, 100, time.Hour, 0); err == nil {
			t.Error("an unfunded client must not be able to plan a launch")
		}
	})

	t.Run("non-positive rate", func(t *testing.T) {
		clientID := fundedTestClient(t, database, 1000)
		if _, err := engine.PlanLaunch(ctx, clientID, 0, time.Hour, 0); err == nil {
			t.Error("a zero hourly rate must be refused, not treated as free")
		}
	})

	t.Run("non-positive maxTTL", func(t *testing.T) {
		clientID := fundedTestClient(t, database, 1000)
		if _, err := engine.PlanLaunch(ctx, clientID, 100, 0, 0); err == nil {
			t.Error("a zero maxTTL must be refused")
		}
	})
}

/*
 * TestPlanLaunch_NPKRegression is the property that names the bug this design
 * exists to avoid.
 *
 * NPK computed maxDuration = campaign_max_price / spotPrice and forgot to
 * multiply by instance count, so an N-node fleet silently got N times the
 * intended budget window. Here each instance reserves its own runway, so N
 * sequential plan+reserve cycles must sum to at most the cap — never N times it.
 */
func TestPlanLaunch_NPKRegression_SequentialLaunchesCannotExceedCap(t *testing.T) {
	engine, database := newEngine(t)
	ctx := context.Background()

	const cap int64 = 1000
	const rate = 100 // cents/hr
	clientID := fundedTestClient(t, database, cap)

	var launched int
	for i := 0; i < 20; i++ { // bounded; the budget should stop us well before this
		plan, err := engine.PlanLaunch(ctx, clientID, rate, time.Hour, 0)
		if err != nil {
			break // budget exhausted, which is the expected exit
		}
		instanceID := newTestInstance(t, database, clientID)
		if _, err := engine.Reserve(ctx, clientID, instanceID, nil, plan, "npk regression"); err != nil {
			break
		}
		launched++
	}

	if launched == 0 {
		t.Fatal("expected at least one launch to fit in a 1000c budget")
	}

	var committed int64
	if err := database.QueryRow(`
		SELECT COALESCE(SUM(cents),0) FROM cloud_spend_ledger
		WHERE client_id = $1 AND kind IN ('reservation','release','reconciliation')`,
		clientID).Scan(&committed); err != nil {
		t.Fatalf("sum ledger: %v", err)
	}

	if committed > cap {
		t.Errorf("committed %d cents against a %d cent cap after %d launches — "+
			"each instance must reserve its own runway", committed, cap, launched)
	}
	t.Logf("%d instances launched, %d/%d cents committed", launched, committed, cap)
}

// TestPlanLaunch_AllowOverageIgnoresTheCap pins the one path that deliberately
// spends past the cap: TTL is bounded only by the operator's maxTTL.
func TestPlanLaunch_AllowOverageIgnoresTheCap(t *testing.T) {
	engine, database := newEngine(t)
	ctx := context.Background()
	clientID := fundedTestClient(t, database, 10)

	repo := repository.NewCloudBudgetRepository(database)
	policy, err := repo.GetPolicy(ctx, clientID)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	policy.ClientID = &clientID
	policy.AllowOverage = true
	if err := repo.UpsertPolicy(ctx, policy); err != nil {
		t.Fatalf("UpsertPolicy: %v", err)
	}

	// 10c cap would normally buy far less than the 5-minute minimum.
	plan, err := engine.PlanLaunch(ctx, clientID, 100, 2*time.Hour, 0)
	if err != nil {
		t.Fatalf("allow_overage must permit the launch: %v", err)
	}
	if plan.TTL != 2*time.Hour {
		t.Errorf("TTL = %s, want the full 2h maxTTL under allow_overage", plan.TTL)
	}
	if plan.ReserveCents != 200 {
		t.Errorf("ReserveCents = %d, want 200 — the reservation is still recorded honestly",
			plan.ReserveCents)
	}
}

// TestAssess_ReflectsLedger walks the full round trip: reserve, accrue, settle.
func TestAssess_ReserveAccrueSettleRoundTrip(t *testing.T) {
	engine, database := newEngine(t)
	ctx := context.Background()
	clientID := fundedTestClient(t, database, 1000)
	instanceID := newTestInstance(t, database, clientID)

	plan, err := engine.PlanLaunch(ctx, clientID, 100, 4*time.Hour, 0)
	if err != nil {
		t.Fatalf("PlanLaunch: %v", err)
	}
	if _, err := engine.Reserve(ctx, clientID, instanceID, nil, plan, "round trip"); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	after, err := engine.Assess(ctx, clientID)
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if after.State.AvailableCents != 1000-plan.ReserveCents {
		t.Errorf("available = %d, want %d", after.State.AvailableCents, 1000-plan.ReserveCents)
	}

	// The instance ran one of its four hours, then was torn down. Settlement
	// goes through the repository directly here; SettleInstance's own arithmetic
	// is covered as a pure case in budget_test.go.
	repo := repository.NewCloudBudgetRepository(database)
	if err := repo.RecordIncurred(ctx, &clientID, instanceID, 100, "one hour"); err != nil {
		t.Fatalf("RecordIncurred: %v", err)
	}
	if err := repo.ReleaseUnused(ctx, &clientID, instanceID, plan.ReserveCents-100, "settled"); err != nil {
		t.Fatalf("ReleaseUnused: %v", err)
	}

	final, err := engine.Assess(ctx, clientID)
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if final.State.AvailableCents != 900 {
		t.Errorf("available after settlement = %d, want 900 (only the hour actually used)",
			final.State.AvailableCents)
	}
	if final.Action != BudgetActionNone {
		t.Errorf("action = %v, want none at 10%% usage", final.Action)
	}
}
