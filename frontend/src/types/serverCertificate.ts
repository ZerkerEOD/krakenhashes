/**
 * Types for the server certificate admin API (/api/admin/tls/*).
 *
 * Field names mirror the Go JSON tags exactly; do not camelCase them here.
 */

export type SanKind = 'ip' | 'dns';

export type DiscoverySource = 'agent_tls_failure' | 'host_header' | 'tls_sni' | 'manual';

export type CandidateStatus = 'new' | 'pending_reissue' | 'in_certificate';

export type TlsMode = 'self-signed' | 'provided' | 'certbot';

export interface CertificateInfo {
  subject: string;
  issuer: string;
  serial: string;
  not_before: string;
  not_after: string;
  days_remaining: number;
  dns_names: string[] | null;
  ip_addresses: string[] | null;
  signature_algorithm: string;
  public_key_bits: number;
  fingerprint_sha256: string;
}

export interface SanSettings {
  additional_ip_addresses: string[] | null;
  additional_dns_names: string[] | null;
  ip_source: 'database' | 'environment';
  dns_source: 'database' | 'environment';
  /** True when a deprecated KH_ADDITIONAL_* variable is still set. */
  deprecated_env_present: boolean;
  /**
   * The fixed set of ranges a certificate name may fall in. Informational and
   * not editable — the policy is deliberately not configurable.
   */
  allowed_ranges: string[] | null;
}

export interface DriftReport {
  in_sync: boolean;
  added?: string[];
  removed?: string[];
}

export interface EffectiveSans {
  dns_names: string[] | null;
  ip_addresses: string[] | null;
}

export interface CertificateStatus {
  tls_mode: TlsMode;
  /** False in provided/certbot mode, where names come from elsewhere. */
  managed: boolean;
  unmanaged_reason?: string;
  settings?: SanSettings;
  certificate?: CertificateInfo;
  ca?: CertificateInfo;
  drift?: DriftReport;
  /** What the certificate WOULD carry if reissued now. */
  effective_sans_preview?: EffectiveSans;
  /** Entries that are always present and cannot be removed. */
  locked?: EffectiveSans;
}

export interface DiscoveredAddress {
  id: number;
  address: string;
  kind: SanKind;
  sources: DiscoverySource[];
  first_seen_at: string;
  last_seen_at: string;
  hit_count: number;
  last_agent_id?: number;
  last_agent_name?: string;
  last_user_agent?: string;
  last_port?: number;
  status: CandidateStatus;
  allowed: boolean;
  rejection_reason?: string;
}

export interface NginxReloadOutcome {
  attempted: boolean;
  succeeded: boolean;
  /**
   * The underlying failure, verbatim. There is deliberately no "run this
   * command" field: any such command names a container or service, and those
   * differ per deployment, so a specific one is wrong more often than right.
   */
  detail?: string;
}

export interface Propagation {
  backend_hot_reloaded: boolean;
  restart_required: boolean;
  existing_connections_unaffected: boolean;
  nginx_reload: NginxReloadOutcome;
  agents_action_required: 'none' | 'refetch-ca';
}

export interface ReissueReport {
  success: boolean;
  reissued: boolean;
  reason: string;
  added_sans?: string[];
  removed_sans?: string[];
  certificate?: CertificateInfo;
  ca?: CertificateInfo;
  backup_dir?: string;
  propagation: Propagation;
  warnings?: string[];
}

export interface SanRejection {
  kind: SanKind;
  value: string;
  reason: string;
}

export interface SanFailureItem {
  address: string;
  agent_names: string[] | null;
  last_seen_at: string;
}

export interface SanFailureSummary {
  active: boolean;
  distinct_addresses: number;
  agent_count: number;
  last_report_at?: string;
  addresses: SanFailureItem[];
}
