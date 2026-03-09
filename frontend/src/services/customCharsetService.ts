import { api } from './api';
import { CustomCharset, CustomCharsetFormData } from '../types/customCharsets';

// Admin CRUD for global charsets
export const listGlobalCharsets = async (): Promise<CustomCharset[]> => {
  const response = await api.get<CustomCharset[]>('/api/admin/custom-charsets');
  return response.data;
};

export const createGlobalCharset = async (data: CustomCharsetFormData): Promise<CustomCharset> => {
  const response = await api.post<CustomCharset>('/api/admin/custom-charsets', data);
  return response.data;
};

export const updateGlobalCharset = async (id: string, data: CustomCharsetFormData): Promise<CustomCharset> => {
  const response = await api.put<CustomCharset>(`/api/admin/custom-charsets/${id}`, data);
  return response.data;
};

export const deleteGlobalCharset = async (id: string): Promise<void> => {
  await api.delete(`/api/admin/custom-charsets/${id}`);
};

// User-facing endpoints (all accessible charsets: global + personal + team)
export const listAccessibleCharsets = async (): Promise<CustomCharset[]> => {
  const response = await api.get<CustomCharset[]>('/api/custom-charsets');
  return response.data;
};

export const createUserCharset = async (data: CustomCharsetFormData): Promise<CustomCharset> => {
  const response = await api.post<CustomCharset>('/api/custom-charsets', data);
  return response.data;
};

export const updateUserCharset = async (id: string, data: CustomCharsetFormData): Promise<CustomCharset> => {
  const response = await api.put<CustomCharset>(`/api/custom-charsets/${id}`, data);
  return response.data;
};

export const deleteUserCharset = async (id: string): Promise<void> => {
  await api.delete(`/api/custom-charsets/${id}`);
};
