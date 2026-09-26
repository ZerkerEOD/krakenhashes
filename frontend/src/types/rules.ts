/**
 * Types for rule management
 */

export enum RuleType {
  HASHCAT = 'hashcat',
  JOHN = 'john'
}

export enum RuleStatus {
  READY = 'verified',
  PROCESSING = 'pending',
  FAILED = 'failed',
  ERROR = 'error',
  DELETED = 'deleted'
}

export interface Rule {
  id: string;
  name: string;
  description: string;
  rule_type: RuleType;
  file_name: string;
  md5_hash: string;
  file_size: number;
  rule_count: number;
  created_at: string;
  updated_at: string;
  verification_status: RuleStatus;
  missing_since?: string; // set when verification_status='failed' because the file is gone from disk (GH #93)
  created_by: string;
  updated_by?: string;
  last_verified_at?: string;
  tags?: string[];
  is_enabled: boolean;
}

export interface RuleUploadResponse {
  id: string;
  name: string;
  message: string;
  success: boolean;
  duplicate?: boolean;
}

export interface RuleFilters {
  search?: string;
  rule_type?: RuleType;
  verification_status?: RuleStatus;
  sortBy?: string;
  sortOrder?: 'asc' | 'desc';
} 