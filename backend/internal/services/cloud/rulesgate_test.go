package cloud

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/crypto"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

// setRules writes a client override through the real repository, so these tests
// exercise the same read path production uses rather than a hand-built struct.
func setRules(t *testing.T, f *provisionFixture, r *models.CloudProvisioningRules) {
	t.Helper()
	r.ClientID = &f.clientID
	repo := repository.NewCloudProvisioningRulesRepository(f.db)
	if err := repo.UpsertRules(context.Background(), r); err != nil {
		t.Fatalf("write provisioning rules: %v", err)
	}
}

// aWindowExcludingNow returns a one-hour window starting two hours from now, in
// UTC. Computed rather than hardcoded because a fixed "02:00-03:00" would pass
// or fail depending on when the suite runs.
func aWindowExcludingNow() (start, end string) {
	now := time.Now().UTC()
	return now.Add(2 * time.Hour).Format("15:04:05"), now.Add(3 * time.Hour).Format("15:04:05")
}

/*
 * TestProvisionForJob_TimeWindowGatesTheManualRouteToo.
 *
 * The window is a HARD rule, and the manual admin route is exactly the path
 * that would otherwise bypass it. A provisioning window normally encodes
 * something external — a contract clause, a client's change freeze — rather
 * than an operator preference, so "I am an admin" is not the authority that
 * overrides it.
 */
func TestProvisionForJob_TimeWindowGatesTheManualRouteToo(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "any", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 50, MaxDuration: 24 * time.Hour},
	})

	start, end := aWindowExcludingNow()
	setRules(t, f, &models.CloudProvisioningRules{
		ProvisioningWindowStart: &start,
		ProvisioningWindowEnd:   &end,
		ProvisioningWindowTZ:    strPtr("UTC"),
	})

	err := f.svc.ProvisionForJob(context.Background(), f.job.JobID)
	if err == nil {
		t.Fatal("rented paid capacity outside the configured provisioning window")
	}
	if !strings.Contains(err.Error(), "provisioning window") {
		t.Errorf("refusal should name the window, got %v", err)
	}
	if f.provider.launched != 0 {
		t.Errorf("the provider was called %d times despite the window being shut", f.provider.launched)
	}
}

// TestProvisionForJob_InsideTheWindowStillProvisions is the control: the gate
// must refuse on the window, not on having a window.
func TestProvisionForJob_InsideTheWindowStillProvisions(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "any", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 50, MaxDuration: 24 * time.Hour},
	})

	// start == end is the in-band OFF value: always open.
	setRules(t, f, &models.CloudProvisioningRules{
		ProvisioningWindowStart: strPtr("00:00:00"),
		ProvisioningWindowEnd:   strPtr("00:00:00"),
		ProvisioningWindowTZ:    strPtr("UTC"),
	})

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("an always-open window must not block provisioning: %v", err)
	}
}

/*
 * TestProvisionForJob_PerJobSpendCapGatesTheManualRoute.
 *
 * An admin-bypassable spend cap is not a spend cap. The cap here is below the
 * cost of a single launch, so the reserve alone crosses it — which is the case
 * that has to be caught BEFORE the instance exists, not after.
 */
func TestProvisionForJob_PerJobSpendCapGatesTheManualRoute(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "any", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 500, MaxDuration: 24 * time.Hour},
	})

	// One cent. Any real reservation exceeds it.
	setRules(t, f, &models.CloudProvisioningRules{MaxSpendPerJobCents: i64Ptr(1)})

	err := f.svc.ProvisionForJob(context.Background(), f.job.JobID)
	if err == nil {
		t.Fatal("rented paid capacity for a job already at its per-job spend cap")
	}
	if !strings.Contains(err.Error(), "max_spend_per_job_cents") {
		t.Errorf("refusal should name the rule, got %v", err)
	}

	// Nothing may survive a refusal at this stage: the cap is checked after
	// PlanLaunch prices the offer but before any row, voucher or reservation
	// is written.
	var rows int
	if err := f.db.QueryRow(
		`SELECT count(*) FROM cloud_instances WHERE job_execution_id = $1`, f.job.JobID).Scan(&rows); err != nil {
		t.Fatalf("count instances: %v", err)
	}
	if rows != 0 {
		t.Errorf("a cap refusal left %d instance row(s) behind", rows)
	}
}

/*
 * TestProvisionForJob_SoftRulesDoNotGateTheManualRoute is the other half of the
 * soft/hard split, and the one most likely to be broken by a well-meaning
 * "apply the rules consistently everywhere" change.
 *
 * A priority floor means "not important enough to spend on AUTOMATICALLY". An
 * operator clicking Provision has made that judgement by hand; refusing them
 * turns an autoscaler tuning knob into a lockout with no override.
 */
func TestProvisionForJob_SoftRulesDoNotGateTheManualRoute(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "any", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 50, MaxDuration: 24 * time.Hour},
	})

	// The seeded job's priority is 5; both soft rules are set to refuse it.
	setRules(t, f, &models.CloudProvisioningRules{
		MinJobPriority:               intPtr(900),
		SkipIfFinishingWithinSeconds: intPtr(86_400),
	})

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("the manual admin route must not be blocked by the soft autoscaler "+
			"rails (priority floor, finishing-soon): %v", err)
	}
}

/*
 * TestCloudEligibleJobs_AppliesThePriorityFloor covers the same two rules from
 * the other side: they must actually filter the autoscaler's candidate set,
 * which is the only place they are meant to bite.
 */
func TestCloudEligibleJobs_AppliesThePriorityFloor(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, nil)
	ctx := context.Background()
	candidates := []uuid.UUID{f.job.JobID}

	// Baseline: with no floor the job is a candidate.
	got, err := f.svc.CloudEligibleJobs(ctx, candidates)
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the job to be eligible with no floor configured, got %d rows", len(got))
	}

	setRules(t, f, &models.CloudProvisioningRules{MinJobPriority: intPtr(900)})

	got, err = f.svc.CloudEligibleJobs(ctx, candidates)
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a job below the priority floor must not be an automatic-provisioning "+
			"candidate, got %d rows", len(got))
	}
}

/*
 * TestCloudEligibleJobs_CarriesTheStarvationThreshold.
 *
 * The threshold is resolved here but compared in the autoscaler, because the
 * age lives in the starvation snapshot and the scheduler rewrites it every 3
 * seconds. If it stops being carried the autoscaler silently reverts to renting
 * on the first starving tick — the pre-rule behaviour, with the rule still
 * displayed as configured in the admin UI.
 */
func TestCloudEligibleJobs_CarriesTheStarvationThreshold(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, nil)
	setRules(t, f, &models.CloudProvisioningRules{MinStarvationSeconds: intPtr(600)})

	got, err := f.svc.CloudEligibleJobs(context.Background(), []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 eligible job, got %d", len(got))
	}
	if got[0].MinStarvationSeconds != 600 {
		t.Errorf("MinStarvationSeconds = %d, want 600 — the autoscaler will rent on the "+
			"first starving tick without it", got[0].MinStarvationSeconds)
	}
}

/*
 * TestJobCommittedCents_NetsReleases pins the query behind the per-job cap.
 *
 * The obvious implementation filters cloud_spend_ledger.job_execution_id, which
 * only `reservation` rows carry — so it sums GROSS reservations with no
 * releases netted out. Every job whose instance terminated early over-reports,
 * and the cap then refuses launches for money that was already handed back.
 */
func TestJobCommittedCents_NetsReleases(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "any", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 100, MaxDuration: 24 * time.Hour},
	})
	ctx := context.Background()

	if err := f.svc.ProvisionForJob(ctx, f.job.JobID); err != nil {
		t.Fatalf("ProvisionForJob: %v", err)
	}

	reserved, err := f.svc.jobCommittedCents(ctx, f.job.JobID)
	if err != nil {
		t.Fatalf("jobCommittedCents: %v", err)
	}
	if reserved <= 0 {
		t.Fatalf("expected a positive reservation after a launch, got %d", reserved)
	}

	// Hand back part of the reservation, the way an early teardown does.
	var instanceID uuid.UUID
	if err := f.db.QueryRow(
		`SELECT id FROM cloud_instances WHERE job_execution_id = $1`, f.job.JobID).Scan(&instanceID); err != nil {
		t.Fatalf("read instance: %v", err)
	}
	refund := -(reserved / 2)
	if _, err := f.db.Exec(`
		INSERT INTO cloud_spend_ledger (client_id, cloud_instance_id, cents, kind, note)
		VALUES ($1, $2, $3, 'release', 'test refund')`,
		f.clientID, instanceID, refund); err != nil {
		t.Fatalf("insert release row: %v", err)
	}

	after, err := f.svc.jobCommittedCents(ctx, f.job.JobID)
	if err != nil {
		t.Fatalf("jobCommittedCents: %v", err)
	}
	if want := reserved + refund; after != want {
		t.Errorf("committed = %d, want %d — releases must net against reservations, or a "+
			"per-job cap refuses launches for money that was given back", after, want)
	}
}

/*
 * TestProvisionForJob_PeerHardwareNeedsThePerJobOptIn.
 *
 * The consent question is "may this client's hashes land on a machine whose
 * owner has root over the container". Without the per-job flag the job must not
 * see peer offers at all.
 *
 * Note this applies to Vast.ai as well as RunPod Community, which TIGHTENS
 * shipped behaviour: a job that bursts to Vast today stops until the flag is
 * set. That is the safe direction for a change that moves client data.
 */
func TestProvisionForJob_PeerHardwareNeedsThePerJobOptIn(t *testing.T) {
	f := newPeerProvisionFixture(t)
	ctx := context.Background()

	err := f.svc.ProvisionForJob(ctx, f.job.JobID)
	if err == nil {
		t.Fatal("placed a job on peer-operated hardware without the per-job opt-in")
	}
	if !strings.Contains(err.Error(), "opted in") {
		t.Errorf("refusal should explain the missing opt-in, got %v", err)
	}
	if f.provider.launched != 0 {
		t.Errorf("the peer provider was called %d times without consent", f.provider.launched)
	}

	// With the flag set, the same job proceeds.
	if _, err := f.db.Exec(
		`UPDATE job_executions SET cloud_allow_community_hosts = true WHERE id = $1`,
		f.job.JobID); err != nil {
		t.Fatalf("set the opt-in flag: %v", err)
	}
	if err := f.svc.ProvisionForJob(ctx, f.job.JobID); err != nil {
		t.Fatalf("with the per-job opt-in set, peer capacity must be usable: %v", err)
	}
	if f.provider.launched != 1 {
		t.Errorf("expected exactly 1 launch after opting in, got %d", f.provider.launched)
	}
}

/*
 * TestProvisionForJob_SecureProvidersNeedNoOptIn is the regression guard for
 * the trust-tier split at the provisioning layer.
 *
 * AWS and RunPod Secure are not peer hardware, so the per-job flag must not
 * apply to them. If it ever does, cloud burst silently stops for every job that
 * did not tick a box about consumer GPUs — on providers where that box is
 * meaningless.
 */
func TestProvisionForJob_SecureProvidersNeedNoOptIn(t *testing.T) {
	// The default fixture's provider kind is `mock`, which is not peer
	// hardware, and its job has cloud_allow_community_hosts = false.
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "any", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 50, MaxDuration: 24 * time.Hour},
	})

	var optedIn bool
	if err := f.db.QueryRow(
		`SELECT cloud_allow_community_hosts FROM job_executions WHERE id = $1`,
		f.job.JobID).Scan(&optedIn); err != nil {
		t.Fatalf("read the opt-in flag: %v", err)
	}
	if optedIn {
		t.Fatal("fixture precondition: the job must NOT have opted in for this test to mean anything")
	}

	if err := f.svc.ProvisionForJob(context.Background(), f.job.JobID); err != nil {
		t.Fatalf("a non-peer provider must not require the peer opt-in: %v", err)
	}
}

/*
 * TestProvisionForJob_RefusesWhenTheRulesCannotBeRead.
 *
 * The system-default row is seeded by migration, so its absence means the
 * table was truncated or the migration did not run. Under the merge semantics
 * an all-nil rules object is MAXIMALLY permissive — any job, any priority, any
 * hour, no cap — so degrading to it would turn a broken database into
 * unrestricted spending permission at the exact moment nobody can see why.
 */
func TestProvisionForJob_RefusesWhenTheRulesCannotBeRead(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, []Offer{
		{ID: "any", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 50, MaxDuration: 24 * time.Hour},
	})

	if _, err := f.db.Exec(`DELETE FROM cloud_provisioning_rules WHERE client_id IS NULL`); err != nil {
		t.Fatalf("remove the system default row: %v", err)
	}

	err := f.svc.ProvisionForJob(context.Background(), f.job.JobID)
	if err == nil {
		t.Fatal("provisioned with an unreadable rules table; a policy we cannot read " +
			"must behave like an engaged one")
	}
	if f.provider.launched != 0 {
		t.Errorf("the provider was called %d times with no readable policy", f.provider.launched)
	}
	if !strings.Contains(err.Error(), "system default row") {
		t.Errorf("refusal should point at the row an operator has to restore, got %v", err)
	}
}

// TestCloudEligibleJobs_DropsJobsWhoseRulesCannotBeRead: the same failure on the
// autoscaler path drops the candidate rather than passing it through. Dropping
// costs a delayed launch; passing through spends money under no policy.
func TestCloudEligibleJobs_DropsJobsWhoseRulesCannotBeRead(t *testing.T) {
	f := newProvisionFixture(t, 1_000_000, nil)

	if _, err := f.db.Exec(`DELETE FROM cloud_provisioning_rules WHERE client_id IS NULL`); err != nil {
		t.Fatalf("remove the system default row: %v", err)
	}

	got, err := f.svc.CloudEligibleJobs(context.Background(), []uuid.UUID{f.job.JobID})
	if err != nil {
		t.Fatalf("CloudEligibleJobs should degrade rather than error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a job whose rules cannot be read must not be an automatic-provisioning "+
			"candidate, got %d rows", len(got))
	}
}

/*
 * newPeerProvisionFixture is newProvisionFixture with a PEER provider kind.
 *
 * Vast.ai rather than runpod_community only because the mock probe stands in
 * for the provider client either way, and vastai exercises the same
 * RequiresThirdPartyAck branch without depending on the RunPod adapter landing
 * first. The client acknowledges the provider at both levels, so the only thing
 * left standing between the job and peer hardware is the per-job opt-in — which
 * is exactly what the test is about.
 */
func newPeerProvisionFixture(t *testing.T) *provisionFixture {
	t.Helper()
	database := requireCloudTestDB(t)

	clientID := testutil.CreateTestClient(t, database, "peer-"+uuid.NewString()[:8],
		testutil.ClientCloudOpts{
			CloudEnabled:      true,
			BudgetCents:       testutil.Int64Ptr(1_000_000),
			ProviderAllowlist: []string{"vastai"},
			MaxTTLMinutes:     testutil.IntPtr(60),
			AckProviders:      []string{"vastai"},
		})
	job := testutil.CreateCloudJob(t, database, clientID, true)

	cfgID := testutil.CreateTestCloudProviderConfig(t, database, "vastai", "peer-"+uuid.NewString()[:8],
		testutil.ProviderConfigOpts{
			Enabled:                true,
			ThirdPartyAcked:        true,
			VPNProvider:            "tailscale",
			VPNCredentialKind:      string(models.VPNCredentialReusableKey),
			MaxInstanceHourlyCents: 1000,
		})

	ciphertext, err := crypto.GetEncryptionService().Encrypt("tskey-reusable-test")
	if err != nil {
		t.Fatalf("encrypt test VPN credential: %v", err)
	}
	if _, err := database.Exec(
		`UPDATE cloud_provider_configs SET vpn_credential_encrypted = $2 WHERE id = $1`,
		cfgID, ciphertext); err != nil {
		t.Fatalf("store test VPN credential: %v", err)
	}

	setGlobalCloudCap(t, database, 1_000_000)

	provider := &probeProvider{offers: []Offer{
		{ID: "peer-offer", InstanceType: "RTX 4090", GPUModel: "RTX 4090",
			GPUCount: 1, HourlyRateCents: 50, MaxDuration: 24 * time.Hour},
	}}

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

	svc.mu.Lock()
	svc.cache[cfgID] = provider
	svc.mu.Unlock()

	return &provisionFixture{
		db: database, svc: svc, provider: provider,
		clientID: clientID, job: job, cfgID: cfgID,
	}
}
