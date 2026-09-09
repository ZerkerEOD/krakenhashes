package cloud

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

	// The deployment-wide ceiling. TruncateAll empties system_settings, and the
	// shipped default is 0 = disabled anyway, so a test that wants to spend has
	// to say so — exactly like an operator enabling the feature.
	setGlobalCloudCap(t, database, 1_000_000)

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
	svc.SystemSettings = repository.NewSystemSettingsRepository(database)

	// Bypass credential-driven construction: the probe IS the provider.
	svc.mu.Lock()
	svc.cache[cfgID] = provider
	svc.mu.Unlock()

	return &provisionFixture{
		db: database, svc: svc, provider: provider,
		clientID: clientID, job: job, cfgID: cfgID,
	}
}

// setGlobalCloudCap writes the deployment-wide monthly ceiling. Upserts because
// TruncateAll removes the row the migration seeded.
func setGlobalCloudCap(t *testing.T, database *db.DB, cents int64) {
	t.Helper()
	_, err := database.Exec(`
		INSERT INTO system_settings (key, value, description, data_type)
		VALUES ($1, $2, 'test', 'integer')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		SettingGlobalMonthlyCapCents, strconv.FormatInt(cents, 10))
	if err != nil {
		t.Fatalf("set %s: %v", SettingGlobalMonthlyCapCents, err)
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

/*
 * TestProvisionForJob_PicksCheapestPerWorkNotPerHour.
 *
 * The whole point of the ranker. Given a slow cheap card and a fast pricier
 * one, cost per HOUR picks the slow one and cost per WORK picks the fast one.
 * Before ranking existed, service.go took offers[0] from a list the provider
 * had sorted by hourly price, so the slow card won every time.
 *
 * The numbers are the real ones from the class table: an A5000 is 0.26x a 4090
 * and a 5090 is 1.60x, so at 27c and 99c the A5000 costs 103.8 cents per
 * reference-GPU-hour against the 5090's 61.9.
 */
func TestProvisionForJob_PicksCheapestPerWorkNotPerHour(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "cheap-slow", InstanceType: "RTX A5000", GPUModel: "RTX A5000",
			GPUCount: 1, HourlyRateCents: 27, MaxDuration: 24 * time.Hour},
		{ID: "pricey-fast", InstanceType: "RTX 5090", GPUModel: "NVIDIA GeForce RTX 5090",
			GPUCount: 1, HourlyRateCents: 99, MaxDuration: 24 * time.Hour},
	})

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("ProvisionForJob: %v", err)
	}

	if got := f.provider.lastReq.Offer.ID; got != "pricey-fast" {
		t.Errorf("rented %q; the RTX 5090 is cheaper per unit of work (61.9 vs 103.8 "+
			"cents per reference-GPU-hour) even though it costs more per hour", got)
	}

	var gpu string
	if err := f.db.QueryRow(
		`SELECT gpu_model FROM cloud_instances WHERE job_execution_id = $1`, f.job.JobID).Scan(&gpu); err != nil {
		t.Fatalf("read back instance: %v", err)
	}
	if !strings.Contains(gpu, "5090") {
		t.Errorf("recorded gpu_model = %q, want the 5090", gpu)
	}
}

/*
 * TestProvisionForJob_FallsForwardWhenAnOfferVanishes.
 *
 * ErrOfferUnavailable's own doc said callers should try the next offer, and
 * nothing did — capacity vanishing between search and launch failed the whole
 * provision. On a marketplace that is an ordinary, frequent event.
 *
 * Retrying is safe only because of what the sentinel means: the provider
 * rejected the create, so no instance exists and nothing is billing.
 */
func TestProvisionForJob_FallsForwardWhenAnOfferVanishes(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "gone", InstanceType: "RTX 5090", GPUModel: "RTX 5090",
			GPUCount: 1, HourlyRateCents: 99, MaxDuration: 24 * time.Hour},
		{ID: "available", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 74, MaxDuration: 24 * time.Hour},
	})

	// The first-ranked offer vanishes; the second succeeds.
	f.provider.onLaunch = func(req LaunchRequest) error {
		if req.Offer.ID == "gone" {
			return ErrOfferUnavailable
		}
		return nil
	}

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("a vanished offer must fall forward to the next candidate, not fail: %v", err)
	}
	if f.provider.launched != 2 {
		t.Errorf("Launch called %d times, want 2 (one vanished, one succeeded)", f.provider.launched)
	}

	/*
	 * The failed attempt's reservation must be RELEASED, not left standing.
	 * Otherwise six attempts against a busy marketplace consume six instances'
	 * worth of a client's budget and the seventh is refused for no reason.
	 */
	var committed int64
	if err := f.db.QueryRow(`
		SELECT COALESCE(SUM(cents),0) FROM cloud_spend_ledger
		WHERE client_id = $1 AND kind IN ('reservation','release')`, f.clientID).Scan(&committed); err != nil {
		t.Fatalf("read ledger: %v", err)
	}

	var reserved int64
	if err := f.db.QueryRow(`
		SELECT COALESCE(SUM(reserved_cents),0) FROM cloud_instances
		WHERE job_execution_id = $1 AND state <> 'failed'`, f.job.JobID).Scan(&reserved); err != nil {
		t.Fatalf("read instances: %v", err)
	}
	if committed != reserved {
		t.Errorf("committed spend is %d cents but only %d is held by a live instance; "+
			"the vanished offer's reservation was not released", committed, reserved)
	}
}

/*
 * TestProvisionForJob_AmbiguousLaunchFailureDoesNotRetry.
 *
 * A timeout may have created a billing instance. Retrying would risk a second
 * one, and the row must stay behind so the reaper can reconcile it by label.
 */
func TestProvisionForJob_AmbiguousLaunchFailureDoesNotRetry(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "first", InstanceType: "RTX 5090", GPUModel: "RTX 5090",
			GPUCount: 1, HourlyRateCents: 99, MaxDuration: 24 * time.Hour},
		{ID: "second", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 74, MaxDuration: 24 * time.Hour},
	})
	f.provider.launchErr = errors.New("connection reset by peer")

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err == nil {
		t.Fatal("an ambiguous launch failure must be reported, not retried away")
	}
	if f.provider.launched != 1 {
		t.Errorf("Launch called %d times; an ambiguous failure must NOT advance to the "+
			"next candidate, because the first may have created a billing instance",
			f.provider.launched)
	}

	var state string
	if err := f.db.QueryRow(`
		SELECT state FROM cloud_instances WHERE job_execution_id = $1`, f.job.JobID).Scan(&state); err != nil {
		t.Fatalf("no row survived an ambiguous launch failure: %v", err)
	}
	if state == string(models.CloudInstanceTerminated) || state == string(models.CloudInstanceFailed) {
		t.Errorf("row finalised as %q; the reaper skips finalised rows, so a machine the "+
			"lost request may have created would never be reconciled", state)
	}
}

/*
 * TestProvisionForJob_RespectsProviderConcurrencyCap.
 *
 * cloud_provider_configs.max_concurrent_instances was stored, validated by the
 * admin handler, returned by the API — and read by nothing. An operator who
 * capped a provider at 3 could end up with any number of instances on it.
 *
 * It is the only PER-PROVIDER bound in the system: the global cap is
 * deployment-wide and the budget is per client, so neither can express "this
 * account's quota is 5" or "I do not trust this provider with more than 2 at
 * once".
 */
func TestProvisionForJob_RespectsProviderConcurrencyCap(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, testOffer(50))

	if _, err := f.db.Exec(
		`UPDATE cloud_provider_configs SET max_concurrent_instances = 1 WHERE id = $1`,
		f.cfgID); err != nil {
		t.Fatalf("set concurrency cap: %v", err)
	}

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("first provision (under the cap): %v", err)
	}

	launchedBefore := f.provider.launched
	err := f.svc.ProvisionForJob(context.Background(), f.job.JobID)
	if err == nil {
		t.Fatal("provisioned a second instance on a provider capped at 1")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Errorf("refusal %q does not say the provider is at its cap; an operator "+
			"cannot tell this apart from a missing allowlist entry", err)
	}
	if f.provider.launched != launchedBefore {
		t.Error("a provider at its concurrency cap was still contacted")
	}
}

// TestProvisionForJob_ZeroConcurrencyCapIsUnlimited: 0 must mean "no cap",
// consistent with every other 0-means-unlimited knob in this feature — and NOT
// "cap of zero, therefore never provision".
func TestProvisionForJob_ZeroConcurrencyCapIsUnlimited(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, testOffer(50))

	if _, err := f.db.Exec(
		`UPDATE cloud_provider_configs SET max_concurrent_instances = 0 WHERE id = $1`,
		f.cfgID); err != nil {
		t.Fatalf("clear concurrency cap: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
			t.Fatalf("provision %d with no cap configured: %v", i+1, err)
		}
	}
	if f.provider.launched != 3 {
		t.Errorf("launched %d instances, want 3 — a zero cap must not bound anything", f.provider.launched)
	}
}

/*
 * TestProvisionForJob_RefusesFinishedJobs.
 *
 * The manual admin route checked cloud_burst_enabled and the client's
 * cloud_enabled flag but never je.status, while the autoscaler's copy of the
 * same predicate did. So an admin could rent a GPU for a cancelled or completed
 * job — an instance the scheduler is then forbidden to give work to, billing by
 * the second until its TTL expires. Both paths now share one predicate.
 */
func TestProvisionForJob_RefusesFinishedJobs(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			f := newProvisionFixture(t, 100_000, testOffer(50))
			if _, err := f.db.Exec(
				`UPDATE job_executions SET status = $2 WHERE id = $1`, f.job.JobID, status); err != nil {
				t.Fatalf("set job status: %v", err)
			}

			if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err == nil {
				t.Fatalf("provisioned a GPU for a %s job", status)
			}
			if f.provider.launched != 0 {
				t.Fatalf("a provider was contacted for a %s job", status)
			}
		})
	}
}

// TestCloudEligibleJobs_MatchesTheManualRoute: the two entry points share one
// predicate now, and the point of sharing it is that they cannot drift again.
func TestCloudEligibleJobs_MatchesTheManualRoute(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	ctx := context.Background()

	eligible, err := f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 1 {
		t.Fatalf("a live cloud-burst job is eligible, got %d rows", len(eligible))
	}

	if _, err := f.db.Exec(
		`UPDATE job_executions SET status = 'cancelled' WHERE id = $1`, f.job.JobID); err != nil {
		t.Fatalf("cancel job: %v", err)
	}
	eligible, err = f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 0 {
		t.Errorf("a cancelled job is still eligible for automatic provisioning")
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

/*
 * TestProvisionForJob_GlobalCapIsAKillSwitch (C8).
 *
 * cloud_global_monthly_cap_cents shipped with a description promising it
 * "disables cloud provisioning entirely" at 0, and with no code reading it at
 * all. An operator who set it to 0 to stop a runaway would have watched
 * instances keep launching.
 *
 * Zero means disabled rather than unlimited on purpose: a feature that spends
 * money should require someone to state a ceiling before it can spend any.
 */
func TestProvisionForJob_GlobalCapIsAKillSwitch(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	setGlobalCloudCap(t, f.db, 0)

	err := f.svc.ProvisionForJob(context.Background(), f.job.JobID)
	if err == nil {
		t.Fatal("provisioned with the system-wide ceiling set to 0; the setting " +
			"promises this disables cloud provisioning entirely")
	}
	if f.provider.launched != 0 {
		t.Fatal("a provider was contacted with provisioning disabled")
	}

	var n int
	if qerr := f.db.QueryRow(`SELECT COUNT(*) FROM cloud_instances`).Scan(&n); qerr != nil {
		t.Fatalf("count instances: %v", qerr)
	}
	if n != 0 {
		t.Errorf("%d instance row(s) created while provisioning was disabled", n)
	}
}

/*
 * TestProvisionForJob_GlobalCapBoundsTheDeployment.
 *
 * Per-client budgets bound one engagement. Twenty funded clients, each inside
 * its own budget, can still produce a bill nobody authorised — no per-client
 * check ever sees the total. This is the check that does.
 */
func TestProvisionForJob_GlobalCapBoundsTheDeployment(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, testOffer(50))

	// One successful launch, then lower the system ceiling below what it
	// committed. The client still has plenty of its own budget left.
	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("first provision: %v", err)
	}

	var committed int64
	if err := f.db.QueryRow(`
		SELECT COALESCE(SUM(cents),0) FROM cloud_spend_ledger
		WHERE kind IN ('reservation','release','reconciliation')`).Scan(&committed); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if committed <= 0 {
		t.Fatalf("expected a committed reservation after a launch, got %d", committed)
	}
	setGlobalCloudCap(t, f.db, committed)

	launchedBefore := f.provider.launched
	err := f.svc.ProvisionForJob(context.Background(), f.job.JobID)
	if err == nil {
		t.Fatal("provisioned past the system-wide ceiling; a per-client budget is not " +
			"a bound on the deployment")
	}
	if f.provider.launched != launchedBefore {
		t.Error("a provider was contacted after the system-wide ceiling was reached")
	}
}

/*
 * TestProvisionForJob_UnreadableCapRefuses.
 *
 * An unreadable kill switch has to behave like an engaged one. The operator who
 * set a ceiling and then lost their settings table is far better served by "no
 * provisioning" than by "unlimited provisioning".
 */
func TestProvisionForJob_UnreadableCapRefuses(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	f.svc.SystemSettings = nil

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err == nil {
		t.Fatal("provisioned without being able to read the system-wide spend ceiling")
	}
	if f.provider.launched != 0 {
		t.Fatal("a provider was contacted with the ceiling unreadable")
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

/*
 * TestCloudEligibleJobs_BlankMaxInstancesInheritsTheDefault.
 *
 * A job that leaves cloud_max_instances blank used to report MaxInstances=0,
 * which the autoscaler reads as "no cap". That was survivable only while the
 * on-prem brakes engaged; with no on-prem agents they are structurally dead and
 * blank means the client budget is the only bound.
 *
 * This is a DB-backed test on purpose. The inheritance is resolved inside the
 * SQL, and the budget default shipped with exactly this bug — the clause was
 * written against the raw column, so every inheriting client silently failed to
 * match and setting a default appeared to do nothing. A test that stubbed the
 * query would not have caught that.
 */
func TestCloudEligibleJobs_BlankMaxInstancesInheritsTheDefault(t *testing.T) {
	f := newProvisionFixture(t, 100_000, testOffer(50))
	ctx := context.Background()

	if _, err := f.db.Exec(
		`UPDATE job_executions SET cloud_max_instances = NULL WHERE id = $1`, f.job.JobID); err != nil {
		t.Fatalf("blank the per-job cap: %v", err)
	}
	if _, err := f.db.Exec(
		`INSERT INTO system_settings (key, value, description, data_type)
		 VALUES ('cloud_default_max_instances_per_job', '3', 'test', 'integer')
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`); err != nil {
		t.Fatalf("seed the default: %v", err)
	}

	eligible, err := f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 1 {
		t.Fatalf("expected the job to remain eligible, got %d rows", len(eligible))
	}
	if eligible[0].MaxInstances != 3 {
		t.Errorf("MaxInstances = %d, want 3 inherited from the server default. "+
			"0 here means unlimited, which is the runaway this default exists to close",
			eligible[0].MaxInstances)
	}

	// An explicit per-job value must still win over the default.
	if _, err := f.db.Exec(
		`UPDATE job_executions SET cloud_max_instances = 1 WHERE id = $1`, f.job.JobID); err != nil {
		t.Fatalf("set an explicit cap: %v", err)
	}
	eligible, err = f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 1 || eligible[0].MaxInstances != 1 {
		t.Errorf("an explicit per-job cap must override the default; got %+v", eligible)
	}

	// A malformed default must read as absent and fall back to unlimited rather
	// than erroring the whole query and stalling every job.
	if _, err := f.db.Exec(
		`UPDATE job_executions SET cloud_max_instances = NULL WHERE id = $1`, f.job.JobID); err != nil {
		t.Fatalf("re-blank the per-job cap: %v", err)
	}
	if _, err := f.db.Exec(
		`UPDATE system_settings SET value = 'not-a-number'
		  WHERE key = 'cloud_default_max_instances_per_job'`); err != nil {
		t.Fatalf("corrupt the default: %v", err)
	}
	eligible, err = f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("a malformed default must not error the query: %v", err)
	}
	if len(eligible) != 1 || eligible[0].MaxInstances != 0 {
		t.Errorf("malformed default should read as absent (0/unlimited); got %+v", eligible)
	}
}

/*
 * TestProvisionForJob_ExplainsWhyIneligible.
 *
 * "job X is not cloud-eligible: sql: no rows in result set" is what an operator
 * saw after clicking Provision now, and it names none of the four conditions
 * that could have failed. Two of them are invisible otherwise: a hashlist with
 * no client can NEVER burst, because both cloud entry points INNER JOIN clients
 * — and on the autoscaler path there is no message at all.
 *
 * DB-backed because the whole thing is SQL, including the LEFT JOIN that is the
 * only difference between detecting a missing client and silently excluding it.
 */
func TestProvisionForJob_ExplainsWhyIneligible(t *testing.T) {
	ctx := context.Background()

	t.Run("burst not enabled", func(t *testing.T) {
		f := newProvisionFixture(t, 100_000, testOffer(50))
		if _, err := f.db.Exec(
			`UPDATE job_executions SET cloud_burst_enabled = false WHERE id = $1`, f.job.JobID); err != nil {
			t.Fatalf("disable burst: %v", err)
		}
		// Pinned rather than assumed: the server-wide default would make this
		// job eligible, and the test DB is shared, so a sibling test that turns
		// it on would otherwise turn this assertion into a coin flip.
		setBurstDefault(t, f.db, false)
		err := f.svc.ProvisionForJob(ctx, f.job.JobID)
		if err == nil {
			t.Fatal("provisioned a job that had not opted in")
		}
		if !strings.Contains(err.Error(), "cloud burst is not enabled") {
			t.Errorf("error = %q, want it to name the disabled flag", err)
		}
		if strings.Contains(err.Error(), "no rows in result set") {
			t.Errorf("error still leaks the raw SQL message: %q", err)
		}
	})

	t.Run("hashlist has no client", func(t *testing.T) {
		f := newProvisionFixture(t, 100_000, testOffer(50))
		if _, err := f.db.Exec(
			`UPDATE hashlists SET client_id = NULL WHERE id = $1`, f.job.HashlistID); err != nil {
			t.Fatalf("unassign client: %v", err)
		}
		// Pinned empty: a nominated fallback would make this job billable and
		// turn the assertion into a coin flip on a shared database.
		setDefaultBillingClient(t, f.db, "")
		err := f.svc.ProvisionForJob(ctx, f.job.JobID)
		if err == nil {
			t.Fatal("provisioned a job whose hashlist has no client")
		}
		if !strings.Contains(err.Error(), "not assigned to a client") {
			t.Errorf("error = %q, want it to name the missing client — this is the case "+
				"that is otherwise completely silent", err)
		}
	})

	t.Run("finished job", func(t *testing.T) {
		f := newProvisionFixture(t, 100_000, testOffer(50))
		if _, err := f.db.Exec(
			`UPDATE job_executions SET status = 'completed' WHERE id = $1`, f.job.JobID); err != nil {
			t.Fatalf("complete job: %v", err)
		}
		err := f.svc.ProvisionForJob(ctx, f.job.JobID)
		if err == nil {
			t.Fatal("provisioned a completed job")
		}
		if !strings.Contains(err.Error(), "completed") {
			t.Errorf("error = %q, want it to name the job status", err)
		}
	})

	t.Run("no such job", func(t *testing.T) {
		f := newProvisionFixture(t, 100_000, testOffer(50))
		err := f.svc.ProvisionForJob(ctx, uuid.New())
		if err == nil {
			t.Fatal("provisioned a job that does not exist")
		}
		if !strings.Contains(err.Error(), "no such job") {
			t.Errorf("error = %q, want 'no such job'", err)
		}
	})
}

// setBurstDefault pins cloud_default_burst_enabled. Upserts rather than updates
// because SetSetting is UPDATE-only and the row's presence depends on whether
// the seeding migration has run against this database.
func setBurstDefault(t *testing.T, database *db.DB, on bool) {
	t.Helper()
	value := "false"
	if on {
		value = "true"
	}
	if _, err := database.Exec(
		`INSERT INTO system_settings (key, value, description, data_type)
		 VALUES ('cloud_default_burst_enabled', $1, 'test', 'boolean')
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, value); err != nil {
		t.Fatalf("set cloud_default_burst_enabled=%s: %v", value, err)
	}
	t.Cleanup(func() {
		// Shared database: leaving this on would silently make every later
		// test's job cloud-eligible.
		if _, err := database.Exec(
			`UPDATE system_settings SET value = 'false'
			  WHERE key = 'cloud_default_burst_enabled'`); err != nil {
			t.Logf("reset cloud_default_burst_enabled: %v", err)
		}
	})
}

// setDefaultBillingClient pins cloud_default_client_id, resetting it afterwards
// so a shared-database sibling test does not inherit a fallback that makes its
// client-less fixtures unexpectedly eligible.
func setDefaultBillingClient(t *testing.T, database *db.DB, value string) {
	t.Helper()
	if _, err := database.Exec(
		`INSERT INTO system_settings (key, value, description, data_type)
		 VALUES ('cloud_default_client_id', $1, 'test', 'string')
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, value); err != nil {
		t.Fatalf("set cloud_default_client_id=%q: %v", value, err)
	}
	t.Cleanup(func() {
		if _, err := database.Exec(
			`UPDATE system_settings SET value = '' WHERE key = 'cloud_default_client_id'`); err != nil {
			t.Logf("reset cloud_default_client_id: %v", err)
		}
	})
}

/*
 * TestDefaultBillingClient_LetsClientlessWorkBurst.
 *
 * Cloud money is per client everywhere, so both entry points joined clients and
 * a hashlist with no client could never rent capacity — which locked out every
 * deployment that does not model clients at all. require_client_for_hashlist
 * does not help: it gates new uploads only, so it neither repairs the hashlists
 * that already exist nor suits a team that has no clients to name.
 *
 * The fallback must hold in BOTH directions. Letting client-less work through
 * with no budget holder would be worse than refusing it, so the unset,
 * malformed and dangling-reference cases all have to keep refusing.
 */
func TestDefaultBillingClient_LetsClientlessWorkBurst(t *testing.T) {
	ctx := context.Background()
	f := newProvisionFixture(t, 100_000, testOffer(50))

	// The fixture's own client becomes the nominated fallback; the hashlist
	// then loses its direct assignment. Same client either way, so anything
	// that changes is the resolution path and not the client's own settings.
	fallback := f.clientID
	if _, err := f.db.Exec(
		`UPDATE hashlists SET client_id = NULL WHERE id = $1`, f.job.HashlistID); err != nil {
		t.Fatalf("unassign client: %v", err)
	}

	// Unset: refuses, and says so in terms that name the fix.
	setDefaultBillingClient(t, f.db, "")
	eligible, err := f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 0 {
		t.Fatalf("with no fallback set, client-less work must stay ineligible; got %d rows", len(eligible))
	}
	if err := f.svc.ProvisionForJob(ctx, f.job.JobID); err == nil {
		t.Error("provisioned client-less work with no fallback client")
	} else if !strings.Contains(err.Error(), "no default billing client") {
		t.Errorf("error = %q, want it to name the missing fallback", err)
	}

	// Nominated: the same job, untouched, is now billable on both paths.
	setDefaultBillingClient(t, f.db, fallback.String())
	eligible, err = f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 1 {
		t.Fatalf("a nominated fallback must make client-less work eligible; got %d rows", len(eligible))
	}
	// The spend has to land on the fallback, or the budget bounds nothing and
	// the client budget page cannot show it.
	if eligible[0].ClientID != fallback {
		t.Errorf("ClientID = %s, want the fallback %s — spend must be attributed to "+
			"the nominated client or no budget applies to it", eligible[0].ClientID, fallback)
	}
	if err := f.svc.ProvisionForJob(ctx, f.job.JobID); err != nil {
		t.Errorf("manual route must agree with the autoscaler: %v", err)
	}

	// Malformed: must refuse, and must not error the query — a raised cast here
	// would stall provisioning for every job, not just this one.
	setDefaultBillingClient(t, f.db, "not-a-uuid")
	eligible, err = f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("a malformed fallback must not error the query: %v", err)
	}
	if len(eligible) != 0 {
		t.Errorf("a malformed fallback must refuse, not permit; got %d rows", len(eligible))
	}

	// Well-formed but naming no client — the shape a deleted client leaves
	// behind. Refuses, rather than provisioning against a budget that is gone.
	setDefaultBillingClient(t, f.db, uuid.New().String())
	eligible, err = f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("a dangling fallback must not error the query: %v", err)
	}
	if len(eligible) != 0 {
		t.Errorf("a fallback naming no client must refuse; got %d rows", len(eligible))
	}
}

/*
 * TestDefaultBillingClient_DoesNotOverrideAnAssignedClient.
 *
 * The fallback is for work that has no client, not a redirect. If it ever won
 * over h.client_id, spend would silently move off the engagement that incurred
 * it and onto someone else's budget — visible only in the spend report, after
 * the money was gone.
 */
func TestDefaultBillingClient_DoesNotOverrideAnAssignedClient(t *testing.T) {
	ctx := context.Background()
	f := newProvisionFixture(t, 100_000, testOffer(50))

	setDefaultBillingClient(t, f.db, uuid.New().String())

	eligible, err := f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 1 {
		t.Fatalf("a job with its own client must be unaffected by the fallback; got %d rows", len(eligible))
	}
	if eligible[0].ClientID != f.clientID {
		t.Errorf("ClientID = %s, want the hashlist's own client %s — the fallback must "+
			"never redirect spend away from the client that incurred it",
			eligible[0].ClientID, f.clientID)
	}
}

/*
 * TestCloudDefaultBurstEnabled_OptsInEveryJob.
 *
 * job_executions.cloud_burst_enabled defaults false and has to be ticked on
 * every preset, workflow and job. That is right when cloud burst extends a
 * fleet you own and exactly backwards when you own no GPUs at all, where a
 * single missed tick is indistinguishable from a broken install: the job sits
 * at pending and nothing says why.
 *
 * Covers both entry points, because the whole point of resolving this inside
 * cloudEligibilityPredicate is that the autoscaler and the manual route cannot
 * drift apart on it — they already had, once, on je.status.
 */
func TestCloudDefaultBurstEnabled_OptsInEveryJob(t *testing.T) {
	ctx := context.Background()
	f := newProvisionFixture(t, 100_000, testOffer(50))

	if _, err := f.db.Exec(
		`UPDATE job_executions SET cloud_burst_enabled = false WHERE id = $1`, f.job.JobID); err != nil {
		t.Fatalf("disable per-job burst: %v", err)
	}

	// Off: the job is invisible to both paths.
	setBurstDefault(t, f.db, false)
	eligible, err := f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 0 {
		t.Fatalf("default off must leave an un-ticked job ineligible, got %d rows", len(eligible))
	}

	// On: the same job, unchanged, is now eligible on both paths. Asserted
	// without touching the row, because the value of resolving this in the
	// predicate is that it frees jobs that are ALREADY queued — which is the
	// state an operator is in when they work out they needed the setting.
	setBurstDefault(t, f.db, true)
	eligible, err = f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(eligible) != 1 {
		t.Errorf("default on must make an un-ticked job eligible, got %d rows", len(eligible))
	}
	if err := f.svc.ProvisionForJob(ctx, f.job.JobID); err != nil {
		t.Errorf("manual route must agree with the autoscaler: %v", err)
	}

	// A malformed value must read as off. This is a spend switch, so the
	// direction it fails in matters more than that it fails.
	if _, err := f.db.Exec(
		`UPDATE system_settings SET value = 'yes-please'
		  WHERE key = 'cloud_default_burst_enabled'`); err != nil {
		t.Fatalf("corrupt the default: %v", err)
	}
	eligible, err = f.svc.CloudEligibleJobs(ctx, []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("a malformed default must not error the query: %v", err)
	}
	if len(eligible) != 0 {
		t.Errorf("a malformed default must refuse, not permit; got %d eligible rows", len(eligible))
	}
}
