export interface HashType {
  id: number;
  name: string;
  description?: string | null;
  example?: string | null;
  needs_processing: boolean;
  processing_logic?: string | null;
  is_enabled: boolean;
  slow: boolean;
  is_salted: boolean;
}

export interface HashTypeCreateRequest {
  id: number;
  name: string;
  description?: string | null;
  example?: string | null;
  is_enabled: boolean;
  slow: boolean;
  is_salted: boolean;
}

export interface HashTypeUpdateRequest {
  name: string;
  description?: string | null;
  example?: string | null;
  is_enabled: boolean;
  slow: boolean;
  is_salted: boolean;
}