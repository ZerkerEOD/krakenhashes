/**
 * Types for wordlist management
 */

export enum WordlistType {
  GENERAL = 'general',
  SPECIALIZED = 'specialized',
  TARGETED = 'targeted',
  CUSTOM = 'custom'
}

export enum WordlistStatus {
  READY = 'verified',
  PROCESSING = 'pending',
  ERROR = 'error',
  DELETED = 'deleted'
}

export interface Wordlist {
  id: string;
  name: string;
  description: string;
  wordlist_type: WordlistType;
  format: string;
  file_name: string;
  md5_hash: string;
  file_size: number;
  word_count: number;
  created_at: string;
  updated_at: string;
  verification_status: WordlistStatus;
  created_by: string;
  updated_by?: string;
  last_verified_at?: string;
  tags?: string[];
  is_enabled: boolean;
  is_potfile?: boolean;
}

export interface WordlistUploadResponse {
  id: string;
  name: string;
  message: string;
  success: boolean;
  duplicate?: boolean;
}

export interface WordlistFilters {
  search?: string;
  wordlist_type?: WordlistType;
  verification_status?: WordlistStatus;
  sortBy?: string;
  sortOrder?: 'asc' | 'desc';
}

// Deletion impact types (shared with rules)
export interface DeletionImpactJob {
  id: string;
  name: string;
  status: string;
  hashlist_name: string;
}

export interface DeletionImpactPresetJob {
  id: string;
  name: string;
  attack_mode: string;
}

export interface DeletionImpactWorkflowStep {
  workflow_id: string;
  workflow_name: string;
  step_order: number;
  preset_job_id: string;
  preset_job_name: string;
}

export interface DeletionImpactWorkflow {
  id: string;
  name: string;
  description: string;
  step_count: number;
}

export interface DeletionImpactDetails {
  jobs: DeletionImpactJob[];
  preset_jobs: DeletionImpactPresetJob[];
  workflow_steps: DeletionImpactWorkflowStep[];
  workflows_to_delete: DeletionImpactWorkflow[];
}

export interface DeletionImpactSummary {
  total_jobs: number;
  total_preset_jobs: number;
  total_workflow_steps: number;
  total_workflows_to_delete: number;
}

export interface DeletionImpact {
  resource_id: number;
  resource_type: 'wordlist' | 'rule';
  can_delete: boolean;
  has_cascading_impact: boolean;
  impact: DeletionImpactDetails;
  summary: DeletionImpactSummary;
} 