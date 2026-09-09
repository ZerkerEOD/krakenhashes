package models

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CloudProvider identifies a supported cloud GPU backend.
type CloudProvider string

const (
	CloudProviderVastAI CloudProvider = "vastai"
	CloudProviderAWS    CloudProvider = "aws"

	/*
	 * RunPod is TWO provider kinds, not one with a flag.
	 *
	 * Its API distinguishes them with a single `cloud: SECURE|COMMUNITY` field,
	 * which makes one kind plus a toggle look natural. It is the wrong split:
	 * the two tiers sit on opposite sides of the only line that matters here.
	 * Secure runs in RunPod's own SOC 2 Type II datacentres; Community runs on
	 * peer-operated machines whose owner has root over the container, and none
	 * of RunPod's compliance claims extend to it.
	 *
	 * Separate kinds mean an admin allowlists them separately, a client can
	 * permit one without the other, and the consent machinery attaches to
	 * exactly the tier that needs it — see RequiresThirdPartyAck.
	 */
	CloudProviderRunPod          CloudProvider = "runpod"
	CloudProviderRunPodCommunity CloudProvider = "runpod_community"

	// CloudProviderMock drives the whole lifecycle against local
	// `agent --test-mode` processes. It exists so the provisioning,
	// isolation, budget and teardown paths can be exercised end to end
	// without renting a GPU.
	CloudProviderMock CloudProvider = "mock"
)

/*
 * RequiresThirdPartyAck reports whether this provider places client hash
 * material on hardware the operator does not control.
 *
 * TRUE for peer/consumer hardware only. Vast.ai rents individually-owned
 * machines and RunPod Community rents peer-operated hosts; in both cases the
 * machine's owner has root over the container, so the protection is a terms-of-
 * service clause rather than an isolation boundary.
 *
 * FALSE for AWS and RunPod Secure, deliberately. AWS runs in the operator's own
 * account under their own IAM, and RunPod Secure runs in RunPod's own SOC 2
 * Type II datacentres — neither is a third party in the sense this gate is
 * about. Gating them behind a data-exposure acknowledgement would not add
 * safety; it would train operators to click through the one warning that is
 * real.
 *
 * This is the single place that question is answered. The ack gate before
 * enabling a config, the per-client allowlist gate, the provider skip during
 * provisioning, the per-job peer opt-in and the red UI chips all route through
 * here, so a sixth provider is one line rather than five scattered ||s.
 */
func (p CloudProvider) RequiresThirdPartyAck() bool {
	return p == CloudProviderVastAI || p == CloudProviderRunPodCommunity
}

// AllCloudProviders is every supported kind, in the order a UI should offer
// them: least surprising first, peer hardware last.
var AllCloudProviders = []CloudProvider{
	CloudProviderAWS,
	CloudProviderRunPod,
	CloudProviderVastAI,
	CloudProviderRunPodCommunity,
	CloudProviderMock,
}

/*
 * IsValid reports whether this is a supported provider kind.
 *
 * The same three-way `case A, B, C:` validation was written out by hand in the
 * provider-config handler, the client-allowlist writer and the per-client
 * acknowledgement writer. Adding a kind meant finding all three, and the
 * failure mode for missing one is quiet and asymmetric: a provider that can be
 * configured but not allowlisted, or allowlisted but not acknowledged, with the
 * error surfacing three screens away from the omission.
 *
 * Note this is NOT the same question as whether a provider can be stored — the
 * database CHECK constraint on cloud_provider_configs.provider is the authority
 * there, and the two must be kept in step.
 */
func (p CloudProvider) IsValid() bool {
	for _, known := range AllCloudProviders {
		if p == known {
			return true
		}
	}
	return false
}

// VPNProvider identifies how a cloud agent joins the operator's private
// network. OpenVPN is deliberately absent: it needs /dev/net/tun and
// CAP_NET_ADMIN, and Vast.ai runs unprivileged containers that have neither.
type VPNProvider string

const (
	VPNProviderTailscale VPNProvider = "tailscale"
	VPNProviderNetBird   VPNProvider = "netbird"
	VPNProviderWireGuard VPNProvider = "wireguard"
)

// VPNCredentialKind describes what the operator gave us, which decides whether
// we can mint a short-lived credential per instance or must reuse a static one.
type VPNCredentialKind string

const (
	// VPNCredentialOAuth / VPNCredentialPAT let us mint a single-use,
	// ephemeral, auto-deregistering credential per instance. Preferred.
	VPNCredentialOAuth VPNCredentialKind = "oauth"
	VPNCredentialPAT   VPNCredentialKind = "pat"
	// VPNCredentialReusableKey is an operator-supplied key shared by every
	// instance. Tailscale caps auth-key lifetime at 90 days, so these expire
	// and must be tracked and rotated.
	VPNCredentialReusableKey VPNCredentialKind = "reusable_key"
	// VPNCredentialStaticConfig is a WireGuard peer config. No per-instance
	// credential and no automatic deregistration.
	VPNCredentialStaticConfig VPNCredentialKind = "static_config"
)

// CloudInstanceState is the lifecycle of a rented instance.
//
// The states before `running` all represent money already being spent (Vast.ai
// bills storage from contract creation), which is why each has a deadline.
type CloudInstanceState string

const (
	// CloudInstanceRequested: row written, provider not yet called.
	CloudInstanceRequested CloudInstanceState = "requested"
	// CloudInstanceLaunching: provider call in flight. If the response is
	// lost, reconciliation by label recovers it.
	CloudInstanceLaunching CloudInstanceState = "launching"
	// CloudInstanceProvisioning: instance exists, agent has not registered.
	CloudInstanceProvisioning CloudInstanceState = "provisioning"
	// CloudInstanceSyncing: agent registered, downloading its job's file set.
	CloudInstanceSyncing CloudInstanceState = "syncing"
	CloudInstanceRunning CloudInstanceState = "running"
	// CloudInstanceDraining: no new chunks dispatched; in-flight work finishing.
	CloudInstanceDraining    CloudInstanceState = "draining"
	CloudInstanceTerminating CloudInstanceState = "terminating"
	CloudInstanceTerminated  CloudInstanceState = "terminated"
	CloudInstanceFailed      CloudInstanceState = "failed"
)

// IsLive reports whether the instance may still be costing money.
func (s CloudInstanceState) IsLive() bool {
	return s != CloudInstanceTerminated && s != CloudInstanceFailed
}

// SpendKind classifies a ledger entry.
type SpendKind string

const (
	// SpendReservation is committed at launch (rate x TTL). This is what makes
	// a budget cap real: it is subtracted from available budget before the
	// instance starts, not after it has already overspent.
	SpendReservation SpendKind = "reservation"
	// SpendIncurred is converted from a reservation as wall-clock accrues.
	SpendIncurred SpendKind = "incurred"
	// SpendRelease returns the unused remainder when an instance ends early.
	SpendRelease SpendKind = "release"
	// SpendReconciliation is a provider-authoritative correction. May be
	// negative. Arrives late (AWS Cost Explorer lags ~24h).
	SpendReconciliation SpendKind = "reconciliation"
)

// CloudProviderConfig is an admin-configured cloud GPU backend.
type CloudProviderConfig struct {
	ID       uuid.UUID     `json:"id"`
	Provider CloudProvider `json:"provider"`
	Name     string        `json:"name"`
	Enabled  bool          `json:"enabled"`

	// CredentialsEncrypted never leaves the backend.
	CredentialsEncrypted string  `json:"-"`
	Settings             JSONMap `json:"settings"`

	MaxConcurrentInstances int `json:"max_concurrent_instances"`
	MaxInstanceHourlyCents int `json:"max_instance_hourly_cents"`

	VPNProvider            VPNProvider       `json:"vpn_provider,omitempty"`
	VPNCredentialKind      VPNCredentialKind `json:"vpn_credential_kind,omitempty"`
	VPNCredentialEncrypted string            `json:"-"`
	VPNCredentialExpiresAt sql.NullTime      `json:"vpn_credential_expires_at,omitempty"`
	VPNTagOrGroup          string            `json:"vpn_tag_or_group,omitempty"`
	BackendVPNHost         string            `json:"backend_vpn_host,omitempty"`

	ThirdPartyAckAt sql.NullTime `json:"third_party_ack_at,omitempty"`
	ThirdPartyAckBy *uuid.UUID   `json:"third_party_ack_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

/*
 * MarshalJSON flattens the nullable timestamps to RFC3339-or-null.
 *
 * Without this the API would emit Go's raw {"Time":...,"Valid":...} shape for
 * every sql.NullTime, which the UI cannot render and — for vpn_credential_
 * expires_at — would silently break the expiry countdown that tells an operator
 * their reusable key is about to strand every future launch. Same approach as
 * Agent.MarshalJSON (agent.go:246); nullTime is that file's helper.
 */
func (c CloudProviderConfig) MarshalJSON() ([]byte, error) {
	type configJSON struct {
		ID                     uuid.UUID         `json:"id"`
		Provider               CloudProvider     `json:"provider"`
		Name                   string            `json:"name"`
		Enabled                bool              `json:"enabled"`
		Settings               JSONMap           `json:"settings"`
		MaxConcurrentInstances int               `json:"max_concurrent_instances"`
		MaxInstanceHourlyCents int               `json:"max_instance_hourly_cents"`
		VPNProvider            VPNProvider       `json:"vpn_provider,omitempty"`
		VPNCredentialKind      VPNCredentialKind `json:"vpn_credential_kind,omitempty"`
		VPNCredentialExpiresAt *time.Time        `json:"vpn_credential_expires_at"`
		VPNTagOrGroup          string            `json:"vpn_tag_or_group,omitempty"`
		BackendVPNHost         string            `json:"backend_vpn_host,omitempty"`
		ThirdPartyAckAt        *time.Time        `json:"third_party_ack_at"`
		ThirdPartyAckBy        *uuid.UUID        `json:"third_party_ack_by,omitempty"`
		// HasCredentials lets the UI show "configured" without ever shipping
		// the secret, the same way the SSO admin API does.
		HasCredentials   bool      `json:"has_credentials"`
		HasVPNCredential bool      `json:"has_vpn_credential"`
		CreatedAt        time.Time `json:"created_at"`
		UpdatedAt        time.Time `json:"updated_at"`
	}

	return json.Marshal(configJSON{
		ID:                     c.ID,
		Provider:               c.Provider,
		Name:                   c.Name,
		Enabled:                c.Enabled,
		Settings:               c.Settings,
		MaxConcurrentInstances: c.MaxConcurrentInstances,
		MaxInstanceHourlyCents: c.MaxInstanceHourlyCents,
		VPNProvider:            c.VPNProvider,
		VPNCredentialKind:      c.VPNCredentialKind,
		VPNCredentialExpiresAt: nullTime(c.VPNCredentialExpiresAt),
		VPNTagOrGroup:          c.VPNTagOrGroup,
		BackendVPNHost:         c.BackendVPNHost,
		ThirdPartyAckAt:        nullTime(c.ThirdPartyAckAt),
		ThirdPartyAckBy:        c.ThirdPartyAckBy,
		HasCredentials:         c.CredentialsEncrypted != "",
		HasVPNCredential:       c.VPNCredentialEncrypted != "",
		CreatedAt:              c.CreatedAt,
		UpdatedAt:              c.UpdatedAt,
	})
}

// CloudProviderConfigInput carries plaintext secrets inbound. Mirrors the SSO
// pattern: secrets are only written when non-empty, so a PUT that omits them
// leaves the stored value alone.
type CloudProviderConfigInput struct {
	Provider               CloudProvider     `json:"provider"`
	Name                   string            `json:"name"`
	Enabled                bool              `json:"enabled"`
	Credentials            string            `json:"credentials,omitempty"`
	Settings               JSONMap           `json:"settings"`
	MaxConcurrentInstances int               `json:"max_concurrent_instances"`
	MaxInstanceHourlyCents int               `json:"max_instance_hourly_cents"`
	VPNProvider            VPNProvider       `json:"vpn_provider,omitempty"`
	VPNCredentialKind      VPNCredentialKind `json:"vpn_credential_kind,omitempty"`
	VPNCredential          string            `json:"vpn_credential,omitempty"`
	VPNTagOrGroup          string            `json:"vpn_tag_or_group,omitempty"`
	BackendVPNHost         string            `json:"backend_vpn_host,omitempty"`
}

// CloudBudgetPolicy is the spend threshold ladder. A policy with a nil ClientID
// is the system default.
//
// Every threshold is configurable because there is no universally right answer:
// "stop provisioning at 99, hard stop at 100, never notify me" is as valid as
// "warn at 50". What actually prevents an overrun is reservation accounting,
// not this ladder — the ladder only decides how gracefully the cap is reached.
type CloudBudgetPolicy struct {
	ID                  uuid.UUID  `json:"id"`
	ClientID            *uuid.UUID `json:"client_id,omitempty"`
	NotifyPct           *int       `json:"notify_pct,omitempty"`
	StopProvisionPct    int        `json:"stop_provision_pct"`
	DrainPct            int        `json:"drain_pct"`
	HardStopPct         int        `json:"hard_stop_pct"`
	AllowOverage        bool       `json:"allow_overage"`
	DrainTimeoutSeconds int        `json:"drain_timeout_seconds"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// CloudInstance is one rented GPU.
type CloudInstance struct {
	ID               uuid.UUID `json:"id"`
	ProviderConfigID uuid.UUID `json:"provider_config_id"`

	// Label is written before the provider is called and is how a lost launch
	// response is reconciled. On Vast.ai, which has no idempotency token, the
	// label IS the idempotency key.
	Label              string `json:"label"`
	IdempotencyKey     string `json:"-"`
	ProviderInstanceID string `json:"provider_instance_id,omitempty"`

	AgentID            *int       `json:"agent_id,omitempty"`
	JobExecutionID     *uuid.UUID `json:"job_execution_id,omitempty"`
	ClientID           *uuid.UUID `json:"client_id,omitempty"`
	ClientNameSnapshot string     `json:"client_name_snapshot,omitempty"`

	State CloudInstanceState `json:"state"`

	GPUModel        string `json:"gpu_model,omitempty"`
	GPUCount        int    `json:"gpu_count,omitempty"`
	HourlyRateCents int    `json:"hourly_rate_cents"`
	DiskGB          int    `json:"disk_gb,omitempty"`
	FilesetBytes    int64  `json:"fileset_bytes,omitempty"`

	ReservedCents      int64  `json:"reserved_cents"`
	EstimatedCostCents int64  `json:"estimated_cost_cents"`
	ActualCostCents    *int64 `json:"actual_cost_cents,omitempty"`

	LaunchDeadlineAt sql.NullTime `json:"launch_deadline_at,omitempty"`
	ReadyDeadlineAt  sql.NullTime `json:"ready_deadline_at,omitempty"`
	TTLEpoch         sql.NullTime `json:"ttl_epoch,omitempty"`

	LaunchedAt sql.NullTime `json:"launched_at,omitempty"`
	ReadyAt    sql.NullTime `json:"ready_at,omitempty"`
	// DrainStartedAt is when the budget ladder put this instance on the drain
	// rung, and the clock drain_timeout_seconds is measured from. Cleared when
	// spend falls back below drain_pct. Not updated_at, which the reaper bumps
	// on every accrual and which would therefore never expire.
	DrainStartedAt     sql.NullTime `json:"drain_started_at,omitempty"`
	TerminatedAt       sql.NullTime `json:"terminated_at,omitempty"`
	TerminationReason  string       `json:"termination_reason,omitempty"`
	TerminateAttempts  int          `json:"terminate_attempts"`
	LastTerminateError string       `json:"last_terminate_error,omitempty"`

	VPNCredentialRef string  `json:"-"`
	ProviderRaw      JSONMap `json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MarshalJSON flattens the nullable timestamps, as above. The TTL countdown in
// the fleet UI depends on ttl_epoch arriving as a plain timestamp.
func (c CloudInstance) MarshalJSON() ([]byte, error) {
	type instanceJSON struct {
		ID                 uuid.UUID          `json:"id"`
		ProviderConfigID   uuid.UUID          `json:"provider_config_id"`
		Label              string             `json:"label"`
		ProviderInstanceID string             `json:"provider_instance_id,omitempty"`
		AgentID            *int               `json:"agent_id,omitempty"`
		JobExecutionID     *uuid.UUID         `json:"job_execution_id,omitempty"`
		ClientID           *uuid.UUID         `json:"client_id,omitempty"`
		ClientNameSnapshot string             `json:"client_name_snapshot,omitempty"`
		State              CloudInstanceState `json:"state"`
		GPUModel           string             `json:"gpu_model,omitempty"`
		GPUCount           int                `json:"gpu_count,omitempty"`
		HourlyRateCents    int                `json:"hourly_rate_cents"`
		DiskGB             int                `json:"disk_gb,omitempty"`
		FilesetBytes       int64              `json:"fileset_bytes,omitempty"`
		ReservedCents      int64              `json:"reserved_cents"`
		EstimatedCostCents int64              `json:"estimated_cost_cents"`
		ActualCostCents    *int64             `json:"actual_cost_cents,omitempty"`
		LaunchDeadlineAt   *time.Time         `json:"launch_deadline_at"`
		ReadyDeadlineAt    *time.Time         `json:"ready_deadline_at"`
		TTLEpoch           *time.Time         `json:"ttl_epoch"`
		LaunchedAt         *time.Time         `json:"launched_at"`
		ReadyAt            *time.Time         `json:"ready_at"`
		DrainStartedAt     *time.Time         `json:"drain_started_at"`
		TerminatedAt       *time.Time         `json:"terminated_at"`
		TerminationReason  string             `json:"termination_reason,omitempty"`
		TerminateAttempts  int                `json:"terminate_attempts"`
		LastTerminateError string             `json:"last_terminate_error,omitempty"`
		CreatedAt          time.Time          `json:"created_at"`
		UpdatedAt          time.Time          `json:"updated_at"`
	}

	return json.Marshal(instanceJSON{
		ID:                 c.ID,
		ProviderConfigID:   c.ProviderConfigID,
		Label:              c.Label,
		ProviderInstanceID: c.ProviderInstanceID,
		AgentID:            c.AgentID,
		JobExecutionID:     c.JobExecutionID,
		ClientID:           c.ClientID,
		ClientNameSnapshot: c.ClientNameSnapshot,
		State:              c.State,
		GPUModel:           c.GPUModel,
		GPUCount:           c.GPUCount,
		HourlyRateCents:    c.HourlyRateCents,
		DiskGB:             c.DiskGB,
		FilesetBytes:       c.FilesetBytes,
		ReservedCents:      c.ReservedCents,
		EstimatedCostCents: c.EstimatedCostCents,
		ActualCostCents:    c.ActualCostCents,
		LaunchDeadlineAt:   nullTime(c.LaunchDeadlineAt),
		ReadyDeadlineAt:    nullTime(c.ReadyDeadlineAt),
		TTLEpoch:           nullTime(c.TTLEpoch),
		LaunchedAt:         nullTime(c.LaunchedAt),
		ReadyAt:            nullTime(c.ReadyAt),
		DrainStartedAt:     nullTime(c.DrainStartedAt),
		TerminatedAt:       nullTime(c.TerminatedAt),
		TerminationReason:  c.TerminationReason,
		TerminateAttempts:  c.TerminateAttempts,
		LastTerminateError: c.LastTerminateError,
		CreatedAt:          c.CreatedAt,
		UpdatedAt:          c.UpdatedAt,
	})
}

// TTLRemaining reports how long the instance has left to live. Zero when the
// TTL has passed or was never set — callers must treat zero as "do not plan
// any more work for this instance".
func (c *CloudInstance) TTLRemaining(now time.Time) time.Duration {
	if !c.TTLEpoch.Valid {
		return 0
	}
	if remaining := c.TTLEpoch.Time.Sub(now); remaining > 0 {
		return remaining
	}
	return 0
}

// CloudSpendEntry is one append-only ledger row.
type CloudSpendEntry struct {
	ID              uuid.UUID  `json:"id"`
	ClientID        *uuid.UUID `json:"client_id,omitempty"`
	JobExecutionID  *uuid.UUID `json:"job_execution_id,omitempty"`
	CloudInstanceID *uuid.UUID `json:"cloud_instance_id,omitempty"`
	Cents           int64      `json:"cents"`
	Kind            SpendKind  `json:"kind"`
	Note            string     `json:"note,omitempty"`
	RecordedAt      time.Time  `json:"recorded_at"`
}

// CloudBudgetState is the computed budget picture for one client in the
// current window.
type CloudBudgetState struct {
	ClientID uuid.UUID `json:"client_id"`
	// CapCents is nil when the client has no funded budget, which means no
	// cloud provisioning is permitted at all.
	CapCents *int64 `json:"cap_cents"`
	// IncurredCents is spend already consumed (incurred + reconciliation).
	IncurredCents int64 `json:"incurred_cents"`
	// ReservedCents is committed-but-not-yet-consumed spend on live instances.
	ReservedCents int64 `json:"reserved_cents"`
	// AvailableCents = cap - incurred - reserved, floored at zero.
	AvailableCents int64 `json:"available_cents"`
	// UsedPct is (incurred + reserved) / cap, the value the threshold ladder
	// is evaluated against.
	UsedPct float64 `json:"used_pct"`
}

/*
 * ClientCloudSettings is the per-client cloud burst configuration.
 *
 * ProviderAllowlist is empty by default and stays empty until an admin opts the
 * client in to a specific provider. That is the control that keeps a client's
 * hashes off Vast.ai's third-party machines while still allowing AWS, which
 * runs inside the operator's own account.
 */
/*
 * ClientCloudSettings is one client's cloud burst configuration.
 *
 * The stored fields are TRI-STATE: nil (or, for the allowlist, empty) means the
 * client has not been configured and inherits the server default. The Effective*
 * fields are the resolved values the rest of the system acts on, returned
 * alongside the raw ones so the UI can show "inheriting $10.00" rather than an
 * empty box that looks like a mistake.
 */
type ClientCloudSettings struct {
	ClientID   uuid.UUID `json:"client_id"`
	ClientName string    `json:"client_name"`
	// Enabled is nil when the client has never been configured either way.
	// A NOT NULL boolean could not distinguish that from "explicitly off",
	// which is why the column is nullable.
	Enabled *bool `json:"cloud_enabled"`
	// ProviderAllowlist holds CloudProvider values. Empty means inherit.
	ProviderAllowlist []string `json:"cloud_provider_allowlist"`
	// BudgetCents is nil when the client inherits the default. A client that
	// inherits when no default is set stays unfunded, which forbids
	// provisioning outright — distinct from a funded budget at zero headroom.
	BudgetCents           *int64        `json:"cloud_budget_cents"`
	BudgetPeriod          *BudgetPeriod `json:"cloud_budget_period"`
	MaxInstanceTTLMinutes *int          `json:"max_instance_ttl_minutes"`
	// ProviderAck records who accepted each provider's data-exposure terms and
	// when: {"vastai": {"at": "...", "by": "<user uuid>"}}.
	ProviderAck JSONMap `json:"provider_ack"`

	// Resolved values, server-computed. Never written back.
	EffectiveEnabled           bool         `json:"effective_cloud_enabled"`
	EffectiveProviderAllowlist []string     `json:"effective_cloud_provider_allowlist"`
	EffectiveBudgetCents       *int64       `json:"effective_cloud_budget_cents"`
	EffectiveBudgetPeriod      BudgetPeriod `json:"effective_cloud_budget_period"`
	// InheritedFields names the fields taking their value from the server
	// default, so the UI does not have to re-derive the comparison and risk
	// disagreeing with the backend about what is actually in force.
	InheritedFields []string `json:"inherited_fields"`
}

// BudgetPeriod is the window a client's spend ceiling applies to. Mirrors
// cloud.BudgetPeriod; kept here so models does not import the service package.
type BudgetPeriod string

const (
	BudgetPeriodMonthly    BudgetPeriod = "monthly"
	BudgetPeriodQuarterly  BudgetPeriod = "quarterly"
	BudgetPeriodSemiannual BudgetPeriod = "semiannual"
)

// IsValid reports whether p is a period this code knows how to bound.
func (p BudgetPeriod) IsValid() bool {
	switch p {
	case BudgetPeriodMonthly, BudgetPeriodQuarterly, BudgetPeriodSemiannual:
		return true
	}
	return false
}

/*
 * ClientCloudSettingsInput is the admin-editable subset. Acknowledgements are
 * not settable here — they are recorded through their own endpoint so the
 * attribution is always the authenticated caller.
 *
 * Every field is a pointer or a slice so that "clear this and inherit again" is
 * expressible. A plain bool could only ever say "off", so a client could be
 * enabled but never returned to inheriting.
 */
type ClientCloudSettingsInput struct {
	Enabled               *bool         `json:"cloud_enabled"`
	ProviderAllowlist     []string      `json:"cloud_provider_allowlist"`
	BudgetCents           *int64        `json:"cloud_budget_cents"`
	BudgetPeriod          *BudgetPeriod `json:"cloud_budget_period"`
	MaxInstanceTTLMinutes *int          `json:"max_instance_ttl_minutes"`
}

// ClientCloudDefaults is the server-side default every client inherits from.
type ClientCloudDefaults struct {
	BudgetCents       *int64       `json:"cloud_budget_cents"`
	BudgetPeriod      BudgetPeriod `json:"cloud_budget_period"`
	Enabled           bool         `json:"cloud_enabled"`
	ProviderAllowlist []string     `json:"cloud_provider_allowlist"`
}

// CloudGPUBenchmark is an observed speed for a GPU model, used ONLY for
// pre-launch cost and ETA estimation.
//
// It is deliberately never written into agent_benchmarks: a synthetic row
// there would make CountAgentsWithRecentBenchmark treat an invented number as
// corroborating evidence from "another agent", which feeds the quarantine
// decision and could disable real on-prem hardware.
type CloudGPUBenchmark struct {
	ID          uuid.UUID `json:"id"`
	Provider    string    `json:"provider"`
	GPUModel    string    `json:"gpu_model"`
	GPUCount    int       `json:"gpu_count"`
	AttackMode  int       `json:"attack_mode"`
	HashType    int       `json:"hash_type"`
	SaltCount   *int      `json:"salt_count,omitempty"`
	Speed       int64     `json:"speed"`
	SampleCount int       `json:"sample_count"`
	UpdatedAt   time.Time `json:"updated_at"`
}

/*
 * CloudProvisioningRules governs WHEN provisioning may happen, as distinct from
 * CloudBudgetPolicy's HOW MUCH may be spent.
 *
 * A rules row with a nil ClientID is the system default. Client rows override it
 * PER FIELD, which is a deliberate divergence from CloudBudgetPolicy — see
 * MergeProvisioningRules for why.
 *
 * Every field is a pointer so "not configured" is distinguishable from "set to
 * zero". Zero is meaningful for all of these: it is each rule's in-band OFF
 * value, which is what lets a client opt out of an inherited rule without a
 * second boolean column per rule.
 */
type CloudProvisioningRules struct {
	ID       uuid.UUID  `json:"id"`
	ClientID *uuid.UUID `json:"client_id,omitempty"`

	// MinJobPriority is an ABSOLUTE job_executions.priority floor. 0 = no floor.
	//
	// Note for any UI: the priority ceiling (max_job_priority) defaults to 1000
	// while the shipped convention doc describes 0-100 bands, so a floor typed
	// as "700" meaning "high priority" matches nothing under that convention and
	// silently disables all automatic provisioning. Render this against the live
	// ceiling, never as a bare number.
	MinJobPriority *int `json:"min_job_priority,omitempty"`

	// MinStarvationSeconds is how long a job must have been continuously
	// starving before paid capacity is rented for it. 0 = rent on the first
	// starving tick.
	MinStarvationSeconds *int `json:"min_starvation_seconds,omitempty"`

	// SkipIfFinishingWithinSeconds refuses to rent for a job projected to
	// complete within this long on existing capacity. 0 = never skip.
	SkipIfFinishingWithinSeconds *int `json:"skip_if_finishing_within_seconds,omitempty"`

	// MaxSpendPerJobCents caps committed cloud spend for ONE job execution over
	// its whole life. 0 = no cap. Not month-windowed.
	MaxSpendPerJobCents *int64 `json:"max_spend_per_job_cents,omitempty"`

	// ProvisioningWindowStart/End bound the wall-clock time at which a launch
	// may START. Equal values mean always; End < Start wraps midnight. Never
	// causes a teardown — TTL and idle drain own that.
	ProvisioningWindowStart *string `json:"provisioning_window_start,omitempty"`
	ProvisioningWindowEnd   *string `json:"provisioning_window_end,omitempty"`
	// ProvisioningWindowTZ is an IANA name, so a UTC server can still express
	// local business hours.
	ProvisioningWindowTZ *string `json:"provisioning_window_tz,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

/*
 * MergeProvisioningRules layers a client override on top of the system default.
 *
 * NULL on an OVERRIDE row means INHERIT.
 * NULL on the SYSTEM DEFAULT row means the rule is not configured and therefore
 * constrains nothing.
 *
 * This deliberately differs from CloudBudgetRepository.GetPolicy, which picks
 * one whole row and never merges. That is right for the budget ladder — its
 * fields are coupled by a CHECK constraint, and an operator setting a client's
 * thresholds means all of them. It is wrong here: these are independent safety
 * rails, and "the admin tightened the default and it never reached the clients
 * that had an override" is the failure mode that produces the invoice.
 *
 * The window is merged as a UNIT rather than field by field. A start inherited
 * from the default paired with an end from an override is not a window anyone
 * configured, and the two values only mean anything together.
 */
func MergeProvisioningRules(def, override *CloudProvisioningRules) *CloudProvisioningRules {
	/*
	 * No system default means no policy, and NIL IS THE FAIL-CLOSED ANSWER.
	 *
	 * The tempting thing is to return an empty struct, but under these
	 * semantics an all-nil rules object is the MAXIMALLY PERMISSIVE one: every
	 * rule whose pointer is nil is skipped, so it reads as "any job, any
	 * priority, any hour, no per-job cap". And def == nil does not mean "the
	 * rules are unconfigured" — it means the system-default ROW is missing,
	 * i.e. the migration did not run or the row was truncated.
	 *
	 * Turning a broken database into unrestricted spending permission is the
	 * wrong direction. decideProvisioningAction refuses on nil for the same
	 * reason checkGlobalCap refuses on an unreadable ceiling: a policy we
	 * cannot read has to behave like an engaged one.
	 */
	if def == nil {
		return nil
	}
	if override == nil {
		out := *def
		return &out
	}

	out := *override
	if out.MinJobPriority == nil {
		out.MinJobPriority = def.MinJobPriority
	}
	if out.MinStarvationSeconds == nil {
		out.MinStarvationSeconds = def.MinStarvationSeconds
	}
	if out.SkipIfFinishingWithinSeconds == nil {
		out.SkipIfFinishingWithinSeconds = def.SkipIfFinishingWithinSeconds
	}
	if out.MaxSpendPerJobCents == nil {
		out.MaxSpendPerJobCents = def.MaxSpendPerJobCents
	}
	/*
	 * The BOUNDS merge as a unit; the ZONE merges on its own.
	 *
	 * Pairing the bounds is the point of the unit rule: a start inherited from
	 * the default against an end from an override is not a window anyone
	 * configured, and the two only mean anything together.
	 *
	 * The zone is not part of that pair, and folding it in loses money in both
	 * directions. Inherit it only alongside the bounds and a client override
	 * that sets its OWN window inherits no zone at all, so it silently
	 * evaluates in UTC: default tz America/Chicago, override 22:00-06:00 with
	 * no tz, and the autoscaler rents straight through the client's evening
	 * business hours and stops at 01:00 — the exact inverse of the policy, with
	 * no error and nothing in the logs. Skip it when the override has no bounds
	 * and a zone-only override (the natural way to say "same hours, our local
	 * time") is validated, stored, and then quietly discarded.
	 *
	 * So: bounds together, zone independently.
	 */
	if out.ProvisioningWindowStart == nil || out.ProvisioningWindowEnd == nil {
		out.ProvisioningWindowStart = def.ProvisioningWindowStart
		out.ProvisioningWindowEnd = def.ProvisioningWindowEnd
	}
	if out.ProvisioningWindowTZ == nil {
		out.ProvisioningWindowTZ = def.ProvisioningWindowTZ
	}

	/*
	 * Identity belongs to the row the caller asked about, not to whichever
	 * input happened to supply the last field. A merged view carrying the
	 * override's ID is the honest answer when an override exists; the
	 * no-override case is handled by GetRules, which stamps the requested
	 * ClientID so the result cannot be fed back into UpsertRules as the system
	 * default.
	 */
	return &out
}
