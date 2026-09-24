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
	/*
	 * DeactivateForCloudInstance kills the credential when the instance it was
	 * minted for definitively fails.
	 *
	 * Part of the issuer interface rather than a loose repository call because
	 * minting and killing are the same responsibility: whatever can hand out a
	 * registration credential must be able to take it back. Returns how many
	 * were deactivated so callers log a real number.
	 */
	DeactivateForCloudInstance(ctx context.Context, cloudInstanceID uuid.UUID) (int64, error)
}

/*
 * readyDeadlineWindow is how long a rented instance has to get its agent
 * registered before it is destroyed unused.
 *
 * Enforced twice on purpose, from this one value: the reaper checks the row's
 * ready_deadline_at, and the guest checks KH_READY_DEADLINE_EPOCH itself. The
 * two cover different outages — the reaper cannot act when the backend is down,
 * and the guest cannot act when its own agent process is wedged — and the
 * scenario that motivated the guest copy is exactly the one where the backend
 * is unreachable from the instance.
 *
 * TEN minutes, not the twenty this started at, because this window IS the unit
 * price of a misconfiguration. An instance that can never register bills for
 * the whole of it, and the autoscaler's dead-on-arrival breaker only stops
 * after several such instances -- so the window multiplies. At twenty minutes
 * and a g4dn.xlarge, three dead-on-arrival launches cost 53 cents; at ten they
 * cost 26.
 *
 * Ten is still generous against measured behaviour. On a real launch the
 * console showed the image pulled by t+106s and the agent started at t+136s,
 * so registration happens inside three minutes; this leaves better than 3x
 * headroom for a slower instance type or a cold image. Raise it if a fleet
 * genuinely boots slower -- but understand that every extra minute is paid for
 * on each failed launch, not just once.
 */
const readyDeadlineWindow = 10 * time.Minute

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

	/*
	 * CrackDrainGrace mirrors the reaper's clock of the same name, and is used
	 * only to size a TTL's drain tail — see drainTail.
	 *
	 * Held here rather than read per launch because, like the reaper's copy, it
	 * is loaded once at startup. Zero means "not wired or deliberately
	 * disabled", and drainTail degrades to the teardown slack alone rather than
	 * to nothing: the chunk planner has already subtracted that slack and
	 * assumed it exists.
	 */
	CrackDrainGrace time.Duration

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

/*
 * decodeSettings re-parses a provider config's untyped JSONB settings into that
 * provider's typed struct.
 *
 * Round-tripping through JSON rather than reflecting over the map keeps the
 * struct tags as the single definition of the wire shape, so a settings key is
 * spelled once. Every field must therefore tolerate being absent: configs
 * written before a setting existed simply decode to its zero value, and that
 * zero value has to reproduce the old behaviour.
 */
func decodeSettings[T any](cfg *models.CloudProviderConfig) (T, error) {
	var out T
	raw, err := json.Marshal(cfg.Settings)
	if err != nil {
		return out, fmt.Errorf("encode %s settings for %s: %w", cfg.Provider, cfg.Name, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("parse %s settings for %s: %w", cfg.Provider, cfg.Name, err)
	}
	return out, nil
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
		// cfg.Settings was ignored entirely for Vast.ai until placement policy
		// existed. Every pre-existing config therefore decodes to the zero
		// VastSettings, which reproduces the old behaviour exactly: verified
		// datacenter hosts only, any country, any card, no reliability floor.
		vastSettings, err := decodeSettings[VastSettings](cfg)
		if err != nil {
			return nil, err
		}
		return NewVastAIProvider(creds, vastSettings), nil

	case models.CloudProviderAWS:
		settings, err := decodeSettings[AWSSettings](cfg)
		if err != nil {
			return nil, err
		}
		var awsCreds AWSCredentials
		if creds != "" {
			if err := json.Unmarshal([]byte(creds), &awsCreds); err != nil {
				return nil, fmt.Errorf("parse AWS credentials for %s: %w", cfg.Name, err)
			}
		}
		return NewAWSProvider(ctx, settings, awsCreds)

	case models.CloudProviderRunPod, models.CloudProviderRunPodCommunity:
		// One adapter, two kinds: cfg.Provider IS the tier, passed straight
		// through. RequiresThirdPartyAck already keys off the same constant,
		// so the Community consent chain needs no adapter code at all.
		runpodSettings, err := decodeSettings[RunPodSettings](cfg)
		if err != nil {
			return nil, err
		}
		return NewRunPodProvider(parseRunPodCredentials(creds), cfg.Provider, runpodSettings)

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
 *
 * cloud_default_burst_enabled exists for cloud-only deployments. je.cloud_burst_enabled
 * is per job and defaults false, so without a server-wide default an operator whose
 * every agent is rented has to tick a box on every preset, workflow and job — and a
 * single missed tick looks identical to a broken install: the job just sits at pending.
 * Resolved here rather than stamped onto rows at job creation so that flipping it also
 * frees the jobs already queued, which is the situation an operator is in when they
 * discover they needed it. It ships false, and COALESCE(..., false) means an
 * unreadable row refuses rather than spends.
 */
const cloudEligibilityPredicate = `
	    (
	        je.cloud_burst_enabled = true
	     OR COALESCE((SELECT lower(btrim(value)) = 'true' FROM system_settings
	                   WHERE key = 'cloud_default_burst_enabled'), false)
	    )
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
 * cloudBillingClientJoin resolves which client a job's cloud spend is billed to.
 *
 * Cloud money is tracked per client everywhere — the ledger, the budget window,
 * the threshold ladder, the spend report — so provisioning needs a client even
 * when the work does not naturally have one. Plenty of deployments never assign
 * clients at all (a single-org install has no billing entity to model), and
 * before this they were locked out of cloud entirely: both entry points joined
 * clients directly, so a client-less hashlist matched no row and the autoscaler
 * said nothing.
 *
 * The fallback is a real client the admin nominates, not a synthetic NULL
 * bucket. That keeps ONE budget path rather than a second one guarded by NULL
 * checks, and it means unassigned spend appears as an ordinary row in the
 * client budget UI and the spend report instead of somewhere those pages cannot
 * render. Name it "Unassigned Work" and the report reads honestly.
 *
 * Still an INNER JOIN, deliberately. If the setting is unset, malformed, or
 * names a client that has since been deleted, COALESCE yields NULL, nothing
 * matches, and the job is ineligible — the same refusal as before this existed.
 * A LEFT JOIN here would let a job through with no budget holder at all, which
 * is the one outcome worse than refusing to rent.
 *
 * The regex guard matters: value::uuid on '' or 'none' raises and would error
 * the whole query, stalling provisioning for every job rather than just this
 * one. Same defensive shape as the numeric guard on the max-instances default.
 *
 * Expects `je` and `h` (hashlists) to be in scope; binds `c`.
 */
const cloudBillingClientID = `COALESCE(
	    h.client_id,
	    (SELECT CASE
	              WHEN btrim(value) ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
	              THEN btrim(value)::uuid
	            END
	       FROM system_settings WHERE key = 'cloud_default_client_id')
	)`

// Shared by the eligibility queries and by explainIneligible, so the diagnostic
// can never name a cause the eligibility check does not actually have. The two
// hand-written copies of the older predicate had already drifted once.
const cloudBillingClientJoin = `JOIN clients c ON c.id = ` + cloudBillingClientID

/*
 * explainIneligible turns "no rows matched" into the specific precondition that
 * failed.
 *
 * Deliberately a separate query run only on the failure path, so the hot
 * eligibility check stays a single indexed lookup. It re-derives each clause of
 * cloudEligibilityPredicate independently — plus the client join, which is not
 * IN the predicate but is what silently excludes a client-less hashlist.
 *
 * Never returns an empty string: an operator reading "not cloud-eligible" with
 * no reason is exactly the situation this exists to end.
 */
/*
 * billingClientPhrase names which client a message is about.
 *
 * "cloud is disabled for its client" sends an operator to a hashlist that has
 * no client, looking for a setting that is not there. When the fallback is in
 * play the client they need is the one they nominated, and nothing on the job
 * points at it.
 */
func billingClientPhrase(isFallback bool) string {
	if isFallback {
		return "the default billing client this unassigned work bills to"
	}
	return "its client"
}

func (s *Service) explainIneligible(ctx context.Context, jobID uuid.UUID) string {
	var (
		jobExists        bool
		burstEnabled     sql.NullBool
		status           sql.NullString
		hasClient        bool
		clientIsFallback bool
		cloudEnabled     sql.NullBool
		hasAllowlist     sql.NullBool
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT true,
		       je.cloud_burst_enabled
		         OR COALESCE((SELECT lower(btrim(value)) = 'true' FROM system_settings
		                       WHERE key = 'cloud_default_burst_enabled'), false),
		       je.status,
		       -- Not "does the hashlist have a client" but "is there a client to
		       -- bill", which the nominated default can satisfy.
		       c.id IS NOT NULL,
		       h.client_id IS NULL,
		       COALESCE(
		           c.cloud_enabled,
		           (SELECT lower(btrim(value)) = 'true' FROM system_settings
		             WHERE key = 'cloud_default_cloud_enabled')
		       ),
		       (
		           array_length(c.cloud_provider_allowlist, 1) > 0
		        OR COALESCE((SELECT btrim(value) FROM system_settings
		                      WHERE key = 'cloud_default_provider_allowlist'), '') <> ''
		       )
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		LEFT JOIN clients c ON c.id = `+cloudBillingClientID+`
		WHERE je.id = $1`, jobID).
		Scan(&jobExists, &burstEnabled, &status, &hasClient, &clientIsFallback,
			&cloudEnabled, &hasAllowlist)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "no such job"
		}
		return fmt.Sprintf("could not determine why (%v)", err)
	}

	switch {
	case !hasClient:
		return "its hashlist is not assigned to a client, and no default billing client " +
			"is set. Cloud spend is tracked and budgeted per client, so there has to be " +
			"one to charge. Either assign a client to the hashlist, or — if this " +
			"deployment does not use clients — nominate one under Cloud Provisioning -> " +
			"System -> Cloud-only deployments and all unassigned work bills to it"
	case burstEnabled.Valid && !burstEnabled.Bool:
		return "cloud burst is not enabled on it. Tick \"Allow cloud burst\" on the job, " +
			"or on the preset/workflow it is created from. In a deployment with no on-prem " +
			"GPUs, set cloud_default_burst_enabled instead so every job is opted in"
	case status.Valid && status.String != "pending" && status.String != "running":
		return fmt.Sprintf("its status is %q; only pending or running jobs can provision", status.String)
	case cloudEnabled.Valid && !cloudEnabled.Bool:
		return "cloud is disabled for " + billingClientPhrase(clientIsFallback) +
			". Enable it on that client, or set the server default in " +
			"Cloud Provisioning -> Client Budgets"
	case hasAllowlist.Valid && !hasAllowlist.Bool:
		return billingClientPhrase(clientIsFallback) + " permits no cloud providers. " +
			"Set an allowlist on that client, or a server default in " +
			"Cloud Provisioning -> Client Budgets"
	}
	return "one of its cloud preconditions is not met"
}

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
	/*
	 * The allowlist is resolved through the server default, not just
	 * NULL-coalesced to empty.
	 *
	 * An empty per-client allowlist means "inherit" since inheritance landed,
	 * exactly as a NULL budget does. Reading it raw made every inheriting
	 * client fail here with "no permitted cloud providers" AFTER passing the
	 * eligibility predicate — the autoscaler picked the job up once a minute
	 * and failed it once a minute, which looks like a provider problem rather
	 * than a resolution bug.
	 *
	 * Elements are trimmed individually because the setting is a
	 * comma-separated string an admin may well have typed with spaces, and
	 * " aws" matches no provider.
	 */
	err := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.max_instance_ttl_minutes, 0),
		       CASE WHEN array_length(c.cloud_provider_allowlist, 1) > 0
		            THEN c.cloud_provider_allowlist
		            ELSE COALESCE((
		                SELECT array_agg(btrim(p))
		                FROM system_settings ss,
		                     unnest(string_to_array(ss.value, ',')) AS p
		                WHERE ss.key = 'cloud_default_provider_allowlist'
		                  AND btrim(p) <> ''
		            ), '{}')
		       END,
		       je.cloud_allow_community_hosts
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		`+cloudBillingClientJoin+`
		WHERE je.id = $1 AND `+cloudEligibilityPredicate, jobID).
		Scan(&clientID, &clientName, &maxTTLMinutes, pq.Array(&allowlist), &allowCommunity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// "no rows in result set" names none of the four conditions that
			// could have failed, and this is the message an operator sees after
			// clicking Provision now. Two of them fail completely silently
			// otherwise: a hashlist with no client can never match, because both
			// entry points INNER JOIN clients.
			return fmt.Errorf("job %s is not cloud-eligible: %s", jobID, s.explainIneligible(ctx, jobID))
		}
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
		switch {
		case errors.Is(lastErr, ErrOfferUnavailable):
			debug.Warning("Cloud: offer %s on %s vanished between search and launch (%d of %d): %v; "+
				"trying the next candidate", cand.ID, cand.cfg.Name, i+1, len(ranked), lastErr)
		case errors.Is(lastErr, errProviderLocal):
			debug.Warning("Cloud: %s cannot launch right now (%d of %d): %v; trying the next candidate, "+
				"which may be on a different provider", cand.cfg.Name, i+1, len(ranked), lastErr)
		default:
			return lastErr
		}
	}
	return fmt.Errorf("all %d ranked cloud offers were exhausted: %w", len(ranked), lastErr)
}

/*
 * errProviderLocal marks a failure that is (a) confined to ONE provider config
 * and (b) provably pre-launch, so nothing was created and nothing is billing.
 *
 * It exists because the launch walk was correctly conservative and therefore
 * too broad: any error other than ErrOfferUnavailable aborted the entire
 * provision, including every candidate belonging to a DIFFERENT provider. An
 * expired Vast.ai reusable key would stop AWS being tried at all — the precise
 * failure that rankedCandidates already goes out of its way to isolate one step
 * earlier, undone one step later.
 *
 * The bar for wearing this sentinel is deliberately high, and ambiguity
 * disqualifies. A timeout at the provider may have created a billing instance,
 * so it must keep aborting the walk and leave the row for the reaper to
 * reconcile by label. Only errors raised before the instance row, the voucher,
 * the reservation and the provider call qualify.
 */
var errProviderLocal = errors.New("provider-local failure")

/*
 * killVoucher deactivates the registration credential for an instance whose
 * launch DEFINITIVELY failed.
 *
 * "Definitively" is the whole contract. A cloud voucher is minted before the
 * provider is called and is a working claim code until its TTL expires, so an
 * attempt that fails in its first second otherwise leaves a live credential for
 * up to an hour — once per candidate offer, which on a multi-zone AWS config is
 * up to nine per provision.
 *
 * Call this ONLY where the instance is known not to exist. Never call it on the
 * ambiguous launch path: there the instance may be running, and its agent needs
 * this code to register.
 *
 * Never blocks the caller. Failing to deactivate is worth an error line, but the
 * provisioning path has already decided what it is doing and the sweep will
 * remove the row later regardless.
 */
func (s *Service) killVoucher(ctx context.Context, instanceID uuid.UUID, why string) {
	n, err := s.vouchers.DeactivateForCloudInstance(ctx, instanceID)
	if err != nil {
		debug.Error("Cloud: could not deactivate the claim voucher for %s (%s): %v; it stays redeemable "+
			"until it expires", instanceID, why, err)
		return
	}
	if n > 0 {
		debug.Info("Cloud: deactivated %d unredeemed claim voucher(s) for %s (%s)", n, instanceID, why)
	}
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
 * sizeTTL narrows the client's TTL ceiling to what THIS job actually needs.
 *
 * The ceiling on its own is a blunt instrument. A client configured for 4-hour
 * rentals gets a 4-hour reservation for a job with twenty minutes of work left,
 * because ReserveCents commits the whole TTL up front. Nothing is ultimately
 * BILLED for that — SettleInstance refunds reserved minus incurred, and idle
 * drain destroys an instance that runs out of work — but the money is committed
 * while the instance runs, and a cap that fits one oversized reservation fits
 * several right-sized ones. Sizing the reservation to the work is what lets a
 * fixed budget buy several instances in parallel instead of one at a time.
 *
 * Commissioning is added on top of the projection, not folded into it: the
 * instance cannot start consuming keyspace until it has booted, synced and
 * benchmarked, so a TTL equal to the remaining work would expire with work
 * still outstanding and force a fresh rental to re-pay the whole setup.
 *
 * NARROWING ONLY. This never raises the ceiling above what the operator or the
 * budget allows; PlanLaunch still applies both bounds and the minimum-rental
 * floor underneath. The worst case here is that the projection is wrong and we
 * reserve more than needed, which is the refundable direction.
 *
 * Called once per launch attempt rather than per autoscaler pass. Project() is
 * several joins and an aggregate over every scheduling unit of the job — costly
 * enough that rulesgate guards it behind the priority floor — but an attempt to
 * actually rent hardware is rare and already about to make network calls.
 */
/*
 * drainTail is the runway a sized TTL must keep AFTER the last chunk finishes,
 * so the instance can hand back what it found instead of being shot mid-upload.
 *
 * Two distinct waits live at the end of a rental and they were not the same
 * length:
 *
 *   - resolveChunkDuration already refuses to plan a chunk past
 *     (remaining TTL - teardown slack), so the last chunk ends ~120s before the
 *     deadline. That 120s is all the upload time a TTL-bound instance had.
 *   - The reaper is willing to hold an instance for CrackDrainGrace (10 minutes
 *     by default) while its agent is still sending cracks, precisely because a
 *     large upload can take minutes.
 *
 * The reaper's patience is worth nothing if the machine is already gone:
 * ttl_epoch is armed IN-GUEST and the watchdog powers off regardless of what
 * the backend would prefer. So an instance sized to finish its work exactly at
 * its TTL gets 120 seconds to upload, and anything slower is killed mid-flush —
 * losing cracks on the ordinary successful path, which is the failure
 * cloud_cracks_lost exists to report and which no amount of backend patience can
 * prevent.
 *
 * Sizing the tail to the grace the reaper already honours makes the two agree.
 * It costs nothing in practice: the tail is only ever RESERVED, and an instance
 * that finishes early is released by the job-finished rung with the remainder
 * refunded.
 */
func (s *Service) drainTail() time.Duration {
	grace := s.CrackDrainGrace
	if grace <= 0 {
		// Unset (a Service built without wiring) or deliberately disabled. The
		// teardown slack is still owed either way — it is the window the chunk
		// planner has already subtracted and assumed is there.
		return defaultTeardownSlack
	}
	return defaultTeardownSlack + grace
}

func (s *Service) sizeTTL(ctx context.Context, p provisionParams, cand rankedCandidate) time.Duration {
	if s.estimator == nil {
		return p.maxTTL
	}

	/*
	 * The hypothetical is what makes this work on the FIRST rental.
	 *
	 * Project derives throughput from agents that already hold a task on the
	 * job. In a cloud-only deployment there are none — that is precisely why the
	 * job is starving — so without a hypothetical the projection is always
	 * unknown at the moment it matters, and job-aware sizing would only ever
	 * apply from the second rental onwards.
	 *
	 * cand.AbsoluteSpeed is zero when the ranker had no calibration anchor, and
	 * zero here means UNKNOWN. Passing it through unchanged is correct: it adds
	 * nothing to the projection, the projection reports itself unknown, and we
	 * fall back to the ceiling below.
	 */
	proj, err := s.estimator.Project(ctx, p.jobID, cand.AbsoluteSpeed, 0, 0)
	if err != nil {
		// A job with no scheduling units cannot be projected at all. Fall back
		// rather than fail the launch: an unprojectable job is still a job
		// someone asked to run, and this is a sizing hint, not a gate.
		debug.Warning("Cloud: could not project job %s for TTL sizing (%v); "+
			"using the full %s ceiling", p.jobID, err, p.maxTTL)
		return p.maxTTL
	}

	if !proj.TimeToFinishKnown {
		debug.Debug("Cloud: job %s has no usable throughput projection "+
			"(offer speed %d h/s); using the full %s TTL ceiling",
			p.jobID, cand.AbsoluteSpeed, p.maxTTL)
		return p.maxTTL
	}

	/*
	 * Only add commissioning and the drain tail once the projection is known to
	 * be SHORTER than the ceiling, because that addition can overflow.
	 *
	 * The estimator saturates at time.Duration(math.MaxInt64) on purpose — see
	 * maxProjection — so a job with negligible throughput legitimately projects
	 * to ~292 years. Adding 20 minutes to that wraps to a large NEGATIVE
	 * duration, which clampTTL then reads as "smaller than the floor" and rounds
	 * UP to the minimum rental. The job that needs the most runway would have
	 * received the least. Comparing first keeps the arithmetic inside a few
	 * hours, where it cannot wrap.
	 */
	target := p.maxTTL
	if proj.TimeToFinish < p.maxTTL {
		target = proj.TimeToFinish + commissioningBudget + s.drainTail()
	}

	sized := clampTTL(target, MinRentalTTL(s.budget.MaxCommissioningPct), p.maxTTL)
	if sized < p.maxTTL {
		debug.Info("Cloud: job %s projected to finish in %s at %d h/s; "+
			"sizing TTL to %s instead of the %s ceiling, so the reservation matches the work",
			p.jobID, proj.TimeToFinish.Round(time.Second), cand.AbsoluteSpeed,
			sized.Round(time.Second), p.maxTTL)
	}
	return sized
}

/*
 * clampTTL fits a desired rental length between the minimum worth making and
 * the most the client and budget allow.
 *
 * THE UPPER BOUND WINS. When the ceiling is itself below the floor, this returns
 * the ceiling and lets PlanLaunch refuse with the operator's own number in the
 * message. Clamping up to the floor instead would hand PlanLaunch a value the
 * client never permitted, and the refusal would quote a lifetime nobody
 * configured.
 *
 * The lower bound exists because narrowing must never CAUSE a refusal. A job
 * with two minutes of work left produces a tiny target; measured against the
 * floor that reads as "not worth renting", even though the unnarrowed ceiling
 * would have launched happily. Sizing back up costs nothing real — the
 * job-finished rung releases the instance as soon as the work is done and
 * SettleInstance refunds the remainder.
 */
func clampTTL(target, floor, upper time.Duration) time.Duration {
	if target > upper {
		return upper
	}
	if target < floor {
		if floor > upper {
			return upper
		}
		return floor
	}
	return target
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
	ttlCeiling := s.sizeTTL(ctx, p, cand)
	extraCents := int64(offer.StorageCentsPerHour) * int64(ttlCeiling/time.Hour+1)
	plan, err := s.budget.PlanLaunch(ctx, p.clientID, offer.HourlyRateCents, ttlCeiling, extraCents)
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
	//
	// Both are marked PROVIDER-LOCAL: they concern this provider config's own
	// VPN credential and they happen before any row, voucher, reservation or
	// provider call, so nothing exists and nothing is billing. The launch walk
	// can move on to another provider's candidates instead of failing the whole
	// provision — which is what an expired Vast.ai reusable key used to do to
	// AWS. See errProviderLocal.
	decryptedVPN, err := s.decrypt(cfg.VPNCredentialEncrypted)
	if err != nil {
		return fmt.Errorf("%w: decrypt VPN credential for %s: %w", errProviderLocal, cfg.Name, err)
	}
	vpnCred, err := s.minter.Mint(ctx, cfg, decryptedVPN, plan.TTL)
	if err != nil {
		return fmt.Errorf("%w: VPN credential for %s: %w", errProviderLocal, cfg.Name, err)
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
		ReadyDeadlineAt:    nullTime(now.Add(readyDeadlineWindow)),
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
		s.killVoucher(ctx, instanceID, "budget reservation failed")
		return fmt.Errorf("reserve budget: %w", err)
	}

	// 8. Launch.
	// readyDeadlineWindow is passed rather than a second literal so the guest's
	// registration rail and the row's ready_deadline_at are the same instant.
	// Two independently written windows would drift, and the failure of the
	// shorter one would look like the other rail misfiring.
	env := BuildAgentEnv(cfg.BackendVPNHost, voucher.Code, string(cfg.VPNProvider),
		vpnCred.AuthKey, vpnCred.LoginServer, vpnCred.Tag,
		plan.TTL, 15*time.Minute, readyDeadlineWindow, string(cfg.Provider))
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
			s.killVoucher(ctx, instanceID, "offer vanished before launch")
			return fmt.Errorf("launch %s: %w", offer.ID, err)
		}

		/*
		 * Anything else is AMBIGUOUS -- a timeout may have created a billing
		 * instance. The row stays behind deliberately. The reaper reconciles it
		 * by label, which is the only way to find an instance whose launch
		 * response was lost but which is nonetheless running and billing.
		 *
		 * THE VOUCHER IS DELIBERATELY LEFT ALIVE HERE, and this is the one
		 * branch of the three where that is true. If the instance did come up,
		 * its agent still has to redeem this code to register — killing it
		 * would strand a machine that is already billing and convert a
		 * recoverable launch into guaranteed waste. The reaper kills the
		 * voucher when it finalises the row, by which point the outcome is
		 * known.
		 */
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

	/*
	 * The deployment-wide INSTANCE ceiling, checked here as well as in the
	 * autoscaler.
	 *
	 * It used to live only in the autoscaler, which meant the admin
	 * "Provision now" button walked straight past it: an operator who had set
	 * "never more than two rented boxes" could click their way to a third, and
	 * nothing anywhere said the cap had been bypassed. The spend cap above was
	 * the only thing still standing, and a cap denominated in dollars does not
	 * stop you having ten instances — it just stops you having them for long.
	 *
	 * Duplicated rather than moved because the two callers need different
	 * behaviour: the autoscaler records a per-job diagnostic and returns
	 * quietly, while this path owes the operator who clicked the button an
	 * error they can read.
	 */
	var countLive func() (int, error)
	if s.instances != nil {
		countLive = func() (int, error) { return s.instances.CountLive(ctx) }
	}
	return enforceInstanceCap(settings.GlobalInstanceCap, countLive)
}

/*
 * enforceInstanceCap is the deployment-wide live-instance ceiling.
 *
 * Extracted from checkGlobalCap so it can be tested without standing up a
 * Service, a settings repository and a database — the alternative was a test
 * that grepped its own source, which passes for the wrong reasons the moment
 * anyone renames a variable.
 *
 * A nil counter means the cap CANNOT BE EVALUATED, and that must fail closed.
 * Treating it as unlimited is the same silent-omission bug the autoscaler
 * guards against: the operator's "never more than N rented boxes" quietly
 * becomes no limit at all, with nothing in the logs, the settings screen or the
 * tests to say so.
 */
func enforceInstanceCap(cap int, countLive func() (int, error)) error {
	if cap <= 0 {
		return nil
	}
	if countLive == nil {
		return fmt.Errorf("the deployment-wide instance cap (%s = %d) cannot be checked because no "+
			"live-instance counter is wired in; refusing to provision rather than treating the cap "+
			"as unlimited", SettingGlobalInstanceCap, cap)
	}
	live, err := countLive()
	if err != nil {
		return fmt.Errorf("cannot verify the deployment-wide instance cap: %w", err)
	}
	if live >= cap {
		return fmt.Errorf("deployment-wide cloud instance cap reached: %d of %d instances are live (%s). "+
			"Raise it in Cloud Provisioning settings, or wait for one to finish",
			live, cap, SettingGlobalInstanceCap)
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

/*
 * JobInstanceState implements Provisioner.
 *
 * "Commissioning" is deliberately defined as "no job_tasks row has ever named
 * this instance's agent", not as a lifecycle state on the row. The states an
 * instance passes through (provisioning, running) say where the PROVIDER
 * thinks it is; they say nothing about whether it has become useful to us. An
 * instance can sit in `running` for twenty minutes downloading a hashcat
 * archive, and during all of that the job it was rented for keeps publishing as
 * starving.
 *
 * agent_id IS NULL counts as commissioning because that instance is still
 * booting — it has not registered at all yet. An instance that was retargeted
 * to a different job carries its earlier tasks and correctly reads as warm.
 */
func (s *Service) JobInstanceState(ctx context.Context, jobID uuid.UUID) (int, int, error) {
	var live, commissioning int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*),
		       count(*) FILTER (
		           WHERE ci.agent_id IS NULL
		              OR NOT EXISTS (
		                  SELECT 1 FROM job_tasks t WHERE t.agent_id = ci.agent_id
		              )
		       )
		FROM cloud_instances ci
		WHERE ci.job_execution_id = $1
		  AND ci.state NOT IN ('terminated', 'failed')`, jobID).Scan(&live, &commissioning)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read instance state for job %s: %w", jobID, err)
	}
	return live, commissioning, nil
}

/*
 * DeadOnArrivalCountForJob implements Provisioner.
 *
 * "Dead on arrival" is launched_at set (the provider really created it, so it
 * really billed) with ready_at never stamped (the agent never completed
 * registration) and the row now terminal. ready_at is written by the agent
 * registration path, which is what makes it the honest test of whether the
 * rented machine was ever reachable rather than merely running.
 *
 * Rows that never launched are excluded on purpose: they cost nothing, and
 * counting them would let a harmless transient — a momentarily empty offer
 * list, a provider 500 — trip a breaker meant for wasted money.
 */
func (s *Service) DeadOnArrivalCountForJob(ctx context.Context, jobID uuid.UUID) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM cloud_instances
		WHERE job_execution_id = $1
		  AND launched_at IS NOT NULL
		  AND ready_at IS NULL
		  AND state IN ('terminated','failed')`, jobID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count dead-on-arrival instances for job %s: %w", jobID, err)
	}
	return n, nil
}

/*
 * ConsecutiveDeadOnArrivals implements Provisioner.
 *
 * Counts back from the most recent launch and stops at the first instance that
 * DID register. A plain total would be permanently poisoned by old failures --
 * a deployment that failed five times in its first week could never provision
 * again -- whereas a streak is self-clearing: one instance that registers
 * proves rented hardware can reach the backend right now, which is the only
 * thing this rail cares about.
 *
 * Bounded to a window because it is only ever compared against a small limit,
 * so reading further back cannot change the answer and would grow with the
 * table forever.
 */
func (s *Service) ConsecutiveDeadOnArrivals(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ready_at IS NULL
		FROM cloud_instances
		WHERE launched_at IS NOT NULL
		  AND state IN ('terminated','failed')
		ORDER BY launched_at DESC
		LIMIT 25`)
	if err != nil {
		return 0, fmt.Errorf("read dead-on-arrival streak: %w", err)
	}
	defer rows.Close()

	streak := 0
	for rows.Next() {
		var deadOnArrival bool
		if err := rows.Scan(&deadOnArrival); err != nil {
			return 0, fmt.Errorf("scan dead-on-arrival streak: %w", err)
		}
		if !deadOnArrival {
			break
		}
		streak++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read dead-on-arrival streak: %w", err)
	}
	return streak, nil
}

// CloudEligibleJobs implements Provisioner: filters candidates to jobs that
// opted in, whose client is funded and has permitted at least one provider.
func (s *Service) CloudEligibleJobs(ctx context.Context, candidates []uuid.UUID) ([]EligibleJob, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT je.id, c.id,
		       /*
		        * A blank per-job cap inherits the server default rather than
		        * meaning "unlimited".
		        *
		        * Unlimited was defensible while two other brakes engaged: the
		        * autoscaler refuses to rent while any on-prem agent is idle,
		        * and the finishing-soon rule skips jobs about to complete. Both
		        * are computed from on-prem agents, so in a cloud-only
		        * deployment both are structurally dead — and "blank" is both
		        * the default and the easy path. That left a job whose only
		        * bound was the budget, renting once per tick until it drained.
		        *
		        * Same shape and the same regex guard as the budget default
		        * below: a malformed setting reads as absent, which falls back
		        * to 0 (unlimited) rather than erroring the whole query.
		        */
		       COALESCE(
		         je.cloud_max_instances,
		         (SELECT CASE WHEN btrim(value) ~ '^[0-9]+$' THEN btrim(value)::int END
		            FROM system_settings WHERE key = 'cloud_default_max_instances_per_job'),
		         0
		       ),
		       je.priority,
		       EXTRACT(EPOCH FROM je.created_at)::bigint * 1000000000
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		`+cloudBillingClientJoin+`
		WHERE je.id = ANY($1::uuid[])
		  AND `+cloudEligibilityPredicate+`
		  /*
		   * Autoscaler-only: an unfunded client should never be picked up
		   * automatically, but on the manual path PlanLaunch's "no budget"
		   * message beats a row that silently does not match.
		   *
		   * Resolved through the server default, because a NULL budget stopped
		   * meaning "unfunded" when inheritance landed and now means "use the
		   * default". Left as a bare IS NOT NULL, this clause excluded every
		   * inheriting client from automatic provisioning -- so setting a
		   * default budget appeared to do nothing at all, which is the exact
		   * failure the default was introduced to fix.
		   *
		   * The regex guard keeps a non-numeric settings value from erroring the
		   * whole query: a malformed default then reads as no default, and the
		   * client is simply not auto-provisioned.
		   */
		  AND COALESCE(
		        c.cloud_budget_cents,
		        (SELECT CASE WHEN btrim(value) ~ '^[0-9]+$' THEN btrim(value)::bigint END
		           FROM system_settings WHERE key = 'cloud_default_client_budget_cents')
		      ) IS NOT NULL`,
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
