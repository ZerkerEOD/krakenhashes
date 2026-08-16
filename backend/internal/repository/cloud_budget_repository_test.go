package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the spend ledger against real Postgres. The SQL IS the
// logic here — the committed-spend formula, the budget window and the
// SELECT ... FOR UPDATE reservation cannot be verified any other way.

func newBudgetRepo(t *testing.T) (*CloudBudgetRepository, *db.DB) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	return NewCloudBudgetRepository(database), database
}

// newInstanceFactory returns a function minting real cloud_instances rows for
// the given client. cloud_spend_ledger.cloud_instance_id carries a foreign key,
// so a reservation cannot reference an arbitrary UUID.
func newInstanceFactory(t *testing.T, database *db.DB, clientID uuid.UUID) func() uuid.UUID {
	t.Helper()
	providerID := testutil.CreateTestCloudProviderConfig(t, database, "mock",
		"m-"+uuid.NewString()[:8], testutil.ProviderConfigOpts{})
	return func() uuid.UUID {
		id, _ := testutil.CreateTestCloudInstance(t, database, providerID,
			testutil.InstanceOpts{ClientID: &clientID})
		return id
	}
}

// fundedClient creates a client with the given cap in cents.
func fundedClient(t *testing.T, database *db.DB, capCents int64) uuid.UUID {
	t.Helper()
	return testutil.CreateTestClient(t, database, "budget-"+uuid.NewString()[:8], testutil.ClientCloudOpts{
		CloudEnabled:      true,
		ProviderAllowlist: []string{"mock"},
		BudgetCents:       testutil.Int64Ptr(capCents),
	})
}

/*
 * TestReserve_ConcurrentRequestsCannotOverspend is the single most important
 * test in this package.
 *
 * Reserve computes available headroom and writes the reservation inside one
 * transaction, serialised by SELECT ... FOR UPDATE on the client row. Without
 * that lock, two provisioning decisions racing each other both read the same
 * headroom, both conclude they fit, and both launch — which for this feature
 * means two rented GPUs charged against a budget that only covered one.
 *
 * The outcome is deterministic even though the scheduling is not: with a cap
 * of 1000 and two requests of 600, exactly one must win.
 */
func TestReserve_ConcurrentRequestsCannotOverspend(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)
	newInstance := newInstanceFactory(t, database, clientID)

	const concurrent = 2
	const each int64 = 600

	// Instances are created up front: doing it inside the goroutines would add
	// unrelated INSERT contention to the window this test is measuring.
	instances := make([]uuid.UUID, concurrent)
	for i := range instances {
		instances[i] = newInstance()
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, concurrent)

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // release both goroutines as close to simultaneously as possible
			_, err := repo.Reserve(ctx, clientID, instances[idx], nil, each, false,
				"concurrency probe")
			errs[idx] = err
		}(i)
	}
	close(start)
	wg.Wait()

	var succeeded, rejected int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrInsufficientBudget):
			rejected++
		default:
			t.Fatalf("unexpected error from Reserve: %v", err)
		}
	}

	assert.Equal(t, 1, succeeded, "exactly one reservation should fit in a 1000c budget")
	assert.Equal(t, 1, rejected, "the loser must be rejected with ErrInsufficientBudget")

	var total int64
	require.NoError(t, database.QueryRow(
		`SELECT COALESCE(SUM(cents),0) FROM cloud_spend_ledger WHERE client_id = $1`,
		clientID).Scan(&total))
	assert.Equal(t, each, total, "ledger must contain exactly one reservation")
}

// TestReserve_ManyConcurrentRequestsRespectCap is the N-way variant: 8 requests
// of 200c against a 1000c cap must admit exactly 5 and land exactly on the cap.
func TestReserve_ManyConcurrentRequestsRespectCap(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)
	newInstance := newInstanceFactory(t, database, clientID)

	const concurrent = 8
	const each int64 = 200

	instances := make([]uuid.UUID, concurrent)
	for i := range instances {
		instances[i] = newInstance()
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, concurrent)

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			_, err := repo.Reserve(ctx, clientID, instances[idx], nil, each, false, "n-way probe")
			errs[idx] = err
		}(i)
	}
	close(start)
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		require.ErrorIs(t, err, ErrInsufficientBudget)
	}
	assert.Equal(t, 5, succeeded, "1000c cap divided by 200c requests admits exactly 5")

	var total int64
	require.NoError(t, database.QueryRow(
		`SELECT COALESCE(SUM(cents),0) FROM cloud_spend_ledger WHERE client_id = $1`,
		clientID).Scan(&total))
	assert.Equal(t, int64(1000), total, "committed spend must land exactly on the cap, never past it")
}

// TestReserve_RejectionWritesNothing proves the transaction rolls back. A
// rejected reservation that still left a ledger row would permanently consume
// budget for an instance that was never launched.
func TestReserve_RejectionWritesNothing(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 500)

	_, err := repo.Reserve(ctx, clientID, newInstanceFactory(t, database, clientID)(), nil, 900, false, "too big")
	require.ErrorIs(t, err, ErrInsufficientBudget)

	var n int
	require.NoError(t, database.QueryRow(
		`SELECT count(*) FROM cloud_spend_ledger WHERE client_id = $1`, clientID).Scan(&n))
	assert.Zero(t, n, "a rejected reservation must leave no ledger row")
}

// TestReserve_UnfundedClientIsDistinctFromExhausted: a client with no budget
// gets ErrNoBudget, not ErrInsufficientBudget. Callers surface these
// differently — one is a configuration gap, the other is a spent budget.
func TestReserve_UnfundedClientIsDistinctFromExhausted(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()

	unfunded := testutil.CreateTestClient(t, database, "unfunded", testutil.ClientCloudOpts{
		CloudEnabled: true,
	})

	_, err := repo.Reserve(ctx, unfunded, newInstanceFactory(t, database, unfunded)(), nil, 100, false, "no budget")
	require.ErrorIs(t, err, ErrNoBudget)
	assert.NotErrorIs(t, err, ErrInsufficientBudget)
}

// TestReserve_AllowOverageExceedsCap pins the one path that intentionally
// spends past the cap.
func TestReserve_AllowOverageExceedsCap(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 500)

	_, err := repo.Reserve(ctx, clientID, newInstanceFactory(t, database, clientID)(), nil, 900, true, "overage permitted")
	require.NoError(t, err, "allowOverage must permit a reservation beyond the cap")

	state, err := repo.GetBudgetState(ctx, clientID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), state.AvailableCents, "available is floored at zero, never negative")
	assert.Greater(t, state.UsedPct, 100.0, "used percentage reports the overage honestly")
}

func TestReserve_NegativeAmountRejected(t *testing.T) {
	repo, database := newBudgetRepo(t)
	clientID := fundedClient(t, database, 1000)

	_, err := repo.Reserve(context.Background(), clientID, newInstanceFactory(t, database, clientID)(), nil, -100, false, "negative")
	require.Error(t, err, "a negative reservation would manufacture budget")
}

/*
 * TestBudgetState_IncurredDoesNotReduceAvailability is the double-count guard.
 *
 * committed = SUM(reservation + release + reconciliation). `incurred` is
 * deliberately excluded: the reservation that covers it already reduced
 * availability. Counting both — as "cap - incurred - reservations" suggests —
 * would refuse launches while the budget was in fact half free.
 */
func TestBudgetState_IncurredDoesNotReduceAvailability(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)
	now := time.Now()

	instanceID, _ := testutil.CreateTestCloudInstance(t, database,
		testutil.CreateTestCloudProviderConfig(t, database, "mock", "m-"+uuid.NewString()[:8],
			testutil.ProviderConfigOpts{}),
		testutil.InstanceOpts{ClientID: &clientID})

	testutil.InsertLedgerEntry(t, database, clientID, &instanceID, 600, "reservation", now)
	testutil.InsertLedgerEntry(t, database, clientID, &instanceID, 300, "incurred", now)

	state, err := repo.GetBudgetState(ctx, clientID)
	require.NoError(t, err)

	assert.Equal(t, int64(400), state.AvailableCents,
		"available must be cap - reservations (1000-600), NOT cap - reservations - incurred")
	assert.Equal(t, int64(300), state.IncurredCents)
	assert.Equal(t, int64(300), state.ReservedCents,
		"outstanding = committed - incurred = 600-300")
	assert.InDelta(t, 60.0, state.UsedPct, 0.01, "used%% is committed/cap = 600/1000")
}

// TestBudgetState_ReleaseReturnsHeadroom: a release is stored negative so that
// summing the ledger yields committed spend directly.
func TestBudgetState_ReleaseReturnsHeadroom(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)
	now := time.Now()

	testutil.InsertLedgerEntry(t, database, clientID, nil, 800, "reservation", now)
	testutil.InsertLedgerEntry(t, database, clientID, nil, -500, "release", now)

	state, err := repo.GetBudgetState(ctx, clientID)
	require.NoError(t, err)
	assert.Equal(t, int64(700), state.AvailableCents, "cap - (800-500) = 700")
}

// TestBudgetState_ReconciliationIsSigned: a provider-authoritative correction
// may be negative and raises availability. Pinned so nobody "fixes" it.
func TestBudgetState_ReconciliationIsSigned(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)
	now := time.Now()

	testutil.InsertLedgerEntry(t, database, clientID, nil, 900, "reservation", now)
	testutil.InsertLedgerEntry(t, database, clientID, nil, -200, "reconciliation", now)

	state, err := repo.GetBudgetState(ctx, clientID)
	require.NoError(t, err)
	assert.Equal(t, int64(300), state.AvailableCents, "cap - (900-200) = 300")
}

/*
 * TestBudgetState_WindowExcludesPriorPeriod covers the billing-period boundary.
 *
 * The window is computed on read as recorded_at >= date_trunc('month', NOW()),
 * so last month's spend must not consume this month's budget. This is only
 * testable because the fixture writes an explicit recorded_at — the repository
 * API always uses NOW().
 */
func TestBudgetState_WindowExcludesPriorPeriod(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)

	now := time.Now()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// One entry safely inside the previous period, one exactly on the boundary.
	testutil.InsertLedgerEntry(t, database, clientID, nil, 900, "reservation",
		thisMonth.AddDate(0, 0, -5))
	testutil.InsertLedgerEntry(t, database, clientID, nil, 100, "reservation", thisMonth)

	state, err := repo.GetBudgetState(ctx, clientID)
	require.NoError(t, err)
	assert.Equal(t, int64(900), state.AvailableCents,
		"only the 100c entry at/after the period start counts; last month's 900c must not")
}

// TestGetPolicy_FallsBackToSystemDefault: a client with no override inherits
// the system-default row.
func TestGetPolicy_FallsBackToSystemDefault(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)

	policy, err := repo.GetPolicy(ctx, clientID)
	require.NoError(t, err)
	assert.Nil(t, policy.ClientID, "expected the system default row (client_id IS NULL)")
	assert.Equal(t, 95, policy.StopProvisionPct)
	assert.Equal(t, 100, policy.HardStopPct)
}

// TestUpsertPolicy_BothConflictTargets covers the two ON CONFLICT branches.
// They differ only in a string, and the system-default branch targets a partial
// unique index that plain ON CONFLICT (client_id) cannot reach — a distinction
// only real Postgres can make.
func TestUpsertPolicy_BothConflictTargets(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)

	t.Run("client override inserts then updates", func(t *testing.T) {
		p := &models.CloudBudgetPolicy{
			ClientID: &clientID, NotifyPct: intPtrLocal(50),
			StopProvisionPct: 90, DrainPct: 95, HardStopPct: 100, DrainTimeoutSeconds: 120,
		}
		require.NoError(t, repo.UpsertPolicy(ctx, p))
		first := p.ID

		p.NotifyPct = intPtrLocal(60)
		require.NoError(t, repo.UpsertPolicy(ctx, p))
		assert.Equal(t, first, p.ID, "second upsert must update the same row, not insert a new one")

		got, err := repo.GetPolicy(ctx, clientID)
		require.NoError(t, err)
		require.NotNil(t, got.ClientID)
		assert.Equal(t, 60, *got.NotifyPct)
	})

	t.Run("system default updates in place", func(t *testing.T) {
		p := &models.CloudBudgetPolicy{
			ClientID: nil, NotifyPct: intPtrLocal(70),
			StopProvisionPct: 93, DrainPct: 97, HardStopPct: 100, DrainTimeoutSeconds: 300,
		}
		require.NoError(t, repo.UpsertPolicy(ctx, p))

		var n int
		require.NoError(t, database.QueryRow(
			`SELECT count(*) FROM cloud_budget_policies WHERE client_id IS NULL`).Scan(&n))
		assert.Equal(t, 1, n, "the partial unique index must keep exactly one system default")
	})
}

// TestUpsertPolicy_RejectsInvertedLadder: the ordering validation must produce
// a readable message rather than a raw constraint violation.
func TestUpsertPolicy_RejectsInvertedLadder(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)

	cases := []struct {
		name   string
		policy *models.CloudBudgetPolicy
	}{
		{"notify above stop", &models.CloudBudgetPolicy{ClientID: &clientID,
			NotifyPct: intPtrLocal(99), StopProvisionPct: 90, DrainPct: 95, HardStopPct: 100}},
		{"stop above drain", &models.CloudBudgetPolicy{ClientID: &clientID,
			StopProvisionPct: 99, DrainPct: 95, HardStopPct: 100}},
		{"drain above hard stop", &models.CloudBudgetPolicy{ClientID: &clientID,
			StopProvisionPct: 90, DrainPct: 99, HardStopPct: 95}},
		{"negative drain timeout", &models.CloudBudgetPolicy{ClientID: &clientID,
			StopProvisionPct: 90, DrainPct: 95, HardStopPct: 100, DrainTimeoutSeconds: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, repo.UpsertPolicy(ctx, tc.policy))
		})
	}
}

// TestClientForJob resolves the paying client through the hashlist, which is
// how a projection finds a budget when the caller does not name one.
func TestClientForJob(t *testing.T) {
	repo, database := newBudgetRepo(t)
	ctx := context.Background()
	clientID := fundedClient(t, database, 1000)
	job := testutil.CreateCloudJob(t, database, clientID, true)

	got, err := repo.ClientForJob(ctx, job.JobID)
	require.NoError(t, err)
	assert.Equal(t, clientID, got)

	_, err = repo.ClientForJob(ctx, uuid.New())
	require.Error(t, err, "an unknown job must not silently resolve to a client")
}

func intPtrLocal(v int) *int { return &v }
