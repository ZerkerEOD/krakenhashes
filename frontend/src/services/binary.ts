import { api } from './api';

export interface BinaryVersion {
  id: number;
  binary_type: 'hashcat' | 'john';
  compression_type: '7z' | 'zip' | 'tar.gz' | 'tar.xz';
  source_url: string | null;
  file_name: string;
  md5_hash: string;
  file_size: number;
  created_at: string;
  is_active: boolean;
  is_default: boolean;
  last_verified_at: string | null;
  verification_status: 'pending' | 'verified' | 'failed' | 'deleted';
  source_type: 'url' | 'upload';
  description?: string;
  version?: string;
}

export interface AddBinaryRequest {
  binary_type: 'hashcat' | 'john';
  compression_type: '7z' | 'zip' | 'tar.gz' | 'tar.xz';
  source_url: string;
  file_name: string;
  set_as_default?: boolean;
  description?: string;
  version?: string;
}

export interface UploadBinaryRequest {
  binary_type: 'hashcat' | 'john';
  compression_type: '7z' | 'zip' | 'tar.gz' | 'tar.xz';
  file: File;
  file_name: string;
  set_as_default?: boolean;
  description?: string;
  version?: string;
}

export const listBinaries = async () => {
  try {
    const response = await api.get<BinaryVersion[]>('/api/admin/binary');
    console.debug('Binary list response:', response);
    return response;
  } catch (error) {
    console.error('Error in listBinaries:', error);
    throw error;
  }
};

export const addBinary = (binary: AddBinaryRequest) => {
  return api.post<BinaryVersion>('/api/admin/binary', binary);
};

export const uploadBinary = async (request: UploadBinaryRequest): Promise<BinaryVersion> => {
  const formData = new FormData();
  formData.append('binary_type', request.binary_type);
  formData.append('compression_type', request.compression_type);
  formData.append('file', request.file);
  formData.append('file_name', request.file_name);
  if (request.version) formData.append('version', request.version);
  if (request.description) formData.append('description', request.description);
  if (request.set_as_default) formData.append('set_as_default', 'true');

  const response = await fetch('/api/admin/binary/upload', {
    method: 'POST',
    body: formData,
    credentials: 'include',
  });

  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(errorText || 'Failed to upload binary');
  }

  return response.json() as Promise<BinaryVersion>;
};

export const verifyBinary = (id: number) => {
  return api.post<void>(`/api/admin/binary/${id}/verify`);
};

export const deleteBinary = (id: number) => {
  return api.delete<void>(`/api/admin/binary/${id}`);
};

export const getBinary = (id: number) => {
  return api.get<BinaryVersion>(`/api/admin/binary/${id}`);
};

export const setDefaultBinary = (id: number) => {
  return api.put<{ message: string }>(`/api/admin/binary/${id}/set-default`);
}; 