export type CustomCharsetScope = 'global' | 'user' | 'team';
export type CustomCharsetType = 'inline' | 'file';

export interface CustomCharset {
  id: string;
  name: string;
  description: string;
  charset_type: CustomCharsetType;
  definition?: string;
  file_path?: string;
  file_md5?: string;
  file_size?: number;
  byte_count?: number;
  is_hex?: boolean;
  scope: CustomCharsetScope;
  owner_id?: string;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface CustomCharsetFormData {
  name: string;
  description: string;
  definition: string;
  is_hex?: boolean;
}
