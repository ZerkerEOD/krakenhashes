package cloud

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * fakeProvider is a hand-written Provider stand-in, matching the scheduler
 * package's narrow-fake style rather than introducing a mocking framework.
 *
 * Only Provider is faked. The reaper is tested against the REAL repository
 * because its interesting bugs are state-transition and SQL bugs, which a
 * repository fake would paper over.
 */
type fakeProvider struct {
	mu sync.Mutex

	// inventory is what ListOwned reports, keyed by label.
	inventory map[string]InstanceStatus
	// statuses is what Status reports, keyed by provider instance ID.
	statuses map[string]InstanceStatus

	listErr    error
	destroyErr error

	destroyed []string
	statusCnt int
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		inventory: map[string]InstanceStatus{},
		statuses:  map[string]InstanceStatus{},
	}
}

func (f *fakeProvider) Name() models.CloudProvider { return models.CloudProviderMock }

func (f *fakeProvider) SearchOffers(context.Context, OfferQuery) ([]Offer, error) {
	return nil, errors.New("not used by the reaper")
}

func (f *fakeProvider) Launch(context.Context, LaunchRequest) (*LaunchResult, error) {
	return nil, errors.New("not used by the reaper")
}

func (f *fakeProvider) Destroy(_ context.Context, providerInstanceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.destroyErr != nil {
		return f.destroyErr
	}
	f.destroyed = append(f.destroyed, providerInstanceID)
	return nil
}

func (f *fakeProvider) Status(_ context.Context, providerInstanceID string) (*InstanceStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusCnt++
	if s, ok := f.statuses[providerInstanceID]; ok {
		return &s, nil
	}
	return &InstanceStatus{State: "running"}, nil
}

func (f *fakeProvider) ListOwned(context.Context) (map[string]InstanceStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make(map[string]InstanceStatus, len(f.inventory))
	for k, v := range f.inventory {
		out[k] = v
	}
	return out, nil
}

func (f *fakeProvider) CostSoFar(context.Context, string) (int64, bool, error) {
	return 0, false, nil
}

func (f *fakeProvider) Preflight(context.Context) (*PreflightReport, error) {
	return &PreflightReport{OK: true}, nil
}

func (f *fakeProvider) wasDestroyed(providerInstanceID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range f.destroyed {
		if id == providerInstanceID {
			return true
		}
	}
	return false
}

// recordingNotifier captures escalations so the alert path is assertable.
type recordingNotifier struct {
	mu       sync.Mutex
	teardown []int // attempt counts
	budget   []BudgetAction
}

func (n *recordingNotifier) CloudTeardownFailed(_ context.Context, _ *models.CloudInstance, attempts int, _ error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.teardown = append(n.teardown, attempts)
}

func (n *recordingNotifier) CloudBudgetThreshold(_ context.Context, _ uuid.UUID, action BudgetAction, _ string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.budget = append(n.budget, action)
}

func (n *recordingNotifier) teardownAlerts() []int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]int(nil), n.teardown...)
}

// reaperFixture wires a reaper against the real repositories and one fake
// provider.
type reaperFixture struct {
	reaper    *Reaper
	provider  *fakeProvider
	notifier  *recordingNotifier
	instances *repository.CloudInstanceRepository
	database  *db.DB
	configID  uuid.UUID
	clientID  uuid.UUID
}

func newReaperFixture(t *testing.T, capCents int64) *reaperFixture {
	t.Helper()
	database := requireCloudTestDB(t)

	instances := repository.NewCloudInstanceRepository(database)
	budget := NewBudgetEngine(repository.NewCloudBudgetRepository(database))
	provider := newFakeProvider()
	notifier := &recordingNotifier{}

	configID := testutil.CreateTestCloudProviderConfig(t, database, "mock",
		"reaper-"+uuid.NewString()[:8], testutil.ProviderConfigOpts{})
	clientID := testutil.CreateTestClient(t, database, "reaper-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{
			CloudEnabled:      true,
			ProviderAllowlist: []string{"mock"},
			BudgetCents:       testutil.Int64Ptr(capCents),
		})

	r := NewReaper(instances, budget,
		func(context.Context, uuid.UUID) (Provider, error) { return provider, nil },
		notifier)

	return &reaperFixture{
		reaper: r, provider: provider, notifier: notifier,
		instances: instances, database: database, configID: configID, clientID: clientID,
	}
}

func (f *reaperFixture) state(t *testing.T, id uuid.UUID) models.CloudInstanceState {
	t.Helper()
	var s string
	if err := f.database.QueryRow(`SELECT state FROM cloud_instances WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read instance state: %v", err)
	}
	return models.CloudInstanceState(s)
}

func ago(d time.Duration) *time.Time   { t := time.Now().Add(-d); return &t }
func ahead(d time.Duration) *time.Time { t := time.Now().Add(d); return &t }

// TestReaper_DestroysPastTTL: the in-guest watchdog should already have fired;
// if we get here it did not, which is exactly why this tier exists.
func TestReaper_DestroysPastTTL(t *testing.T) {
	f := newReaperFixture(t, 10_000)

	id, _ := testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-ttl",
		TTLEpoch:           ago(time.Minute),
		HourlyRateCents:    100,
		ReservedCents:      400,
	})

	f.reaper.SweepOnce(context.Background())

	if !f.provider.wasDestroyed("prov-ttl") {
		t.Error("an instance past its TTL must be destroyed")
	}
	if got := f.state(t, id); got != models.CloudInstanceTerminated {
		t.Errorf("state = %s, want terminated", got)
	}
}

// TestReaper_DestroysWhenAgentNeverRegistered covers the readiness deadline.
func TestReaper_DestroysWhenAgentNeverRegistered(t *testing.T) {
	f := newReaperFixture(t, 10_000)

	testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-noagent",
		ReadyDeadlineAt:    ago(time.Minute),
		TTLEpoch:           ahead(time.Hour),
		HourlyRateCents:    100,
	})

	f.reaper.SweepOnce(context.Background())

	if !f.provider.wasDestroyed("prov-noagent") {
		t.Error("an instance whose agent never registered must be destroyed")
	}
}

// TestReaper_KeepsInstanceWithAttachedAgent is the counterpart: once an agent
// is attached, the readiness deadline must stop applying. Without this the
// reaper kills healthy instances 20 minutes after launch.
func TestReaper_KeepsInstanceWithAttachedAgent(t *testing.T) {
	f := newReaperFixture(t, 10_000)

	owner := testutil.CreateTestUser(t, f.database, "reaper-owner-"+uuid.NewString()[:8],
		"reaper-owner-"+uuid.NewString()[:8]+"@test.local", testutil.DefaultTestPassword, "admin")
	agentID := testutil.CreateTestAgent(t, f.database, owner.ID, nil)
	testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-healthy",
		AgentID:            &agentID,
		ReadyDeadlineAt:    ago(time.Hour),
		TTLEpoch:           ahead(time.Hour),
		HourlyRateCents:    100,
	})

	f.reaper.SweepOnce(context.Background())

	if f.provider.wasDestroyed("prov-healthy") {
		t.Error("an instance with an attached agent must survive its readiness deadline")
	}
}

// TestReaper_DestroysOnTerminalProviderState: Vast.ai's exited/unknown/offline
// never recover, so polling them further is pure spend.
func TestReaper_DestroysOnTerminalProviderState(t *testing.T) {
	f := newReaperFixture(t, 10_000)
	f.provider.statuses["prov-dead"] = InstanceStatus{State: "gone", Terminal: true, Message: "exited"}

	testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-dead",
		TTLEpoch:           ahead(time.Hour),
		HourlyRateCents:    100,
	})

	f.reaper.SweepOnce(context.Background())

	if !f.provider.wasDestroyed("prov-dead") {
		t.Error("a terminal provider state must trigger teardown")
	}
}

// TestReaper_AdoptsLostLaunchResponse: the launch applied but the response was
// lost, so no provider ID was recorded. Reconciling by label is the only
// recovery, and on providers without an idempotency token it is what prevents
// paying for the same work twice.
func TestReaper_AdoptsLostLaunchResponse(t *testing.T) {
	f := newReaperFixture(t, 10_000)

	id, label := testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:         &f.clientID,
		State:            "launching",
		LaunchDeadlineAt: ahead(10 * time.Minute),
		TTLEpoch:         ahead(time.Hour),
		HourlyRateCents:  100,
	})
	f.provider.inventory[label] = InstanceStatus{ProviderInstanceID: "prov-adopted", State: "running"}

	f.reaper.SweepOnce(context.Background())

	var provID string
	if err := f.database.QueryRow(
		`SELECT COALESCE(provider_instance_id,'') FROM cloud_instances WHERE id = $1`, id).Scan(&provID); err != nil {
		t.Fatalf("read provider id: %v", err)
	}
	if provID != "prov-adopted" {
		t.Errorf("provider_instance_id = %q, want the adopted ID — a lost launch response "+
			"must be reconciled by label, not abandoned", provID)
	}
	if f.provider.wasDestroyed("prov-adopted") {
		t.Error("adoption must not destroy the instance it just recovered")
	}
}

// TestReaper_FailsInstanceThatNeverLaunched: past the launch deadline with
// nothing at the provider, give up rather than tracking it forever.
func TestReaper_FailsInstanceThatNeverLaunched(t *testing.T) {
	f := newReaperFixture(t, 10_000)

	id, _ := testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:         &f.clientID,
		State:            "launching",
		LaunchDeadlineAt: ago(time.Minute),
		HourlyRateCents:  100,
		ReservedCents:    400,
	})

	f.reaper.SweepOnce(context.Background())

	if got := f.state(t, id); got != models.CloudInstanceFailed {
		t.Errorf("state = %s, want failed", got)
	}
}

// TestReaper_DestroysOrphans: present at the provider, absent from the
// database. This is why a dedicated provider account is recommended.
func TestReaper_DestroysOrphans(t *testing.T) {
	f := newReaperFixture(t, 10_000)

	// A tracked instance is required for the sweep to fetch inventory at all.
	testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-tracked",
		TTLEpoch:           ahead(time.Hour),
		HourlyRateCents:    100,
	})
	f.provider.inventory["kh-unknown-label"] = InstanceStatus{
		ProviderInstanceID: "prov-orphan", State: "running",
	}

	f.reaper.SweepOnce(context.Background())

	if !f.provider.wasDestroyed("prov-orphan") {
		t.Error("an instance at the provider with no database row must be destroyed")
	}
	if f.provider.wasDestroyed("prov-tracked") {
		t.Error("the tracked instance must not be mistaken for an orphan")
	}
}

// TestReaper_ListFailureDoesNotDestroyAnything: without inventory the reaper
// cannot tell an orphan from a tracked instance, so it must not guess.
func TestReaper_ListFailureDoesNotDestroyAnything(t *testing.T) {
	f := newReaperFixture(t, 10_000)
	f.provider.listErr = errors.New("provider API unavailable")

	testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-safe",
		TTLEpoch:           ahead(time.Hour),
		HourlyRateCents:    100,
	})

	f.reaper.SweepOnce(context.Background())

	if len(f.provider.destroyed) != 0 {
		t.Errorf("a failed inventory listing must not destroy anything, destroyed: %v",
			f.provider.destroyed)
	}
}

/*
 * TestReaper_CountsDestroyFailuresAndEscalates covers the scariest branch:
 * automation has lost control of something that is still billing.
 *
 * The instance must stay in a live state so it keeps accruing and keeps
 * blocking new provisioning for that client — reporting it terminated would
 * hide real spend.
 */
func TestReaper_CountsDestroyFailuresAndEscalates(t *testing.T) {
	f := newReaperFixture(t, 10_000)
	f.provider.destroyErr = errors.New("provider refused")

	id, _ := testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-stuck",
		TTLEpoch:           ago(time.Minute),
		HourlyRateCents:    100,
	})

	ctx := context.Background()
	for i := 0; i < terminateFailureAlertThreshold; i++ {
		f.reaper.SweepOnce(ctx)
	}

	var attempts int
	if err := f.database.QueryRow(
		`SELECT terminate_attempts FROM cloud_instances WHERE id = $1`, id).Scan(&attempts); err != nil {
		t.Fatalf("read terminate_attempts: %v", err)
	}
	if attempts < terminateFailureAlertThreshold {
		t.Errorf("terminate_attempts = %d, want at least %d", attempts, terminateFailureAlertThreshold)
	}

	if got := f.state(t, id); got == models.CloudInstanceTerminated {
		t.Error("an instance we failed to destroy must NOT be reported terminated — " +
			"it is still billing")
	}

	if alerts := f.notifier.teardownAlerts(); len(alerts) == 0 {
		t.Errorf("expected a teardown-failure escalation after %d attempts",
			terminateFailureAlertThreshold)
	}
}

// TestReaper_HardStopBudgetDestroys: a client past its hard stop has its
// instances torn down.
func TestReaper_HardStopBudgetDestroys(t *testing.T) {
	f := newReaperFixture(t, 100)

	// Commit more than the cap so the ladder reports hard stop.
	testutil.InsertLedgerEntry(t, f.database, f.clientID, nil, 200, "reservation", time.Now())

	testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-broke",
		TTLEpoch:           ahead(time.Hour),
		HourlyRateCents:    100,
	})

	f.reaper.SweepOnce(context.Background())

	if !f.provider.wasDestroyed("prov-broke") {
		t.Error("a client past its hard stop must have its instances destroyed")
	}
}

// TestReaper_BudgetIsPerClient: one client exhausting its budget must never
// tear down another client's instances.
func TestReaper_BudgetIsPerClient(t *testing.T) {
	f := newReaperFixture(t, 100)

	solvent := testutil.CreateTestClient(t, f.database, "solvent-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{
			CloudEnabled:      true,
			ProviderAllowlist: []string{"mock"},
			BudgetCents:       testutil.Int64Ptr(100_000),
		})

	testutil.InsertLedgerEntry(t, f.database, f.clientID, nil, 500, "reservation", time.Now())

	testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID: &f.clientID, ProviderInstanceID: "prov-broke",
		TTLEpoch: ahead(time.Hour), HourlyRateCents: 100,
	})
	testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID: &solvent, ProviderInstanceID: "prov-solvent",
		TTLEpoch: ahead(time.Hour), HourlyRateCents: 100,
	})

	f.reaper.SweepOnce(context.Background())

	if !f.provider.wasDestroyed("prov-broke") {
		t.Error("the over-budget client's instance must be destroyed")
	}
	if f.provider.wasDestroyed("prov-solvent") {
		t.Error("a solvent client's instance must NOT be destroyed by another client's overspend")
	}
}

// TestReaper_DrainAllDestroysEverything covers the SIGTERM path.
func TestReaper_DrainAllDestroysEverything(t *testing.T) {
	f := newReaperFixture(t, 10_000)

	for _, pid := range []string{"prov-a", "prov-b", "prov-c"} {
		testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
			ClientID: &f.clientID, ProviderInstanceID: pid,
			TTLEpoch: ahead(time.Hour), HourlyRateCents: 100,
		})
	}

	f.reaper.DrainAll(context.Background())

	for _, pid := range []string{"prov-a", "prov-b", "prov-c"} {
		if !f.provider.wasDestroyed(pid) {
			t.Errorf("DrainAll must destroy %s", pid)
		}
	}
}

// TestReaper_SettlesBudgetOnTeardown: a job that finished early hands its
// unused reservation back.
func TestReaper_SettlesBudgetOnTeardown(t *testing.T) {
	f := newReaperFixture(t, 10_000)
	ctx := context.Background()

	id, _ := testutil.CreateTestCloudInstance(t, f.database, f.configID, testutil.InstanceOpts{
		ClientID:           &f.clientID,
		ProviderInstanceID: "prov-settle",
		TTLEpoch:           ago(time.Minute),
		HourlyRateCents:    100,
		ReservedCents:      400,
		EstimatedCostCents: 100,
	})
	testutil.InsertLedgerEntry(t, f.database, f.clientID, &id, 400, "reservation", time.Now())

	f.reaper.SweepOnce(ctx)

	var released int64
	if err := f.database.QueryRow(`
		SELECT COALESCE(SUM(cents),0) FROM cloud_spend_ledger
		WHERE cloud_instance_id = $1 AND kind = 'release'`, id).Scan(&released); err != nil {
		t.Fatalf("sum releases: %v", err)
	}
	if released != -300 {
		t.Errorf("released = %d, want -300 (400 reserved minus 100 used, stored negative)", released)
	}
}
