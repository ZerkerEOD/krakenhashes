package testutil

import (
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ClientCloudOpts configures a test client's cloud burst settings.
//
// BudgetCents is a pointer because nil (no funded budget) and 0 (funded but
// exhausted) are different states: the first forbids provisioning outright,
// the second is a client at its cap.
type ClientCloudOpts struct {
	CloudEnabled      bool
	ProviderAllowlist []string
	BudgetCents       *int64
	MaxTTLMinutes     *int
	// AckProviders are recorded in provider_ack as if an admin had accepted
	// each provider's data-exposure terms.
	AckProviders []string
}

// CreateTestClient inserts a client with the given cloud configuration and
// returns its ID.
func CreateTestClient(t *testing.T, database *db.DB, name string, opts ClientCloudOpts) uuid.UUID {
	t.Helper()

	allowlist := opts.ProviderAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}

	var id uuid.UUID
	err := database.QueryRow(`
		INSERT INTO clients (name, cloud_enabled, cloud_provider_allowlist,
		                     cloud_budget_cents, max_instance_ttl_minutes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		name, opts.CloudEnabled, pq.Array(allowlist), opts.BudgetCents, opts.MaxTTLMinutes,
	).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create test client %q: %v", name, err)
	}

	for _, provider := range opts.AckProviders {
		_, err := database.Exec(`
			UPDATE clients
			SET provider_ack = jsonb_set(
				COALESCE(provider_ack, '{}'::jsonb),
				ARRAY[$2::text],
				jsonb_build_object('at', to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
				                   'by', $3::text),
				true)
			WHERE id = $1`, id, provider, uuid.New().String())
		if err != nil {
			t.Fatalf("Failed to record %s acknowledgement for client %s: %v", provider, id, err)
		}
	}

	return id
}

// ProviderConfigOpts configures a test cloud provider row.
type ProviderConfigOpts struct {
	Enabled                bool
	Settings               string // JSON object; empty means '{}'
	ThirdPartyAcked        bool
	VPNProvider            string
	VPNCredentialKind      string
	MaxConcurrentInstances int
	MaxInstanceHourlyCents int
}

// CreateTestCloudProviderConfig inserts a provider configuration and returns
// its ID. Credentials are stored as opaque non-empty text — tests that need
// real decryption should encrypt them through the crypto package instead.
func CreateTestCloudProviderConfig(t *testing.T, database *db.DB, provider, name string, opts ProviderConfigOpts) uuid.UUID {
	t.Helper()

	settings := opts.Settings
	if settings == "" {
		settings = "{}"
	}
	vpnProvider := opts.VPNProvider
	if vpnProvider == "" {
		vpnProvider = "tailscale"
	}
	vpnKind := opts.VPNCredentialKind
	if vpnKind == "" {
		vpnKind = "oauth"
	}

	var id uuid.UUID
	err := database.QueryRow(`
		INSERT INTO cloud_provider_configs (
			provider, name, enabled, credentials_encrypted, settings,
			max_concurrent_instances, max_instance_hourly_cents,
			vpn_provider, vpn_credential_kind, vpn_credential_encrypted,
			vpn_tag_or_group, backend_vpn_host, third_party_ack_at
		) VALUES ($1,$2,$3,'test-credentials',$4::jsonb,$5,$6,$7,$8,'test-vpn-credential',
		          'tag:kraken-test','kraken.test.internal',
		          CASE WHEN $9 THEN NOW() ELSE NULL END)
		RETURNING id`,
		provider, name, opts.Enabled, settings,
		opts.MaxConcurrentInstances, opts.MaxInstanceHourlyCents,
		vpnProvider, vpnKind, opts.ThirdPartyAcked,
	).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create cloud provider config %q: %v", name, err)
	}
	return id
}

// InstanceOpts configures a test cloud instance row.
type InstanceOpts struct {
	State              string
	ClientID           *uuid.UUID
	JobExecutionID     *uuid.UUID
	AgentID            *int
	HourlyRateCents    int
	ReservedCents      int64
	EstimatedCostCents int64
	TTLEpoch           *time.Time
	ReadyDeadlineAt    *time.Time
	LaunchDeadlineAt   *time.Time
	ProviderInstanceID string
	TerminateAttempts  int
}

// CreateTestCloudInstance inserts a cloud instance and returns its ID and
// label. The label is generated to match the production shape (kh-<uuid18>),
// which is the reconciliation key on providers that have no idempotency token.
func CreateTestCloudInstance(t *testing.T, database *db.DB, providerConfigID uuid.UUID, opts InstanceOpts) (uuid.UUID, string) {
	t.Helper()

	state := opts.State
	if state == "" {
		state = "running"
	}
	label := "kh-" + uuid.New().String()[:18]

	var id uuid.UUID
	err := database.QueryRow(`
		INSERT INTO cloud_instances (
			provider_config_id, label, idempotency_key, provider_instance_id,
			agent_id, job_execution_id, client_id, state,
			hourly_rate_cents, reserved_cents, estimated_cost_cents,
			launch_deadline_at, ready_deadline_at, ttl_epoch, terminate_attempts
		) VALUES ($1,$2,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id`,
		providerConfigID, label, opts.ProviderInstanceID,
		opts.AgentID, opts.JobExecutionID, opts.ClientID, state,
		opts.HourlyRateCents, opts.ReservedCents, opts.EstimatedCostCents,
		opts.LaunchDeadlineAt, opts.ReadyDeadlineAt, opts.TTLEpoch, opts.TerminateAttempts,
	).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create cloud instance: %v", err)
	}
	return id, label
}

/*
 * InsertLedgerEntry appends a spend ledger row at an explicit timestamp.
 *
 * The recordedAt parameter is the entire point of this helper. The repository
 * API always writes NOW(), so the budget window boundary — entries older than
 * date_trunc('month', NOW()) must not count toward the cap — cannot be tested
 * through it at all.
 */
func InsertLedgerEntry(t *testing.T, database *db.DB, clientID uuid.UUID, instanceID *uuid.UUID, cents int64, kind string, recordedAt time.Time) {
	t.Helper()

	_, err := database.Exec(`
		INSERT INTO cloud_spend_ledger (client_id, cloud_instance_id, cents, kind, recorded_at)
		VALUES ($1, $2, $3, $4, $5)`,
		clientID, instanceID, cents, kind, recordedAt)
	if err != nil {
		t.Fatalf("Failed to insert ledger entry (%s %d): %v", kind, cents, err)
	}
}

/*
 * CreateTestAgent inserts a minimal agents row and returns its ID.
 *
 * cloud_instances.agent_id carries a foreign key, so any test asserting on an
 * attached agent needs a real row rather than an arbitrary integer.
 *
 * cloudInstanceID is written to agents.cloud_instance_id — the column the
 * scheduler reads for is_cloud and the WebSocket handler reads to suppress the
 * full-corpus file sync. Pass nil for an on-prem agent.
 */
func CreateTestAgent(t *testing.T, database *db.DB, ownerID uuid.UUID, cloudInstanceID *uuid.UUID) int {
	t.Helper()

	// name, version, hardware and created_by_id are NOT NULL without defaults.
	var id int
	err := database.QueryRow(`
		INSERT INTO agents (name, status, version, hardware, created_by_id, api_key, cloud_instance_id)
		VALUES ($1, 'active', '0.0.0-test', '{}'::jsonb, $2, $3, $4)
		RETURNING id`,
		"test-agent-"+uuid.NewString()[:8], ownerID, uuid.NewString(), cloudInstanceID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to create test agent: %v", err)
	}
	return id
}

// CloudJob is the set of rows a cloud-eligible job needs.
type CloudJob struct {
	UserID     uuid.UUID
	ClientID   uuid.UUID
	HashlistID int64
	JobID      uuid.UUID
}

/*
 * CreateCloudJob builds the full row chain a cloud provision requires:
 * user -> client -> hashlist -> job_execution.
 *
 * A cloud-specific variant is required rather than reusing the scheduler's
 * createSchedulerV2Prereqs, which does not set hashlists.client_id — the column
 * every cloud query joins on to find the paying client.
 */
func CreateCloudJob(t *testing.T, database *db.DB, clientID uuid.UUID, cloudBurst bool) CloudJob {
	t.Helper()

	user := CreateTestUser(t, database, "cloud-"+uuid.New().String()[:8],
		"cloud-"+uuid.New().String()[:8]+"@test.local", DefaultTestPassword, "admin")

	var hashlistID int64
	err := database.QueryRow(`
		INSERT INTO hashlists (name, user_id, client_id, hash_type_id,
		                       total_hashes, cracked_hashes, status)
		VALUES ($1, $2, $3, 0, 100, 0, 'ready')
		RETURNING id`,
		"cloud-hashlist-"+uuid.New().String()[:8], user.ID, clientID,
	).Scan(&hashlistID)
	if err != nil {
		t.Fatalf("Failed to create test hashlist: %v", err)
	}

	var jobID uuid.UUID
	err = database.QueryRow(`
		INSERT INTO job_executions (
			hashlist_id, status, priority, max_agents, attack_mode, created_by,
			name, wordlist_ids, rule_ids, hash_type, chunk_size_seconds,
			status_updates_enabled, allow_high_priority_override,
			increment_mode, multiplication_factor, is_accurate_keyspace,
			cloud_burst_enabled
		) VALUES ($1,'pending',5,1,0,$2,$3,'[]'::jsonb,'[]'::jsonb,0,1200,
		          true,false,'off',1,false,$4)
		RETURNING id`,
		hashlistID, user.ID, "cloud-job-"+uuid.New().String()[:8], cloudBurst,
	).Scan(&jobID)
	if err != nil {
		t.Fatalf("Failed to create test job execution: %v", err)
	}

	return CloudJob{UserID: user.ID, ClientID: clientID, HashlistID: hashlistID, JobID: jobID}
}

// Int64Ptr and IntPtr are conveniences for the pointer-valued option fields.
func Int64Ptr(v int64) *int64 { return &v }

// IntPtr returns a pointer to v.
func IntPtr(v int) *int { return &v }
