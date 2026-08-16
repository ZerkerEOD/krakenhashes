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
	// CloudProviderMock drives the whole lifecycle against local
	// `agent --test-mode` processes. It exists so the provisioning,
	// isolation, budget and teardown paths can be exercised end to end
	// without renting a GPU.
	CloudProviderMock CloudProvider = "mock"
)

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

	LaunchedAt         sql.NullTime `json:"launched_at,omitempty"`
	ReadyAt            sql.NullTime `json:"ready_at,omitempty"`
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
type ClientCloudSettings struct {
	ClientID   uuid.UUID `json:"client_id"`
	ClientName string    `json:"client_name"`
	Enabled    bool      `json:"cloud_enabled"`
	// ProviderAllowlist holds CloudProvider values. Empty means no bursting.
	ProviderAllowlist []string `json:"cloud_provider_allowlist"`
	// BudgetCents is nil when the client is unfunded, which forbids
	// provisioning outright — distinct from a funded budget at zero headroom.
	BudgetCents           *int64 `json:"cloud_budget_cents"`
	MaxInstanceTTLMinutes *int   `json:"max_instance_ttl_minutes"`
	// ProviderAck records who accepted each provider's data-exposure terms and
	// when: {"vastai": {"at": "...", "by": "<user uuid>"}}.
	ProviderAck JSONMap `json:"provider_ack"`
}

// ClientCloudSettingsInput is the admin-editable subset. Acknowledgements are
// not settable here — they are recorded through their own endpoint so the
// attribution is always the authenticated caller.
type ClientCloudSettingsInput struct {
	Enabled               bool     `json:"cloud_enabled"`
	ProviderAllowlist     []string `json:"cloud_provider_allowlist"`
	BudgetCents           *int64   `json:"cloud_budget_cents"`
	MaxInstanceTTLMinutes *int     `json:"max_instance_ttl_minutes"`
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
