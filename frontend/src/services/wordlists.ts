/**
 * API services for wordlist management
 */
import { api } from './api';
import { Wordlist, WordlistFilters, WordlistUploadResponse, DeletionImpact } from '../types/wordlists';

// Get all wordlists with optional filtering
export const getWordlists = (filters?: WordlistFilters) => 
  api.get<Wordlist[]>('/api/wordlists', { params: filters });

// Get a single wordlist by ID
export const getWordlist = (id: string) => 
  api.get<Wordlist>(`/api/wordlists/${id}`);

// Upload a new wordlist
export const uploadWordlist = (formData: FormData, onProgress?: (progress: number, eta?: number, speed?: number) => void) => {
  let startTime = Date.now();
  let lastTime = startTime;
  let lastLoaded = 0;
  
  return api.post<WordlistUploadResponse>('/api/wordlists/upload', formData, {
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
      
      console.debug(`[Wordlist Upload] ${percentCompleted}% completed${etaSeconds > 0 ? `, ETA: ${Math.round(etaSeconds)}s` : ''}${avgSpeed > 0 ? `, Speed: ${(avgSpeed / 1024 / 1024).toFixed(2)}MB/s` : ''}`);
      
      if (onProgress) {
        onProgress(percentCompleted, etaSeconds, avgSpeed);
      }
      
      lastTime = now;
      lastLoaded = loaded;
    },
    timeout: 86400000 // 24 hours timeout for extremely large files
  });
};

// Update wordlist metadata
export const updateWordlist = (id: string, data: Partial<Wordlist>) => 
  api.put<Wordlist>(`/api/wordlists/${id}`, data, {
    withCredentials: true // Ensure cookies are sent with the request
  });

// Get deletion impact for a wordlist
export const getWordlistDeletionImpact = (id: string) =>
  api.get<DeletionImpact>(`/api/wordlists/${id}/deletion-impact`, { withCredentials: true });

// Delete a wordlist (with optional confirmation for cascade delete)
export const deleteWordlist = (id: string, confirmId?: number) =>
  api.delete(`/api/wordlists/${id}`, {
    withCredentials: true,
    data: confirmId !== undefined ? { confirm_id: confirmId } : undefined
  });

// Verify a wordlist
export const verifyWordlist = (id: string, status: 'verified' | 'failed' | 'pending', wordCount?: number) => 
  api.post(`/api/wordlists/${id}/verify`, { 
    status, 
    word_count: wordCount 
  });

// Enable/disable a wordlist
export const toggleWordlistStatus = (id: string, isEnabled: boolean) => 
  api.put<Wordlist>(`/api/wordlists/${id}/status`, { is_enabled: isEnabled });

// Download a wordlist
export const downloadWordlist = (id: string) =>
  api.get(`/api/wordlists/${id}/download`, { responseType: 'blob' });

// Refresh wordlist metadata (MD5, word count, file size)
export const refreshWordlist = (id: string) =>
  api.post(`/api/wordlists/${id}/refresh`, {}, { withCredentials: true }); 