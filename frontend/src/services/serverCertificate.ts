import { api } from './api';
import {
  CertificateStatus,
  DiscoveredAddress,
  ReissueReport,
  SanFailureSummary,
} from '../types/serverCertificate';

/**
 * Client for /api/admin/tls/*.
 *
 * Mounted under /admin/tls rather than /admin/settings because the settings
 * router has a `{key}` catch-all that would swallow these paths.
 */

export const getCertificateStatus = async (): Promise<CertificateStatus> => {
  const response = await api.get<CertificateStatus>('/api/admin/tls/certificate');
  return response.data;
};

export const getDiscoveredAddresses = async (): Promise<DiscoveredAddress[]> => {
  const response = await api.get<DiscoveredAddress[] | null>('/api/admin/tls/discovered');
  return response.data ?? [];
};

export const dismissDiscoveredAddress = async (id: number): Promise<void> => {
  await api.delete(`/api/admin/tls/discovered/${id}`);
};

export const getSanFailures = async (): Promise<SanFailureSummary> => {
  const response = await api.get<SanFailureSummary>('/api/admin/tls/san-failures');
  return response.data;
};

/**
 * Saves both name lists, optionally reissuing in the same call.
 *
 * The server validates every entry and writes nothing if any is rejected, so a
 * 400 here means the stored configuration is unchanged.
 */
export const updateSans = async (
  ipAddresses: string[],
  dnsNames: string[],
  apply: boolean
): Promise<CertificateStatus | ReissueReport> => {
  const response = await api.put<CertificateStatus | ReissueReport>('/api/admin/tls/sans', {
    additional_ip_addresses: ipAddresses,
    additional_dns_names: dnsNames,
    apply,
  });
  return response.data;
};

export const reissueCertificate = async (
  force = false,
  includeClientLeaf = false
): Promise<ReissueReport> => {
  const response = await api.post<ReissueReport>('/api/admin/tls/reissue', {
    force,
    include_client_leaf: includeClientLeaf,
  });
  return response.data;
};

/**
 * Confirmation phrase for CA rotation.
 *
 * Deliberately not translated: a type-to-confirm guard only works if the string
 * the user must type is stable across locales.
 */
export const ROTATE_CA_CONFIRMATION = 'ROTATE CA';

export const rotateCertificateAuthority = async (confirm: string): Promise<ReissueReport> => {
  const response = await api.post<ReissueReport>('/api/admin/tls/rotate-ca', { confirm });
  return response.data;
};
