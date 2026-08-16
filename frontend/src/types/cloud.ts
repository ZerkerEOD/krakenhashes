export type CloudProviderKind = 'vastai' | 'aws' | 'mock';

/**
 * OpenVPN is deliberately absent: it needs /dev/net/tun and CAP_NET_ADMIN,
 * neither of which exists in a Vast.ai unprivileged container.
 */
export type VPNProviderKind = 'tailscale' | 'netbird' | 'wireguard';

/**
 * oauth/pat let the backend mint a single-use ephemeral credential per
 * instance. reusable_key is one shared key that expires (Tailscale caps auth
 * keys at 90 days) and strands every launch once it lapses. static_config is
 * WireGuard: no per-instance credential, no auto-deregistration.
 */
export type VPNCredentialKind = 'oauth' | 'pat' | 'reusable_key' | 'static_config';

/** A configured cloud backend. Secrets are never returned. */
export interface CloudProviderConfig {
  id: string;
  provider: CloudProviderKind;
  name: string;
  enabled: boolean;
  settings: Record<string, any>;
  max_concurrent_instances: number;
  max_instance_hourly_cents: number;
  vpn_provider?: VPNProviderKind;
  vpn_credential_kind?: VPNCredentialKind;
  vpn_credential_expires_at: string | null;
  vpn_tag_or_group?: string;
  backend_vpn_host?: string;
  third_party_ack_at: string | null;
  third_party_ack_by?: string;
  /** Whether a secret is stored — the value itself never leaves the backend. */
  has_credentials: boolean;
  has_vpn_credential: boolean;
  created_at: string;
  updated_at: string;
}

/**
 * Inbound provider configuration.
 *
 * credentials/vpn_credential are write-only and omitted-means-unchanged, so
 * editing an unrelated field cannot blank a stored secret.
 */
export interface CloudProviderConfigInput {
  provider: CloudProviderKind;
  name: string;
  enabled: boolean;
  credentials?: string;
  settings: Record<string, any>;
  max_concurrent_instances: number;
  max_instance_hourly_cents: number;
  vpn_provider?: VPNProviderKind;
  vpn_credential_kind?: VPNCredentialKind;
  vpn_credential?: string;
  vpn_tag_or_group?: string;
  backend_vpn_host?: string;
}

/** Per-client cloud burst configuration. */
export interface ClientCloudSettings {
  client_id: string;
  client_name: string;
  cloud_enabled: boolean;
  /**
   * Empty by default. A client must be explicitly opted in to each provider —
   * this is what keeps a client's hashes off Vast.ai's third-party machines
   * while still permitting AWS.
   */
  cloud_provider_allowlist: CloudProviderKind[];
  /** null means unfunded, which forbids provisioning outright. */
  cloud_budget_cents: number | null;
  max_instance_ttl_minutes: number | null;
  /** { "vastai": { at, by } } — per-provider data-exposure acknowledgement. */
  provider_ack: Record<string, { at: string; by: string }>;
}

export interface ClientCloudSettingsInput {
  cloud_enabled: boolean;
  cloud_provider_allowlist: CloudProviderKind[];
  cloud_budget_cents: number | null;
  max_instance_ttl_minutes: number | null;
}

export type CloudInstanceState =
  | 'requested'
  | 'launching'
  | 'provisioning'
  | 'syncing'
  | 'running'
  | 'draining'
  | 'terminating'
  | 'terminated'
  | 'failed';

/** A rented GPU instance. Anything not terminated/failed may still be billing. */
export interface CloudInstance {
  id: string;
  provider_config_id: string;
  label: string;
  provider_instance_id?: string;
  agent_id?: number;
  job_execution_id?: string;
  client_id?: string;
  client_name_snapshot?: string;
  state: CloudInstanceState;
  gpu_model?: string;
  gpu_count?: number;
  hourly_rate_cents: number;
  disk_gb?: number;
  fileset_bytes?: number;
  reserved_cents: number;
  estimated_cost_cents: number;
  actual_cost_cents?: number;
  ttl_epoch?: string;
  launched_at?: string;
  ready_at?: string;
  terminated_at?: string;
  termination_reason?: string;
  /** Non-zero means teardown is failing and the instance is still billing. */
  terminate_attempts: number;
  last_terminate_error?: string;
  created_at: string;
  updated_at: string;
}

export interface CloudBudgetState {
  client_id: string;
  /** null means the client has no funded budget, so nothing may be rented. */
  cap_cents: number | null;
  incurred_cents: number;
  reserved_cents: number;
  available_cents: number;
  used_pct: number;
}

export interface CloudBudgetPolicy {
  id: string;
  client_id?: string;
  /** null disables threshold notifications entirely. */
  notify_pct?: number | null;
  stop_provision_pct: number;
  drain_pct: number;
  hard_stop_pct: number;
  allow_overage: boolean;
  drain_timeout_seconds: number;
}

export type BudgetAction = 'none' | 'notify' | 'stop_provisioning' | 'drain' | 'hard_stop';

export interface CloudBudgetAssessment {
  state: CloudBudgetState;
  policy: CloudBudgetPolicy;
  action: BudgetAction;
  reason?: string;
}

/** A provider's self-assessment. Inconclusive checks count as failure. */
export interface CloudPreflightReport {
  ok: boolean;
  identity?: string;
  missing_permissions?: string[];
  inconclusive?: string[];
  quota_limit?: number;
  quota_used?: number;
  quota_source?: string;
  warnings?: string[];
  errors?: string[];
}

/**
 * Completion projection for a job.
 *
 * coverage_pct below 100 means the budget runs out before the work does. The
 * UI must require an explicit confirmation in that case rather than launching
 * silently — a silently-enforced cap is how operators get surprised.
 */
export interface CloudProjection {
  job_execution_id: string;
  remaining_base_keyspace: number;
  onprem_speed: number;
  cloud_speed: number;
  time_to_finish_seconds: number;
  projected_cost_cents: number;
  available_cents: number;
  coverage_pct: number;
  will_finish: boolean;
  /** Salted hash type: real throughput will exceed this estimate. */
  pessimistic: boolean;
  note?: string;
}
