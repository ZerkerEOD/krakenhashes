/**
 * Utility functions for calculating job progress with enhanced chunking support
 */

import { JobSummary, JobDetail } from '../types/jobs';

export interface ProgressInfo {
  percentage: number;
  processed: number;
  total: number;
  displayText: string;
  hasMultiplier: boolean;
  multiplierText?: string;
}

/**
 * Format keyspace numbers for display
 * @param keyspace The keyspace value to format
 * @returns Formatted string (e.g., "1.2M" instead of "1200000")
 */
export const formatKeyspace = (keyspace: number | string | null | undefined): string => {
  // Effective keyspace fields arrive as decimal STRINGS from the backend (NUMERIC,
  // can exceed 2^53). Coerce to Number for the abbreviated K/M/B/T display — only
  // ~3 significant figures are shown, so float precision is irrelevant here.
  const n = typeof keyspace === 'string' ? Number(keyspace) : (keyspace ?? 0);
  if (!n || !isFinite(n) || n <= 0) return '0';

  const units = ['', 'K', 'M', 'B', 'T', 'Q', 'Qi', 'S'];
  const k = 1000;

  // Find the appropriate unit
  const i = Math.floor(Math.log(n) / Math.log(k));

  if (i === 0) {
    return n.toString();
  }

  // Cap at the highest unit we support
  const unitIndex = Math.min(i, units.length - 1);

  // Format with appropriate precision
  const value = n / Math.pow(k, unitIndex);
  const precision = value < 10 ? 2 : value < 100 ? 1 : 0;

  return `${value.toFixed(precision)}${units[unitIndex]}`;
};

/**
 * Calculate job progress accounting for effective keyspace
 * @param job The job to calculate progress for
 * @returns Progress information including percentage and display text
 */
export const calculateJobProgress = (job: JobSummary | JobDetail): ProgressInfo => {
  // Get the effective keyspace for calculations. These arrive as decimal strings
  // (NUMERIC) from the backend; coerce to Number for display/comparison.
  const total = Number(job.effective_keyspace || 0);
  const processed = Number(job.processed_keyspace || 0);
  const dispatched = Number(job.dispatched_keyspace || 0);
  
  // Use backend-calculated overall progress if available
  const percentage = job.overall_progress_percent || 0;
  
  // Check if we have multiplication factor
  const hasMultiplier = job.multiplication_factor !== undefined && 
                       job.multiplication_factor !== null && 
                       job.multiplication_factor > 1;
  
  // Build display text based on whether we have keyspace info
  let displayText = '';
  let multiplierText: string | undefined;
  
  if (dispatched > 0) {
    // Show searched / dispatched (not total)
    displayText = `${formatKeyspace(processed)} / ${formatKeyspace(dispatched)}`;
  } else if (total > 0) {
    // Fallback if no dispatched keyspace
    displayText = `${formatKeyspace(processed)} / ${formatKeyspace(total)}`;
  } else {
    // No keyspace info available
    displayText = 'No keyspace data';
  }
  
  if (hasMultiplier) {
    multiplierText = `×${job.multiplication_factor}`;
    // Don't add multiplier to display text - it will be shown in keyspace column
  }
  
  return {
    percentage: Math.round(percentage * 10) / 10, // Round to 1 decimal place
    processed,
    total,
    displayText,
    hasMultiplier,
    multiplierText,
  };
};

/**
 * Get tooltip text explaining the effective keyspace
 * @param job The job to get tooltip for
 * @returns Tooltip text or undefined if not applicable
 */
export const getKeyspaceTooltip = (job: JobSummary | JobDetail): string | undefined => {
  if (!job.effective_keyspace || Number(job.effective_keyspace) === 0) {
    return undefined;
  }
  
  const parts: string[] = [];
  
  if (job.base_keyspace) {
    parts.push(`Base keyspace: ${formatKeyspace(job.base_keyspace)}`);
  }
  
  if (job.multiplication_factor && job.multiplication_factor > 1) {
    parts.push(`Multiplication factor: ×${job.multiplication_factor}`);
  }

  if (parts.length === 0) {
    return undefined;
  }
  
  return parts.join('\n');
};

/**
 * Calculate progress percentage from dispatched and searched percentages
 * This is used when keyspace information is not available
 * @param dispatchedPercent The dispatched percentage
 * @param searchedPercent The searched percentage
 * @returns Combined progress percentage
 */
export const calculateLegacyProgress = (dispatchedPercent: number, searchedPercent: number): number => {
  // Use searched percentage as the primary indicator
  // Fall back to dispatched percentage if searched is 0
  return searchedPercent > 0 ? searchedPercent : dispatchedPercent;
};

// Export all utilities as a single object for convenience
export const jobProgressUtils = {
  calculateJobProgress,
  formatKeyspace,
  getKeyspaceTooltip,
  calculateLegacyProgress,
};