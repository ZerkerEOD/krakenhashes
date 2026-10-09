/**
 * Branding service (GitHub issue #41).
 *
 * `getPublicBranding` is called before authentication (login page, tab title,
 * theme) so it uses a plain fetch like `api/version.ts` and stays clear of the
 * axios 401 interceptor. The admin calls go through the shared axios instance.
 */
import { api } from './api';
import { apiUrl } from '../config';

export interface PublicBranding {
  app_name: string;
  page_title: string;
  powered_by: string;
  branded: boolean;
  primary_color: string;
  secondary_color: string | null;
  logo_url: string | null;
  favicon_url: string | null;
  version: number;
}

export interface BrandingSettingsInput {
  app_name: string;
  page_title: string;
  primary_color: string;
  secondary_color: string;
}

export interface AdminBranding {
  settings: BrandingSettingsInput;
  has_logo: boolean;
  has_favicon: boolean;
  effective: PublicBranding;
}

export type BrandingAssetKind = 'logo' | 'favicon';

export const DEFAULT_PRIMARY_COLOR = '#ff0000';

export const DEFAULT_BRANDING: PublicBranding = {
  app_name: 'KrakenHashes',
  page_title: 'KrakenHashes',
  powered_by: 'powered by KrakenHashes',
  branded: false,
  primary_color: DEFAULT_PRIMARY_COLOR,
  secondary_color: null,
  logo_url: null,
  favicon_url: null,
  version: 0,
};

export const getPublicBranding = async (): Promise<PublicBranding> => {
  const response = await fetch(`${apiUrl}/api/branding`, {
    method: 'GET',
    headers: { Accept: 'application/json' },
    credentials: 'include',
    cache: 'no-cache',
  });
  if (!response.ok) {
    throw new Error(`Failed to fetch branding: ${response.statusText}`);
  }
  const data = (await response.json()) as Partial<PublicBranding>;
  return { ...DEFAULT_BRANDING, ...data };
};

export const getBrandingSettings = async (): Promise<AdminBranding> => {
  const response = await api.get<AdminBranding>('/api/admin/settings/branding');
  return response.data;
};

export const updateBrandingSettings = async (input: BrandingSettingsInput): Promise<AdminBranding> => {
  const response = await api.put<AdminBranding>('/api/admin/settings/branding', input);
  return response.data;
};

export const uploadBrandingAsset = async (kind: BrandingAssetKind, file: File): Promise<AdminBranding> => {
  const formData = new FormData();
  formData.append('file', file);
  const response = await api.post<AdminBranding>(`/api/admin/settings/branding/${kind}`, formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  });
  return response.data;
};

export const deleteBrandingAsset = async (kind: BrandingAssetKind): Promise<AdminBranding> => {
  const response = await api.delete<AdminBranding>(`/api/admin/settings/branding/${kind}`);
  return response.data;
};
