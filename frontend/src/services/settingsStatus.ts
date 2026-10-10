import { api } from './api';

/** GET /api/admin/settings/status — one probe per settings area for the hub. */
export interface SettingsStatus {
  email: { configured: boolean; error?: string };
  certificate: { managed: boolean; tls_mode: string; days_remaining?: number | null; not_after?: string | null; error?: string };
  sso: { ephemeral_key: boolean; providers_enabled: number; providers_total: number; error?: string };
  binaries: { total: number; verified_active: number; error?: string };
  cloud: { providers: number; monthly_cap_cents: number; error?: string };
  storage: { enabled: boolean; reachable: boolean; backend: string; migration_state: string; error?: string };
  webhook: { enabled: boolean; has_url: boolean; has_secret: boolean; error?: string };
  generated_at: string;
}

export type StatusKey = keyof Omit<SettingsStatus, 'generated_at'>;

export type AttentionSeverity = 'ok' | 'warning' | 'error';

export interface Attention {
  severity: AttentionSeverity;
  /** i18n key under admin:hub.attention.* */
  messageKey: string;
  params?: Record<string, unknown>;
}

export const getSettingsStatus = async (): Promise<SettingsStatus> => {
  const response = await api.get<SettingsStatus>('/api/admin/settings/status');
  return response.data;
};

const ok: Attention = { severity: 'ok', messageKey: 'ok' };

/** Decides which cards need attention. Keep in sync with the hub copy in admin.json. */
export const deriveAttention = (s: SettingsStatus): Record<StatusKey, Attention> => ({
  email: s.email.error
    ? { severity: 'warning', messageKey: 'probeFailed' }
    : s.email.configured
    ? ok
    : { severity: 'warning', messageKey: 'emailNotConfigured' },
  certificate: s.certificate.error
    ? { severity: 'warning', messageKey: 'probeFailed' }
    : !s.certificate.managed
    ? ok
    : s.certificate.days_remaining != null && s.certificate.days_remaining < 7
    ? { severity: 'error', messageKey: 'certExpiring', params: { days: s.certificate.days_remaining } }
    : s.certificate.days_remaining != null && s.certificate.days_remaining < 30
    ? { severity: 'warning', messageKey: 'certExpiring', params: { days: s.certificate.days_remaining } }
    : ok,
  sso: s.sso.error
    ? { severity: 'warning', messageKey: 'probeFailed' }
    : s.sso.ephemeral_key
    ? { severity: s.sso.providers_enabled > 0 ? 'error' : 'warning', messageKey: 'ssoEphemeralKey' }
    : ok,
  binaries: s.binaries.error
    ? { severity: 'warning', messageKey: 'probeFailed' }
    : s.binaries.verified_active === 0
    ? { severity: 'error', messageKey: 'noVerifiedBinary' }
    : ok,
  cloud: s.cloud.error
    ? { severity: 'warning', messageKey: 'probeFailed' }
    : s.cloud.providers > 0 && s.cloud.monthly_cap_cents <= 0
    ? { severity: 'warning', messageKey: 'cloudCapZero' }
    : ok,
  storage: s.storage.error
    ? { severity: 'warning', messageKey: 'probeFailed' }
    : s.storage.enabled && !s.storage.reachable
    ? { severity: 'error', messageKey: 'shareUnreachable' }
    : ['running', 'failed'].includes(s.storage.migration_state)
    ? { severity: 'warning', messageKey: 'migration', params: { state: s.storage.migration_state } }
    : ok,
  webhook: s.webhook.error
    ? { severity: 'warning', messageKey: 'probeFailed' }
    : s.webhook.enabled && !s.webhook.has_url
    ? { severity: 'warning', messageKey: 'webhookNoUrl' }
    : ok,
});

export const worst = (items: Attention[]): AttentionSeverity =>
  items.some((a) => a.severity === 'error') ? 'error' : items.some((a) => a.severity === 'warning') ? 'warning' : 'ok';
