/**
 * API services for rule management
 */
import { api } from './api';
import { Rule, RuleFilters, RuleUploadResponse } from '../types/rules';
import { DeletionImpact } from '../types/wordlists';

// Get all rules with optional filtering
export const getRules = (filters?: RuleFilters) => 
  api.get<Rule[]>('/api/rules', { params: filters });

// Get a single rule by ID
export const getRule = (id: string) => 
  api.get<Rule>(`/api/rules/${id}`);

// Upload a new rule
export const uploadRule = (formData: FormData, onProgress?: (progress: number, eta?: number, speed?: number) => void) => {
  let startTime = Date.now();
  let lastTime = startTime;
  let lastLoaded = 0;
  
  return api.post<RuleUploadResponse>('/api/rules/upload', formData, {
    headers: {
      'Content-Type': 'multipart/form-data'
    },
    withCredentials: true, // Ensure cookies are sent with the request
    onUploadProgress: (progressEvent) => {
      const now = Date.now();
      const loaded = progressEvent.loaded;
      const total = progressEvent.total || 1;
      const percentCompleted = Math.round((loaded * 100) / total);
      
      // Calculate upload speed and ETA
      let etaSeconds = 0;
      let avgSpeed = 0;
      if (lastLoaded > 0) {
        // Use overall average speed for more stable ETA
        const totalTime = (now - startTime) / 1000; // seconds
        avgSpeed = loaded / totalTime; // bytes per second
        
        const remaining = total - loaded;
        etaSeconds = avgSpeed > 0 ? remaining / avgSpeed : 0;
      }
      
      console.debug(`[Rule Upload] ${percentCompleted}% completed${etaSeconds > 0 ? `, ETA: ${Math.round(etaSeconds)}s` : ''}${avgSpeed > 0 ? `, Speed: ${(avgSpeed / 1024 / 1024).toFixed(2)}MB/s` : ''}`);
      
      if (onProgress) {
        onProgress(percentCompleted, etaSeconds, avgSpeed);
      }
      
      lastTime = now;
      lastLoaded = loaded;
    },
    timeout: 86400000 // 24 hours timeout for extremely large files
  });
};

// Update rule metadata
export const updateRule = (id: string, data: Partial<Rule>) => 
  api.put<Rule>(`/api/rules/${id}`, data, {
    withCredentials: true // Ensure cookies are sent with the request
  });

// Get deletion impact for a rule
export const getRuleDeletionImpact = (id: string) =>
  api.get<DeletionImpact>(`/api/rules/${id}/deletion-impact`, { withCredentials: true });

// Delete a rule (with optional confirmation for cascade delete)
export const deleteRule = (id: string, confirmId?: number) =>
  api.delete(`/api/rules/${id}`, {
    withCredentials: true,
    data: confirmId !== undefined ? { confirm_id: confirmId } : undefined
  });

// Enable/disable a rule
export const toggleRuleStatus = (id: string, isEnabled: boolean) => 
  api.put<Rule>(`/api/rules/${id}/status`, { is_enabled: isEnabled });

// Download a rule
export const downloadRule = (id: string) => 
  api.get(`/api/rules/${id}/download`, { responseType: 'blob' });

// Verify a rule
export const verifyRule = (id: string, status: 'verified' | 'failed' | 'pending', ruleCount?: number) => 
  api.post(`/api/rules/${id}/verify`, { 
    status, 
    rule_count: ruleCount 
  }, { withCredentials: true }); 