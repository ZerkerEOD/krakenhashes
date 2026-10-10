import i18n from '../i18n';
/**
 * Utility functions for formatting data
 */

/**
 * Format a file size in bytes to a human-readable string
 * @param bytes File size in bytes
 * @param decimals Number of decimal places to show
 * @returns Formatted string with appropriate unit (B, KB, MB, GB, TB)
 */
export const formatFileSize = (bytes: number, decimals: number = 2): string => {
  if (bytes === 0) return '0 Bytes';

  const k = 1024;
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
  
  // Determine the appropriate unit
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  
  // Format the number with the appropriate unit
  return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i];
};

/**
 * Format a hash rate in hashes per second to a human-readable string
 * @param hashesPerSecond Hash rate in H/s
 * @param decimals Number of decimal places to show
 * @returns Formatted string with appropriate unit (H/s, KH/s, MH/s, GH/s, TH/s)
 */
export const formatHashRate = (hashesPerSecond: number, decimals: number = 1): string => {
  if (hashesPerSecond === 0) return '0 H/s';

  const k = 1000;
  const sizes = ['H/s', 'KH/s', 'MH/s', 'GH/s', 'TH/s'];
  
  // Determine the appropriate unit
  const i = Math.floor(Math.log(hashesPerSecond) / Math.log(k));
  
  // Format the number with the appropriate unit
  return parseFloat((hashesPerSecond / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i];
};

/**
 * Format a duration in seconds to a human-readable string
 * @param seconds Duration in seconds
 * @returns Formatted string (e.g., "2h 30m", "45s", "1d 3h")
 */
export const formatDuration = (seconds: number): string => {
  if (seconds < 60) return `${seconds}s`;
  
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  
  if (days > 0) {
    const remainingHours = hours % 24;
    return remainingHours > 0 ? `${days}d ${remainingHours}h` : `${days}d`;
  }
  
  if (hours > 0) {
    const remainingMinutes = minutes % 60;
    return remainingMinutes > 0 ? `${hours}h ${remainingMinutes}m` : `${hours}h`;
  }
  
  return `${minutes}m`;
};

/**
 * Format a percentage with appropriate precision
 * @param value Percentage value (0-100)
 * @param decimals Number of decimal places to show
 * @returns Formatted percentage string
 */
export const formatPercentage = (value: number, decimals: number = 1): string => {
  return `${value.toFixed(decimals)}%`;
};

/**
 * Format an attack mode number or string to a human-readable label
 * @param mode Attack mode value (number or string)
 * @returns Human-readable attack mode label
 */
export const formatAttackMode = (mode: number | string): string => {
  const modeNum = typeof mode === 'string' ? parseInt(mode, 10) : mode;
  switch (modeNum) {
    case 0: return 'Straight';
    case 1: return 'Combination';
    case 3: return 'Brute-Force';
    case 6: return 'Hybrid (Wordlist + Mask)';
    case 7: return 'Hybrid (Mask + Wordlist)';
    case 9: return 'Association';
    default: return `Unknown (${mode})`;
  }
};

// Export all formatters as a single object for easier importing
/** "yyyy-MM-dd HH:mm" in local time; "-" for empty or unparseable input. */
export const formatDateTime = (value?: string | number | Date | null): string => {
  if (value === null || value === undefined || value === '') return '-';
  const d = value instanceof Date ? value : new Date(value);
  if (isNaN(d.getTime())) return '-';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
};

/** Compact relative time ("3 min. ago") in the UI language, falling back to the date after 30 days. */
export const formatRelativeTime = (value?: string | number | Date | null): string => {
  if (value === null || value === undefined || value === '') return '-';
  const d = value instanceof Date ? value : new Date(value);
  const ms = Date.now() - d.getTime();
  if (isNaN(ms)) return '-';
  let rtf: Intl.RelativeTimeFormat;
  try {
    rtf = new Intl.RelativeTimeFormat(i18n.language || 'en', { numeric: 'auto', style: 'short' });
  } catch {
    rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto', style: 'short' });
  }
  const s = Math.round(ms / 1000);
  if (s < 45) return rtf.format(0, 'second');
  const m = Math.round(s / 60);
  if (m < 60) return rtf.format(-m, 'minute');
  const h = Math.round(m / 60);
  if (h < 24) return rtf.format(-h, 'hour');
  const days = Math.round(h / 24);
  if (days <= 30) return rtf.format(-days, 'day');
  return formatDateTime(d);
};

export const formatters = {
  formatFileSize,
  formatHashRate,
  formatDuration,
  formatPercentage,
  formatAttackMode,
}; 