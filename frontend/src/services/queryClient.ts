import { QueryClient } from '@tanstack/react-query';
import { getErrorStatus } from '../utils/errors';

/**
 * Shared React Query client.
 *
 * - `staleTime` 10s: list pages re-mounting within that window render cached
 *   data instantly and refetch in the background.
 * - Retries skip 4xx: a 403/404 will not become a 3x slower 403/404.
 * - `refetchIntervalInBackground: false`: polling pauses in hidden tabs.
 */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: true,
      refetchIntervalInBackground: false,
      retry: (failureCount, error) => {
        const status = getErrorStatus(error);
        if (status !== undefined && status >= 400 && status < 500) return false;
        return failureCount < 2;
      },
    },
    mutations: {
      retry: false,
    },
  },
});

export default queryClient;
