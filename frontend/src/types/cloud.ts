export type CloudProviderKind =
  | 'vastai'
  | 'aws'
  | 'runpod'
  | 'runpod_community'
  | 'mock';

/**
 * Every kind, in the order the UI should offer them: least surprising first,
 * peer hardware last.
 */
export const CLOUD_PROVIDER_KINDS: CloudProviderKind[] = [
  'aws',
  'runpod',
  'vastai',
  'runpod_community',
  'mock',
];

/**
 * Whether this provider places client hash material on hardware the operator
 * does not control. MIRRORS CloudProvider.RequiresThirdPartyAck in
 * backend/internal/models/cloud.go — keep the two in step.
 *
 * True for peer/consumer hardware only: Vast.ai rents individually-owned
 * machines and RunPod Community rents peer-operated hosts, and in both cases
 * the host's owner has root over the container.
 *
 * Deliberately FALSE for AWS and RunPod Secure. AWS is the operator's own
 * account; RunPod Secure is RunPod's own SOC 2 Type II datacentres. Both must
 * render with NO warning surface at all — every red chip, every acknowledge
 * dialog and every consent gate keys off this function, so making it true for
 * them would not add safety, it would teach operators that the warning is noise
 * and get the one real instance of it clicked through.
 */
export function requiresThirdPartyAck(kind: CloudProviderKind): boolean {
  return kind === 'vastai' || kind === 'runpod_community';
}

/** Operator-facing name. The two RunPod tiers must never both read "RunPod". */
export function cloudProviderLabel(kind: CloudProviderKind): string {
  switch (kind) {
    case 'aws':
      return 'AWS';
    case 'runpod':
      return 'RunPod Secure Cloud';
    case 'runpod_community':
      return 'RunPod Community Cloud';
    case 'vastai':
      return 'Vast.ai';
    case 'mock':
      return 'Mock (testing)';
    default:
      return kind;
  }
}

/**
 * The rules an admin sets for WHEN provisioning may happen, as opposed to the
 * budget ladder's HOW MUCH.
 *
 * Every field is nullable, and null means two different things depending on the
 * row: on the system default it means the rule is not configured and constrains
 * nothing; on a client override it means INHERIT. That is why each rule has an
 * in-band "off" value (0, or start === end for the window) — so a client can
 * switch off an inherited rule without null having to carry both meanings.
 */
export interface CloudProvisioningRules {
  id: string;
  client_id?: string | null;
  /** Absolute job priority floor. 0 = no floor. */
  min_job_priority?: number | null;
  /** Continuous starvation required before renting. 0 = first starving tick. */
  min_starvation_seconds?: number | null;
  /** Refuse to rent for a job projected to finish this soon. 0 = never skip. */
  skip_if_finishing_within_seconds?: number | null;
  /** Whole-life cap on cloud spend for one job. 0 = no cap. Not month-windowed. */
  max_spend_per_job_cents?: number | null;
  /** "HH:MM:SS". Equal values mean always; end < start wraps midnight. */
  provisioning_window_start?: string | null;
  provisioning_window_end?: string | null;
  /** IANA zone name. */
  provisioning_window_tz?: string | null;
  created_at: string;
  updated_at: string;
}

/**
 * The per-client rules view.
 *
 * All three are returned because `effective` alone cannot tell "inherited" from
 * "set to the same value as the default", and the two behave differently: only
 * the inherited one follows a later change to the default.
 */
export interface CloudClientRulesView {
  effective: CloudProvisioningRules;
  override: CloudProvisioningRules | null;
  system_default: CloudProvisioningRules;
}

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

/** The window a client's spend ceiling applies to. */
export type BudgetPeriod = 'monthly' | 'quarterly' | 'semiannual';

export const BUDGET_PERIODS: BudgetPeriod[] = ['monthly', 'quarterly', 'semiannual'];

/**
 * Server-side values a client inherits when it has not set its own.
 *
 * cloud_budget_cents is nullable and null is meaningful: no default configured,
 * so an inheriting client stays unfunded. That is deliberately distinct from a
 * default of 0, which would be the same outcome by accident.
 */
export interface ClientCloudDefaults {
  cloud_budget_cents: number | null;
  cloud_budget_period: BudgetPeriod;
  cloud_enabled: boolean;
  cloud_provider_allowlist: CloudProviderKind[];
}

/** Per-client cloud burst configuration. */
export interface ClientCloudSettings {
  client_id: string;
  client_name: string;
  /** null means the client has never been configured and inherits the default. */
  cloud_enabled: boolean | null;
  cloud_budget_period: BudgetPeriod | null;
  /** Server-resolved values. Read-only; never sent back. */
  effective_cloud_enabled: boolean;
  effective_cloud_provider_allowlist: CloudProviderKind[];
  effective_cloud_budget_cents: number | null;
  effective_cloud_budget_period: BudgetPeriod;
  /** Which fields are taking their value from the server default. */
  inherited_fields: string[];
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

/**
 * Every field is nullable so "clear this and go back to inheriting" is
 * expressible. A plain boolean could only ever say "off", leaving no way to
 * return an explicitly-set client to the default.
 */
export interface ClientCloudSettingsInput {
  cloud_enabled: boolean | null;
  cloud_provider_allowlist: CloudProviderKind[];
  cloud_budget_cents: number | null;
  cloud_budget_period: BudgetPeriod | null;
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
