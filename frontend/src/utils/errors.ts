import axios from 'axios';

/**
 * Turn anything thrown by an API call (axios error, Error, string, backend
 * `{error}` / `{message}` body) into a user-facing string. Never throws.
 */
export const getErrorMessage = (err: unknown, fallback = 'Something went wrong'): string => {
  if (err == null) return fallback;
  if (typeof err === 'string') return err;
  if (axios.isAxiosError(err)) {
    const data = err.response?.data as any;
    if (data) {
      if (typeof data === 'string' && data.trim()) return data.trim();
      if (typeof data.error === 'string' && data.error) return data.error;
      if (typeof data.message === 'string' && data.message) return data.message;
      if (data.error && typeof data.error.message === 'string') return data.error.message;
    }
    if (err.code === 'ERR_NETWORK') return 'Network error. Please check your connection.';
    if (err.response?.status === 401) return 'Your session has expired. Please log in again.';
    if (err.response?.status === 403) return 'Permission denied';
    if (err.response?.status === 404) return 'Not found';
    return err.message || fallback;
  }
  if (err instanceof Error) return err.message || fallback;
  if (typeof err === 'object' && 'message' in (err as any) && typeof (err as any).message === 'string') {
    return (err as any).message;
  }
  return fallback;
};

export const getErrorStatus = (err: unknown): number | undefined =>
  axios.isAxiosError(err) ? err.response?.status : undefined;

export const isNotFound = (err: unknown): boolean => getErrorStatus(err) === 404;
