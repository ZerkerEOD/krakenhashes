import { api } from './api';
import {
  CloudInstance,
  CloudBudgetAssessment,
  CloudBudgetPolicy,
  CloudPreflightReport,
  CloudProjection,
  CloudProviderConfig,
  CloudProviderConfigInput,
  CloudProviderKind,
  CloudProvisioningRules,
  CloudClientRulesView,
  ClientCloudSettings,
  ClientCloudSettingsInput,
} from '../types/cloud';

export interface CloudProviderListResponse {
  providers: CloudProviderConfig[];
  /**
   * True when the encryption key was generated at startup, which means every
   * stored secret becomes unreadable after the next restart.
   */
  encryptionKeyEphemeral: boolean;
}

export const listCloudProviders = async (): Promise<CloudProviderListResponse> => {
  const response = await api.get<{
    data: CloudProviderConfig[];
    encryption_key_ephemeral: boolean;
  }>('/api/admin/cloud/providers');
  return {
    providers: response.data.data ?? [],
    encryptionKeyEphemeral: Boolean(response.data.encryption_key_ephemeral),
  };
};

export const createCloudProvider = async (
  input: CloudProviderConfigInput
): Promise<CloudProviderConfig> => {
  const response = await api.post<CloudProviderConfig>('/api/admin/cloud/providers', input);
  return response.data;
};

/** Omit credentials/vpn_credential to leave the stored secrets untouched. */
export const updateCloudProvider = async (
  id: string,
  input: CloudProviderConfigInput
): Promise<CloudProviderConfig> => {
  const response = await api.put<CloudProviderConfig>(`/api/admin/cloud/providers/${id}`, input);
  return response.data;
};

/** Fails with 409 while any rented instance still references the config. */
export const deleteCloudProvider = async (id: string): Promise<void> => {
  await api.delete(`/api/admin/cloud/providers/${id}`);
};

/** Records that this admin accepted the provider's data-exposure terms. */
export const acknowledgeCloudProvider = async (id: string): Promise<void> => {
  await api.post(`/api/admin/cloud/providers/${id}/acknowledge`);
};

export const listClientCloudSettings = async (): Promise<ClientCloudSettings[]> => {
  const response = await api.get<{ data: ClientCloudSettings[] }>('/api/admin/cloud/clients');
  return response.data.data ?? [];
};

export const getClientCloudSettings = async (clientId: string): Promise<ClientCloudSettings> => {
  const response = await api.get<ClientCloudSettings>(
    `/api/admin/cloud/clients/${clientId}/settings`
  );
  return response.data;
};

export const updateClientCloudSettings = async (
  clientId: string,
  input: ClientCloudSettingsInput
): Promise<ClientCloudSettings> => {
  const response = await api.put<ClientCloudSettings>(
    `/api/admin/cloud/clients/${clientId}/settings`,
    input
  );
  return response.data;
};

/**
 * Records this client's acknowledgement for one provider. Required before
 * Vast.ai may be added to the client's allowlist: the provider-level
 * acknowledgement says the operator understands Vast.ai, not that this
 * engagement's data may go there.
 */
export const acknowledgeClientProvider = async (
  clientId: string,
  provider: CloudProviderKind
): Promise<void> => {
  await api.post(`/api/admin/cloud/clients/${clientId}/acknowledge`, { provider });
};

export const getDefaultBudgetPolicy = async (): Promise<CloudBudgetPolicy> => {
  const response = await api.get<CloudBudgetPolicy>('/api/admin/cloud/policy');
  return response.data;
};

export const updateDefaultBudgetPolicy = async (
  policy: CloudBudgetPolicy
): Promise<CloudBudgetPolicy> => {
  const response = await api.put<CloudBudgetPolicy>('/api/admin/cloud/policy', policy);
  return response.data;
};

export const updateClientBudgetPolicy = async (
  clientId: string,
  policy: CloudBudgetPolicy
): Promise<CloudBudgetPolicy> => {
  const response = await api.put<CloudBudgetPolicy>(
    `/api/admin/cloud/clients/${clientId}/policy`,
    policy
  );
  return response.data;
};

/** Drops a client override so the client falls back to the system default. */
export const deleteClientBudgetPolicy = async (clientId: string): Promise<void> => {
  await api.delete(`/api/admin/cloud/clients/${clientId}/policy`);
};

/** Live rented instances — anything that may still be costing money. */
export const listCloudInstances = async (): Promise<CloudInstance[]> => {
  const response = await api.get<{ data: CloudInstance[] }>('/api/admin/cloud/instances');
  return response.data.data ?? [];
};

/**
 * Destroy a rented instance immediately.
 *
 * The backend calls the provider directly rather than waiting for the reaper:
 * when an operator reaches for this, the next reaper tick is up to a minute of
 * billing away. A 502 means the provider refused and the instance is STILL
 * billing — surface that verbatim rather than treating it as a generic error.
 */
export const destroyCloudInstance = async (id: string): Promise<void> => {
  await api.delete(`/api/admin/cloud/instances/${id}`);
};

/** Ask a provider whether it could actually launch anything right now. */
export const runCloudPreflight = async (providerConfigId: string): Promise<CloudPreflightReport> => {
  const response = await api.post<CloudPreflightReport>(
    `/api/admin/cloud/providers/${providerConfigId}/preflight`
  );
  return response.data;
};

/** Current spend picture for a client, plus the action the ladder implies. */
export const getClientCloudBudget = async (clientId: string): Promise<CloudBudgetAssessment> => {
  const response = await api.get<CloudBudgetAssessment>(`/api/admin/cloud/clients/${clientId}/budget`);
  return response.data;
};

/**
 * Projected completion and cost for a job.
 *
 * cloudSpeed/hourlyRateCents describe the capacity being considered, so the
 * caller can model "what if I add one of these" before spending anything.
 */
export const getJobProjection = async (
  jobId: string,
  opts: { cloudSpeed?: number; hourlyRateCents?: number; clientId?: string } = {}
): Promise<CloudProjection> => {
  const params = new URLSearchParams();
  if (opts.cloudSpeed) params.set('cloud_speed', String(opts.cloudSpeed));
  if (opts.hourlyRateCents) params.set('hourly_rate_cents', String(opts.hourlyRateCents));
  if (opts.clientId) params.set('client_id', opts.clientId);

  const qs = params.toString();
  const response = await api.get<CloudProjection>(
    `/api/admin/cloud/jobs/${jobId}/projection${qs ? `?${qs}` : ''}`
  );
  return response.data;
};

/**
 * Rent exactly one instance for a job, right now.
 *
 * This spends real money on the spot, so it is deliberately one instance per
 * call rather than "scale this job up" — a burst has to be asked for
 * repeatedly. The autoscaler covers the unattended case; this is the manual
 * override and the thing to reach for when the autoscaler is not doing what
 * you expect.
 *
 * Resolves once the instance is recorded and requested from the provider, not
 * once the agent has registered. Watch the fleet view for that.
 */
export const provisionInstanceForJob = async (jobId: string): Promise<void> => {
  await api.post(`/api/admin/cloud/jobs/${jobId}/provision`);
};

/** Cents to a display string. All money crosses the wire as integer cents. */
export const formatCents = (cents: number): string => `$${(cents / 100).toFixed(2)}`;

/** Seconds to a compact duration. */
export const formatDuration = (seconds: number): string => {
  if (seconds <= 0) return '—';
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${Math.floor(seconds)}s`;
};

/** Remaining TTL in seconds, or 0 when expired/unset. */
export const ttlRemainingSeconds = (instance: CloudInstance): number => {
  if (!instance.ttl_epoch) return 0;
  const remaining = (new Date(instance.ttl_epoch).getTime() - Date.now()) / 1000;
  return remaining > 0 ? remaining : 0;
};

// ---------------------------------------------------------------------------
// Provisioning rules — WHEN the system may spend, as opposed to how much.
// ---------------------------------------------------------------------------

/**
 * The system default, UNMERGED.
 *
 * Deliberately not the merged view: an admin editing the defaults has to see
 * what the defaults themselves say. Saving back a merged result would bake one
 * client's override into the defaults for everyone.
 */
export const getDefaultProvisioningRules = async (): Promise<CloudProvisioningRules> => {
  const response = await api.get<CloudProvisioningRules>('/api/admin/cloud/rules');
  return response.data;
};

export const updateDefaultProvisioningRules = async (
  rules: Partial<CloudProvisioningRules>
): Promise<CloudProvisioningRules> => {
  const response = await api.put<CloudProvisioningRules>('/api/admin/cloud/rules', rules);
  return response.data;
};

/**
 * What one client is actually subject to, plus the raw override and the default
 * it was merged from. All three are needed because `effective` alone cannot
 * distinguish "inherited" from "set to the same value as the default", and only
 * the inherited one follows a later change to the default.
 */
export const getClientProvisioningRules = async (
  clientId: string
): Promise<CloudClientRulesView> => {
  const response = await api.get<CloudClientRulesView>(
    `/api/admin/cloud/clients/${clientId}/rules`
  );
  return response.data;
};

export const updateClientProvisioningRules = async (
  clientId: string,
  rules: Partial<CloudProvisioningRules>
): Promise<CloudProvisioningRules> => {
  const response = await api.put<CloudProvisioningRules>(
    `/api/admin/cloud/clients/${clientId}/rules`,
    rules
  );
  return response.data;
};

/** Drops a client override so every field inherits again. */
export const deleteClientProvisioningRules = async (clientId: string): Promise<void> => {
  await api.delete(`/api/admin/cloud/clients/${clientId}/rules`);
};
