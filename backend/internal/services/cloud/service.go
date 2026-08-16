package cloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
		db:        database,
		providers: providers,
		instances: instances,
		budget:    budget,
		fileset:   fileset,
		vouchers:  vouchers,
		minter:    NewVPNMinter(),
		cache:     make(map[uuid.UUID]Provider),
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
	err := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.name, COALESCE(c.max_instance_ttl_minutes, 0), COALESCE(c.cloud_provider_allowlist, '{}')
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		JOIN clients c ON c.id = h.client_id
		WHERE je.id = $1 AND je.cloud_burst_enabled = true AND c.cloud_enabled = true`, jobID).
		Scan(&clientID, &clientName, &maxTTLMinutes, pq.Array(&allowlist))
	if err != nil {
		return fmt.Errorf("job %s is not cloud-eligible: %w", jobID, err)
	}

	cfg, err := s.pickProvider(ctx, allowlist)
	if err != nil {
		return err
	}
	provider, err := s.ProviderFor(ctx, cfg.ID)
	if err != nil {
		return err
	}

	// 2. File set and disk. Vast.ai disk is immutable after creation, so this
	//    must be right before anything is rented.
	fileSet, err := s.fileset.Resolve(ctx, jobID)
	if err != nil {
		return fmt.Errorf("resolve job file set: %w", err)
	}
	diskGB := fileSet.RequiredDiskGB(DefaultDiskSizing())

	maxTTL := time.Duration(maxTTLMinutes) * time.Minute
	if maxTTL <= 0 {
		maxTTL = 4 * time.Hour
	}

	// 3. Offers.
	offers, err := provider.SearchOffers(ctx, OfferQuery{
		MinGPUCount:        1,
		MaxHourlyRateCents: cfg.MaxInstanceHourlyCents,
		MinDiskGB:          diskGB,
		MinDuration:        2 * maxTTL, // capacity that expires mid-job is wasted spend
		VerifiedOnly:       true,
		Limit:              10,
	})
	if err != nil {
		return fmt.Errorf("search offers: %w", err)
	}
	if len(offers) == 0 {
		return fmt.Errorf("no cloud offer satisfies the request (disk >= %dGB, rate <= %d cents/hr)", diskGB, cfg.MaxInstanceHourlyCents)
	}
	offer := offers[0]

	// 4. Budget and TTL.
	extraCents := int64(offer.StorageCentsPerHour) * int64(maxTTL/time.Hour+1)
	plan, err := s.budget.PlanLaunch(ctx, clientID, offer.HourlyRateCents, maxTTL, extraCents)
	if err != nil {
		return fmt.Errorf("budget: %w", err)
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
		JobExecutionID:     &jobID,
		ClientID:           &clientID,
		ClientNameSnapshot: clientName,
		State:              models.CloudInstanceRequested,
		GPUModel:           offer.GPUModel,
		GPUCount:           offer.GPUCount,
		HourlyRateCents:    offer.HourlyRateCents,
		DiskGB:             diskGB,
		FilesetBytes:       fileSet.TotalBytes,
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

	if _, err := s.budget.Reserve(ctx, clientID, instanceID, &jobID, plan,
		fmt.Sprintf("launch %s (%s @ %d cents/hr for %s)", label, offer.GPUModel, offer.HourlyRateCents, plan.TTL)); err != nil {
		_ = s.instances.SetState(ctx, instanceID, models.CloudInstanceFailed, "budget reservation failed")
		return fmt.Errorf("reserve budget: %w", err)
	}

	// 8. Launch.
	env := BuildAgentEnv(cfg.BackendVPNHost, voucher.Code, string(cfg.VPNProvider),
		vpnCred.AuthKey, vpnCred.LoginServer, vpnCred.Tag,
		plan.TTL, 15*time.Minute, string(cfg.Provider))
	env["KH_JOB_ID"] = jobID.String()

	if err := s.instances.SetState(ctx, instanceID, models.CloudInstanceLaunching, ""); err != nil {
		debug.Warning("could not mark instance launching: %v", err)
	}

	result, err := provider.Launch(ctx, LaunchRequest{
		Label:          label,
		IdempotencyKey: label,
		Offer:          offer,
		DiskGB:         diskGB,
		TTL:            plan.TTL,
		Env:            env,
		Image:          s.AgentImage,
	})
	if err != nil {
		// The row stays behind deliberately. The reaper reconciles it by
		// label, which is the only way to find an instance whose launch
		// response was lost but which is nonetheless running and billing.
		_ = s.instances.SetState(ctx, instanceID, models.CloudInstanceRequested, fmt.Sprintf("launch failed: %v", err))
		return fmt.Errorf("launch: %w", err)
	}

	if err := s.instances.MarkLaunched(ctx, instanceID, result.ProviderInstanceID,
		result.BilledFrom, now.Add(plan.TTL), result.Raw); err != nil {
		return fmt.Errorf("record launch: %w", err)
	}

	debug.Info("Provisioned cloud instance %s (%s) for job %s: %s, %d cents/hr, TTL %s",
		label, result.ProviderInstanceID, jobID, offer.GPUModel, offer.HourlyRateCents, plan.TTL)
	return nil
}

// pickProvider returns the cheapest enabled provider the client permits.
func (s *Service) pickProvider(ctx context.Context, allowlist []string) (*models.CloudProviderConfig, error) {
	if len(allowlist) == 0 {
		return nil, fmt.Errorf("client has no permitted cloud providers (cloud_provider_allowlist is empty)")
	}
	permitted := make(map[string]bool, len(allowlist))
	for _, p := range allowlist {
		permitted[p] = true
	}

	configs, err := s.providers.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	for _, cfg := range configs {
		if !permitted[string(cfg.Provider)] {
			continue
		}
		// Vast.ai places client hash material on machines the operator does
		// not control, so it stays unusable until acknowledged.
		if cfg.Provider == models.CloudProviderVastAI && !cfg.ThirdPartyAckAt.Valid {
			debug.Warning("Vast.ai provider %s is enabled but the third-party data-exposure acknowledgement is missing; skipping", cfg.Name)
			continue
		}
		return cfg, nil
	}
	return nil, fmt.Errorf("no enabled provider is permitted for this client")
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
		  AND je.cloud_burst_enabled = true
		  AND je.status IN ('pending','running')
		  AND c.cloud_enabled = true
		  AND c.cloud_budget_cents IS NOT NULL
		  AND array_length(c.cloud_provider_allowlist, 1) > 0`,
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
	return out, rows.Err()
}

// LoadAgentJobLocks implements scheduler.CloudAgentLocks.
func (s *Service) LoadAgentJobLocks(ctx context.Context) (map[int]uuid.UUID, error) {
	return s.instances.LoadAgentJobLocks(ctx)
}

func nullTime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }
