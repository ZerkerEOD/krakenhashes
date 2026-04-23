import { api } from './api';
import { CustomCharset, CustomCharsetFormData } from '../types/customCharsets';

interface CharsetCreatePayload {
  name: string;
  description: string;
  definition: string;
  is_hex?: boolean;
}

// Admin CRUD for global charsets
export const listGlobalCharsets = async (): Promise<CustomCharset[]> => {
  const response = await api.get<CustomCharset[]>('/api/admin/custom-charsets');
  return response.data;
};

export const createGlobalCharset = async (data: CustomCharsetFormData): Promise<CustomCharset> => {
  const payload: CharsetCreatePayload = {
    name: data.name,
    description: data.description,
    definition: data.definition,
    is_hex: data.is_hex,
  };
  const response = await api.post<CustomCharset>('/api/admin/custom-charsets', payload);
  return response.data;
};

export const uploadGlobalCharsetFile = async (formData: FormData): Promise<CustomCharset> => {
  const response = await api.post<CustomCharset>('/api/admin/custom-charsets/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  });
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
  const payload: CharsetCreatePayload = {
    name: data.name,
    description: data.description,
    definition: data.definition,
    is_hex: data.is_hex,
  };
  const response = await api.post<CustomCharset>('/api/custom-charsets', payload);
  return response.data;
};

export const uploadUserCharsetFile = async (formData: FormData): Promise<CustomCharset> => {
  const response = await api.post<CustomCharset>('/api/custom-charsets/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  });
  return response.data;
};

export const updateUserCharset = async (id: string, data: CustomCharsetFormData): Promise<CustomCharset> => {
  const response = await api.put<CustomCharset>(`/api/custom-charsets/${id}`, data);
  return response.data;
};

export const deleteUserCharset = async (id: string): Promise<void> => {
  await api.delete(`/api/custom-charsets/${id}`);
};
