package cloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/crypto"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// VoucherIssuer mints the one-time claim code a cloud agent registers with.
//
// The instance id is part of the mint rather than a later update: registration
// reads the agent's cloud identity off the voucher, so a voucher that exists
// unbound for even a moment is one an agent could redeem to register as an
// ordinary on-prem agent with none of the isolation that applies to rented
// hardware.
type VoucherIssuer interface {
	CreateCloudVoucher(ctx context.Context, expiresIn time.Duration, cloudInstanceID uuid.UUID) (*models.ClaimVoucher, error)
}

// Service orchestrates cloud provisioning.
type Service struct {
	db        *db.DB
	providers *repository.CloudProviderRepository
	instances *repository.CloudInstanceRepository
	budget    *BudgetEngine
	fileset   *FileSetResolver
	vouchers  VoucherIssuer
	minter    *VPNMinter

	// AgentImage is the container image cloud agents run.
	AgentImage string
	// SystemUserID owns vouchers minted for cloud agents.
	SystemUserID string
	// benchmarks supplies measured per-GPU speeds so offers can be ranked by
	// cost per unit of WORK rather than cost per hour. Nil is survivable: the
	// ranker falls back to static class priors, which is the cold-start path
	// every fresh deployment starts on anyway.
	benchmarks *repository.CloudGPUBenchmarkRepository

	// SystemSettings supplies the deployment-wide spend ceiling. Nil means the
	// ceiling cannot be read, and ProvisionForJob then refuses rather than
	// assuming there is none — an unreadable kill switch has to behave like an
	// engaged one.
	SystemSettings *repository.SystemSettingsRepository

	// rules supplies the admin provisioning rails — WHEN spending may happen,
	// as distinct from the budget engine's HOW MUCH.
	rules *repository.CloudProvisioningRulesRepository
	// estimator answers "how long will this take", which the finishing-soon
	// rule needs. It is read-only and cheap enough to build here rather than
	// threading another constructor argument through main.go.
	estimator *Estimator

	mu    sync.Mutex
	cache map[uuid.UUID]Provider
}

// NewService creates the cloud service.
func NewService(
	database *db.DB,
	providers *repository.CloudProviderRepository,
	instances *repository.CloudInstanceRepository,
	budget *BudgetEngine,
	fileset *FileSetResolver,
	vouchers VoucherIssuer,
) *Service {
	return &Service{
		db:         database,
		providers:  providers,
		instances:  instances,
		budget:     budget,
		fileset:    fileset,
		vouchers:   vouchers,
		benchmarks: repository.NewCloudGPUBenchmarkRepository(database),
		rules:      repository.NewCloudProvisioningRulesRepository(database),
		estimator:  NewEstimator(database),
		minter:     NewVPNMinter(),
		cache:      make(map[uuid.UUID]Provider),
	}
}

// ProviderFor resolves and caches a live provider client for a config.
func (s *Service) ProviderFor(ctx context.Context, configID uuid.UUID) (Provider, error) {
	s.mu.Lock()
	if p, ok := s.cache[configID]; ok {
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	cfg, err := s.providers.GetByID(ctx, configID)
	if err != nil {
		return nil, err
	}
	p, err := s.buildProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[configID] = p
	s.mu.Unlock()
	return p, nil
}

// InvalidateProvider drops a cached client after its configuration changes.
func (s *Service) InvalidateProvider(configID uuid.UUID) {
	s.mu.Lock()
	delete(s.cache, configID)
	s.mu.Unlock()
}

func (s *Service) buildProvider(ctx context.Context, cfg *models.CloudProviderConfig) (Provider, error) {
	creds, err := s.decrypt(cfg.CredentialsEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials for %s: %w", cfg.Name, err)
	}

	switch cfg.Provider {
	case models.CloudProviderVastAI:
		if creds == "" {
			return nil, fmt.Errorf("vast.ai provider %s has no API key", cfg.Name)
		}
		return NewVastAIProvider(creds), nil

	case models.CloudProviderAWS:
		settingsJSON, err := json.Marshal(cfg.Settings)
		if err != nil {
			return nil, err
		}
		var settings AWSSettings
		if err := json.Unmarshal(settingsJSON, &settings); err != nil {
			return nil, fmt.Errorf("parse AWS settings for %s: %w", cfg.Name, err)
		}
		var awsCreds AWSCredentials
		if creds != "" {
			if err := json.Unmarshal([]byte(creds), &awsCreds); err != nil {
				return nil, fmt.Errorf("parse AWS credentials for %s: %w", cfg.Name, err)
			}
		}
		return NewAWSProvider(ctx, settings, awsCreds)

	case models.CloudProviderMock:
		binary, _ := cfg.Settings["agent_binary"].(string)
		host, _ := cfg.Settings["backend_host"].(string)
		m := NewMockProvider(binary, host)

		// Fault injection, driven from the provider row so a developer can
		// exercise the failure branches without recompiling. JSONB numbers
		// unmarshal as float64.
		if v, ok := cfg.Settings["hourly_rate_cents"].(float64); ok {
			m.HourlyRateCents = int(v)
		}
		if v, ok := cfg.Settings["boot_delay_seconds"].(float64); ok {
			m.BootDelay = time.Duration(v) * time.Second
		}
		m.FailLaunch, _ = cfg.Settings["fail_launch"].(bool)
		m.DropLaunchResponse, _ = cfg.Settings["drop_launch_response"].(bool)
		m.NeverRegister, _ = cfg.Settings["never_register"].(bool)
		m.FailDestroy, _ = cfg.Settings["fail_destroy"].(bool)
		return m, nil

	default:
		return nil, fmt.Errorf("unsupported cloud provider %q", cfg.Provider)
	}
}

func (s *Service) decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	return crypto.GetEncryptionService().Decrypt(ciphertext)
}

/*
 * cloudEligibilityPredicate is the SQL both entry points into provisioning must
 * agree on: the autoscaler's CloudEligibleJobs and the manual admin route via
 * ProvisionForJob.
 *
 * Kept in one place because the two hand-written copies had ALREADY DRIFTED.
 * Only the autoscaler's copy checked je.status, so the manual route would
 * happily rent a GPU for a cancelled or completed job — an instance the
 * scheduler is then forbidden to give work to, billing by the second until its
 * TTL expires.
 *
 * Deliberately NOT included here, and left to the autoscaler's own query:
 *
 *   c.cloud_budget_cents IS NOT NULL — PlanLaunch already refuses an unfunded
 *   client with a message naming the problem, which is far better on the manual
 *   path than a row that silently fails to match.
 *
 * Expects `je` (job_executions) and `c` (clients) to be in scope.
 */
/*
 * The client-side clauses read through the server defaults, because
 * clients.cloud_enabled is now tri-state and an empty allowlist means "inherit"
 * rather than "nothing allowed".
 *
 * Written as scalar subqueries against system_settings rather than as bound
 * parameters so that BOTH entry points get the resolution for free. The two
 * copies of this predicate had already drifted once; a version that has to be
 * fed the defaults correctly by each caller is a version that will drift again,
 * and the direction it fails in is renting hardware for a client nobody funded.
 *
 * COALESCE on cloud_enabled: NULL means the client never chose, so fall through
 * to cloud_default_cloud_enabled. A missing settings row yields NULL, and
 * `NULL = true` is not true, so an unreadable default refuses rather than
 * permits.
 */
const cloudEligibilityPredicate = `
	    je.cloud_burst_enabled = true
	AND je.status IN ('pending','running')
	AND COALESCE(
	        c.cloud_enabled,
	        (SELECT lower(btrim(value)) = 'true' FROM system_settings
	          WHERE key = 'cloud_default_cloud_enabled')
	    ) = true
	AND (
	        array_length(c.cloud_provider_allowlist, 1) > 0
	     OR COALESCE((SELECT btrim(value) FROM system_settings
	                   WHERE key = 'cloud_default_provider_allowlist'), '') <> ''
	    )`

/*
 * ProvisionForJob rents one instance for a job.
 *
 * ORDER IS THE CONTRACT. Everything that can fail without costing money
 * happens first, and the database row plus its budget reservation are written
 * BEFORE the provider is called:
 *
 *   1. resolve job, client, provider, and check consent
 *   2. resolve the job's file set and size the disk
 *   3. search offers
 *   4. plan the budget and derive the TTL
 *   5. mint the VPN credential      <- fail closed
 *   6. mint the claim voucher       <- fail closed
 *   7. write the row + reserve budget
 *   8. call the provider
 *
 * If the process dies between 7 and 8, a row exists with a label and no
 * provider ID, and the reaper reconciles it by label. If we wrote the row
 * after the provider responded, a lost response would leave a running,
 * billing instance nothing in the system knew about.
 */
func (s *Service) ProvisionForJob(ctx context.Context, jobID uuid.UUID) error {
	// 1. Job, client and consent.
	var clientID uuid.UUID
	var clientName string
	var maxTTLMinutes int
	var allowlist []string
	var allowCommunity bool
	err := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.max_instance_ttl_minutes, 0), COALESCE(c.cloud_provider_allowlist, '{}'),
		       je.cloud_allow_community_hosts
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		JOIN clients c ON c.id = h.client_id
		WHERE je.id = $1 AND `+cloudEligibilityPredicate, jobID).
		Scan(&clientID, &clientName, &maxTTLMinutes, pq.Array(&allowlist), &allowCommunity)
	if err != nil {
		return fmt.Errorf("job %s is not cloud-eligible: %w", jobID, err)
	}

	/*
	 * 1a. The admin provisioning rails that are NOT admin-bypassable.
	 *
	 * Only two of the five rules are enforced here, and the split is the whole
	 * design. The priority floor and the finishing-soon skip mean "not worth
	 * spending on AUTOMATICALLY" — an operator clicking Provision has made that
	 * judgement by hand, so those are filters on the autoscaler's candidate set
	 * rather than gates on this path. The spend cap and the time window are
	 * different in kind:
	 *
	 *   A per-job spend cap an admin can click past is not a spend cap.
	 *   A provisioning window usually encodes something EXTERNAL — a contract
	 *   clause, a change freeze, a client's own policy — not an operator
	 *   preference, so "I am an admin" is not the authority that overrides it.
	 *
	 * Both fail closed on an unreadable figure, exactly as checkGlobalCap does.
	 */
	if err := s.checkProvisioningRules(ctx, jobID, clientID); err != nil {
		return err
	}

	// 1b. The deployment-wide ceiling, checked before anything else costs
	//     anything. Per-client budgets bound one engagement; this bounds the
	//     whole install, and it is also the kill switch an operator reaches for
	//     when they want provisioning to stop right now.
	if err := s.checkGlobalCap(ctx); err != nil {
		return err
	}

	// 2. File set and disk. Vast.ai disk is immutable after creation, so this
	//    must be right before anything is rented. Provider-independent, so it
	//    is resolved once and shared by every attempt below.
	fileSet, err := s.fileset.Resolve(ctx, jobID)
	if err != nil {
		return fmt.Errorf("resolve job file set: %w", err)
	}

	p := provisionParams{
		jobID:        jobID,
		clientID:     clientID,
		clientName:   clientName,
		diskGB:       fileSet.RequiredDiskGB(DefaultDiskSizing()),
		filesetBytes: fileSet.TotalBytes,
		maxTTL:       time.Duration(maxTTLMinutes) * time.Minute,
	}
	if p.maxTTL <= 0 {
		p.maxTTL = 4 * time.Hour
	}

	// 3. Offers from EVERY permitted provider, ranked together by cost per unit
	//    of work. All of it is read-only, so it stays inside the free prefix.
	ranked, err := s.rankedCandidates(ctx, allowlist, p, allowCommunity)
	if err != nil {
		return err
	}

	/*
	 * 4-8, attempted per candidate.
	 *
	 * ErrOfferUnavailable is the ONLY retryable error, and the sentinel's own
	 * doc says callers should try the next offer — but until now nothing did,
	 * so capacity vanishing between search and launch failed the whole
	 * provision. On a marketplace that is an ordinary, frequent event.
	 *
	 * Retrying is safe only because of what the sentinel MEANS: the provider
	 * rejected the create, so no instance exists and no money is running. Any
	 * other error is ambiguous — a timeout may have created a billing
	 * instance — so it stops the loop and leaves the row for the reaper to
	 * reconcile by label.
	 */
	var lastErr error
	for i, cand := range ranked {
		lastErr = s.attemptLaunch(ctx, p, cand)
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, ErrOfferUnavailable) {
			return lastErr
		}
		debug.Warning("Cloud: offer %s on %s vanished between search and launch (%d of %d); trying the next candidate",
			cand.ID, cand.cfg.Name, i+1, len(ranked))
	}
	return fmt.Errorf("every one of the %d ranked cloud offers became unavailable: %w", len(ranked), lastErr)
}

// provisionParams is everything an attempt needs that does not vary by offer,
// so a retry never re-resolves the file set or re-reads the client.
type provisionParams struct {
	jobID        uuid.UUID
	clientID     uuid.UUID
	clientName   string
	diskGB       int
	filesetBytes int64
	maxTTL       time.Duration
}

// rankedCandidate is a ranked offer together with the provider that quoted it.
type rankedCandidate struct {
	RankedOffer
	cfg      *models.CloudProviderConfig
	provider Provider
}

/*
 * attemptLaunch runs steps 4-8 for one candidate offer.
 *
 * ORDER IS THE CONTRACT, and it is preserved per attempt: the row and its
 * reservation are written before the provider is called, so a lost response
 * always leaves something the reaper can reconcile by label.
 *
 * Each attempt gets its OWN plan, row and reservation rather than reusing the
 * first one. That is not incidental. Ranking by cost per WORK means the second
 * candidate can be more expensive per hour than the first, so a reservation
 * made against the first offer's rate would silently under-reserve — and the
 * client's cap is only as hard as the arithmetic behind it.
 */
func (s *Service) attemptLaunch(ctx context.Context, p provisionParams, cand rankedCandidate) error {
	cfg, provider, offer := cand.cfg, cand.provider, cand.Offer

	// 4. Budget and TTL.
	extraCents := int64(offer.StorageCentsPerHour) * int64(p.maxTTL/time.Hour+1)
	plan, err := s.budget.PlanLaunch(ctx, p.clientID, offer.HourlyRateCents, p.maxTTL, extraCents)
	if err != nil {
		return fmt.Errorf("budget: %w", err)
	}

	// 4a. The per-job ceiling, now that the launch has a price. This is the
	//     first moment the reserve amount exists, and it is still before any
	//     row, voucher or reservation is written, so refusing here costs
	//     nothing and leaves nothing to clean up.
	if err := s.checkJobSpendCap(ctx, p.jobID, p.clientID, plan.ReserveCents); err != nil {
		return err
	}

	// 5 & 6. Credentials. Both fail closed: an instance that cannot join the
	// VPN or cannot register can never reach the backend, so it would burn
	// money until its watchdog fired.
	decryptedVPN, err := s.decrypt(cfg.VPNCredentialEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt VPN credential: %w", err)
	}
	vpnCred, err := s.minter.Mint(ctx, cfg, decryptedVPN, plan.TTL)
	if err != nil {
		return fmt.Errorf("VPN credential: %w", err)
	}

	// 7. Row + voucher + reservation, before the provider is touched.
	instanceID := uuid.New()
	label := fmt.Sprintf("kh-%s", instanceID.String()[:18])
	now := time.Now()

	inst := &models.CloudInstance{
		ID:                 instanceID,
		ProviderConfigID:   cfg.ID,
		Label:              label,
		IdempotencyKey:     label,
		JobExecutionID:     &p.jobID,
		ClientID:           &p.clientID,
		ClientNameSnapshot: p.clientName,
		State:              models.CloudInstanceRequested,
		GPUModel:           offer.GPUModel,
		GPUCount:           offer.GPUCount,
		HourlyRateCents:    offer.HourlyRateCents,
		DiskGB:             p.diskGB,
		FilesetBytes:       p.filesetBytes,
		ReservedCents:      plan.ReserveCents,
		LaunchDeadlineAt:   nullTime(now.Add(10 * time.Minute)),
		ReadyDeadlineAt:    nullTime(now.Add(20 * time.Minute)),
		TTLEpoch:           nullTime(now.Add(plan.TTL)),
		VPNCredentialRef:   vpnCred.Ref,
	}
	if err := s.instances.Create(ctx, inst); err != nil {
		return fmt.Errorf("record instance: %w", err)
	}

	// The voucher is minted AFTER the row exists because it carries a foreign
	// key to it — and that ordering is deliberate rather than incidental. A
	// voucher that briefly exists unbound is a registration credential an agent
	// could redeem to join as an ordinary on-prem agent, with none of the
	// job locking or file-sync scoping that rented hardware must have.
	//
	// Failing here is clean: the row is marked failed and nothing has been
	// reserved or rented yet.
	voucher, err := s.vouchers.CreateCloudVoucher(ctx, plan.TTL, instanceID)
	if err != nil {
		_ = s.instances.SetState(ctx, instanceID, models.CloudInstanceFailed, "claim voucher could not be issued")
		return fmt.Errorf("claim voucher: %w", err)
	}

	if _, err := s.budget.Reserve(ctx, p.clientID, instanceID, &p.jobID, plan,
		fmt.Sprintf("launch %s (%s @ %d cents/hr for %s)", label, offer.GPUModel, offer.HourlyRateCents, plan.TTL)); err != nil {
		_ = s.instances.SetState(ctx, instanceID, models.CloudInstanceFailed, "budget reservation failed")
		return fmt.Errorf("reserve budget: %w", err)
	}

	// 8. Launch.
	env := BuildAgentEnv(cfg.BackendVPNHost, voucher.Code, string(cfg.VPNProvider),
		vpnCred.AuthKey, vpnCred.LoginServer, vpnCred.Tag,
		plan.TTL, 15*time.Minute, string(cfg.Provider))
	env["KH_JOB_ID"] = p.jobID.String()

	if err := s.instances.SetState(ctx, instanceID, models.CloudInstanceLaunching, ""); err != nil {
		debug.Warning("could not mark instance launching: %v", err)
	}

	result, err := provider.Launch(ctx, LaunchRequest{
		Label:          label,
		IdempotencyKey: label,
		Offer:          offer,
		DiskGB:         p.diskGB,
		TTL:            plan.TTL,
		Env:            env,
		Image:          s.agentImage(ctx),
	})
	if err != nil {
		/*
		 * A VANISHED OFFER is the one failure we can clean up after.
		 *
		 * ErrOfferUnavailable means the provider rejected the create, so no
		 * instance exists and nothing is billing. Releasing the reservation and
		 * finalising the row keeps the caller free to try the next candidate
		 * without stacking a dead reservation per attempt -- six attempts
		 * against a busy marketplace would otherwise consume six instances'
		 * worth of a client's budget and refuse the seventh for no reason.
		 *
		 * The error is returned unwrapped enough for errors.Is to see the
		 * sentinel, because that is what tells the caller this attempt is
		 * genuinely retryable.
		 */
		if errors.Is(err, ErrOfferUnavailable) {
			if relErr := s.budget.repo.ReleaseUnused(ctx, &p.clientID, instanceID, plan.ReserveCents,
				fmt.Sprintf("offer %s vanished before launch", offer.ID)); relErr != nil {
				debug.Error("could not release the reservation for vanished offer %s: %v", label, relErr)
			}
			_ = s.instances.SetState(ctx, instanceID, models.CloudInstanceFailed, "offer no longer available")
			return fmt.Errorf("launch %s: %w", offer.ID, err)
		}

		// Anything else is AMBIGUOUS -- a timeout may have created a billing
		// instance. The row stays behind deliberately. The reaper reconciles it
		// by label, which is the only way to find an instance whose launch
		// response was lost but which is nonetheless running and billing.
		_ = s.instances.SetState(ctx, instanceID, models.CloudInstanceRequested, fmt.Sprintf("launch failed: %v", err))
		return fmt.Errorf("launch: %w", err)
	}

	if err := s.instances.MarkLaunched(ctx, instanceID, result.ProviderInstanceID,
		result.BilledFrom, now.Add(plan.TTL), result.Raw); err != nil {
		return fmt.Errorf("record launch: %w", err)
	}

	debug.Info("Provisioned cloud instance %s (%s) on %s for job %s: %s x%d, %d cents/hr, TTL %s -- %s",
		label, result.ProviderInstanceID, cfg.Name, p.jobID, offer.GPUModel, offer.GPUCount,
		offer.HourlyRateCents, plan.TTL, cand.Reason)
	return nil
}

/*
 * checkGlobalCap enforces cloud_global_monthly_cap_cents.
 *
 * Zero means DISABLED, not unlimited. That reading is the one the setting's own
 * description has always promised, and it is the right default for a feature
 * that spends money: someone must state a ceiling before this deployment can
 * rent anything. The alternative reading — 0 as unlimited — would mean a fresh
 * install ships with the system-wide brake off.
 *
 * Fails closed on an unreadable setting for the same reason. An operator who
 * set a cap and then lost their database is far better served by "no
 * provisioning" than by "unlimited provisioning".
 */
/*
 * agentImage resolves the container image rented instances pull.
 *
 * Read per launch rather than latched at startup, so changing it in the admin
 * UI takes effect on the next instance instead of the next restart. Precedence:
 * the database setting, then the AgentImage field main.go seeded from
 * KH_CLOUD_AGENT_IMAGE, then the compiled default.
 *
 * The fallbacks matter because the compiled default (:latest) does not exist
 * until a release is tagged, and a wrong image is not a startup error: the
 * instance boots, `docker pull` fails, the bootstrap disarms the deadline and
 * the host terminates about a minute later. That minute is billed and the
 * evidence dies with the instance.
 */
func (s *Service) agentImage(ctx context.Context) string {
	if s.SystemSettings != nil {
		if image := LoadSettings(ctx, s.SystemSettings).AgentImage; image != "" {
			return image
		}
	}
	if s.AgentImage != "" {
		return s.AgentImage
	}
	return DefaultAgentImage
}

func (s *Service) checkGlobalCap(ctx context.Context) error {
	if s.SystemSettings == nil {
		return fmt.Errorf("cloud provisioning is not configured: the system-wide spend " +
			"ceiling (" + SettingGlobalMonthlyCapCents + ") cannot be read")
	}

	settings := LoadSettings(ctx, s.SystemSettings)
	if settings.GlobalMonthlyCapCents <= 0 {
		return fmt.Errorf("cloud provisioning is disabled: set %s to a non-zero monthly "+
			"ceiling in cents to enable it", SettingGlobalMonthlyCapCents)
	}

	committed, err := s.budget.repo.GlobalCommittedThisMonth(ctx)
	if err != nil {
		// Unable to tell how much this deployment has already committed. Renting
		// now could be the launch that blows past the ceiling.
		return fmt.Errorf("cannot verify the system-wide cloud spend ceiling: %w", err)
	}
	if committed >= settings.GlobalMonthlyCapCents {
		return fmt.Errorf("system-wide cloud spend ceiling reached: %d of %d cents "+
			"committed this month (%s)", committed, settings.GlobalMonthlyCapCents,
			SettingGlobalMonthlyCapCents)
	}
	return nil
}

/*
 * rankedCandidates gathers offers from EVERY permitted provider and ranks them
 * together by cost per unit of work.
 *
 * Until this existed, pickProvider returned the FIRST permitted provider and
 * offers were searched only inside it, then offers[0] was taken — the cheapest
 * per HOUR, from whichever provider happened to sort first. Two separate ways
 * to pay more than necessary: never comparing vendors, and comparing the wrong
 * quantity within one.
 *
 * Read-only from end to end, so it all stays inside ProvisionForJob's free
 * prefix — nothing here can cost money or leave state behind.
 */
func (s *Service) rankedCandidates(ctx context.Context, allowlist []string, p provisionParams, allowCommunity bool) ([]rankedCandidate, error) {
	cfgs, skipped, err := s.eligibleProviders(ctx, allowlist, allowCommunity)
	if err != nil {
		return nil, err
	}
	if len(cfgs) == 0 {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("no cloud provider is currently usable for this client: %s",
				strings.Join(skipped, "; "))
		}
		return nil, fmt.Errorf("no enabled provider is permitted for this client")
	}

	work, err := s.workSignature(ctx, p.jobID)
	if err != nil {
		// Ranking still works without it — it just falls back to class priors
		// for every offer, which is the cold-start behaviour. Not worth
		// refusing to provision over.
		debug.Warning("Cloud: could not resolve the work signature for job %s (%v); "+
			"ranking on static GPU priors only", p.jobID, err)
	}

	var all []rankedCandidate
	for _, cfg := range cfgs {
		provider, err := s.ProviderFor(ctx, cfg.ID)
		if err != nil {
			// FAILURE ISOLATION: one provider whose credentials no longer
			// decrypt, or whose API is down, must not stop the others from
			// being considered. Renting nothing because Vast.ai is having an
			// outage, while AWS sits there ready, is a worse answer than a
			// slightly narrower candidate list.
			debug.Error("Cloud: provider %s unavailable, skipping it: %v", cfg.Name, err)
			skipped = append(skipped, fmt.Sprintf("%s (unavailable: %v)", cfg.Name, err))
			continue
		}

		offers, err := provider.SearchOffers(ctx, OfferQuery{
			MinGPUCount:        1,
			MaxHourlyRateCents: cfg.MaxInstanceHourlyCents,
			MinDiskGB:          p.diskGB,
			MinDuration:        2 * p.maxTTL, // capacity that expires mid-job is wasted spend
			VerifiedOnly:       true,
			Limit:              25,
		})
		if err != nil {
			debug.Error("Cloud: offer search failed on %s, skipping it: %v", cfg.Name, err)
			skipped = append(skipped, fmt.Sprintf("%s (offer search failed: %v)", cfg.Name, err))
			continue
		}
		if len(offers) == 0 {
			skipped = append(skipped, fmt.Sprintf("%s (no offer satisfies the request)", cfg.Name))
			continue
		}

		/*
		 * Ranked per provider, then merged.
		 *
		 * rankOffers is deliberately single-provider because observations are
		 * provider-scoped — the same die in someone else's chassis is weaker
		 * evidence than the hardware we would actually rent. Merging afterwards
		 * is sound precisely because the ranking key is cost per REFERENCE-GPU
		 * hour, which is provider-independent by construction. That is the
		 * whole reason a cross-vendor comparison means anything at all.
		 */
		observed := s.observationsFor(ctx, cfg.Provider, work)
		ranked, _ := rankOffers(RankInput{
			Candidates:        offers,
			Provider:          cfg.Provider,
			Work:              work,
			Observed:          observed,
			Class:             DefaultGPUClasses,
			MinSamples:        DefaultMinSamples,
			MaxObservationAge: DefaultMaxObservationAge,
			UnknownRelative:   DefaultUnknownRelative,
			Now:               time.Now(),
		})
		for _, r := range ranked {
			all = append(all, rankedCandidate{RankedOffer: r, cfg: cfg, provider: provider})
		}
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no cloud offer satisfies the request (disk >= %dGB): %s",
			p.diskGB, strings.Join(skipped, "; "))
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].CostPerWorkUnitCents != all[j].CostPerWorkUnitCents {
			return all[i].CostPerWorkUnitCents < all[j].CostPerWorkUnitCents
		}
		if all[i].Confidence != all[j].Confidence {
			return all[i].Confidence > all[j].Confidence
		}
		if all[i].HourlyRateCents != all[j].HourlyRateCents {
			return all[i].HourlyRateCents < all[j].HourlyRateCents
		}
		// Provider name last, so a tie across vendors is at least deterministic
		// and a test cannot flake on map iteration order.
		return all[i].cfg.Name < all[j].cfg.Name
	})

	debug.Info("Cloud: %d candidate offer(s) across %d provider(s) for job %s; best is %s",
		len(all), len(cfgs), p.jobID, all[0].Reason)
	return all, nil
}

/*
 * workSignature resolves the (attack mode, hash type, salt count) a job's
 * throughput actually depends on.
 *
 * The salt-count rule MIRRORS the write path in HandleBenchmarkResult exactly:
 * a salted hash type is keyed by the hashlist's TOTAL hash count, and an
 * unsalted one by NULL. It has to, because that column is part of the
 * observation key — a read that derives it differently would look up rows that
 * were never written under that key, and cost-per-work ranking would silently
 * fall back to static priors forever with nothing in the logs.
 */
func (s *Service) workSignature(ctx context.Context, jobID uuid.UUID) (WorkSignature, error) {
	var w WorkSignature
	var salted bool
	var totalHashes int

	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE((
		           SELECT su.attack_mode FROM scheduling_units su
		           WHERE su.parent_job_id = je.id
		           ORDER BY su.layer_index, su.created_at LIMIT 1), 0),
		       h.hash_type_id,
		       ht.is_salted,
		       COALESCE(h.total_hashes, 0)
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		JOIN hash_types ht ON ht.id = h.hash_type_id
		WHERE je.id = $1`, jobID).
		Scan(&w.AttackMode, &w.HashType, &salted, &totalHashes)
	if err != nil {
		return w, err
	}
	if salted && totalHashes > 0 {
		n := totalHashes
		w.SaltCount = &n
	}
	return w, nil
}

// observationsFor loads the measured speeds for one provider and work
// signature, keyed the way the ranker looks them up. A failure is not fatal:
// the ranker falls back to class priors, which is the cold-start path.
func (s *Service) observationsFor(ctx context.Context, provider models.CloudProvider, w WorkSignature) map[string]ObservedSpeed {
	if s.benchmarks == nil {
		return nil
	}
	rows, err := s.benchmarks.List(ctx, w.AttackMode, w.HashType, w.SaltCount)
	if err != nil {
		debug.Warning("Cloud: could not load GPU observations (%v); ranking on static priors", err)
		return nil
	}
	out := make(map[string]ObservedSpeed, len(rows))
	for _, r := range rows {
		if models.CloudProvider(r.Provider) != provider {
			continue
		}
		o := ObservedSpeed{
			Provider:    models.CloudProvider(r.Provider),
			GPUKey:      r.GPUModel,
			GPUCount:    r.GPUCount,
			Speed:       r.Speed,
			SampleCount: r.SampleCount,
			UpdatedAt:   r.UpdatedAt,
		}
		out[observationKey(o.Provider, o.GPUKey, o.GPUCount)] = o
	}
	return out
}

/*
 * eligibleProviders returns every enabled provider the client permits, plus a
 * human-readable reason for each one that was rejected.
 *
 * The reasons matter: "no enabled provider is permitted" is indistinguishable
 * between a missing allowlist entry, a missing acknowledgement and a provider
 * sitting at its concurrency cap — three problems with three different fixes.
 */
func (s *Service) eligibleProviders(ctx context.Context, allowlist []string, allowCommunity bool) ([]*models.CloudProviderConfig, []string, error) {
	if len(allowlist) == 0 {
		return nil, nil, fmt.Errorf("client has no permitted cloud providers (cloud_provider_allowlist is empty)")
	}
	permitted := make(map[string]bool, len(allowlist))
	for _, p := range allowlist {
		permitted[p] = true
	}

	configs, err := s.providers.ListEnabled(ctx)
	if err != nil {
		return nil, nil, err
	}

	var out []*models.CloudProviderConfig
	var skipped []string
	for _, cfg := range configs {
		if !permitted[string(cfg.Provider)] {
			continue
		}
		if cfg.Provider.RequiresThirdPartyAck() {
			if !cfg.ThirdPartyAckAt.Valid {
				debug.Warning("Peer provider %s is enabled but the third-party data-exposure acknowledgement is missing; skipping", cfg.Name)
				skipped = append(skipped, fmt.Sprintf("%s (data-exposure acknowledgement missing)", cfg.Name))
				continue
			}
			/*
			 * The per-job peer opt-in, enforced as a PROVIDER FILTER rather
			 * than as a refusal.
			 *
			 * A job without the flag simply does not see peer offers; it can
			 * still rent secure capacity from the rest of the allowlist. That
			 * is deliberately softer than a hard error: the consent question
			 * is "may this client's hashes land on someone else's machine",
			 * and the answer being "no" is not a reason to stop the job from
			 * bursting to AWS or RunPod Secure.
			 *
			 * NOTE this also gates Vast.ai, which TIGHTENS existing behaviour:
			 * a job that bursts to Vast today stops doing so until the flag is
			 * set. That is the consistent reading of "peer hosts need a per-job
			 * opt-in" and the safe direction for a change that moves client
			 * data.
			 */
			if !allowCommunity {
				skipped = append(skipped, fmt.Sprintf(
					"%s (peer-operated hardware; this job has not opted in)", cfg.Name))
				continue
			}
		}
		if cfg.MaxConcurrentInstances > 0 {
			live, err := s.instances.CountLiveForConfig(ctx, cfg.ID)
			if err != nil {
				debug.Error("Cloud: cannot count live instances for provider %s; skipping it: %v", cfg.Name, err)
				skipped = append(skipped, fmt.Sprintf("%s (instance count unavailable)", cfg.Name))
				continue
			}
			if live >= cfg.MaxConcurrentInstances {
				skipped = append(skipped, fmt.Sprintf("%s (at its %d-instance cap)", cfg.Name, cfg.MaxConcurrentInstances))
				continue
			}
		}
		out = append(out, cfg)
	}
	return out, skipped, nil
}

// LiveInstanceCountForJob implements Provisioner.
func (s *Service) LiveInstanceCountForJob(ctx context.Context, jobID uuid.UUID) (int, error) {
	instances, err := s.instances.ListLiveForJob(ctx, jobID)
	if err != nil {
		return 0, err
	}
	return len(instances), nil
}

// CloudEligibleJobs implements Provisioner: filters candidates to jobs that
// opted in, whose client is funded and has permitted at least one provider.
func (s *Service) CloudEligibleJobs(ctx context.Context, candidates []uuid.UUID) ([]EligibleJob, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT je.id, c.id, COALESCE(je.cloud_max_instances, 0), je.priority,
		       EXTRACT(EPOCH FROM je.created_at)::bigint * 1000000000
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		JOIN clients c ON c.id = h.client_id
		WHERE je.id = ANY($1::uuid[])
		  AND `+cloudEligibilityPredicate+`
		  -- Autoscaler-only: an unfunded client should never be picked up
		  -- automatically, but on the manual path PlanLaunch's "no budget"
		  -- message beats a row that silently does not match.
		  AND c.cloud_budget_cents IS NOT NULL`,
		pq.Array(candidates))
	if err != nil {
		return nil, fmt.Errorf("resolve cloud-eligible jobs: %w", err)
	}
	defer rows.Close()

	var out []EligibleJob
	for rows.Next() {
		var j EligibleJob
		if err := rows.Scan(&j.JobExecutionID, &j.ClientID, &j.MaxInstances, &j.Priority, &j.CreatedAtNanos); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	/*
	 * The SOFT admin rails, applied in Go rather than as WHERE clauses.
	 *
	 * Neither can be expressed in the query above: the rules are a per-client
	 * merge of two rows with NULL meaning inherit, and the finishing-soon rule
	 * needs the estimator. Filtering here also keeps the manual admin route —
	 * which does not call this function — free of them, which is the intended
	 * asymmetry: these rules mean "not worth spending on automatically".
	 */
	return s.applySoftRules(ctx, out), nil
}

// LoadAgentJobLocks implements scheduler.CloudAgentLocks.
func (s *Service) LoadAgentJobLocks(ctx context.Context) (map[int]uuid.UUID, error) {
	return s.instances.LoadAgentJobLocks(ctx)
}

func nullTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }
