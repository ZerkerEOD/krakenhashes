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

/**
 * How far this provider has been proven against the real thing, spending real
 * money. MIRRORS providerMaturity in backend/internal/models/cloud.go — keep
 * the two in step, and note the backend is AUTHORITATIVE: every saved config
 * carries a server-computed `maturity` field, and that value must win wherever
 * one exists. This mirror is only for the CREATE form, where the operator has
 * picked a kind but nothing has been saved to ask the server about yet.
 *
 * Experimental does not mean unfinished. Vast.ai is fully implemented and has
 * never once been paid for; AWS has been driven end to end through teardown and
 * a settled refund. Only the second earns "stable".
 */
export function providerMaturity(kind: CloudProviderKind): ProviderMaturity {
  switch (kind) {
    case 'aws':
    case 'mock':
      return 'stable';
    case 'vastai':
    case 'runpod':
    case 'runpod_community':
      return 'experimental';
  }
}

/*
 * Where a problem with an experimental provider goes.
 *
 * TWO destinations on purpose. The bug report is public and permanent, so it
 * gets the narrative; the diagnostic bundle is not, because it carries client
 * names, hostnames and job metadata that must not land in a public issue. The
 * split is restated in the issue template and in
 * docs/admin-guide/system-setup/cloud-providers.md — keep the three in step.
 */
export const CLOUD_ISSUE_URL = 'https://github.com/ZerkerEOD/krakenhashes/issues/new/choose';
export const CLOUD_DISCORD_URL = 'https://discord.gg/taafA9cSFV';

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
  /**
   * How far this adapter has been proven against the real provider, spending
   * real money. Derived server-side from the provider kind, never stored, so
   * it cannot be edited into a lie by an admin who would rather not see the
   * warning.
   *
   * This is NOT "how finished is the code". Vast.ai is fully implemented and
   * has never once been paid for; AWS has been driven through boot,
   * commissioning, cracking, clean release and a settled refund. Those look
   * identical from outside and cost very differently when wrong.
   */
  maturity: ProviderMaturity;
  created_at: string;
  updated_at: string;
}

/** @see CloudProviderConfig.maturity */
export type ProviderMaturity = 'stable' | 'experimental';

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
  /**
   * Set once the budget ladder's drain rung takes this instance out of
   * dispatch. drain_timeout_seconds is measured from here.
   */
  drain_started_at?: string;
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
 * One placement the operator has approved for launches.
 *
 * subnet_id is the only field a launch needs — RunInstances takes a subnet, not
 * a zone. zone is the display alias and zone_id the stable physical id, which
 * is what capacity APIs report and therefore the only safe join key.
 *
 * An EMPTY instance_types means "every type I have priced", not "none". That is
 * what a select-all click stores, and it keeps the selection from silently
 * narrowing when a new instance type is priced later.
 */
export interface AWSZoneSelection {
  zone: string;
  zone_id: string;
  subnet_id: string;
  instance_types: string[];
}

/** How much weight the UI should give a capacity signal. @see SignalDescriptor */
export type SignalTrust = 'definitive' | 'measured' | 'advisory';

/** Which per-cell column a signal describes. */
export type SignalKind =
  | 'offered'
  | 'available_count'
  | 'live_price'
  | 'configured_price'
  | 'score';

/**
 * What one grid column means and how far to trust it.
 *
 * Providers differ far more in signal QUALITY than in structure, and the UI must
 * not render a guess and a measurement identically. AWS's placement score is an
 * opinion that has been measured contradicting reality (1/10 in every zone,
 * minutes before a first-try launch succeeded); Vast.ai's rentable count is real
 * inventory. Both are numbers in a cell, and only one is worth acting on.
 */
export interface SignalDescriptor {
  kind: SignalKind;
  label: string;
  trust: SignalTrust;
  /** Shown to the operator verbatim. Says what the signal is worth, not what it is called. */
  explanation: string;
}

/** One cell of the placement x hardware grid. */
export interface HardwareCapacity {
  /** Provider key: an AWS instance type, a Vast.ai GPU name, a RunPod gpuType id. */
  id: string;
  /**
   * Does the provider sell this hardware in this placement at all? The only
   * definitive signal in the grid — false is a hard no.
   *
   * A provider that cannot answer reports true, never false: "we could not find
   * out" rendered as "not available" hides capacity that exists.
   */
  offered: boolean;
  selected: boolean;
  /** The operator's declared rate, where the provider has one. This is what the budget reserves against. */
  configured_cents?: number;
  /** What the provider charges right now. ZERO MEANS UNKNOWN, never free. */
  live_cents?: number;
  observed_at?: string;
  /** Live count of rentable units. ZERO MEANS UNKNOWN. The strongest per-cell signal any provider gives. */
  available_count?: number;
  /** Provider's 1-10 ranking for this cell. ZERO MEANS UNKNOWN. Orders pools; never removes one. */
  score?: number;
  gpu_model?: string;
  gpu_count?: number;
  note?: string;
}

/** One placement: an AWS zone, a Vast.ai country, a RunPod data centre. */
export interface PlacementCapacity {
  /** Stable provider key and the safe join key for any capacity API. */
  id: string;
  /** What to show a human. On AWS this is the per-account zone alias. */
  name: string;
  /** What a launch needs to reach here: a subnet id, a data centre id, a geolocation. */
  ref?: string;
  detail?: string;
  selected: boolean;
  hardware: HardwareCapacity[];
  /**
   * A placement-wide ranking where the provider has one. ZERO MEANS UNKNOWN.
   *
   * On AWS this scores all priced instance types TOGETHER, which is usually a
   * much better number than any single type's — g6 alone scored 1/10 in a zone
   * where g4dn + g5 + g6 together scored 9/10. That gap is the argument for
   * ticking more boxes, in the provider's own numbers.
   */
  score?: number;
  /** False when this placement can never launch; notes says why. */
  usable: boolean;
  notes?: string[];
}

/**
 * Where a provider could actually get a GPU: a grid of placement x hardware.
 *
 * pool_count is the number this screen exists to raise. Capacity generally
 * belongs to the (hardware, placement) PAIR rather than to either alone, so it
 * is the count of distinct pools a launch may fall back through. One means one
 * chance.
 */
export interface CloudCapacityReport {
  region?: string;
  /** Axis names in the provider's own vocabulary — "Availability zone" vs "Country". */
  placement_label: string;
  hardware_label: string;
  placements: PlacementCapacity[];
  hardware: string[];
  signals?: SignalDescriptor[];
  warnings?: string[];
  pool_count: number;
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
