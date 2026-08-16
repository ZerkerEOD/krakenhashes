package cloud

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/crypto"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

/*
 * ProvisionForJob's ordering is the contract that makes losing a launch
 * response survivable.
 *
 * The row and its budget reservation must exist BEFORE the provider is called.
 * If they did not, a process that died between "the provider created an
 * instance" and "we wrote it down" would leave a GPU billing forever with
 * nothing in the database pointing at it — not reconcilable by the reaper, not
 * visible in the UI, and not counted against any budget. The label written in
 * advance is the only handle on that machine.
 *
 * These tests assert the ordering from inside the provider call itself, which
 * is the one vantage point that cannot be faked by reading the final state.
 */

// probeProvider runs an arbitrary check at the moment Launch is invoked, so
// the database can be inspected mid-provision.
type probeProvider struct {
	offers []Offer
	// onLaunch runs inside Launch, before it returns. Its error fails the
	// launch, which surfaces the assertion through ProvisionForJob's error.
	onLaunch func(req LaunchRequest) error
	launched int
	lastReq  LaunchRequest

	launchErr error
}

func (p *probeProvider) Name() models.CloudProvider { return models.CloudProviderMock }

func (p *probeProvider) SearchOffers(context.Context, OfferQuery) ([]Offer, error) {
	return p.offers, nil
}

func (p *probeProvider) Launch(_ context.Context, req LaunchRequest) (*LaunchResult, error) {
	p.launched++
	p.lastReq = req
	if p.onLaunch != nil {
		if err := p.onLaunch(req); err != nil {
			return nil, err
		}
	}
	if p.launchErr != nil {
		return nil, p.launchErr
	}
	now := time.Now()
	return &LaunchResult{
		ProviderInstanceID: "prov-" + req.Label,
		LaunchedAt:         now,
		BilledFrom:         now,
	}, nil
}

func (p *probeProvider) Destroy(context.Context, string) error { return nil }
func (p *probeProvider) Status(context.Context, string) (*InstanceStatus, error) {
	return &InstanceStatus{State: "running"}, nil
}
func (p *probeProvider) ListOwned(context.Context) (map[string]InstanceStatus, error) {
	return map[string]InstanceStatus{}, nil
}
func (p *probeProvider) CostSoFar(context.Context, string) (int64, bool, error) {
	return 0, false, nil
}
func (p *probeProvider) Preflight(context.Context) (*PreflightReport, error) {
	return &PreflightReport{OK: true}, nil
}

// provisionFixture assembles a service wired to a probe provider, with a funded
// client and a cloud-eligible job ready to provision.
type provisionFixture struct {
	db       *db.DB
	svc      *Service
	provider *probeProvider
	clientID uuid.UUID
	job      testutil.CloudJob
	cfgID    uuid.UUID
}

func newProvisionFixture(t *testing.T, budgetCents int64, offers []Offer) *provisionFixture {
	t.Helper()
	database := requireCloudTestDB(t)

	clientID := testutil.CreateTestClient(t, database, "payer-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{
			CloudEnabled:      true,
			BudgetCents:       testutil.Int64Ptr(budgetCents),
			ProviderAllowlist: []string{"mock"},
			MaxTTLMinutes:     testutil.IntPtr(60),
			AckProviders:      []string{"mock"},
		})
	job := testutil.CreateCloudJob(t, database, clientID, true)

	cfgID := testutil.CreateTestCloudProviderConfig(t, database, "mock", "mock-"+uuid.NewString()[:8],
		testutil.ProviderConfigOpts{
			Enabled:                true,
			ThirdPartyAcked:        true,
			VPNProvider:            "tailscale",
			VPNCredentialKind:      string(models.VPNCredentialReusableKey),
			MaxInstanceHourlyCents: 1000,
		})

	// A reusable key mints without any network call. It still has to survive
	// decryption, so it is stored through the same service the code reads with.
	ciphertext, err := crypto.GetEncryptionService().Encrypt("tskey-reusable-test")
	if err != nil {
		t.Fatalf("encrypt test VPN credential: %v", err)
	}
	if _, err := database.Exec(
		`UPDATE cloud_provider_configs SET vpn_credential_encrypted = $2 WHERE id = $1`,
		cfgID, ciphertext); err != nil {
		t.Fatalf("store test VPN credential: %v", err)
	}

	provider := &probeProvider{offers: offers}

	svc := NewService(
		database,
		repository.NewCloudProviderRepository(database),
		repository.NewCloudInstanceRepository(database),
		NewBudgetEngine(repository.NewCloudBudgetRepository(database)),
		NewFileSetResolver(database),
		services.NewClaimVoucherService(repository.NewClaimVoucherRepository(database)),
	)
	svc.AgentImage = "test/agent:latest"
	svc.SystemUserID = models.SystemUserID.String()

	// Bypass credential-driven construction: the probe IS the provider.
	svc.mu.Lock()
	svc.cache[cfgID] = provider
	svc.mu.Unlock()

	return &provisionFixture{
		db: database, svc: svc, provider: provider,
		clientID: clientID, job: job, cfgID: cfgID,
	}
}

func testOffer(hourlyCents int) []Offer {
	return []Offer{{
		ID:              "offer-1",
		InstanceType:    "test.gpu",
		GPUModel:        "RTX 4090",
		GPUCount:        1,
		HourlyRateCents: hourlyCents,
		MaxDuration:     24 * time.Hour,
	}}
}

/*
 * TestProvisionForJob_RowAndReservationExistBeforeLaunch is the ordering proof.
 */
func TestProvisionForJob_RowAndReservationExistBeforeLaunch(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))

	var probeErr error
	f.provider.onLaunch = func(req LaunchRequest) error {
		// At this instant the provider is about to create a billable machine.
		// Everything needed to find and pay for it must already be written.
		var state string
		var reserved int64
		var instanceID uuid.UUID
		err := f.db.QueryRow(`
			SELECT id, state, reserved_cents FROM cloud_instances WHERE label = $1`,
			req.Label).Scan(&instanceID, &state, &reserved)
		if err != nil {
			probeErr = fmt.Errorf("no cloud_instances row exists for label %q at launch time: %w", req.Label, err)
			return probeErr
		}
		if reserved <= 0 {
			probeErr = fmt.Errorf("instance row exists but reserved_cents is %d", reserved)
			return probeErr
		}

		var ledgerCents int64
		var n int
		if err := f.db.QueryRow(`
			SELECT COUNT(*), COALESCE(SUM(cents),0) FROM cloud_spend_ledger
			WHERE cloud_instance_id = $1 AND kind = 'reservation'`, instanceID).Scan(&n, &ledgerCents); err != nil {
			probeErr = fmt.Errorf("read ledger: %w", err)
			return probeErr
		}
		if n != 1 {
			probeErr = fmt.Errorf("%d reservation ledger rows exist at launch time, want exactly 1", n)
			return probeErr
		}
		if ledgerCents != reserved {
			probeErr = fmt.Errorf("ledger reserved %d cents but the instance row says %d", ledgerCents, reserved)
			return probeErr
		}
		return nil
	}

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("ProvisionForJob: %v", err)
	}
	if probeErr != nil {
		t.Fatalf("ordering contract violated: %v", probeErr)
	}
	if f.provider.launched != 1 {
		t.Fatalf("provider Launch called %d times, want 1", f.provider.launched)
	}
}

/*
 * TestProvisionForJob_VoucherIsBoundToTheInstance.
 *
 * The claim code handed to the instance is the agent's whole identity. If it
 * were not bound, an agent redeeming it would register as an ordinary on-prem
 * agent: no job lock, no file-sync scoping, and included in the full-corpus
 * sync that ships every client potfile.
 */
func TestProvisionForJob_VoucherIsBoundToTheInstance(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("ProvisionForJob: %v", err)
	}

	code, ok := f.provider.lastReq.Env["KH_CLAIM_CODE"]
	if !ok || code == "" {
		t.Fatalf("no claim code was passed to the instance; env keys: %v", envKeys(f.provider.lastReq.Env))
	}

	var boundInstance uuid.NullUUID
	var isContinuous bool
	var expires *time.Time
	// The launch env carries the display-formatted code; storage is normalised.
	normalised := normaliseTestCode(code)
	if err := f.db.QueryRow(`
		SELECT cloud_instance_id, is_continuous, expires_at
		FROM claim_vouchers WHERE code = $1`, normalised).
		Scan(&boundInstance, &isContinuous, &expires); err != nil {
		t.Fatalf("look up voucher %q: %v", normalised, err)
	}

	if !boundInstance.Valid {
		t.Fatal("the voucher handed to a rented instance is not bound to it; an agent " +
			"redeeming it would register with none of the cloud isolation applied")
	}
	var label string
	if err := f.db.QueryRow(`SELECT label FROM cloud_instances WHERE id = $1`,
		boundInstance.UUID).Scan(&label); err != nil {
		t.Fatalf("the voucher points at an instance that does not exist: %v", err)
	}
	if label != f.provider.lastReq.Label {
		t.Errorf("voucher is bound to instance %q but was launched on %q", label, f.provider.lastReq.Label)
	}
	if isContinuous {
		t.Error("a cloud voucher must be single-use; a continuous one would let any " +
			"number of agents claim the same rented identity")
	}
	if expires == nil {
		t.Error("a cloud voucher must expire; otherwise the credential outlives the " +
			"instance it was minted for")
	}
}

/*
 * TestProvisionForJob_LostLaunchResponseLeavesAReconcilableRow.
 *
 * A launch that errors may still have created a machine — the request can
 * succeed at the provider and fail on the way back. The row must survive so the
 * reaper can find that machine by label; deleting it would strand a billing GPU
 * with nothing pointing at it.
 */
func TestProvisionForJob_LostLaunchResponseLeavesAReconcilableRow(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	f.provider.launchErr = errors.New("connection reset by peer")

	err := f.svc.ProvisionForJob(context.Background(), f.job.JobID)
	if err == nil {
		t.Fatal("a failed launch must be reported as an error")
	}

	var label, state string
	var reserved int64
	if qerr := f.db.QueryRow(`
		SELECT label, state, reserved_cents FROM cloud_instances
		WHERE job_execution_id = $1`, f.job.JobID).Scan(&label, &state, &reserved); qerr != nil {
		t.Fatalf("no row survived a lost launch response — a machine the provider may "+
			"have created is now untracked and unbounded: %v", qerr)
	}
	if label != f.provider.lastReq.Label {
		t.Errorf("surviving row has label %q, launch used %q; reconciliation is by label", label, f.provider.lastReq.Label)
	}
	if state == string(models.CloudInstanceTerminated) || state == string(models.CloudInstanceFailed) {
		t.Errorf("row was finalised as %q; the reaper skips finalised rows, so a machine "+
			"created by the lost request would never be reconciled", state)
	}
	if reserved <= 0 {
		t.Error("the reservation was dropped; spend that may be happening is uncounted")
	}
}

/*
 * TestProvisionForJob_RefusesWhenBudgetCannotCoverAUsefulRun.
 *
 * The most important refusal in the feature: renting a box for less time than
 * it takes to be useful spends money for nothing.
 */
func TestProvisionForJob_RefusesWhenBudgetCannotCoverAUsefulRun(t *testing.T) {
	// 10 cents against a 500 cents/hour offer buys just over a minute.
	f := newProvisionFixture(t, 10, testOffer(500))

	err := f.svc.ProvisionForJob(context.Background(), f.job.JobID)
	if err == nil {
		t.Fatal("provisioned an instance the client cannot afford to run usefully")
	}
	if f.provider.launched != 0 {
		t.Fatal("the provider was called despite the budget refusal")
	}

	var n int
	if qerr := f.db.QueryRow(`SELECT COUNT(*) FROM cloud_spend_ledger WHERE client_id = $1`,
		f.clientID).Scan(&n); qerr != nil {
		t.Fatalf("count ledger: %v", qerr)
	}
	if n != 0 {
		t.Errorf("%d ledger row(s) written for a refused launch; committed spend must "+
			"only ever reflect instances that were actually requested", n)
	}
}

// TestProvisionForJob_RefusesJobsThatDidNotOptIn: cloud_burst_enabled is the
// consent flag, and nothing bursts to paid capacity by accident.
func TestProvisionForJob_RefusesJobsThatDidNotOptIn(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	onPrem := testutil.CreateCloudJob(t, f.db, f.clientID, false)

	if err := f.svc.ProvisionForJob(context.Background(), onPrem.JobID); err == nil {
		t.Fatal("provisioned paid capacity for a job that never opted in")
	}
	if f.provider.launched != 0 {
		t.Fatal("the provider was called for a job with cloud_burst_enabled = false")
	}
}

// TestProvisionForJob_RefusesWhenNoOfferFits: an empty offer list must be a
// refusal, never a launch with a zero-valued offer.
func TestProvisionForJob_RefusesWhenNoOfferFits(t *testing.T) {
	f := newProvisionFixture(t, 100_000, nil)

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err == nil {
		t.Fatal("provisioning succeeded with no offers available")
	}
	if f.provider.launched != 0 {
		t.Fatal("the provider was asked to launch with no offer selected")
	}
}

func envKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// normaliseTestCode mirrors the storage form of a claim code (upper-case, no
// hyphens), which is what the vouchers table is keyed on.
func normaliseTestCode(code string) string {
	out := make([]byte, 0, len(code))
	for i := 0; i < len(code); i++ {
		c := code[i]
		if c == '-' {
			continue
		}
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
