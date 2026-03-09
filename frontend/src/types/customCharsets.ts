export type CustomCharsetScope = 'global' | 'user' | 'team';

export interface CustomCharset {
  id: string;
  name: string;
  description: string;
  definition: string;
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
}
