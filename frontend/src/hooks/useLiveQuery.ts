import { QueryKey, UseQueryOptions, UseQueryResult, useQuery } from '@tanstack/react-query';
import { usePolling } from '../contexts/PollingContext';

/**
 * Polling tiers. Pick by how fast the underlying state changes, not by page:
 *  - live:  an active job's progress (2s)
 *  - fast:  dashboard tiles, agent activity (5s)
 *  - list:  list pages (10s)
 *  - admin: admin status panels (30s)
 *  - slow:  budgets, blocklists (60s)
 */
export const POLL = { live: 2_000, fast: 5_000, list: 10_000, admin: 30_000, slow: 60_000 } as const;
export type PollTier = keyof typeof POLL;

export interface LiveOptions<TData> {
  tier: PollTier;
  /** Hard off switch for this query (e.g. while an inline editor is open). */
  enabled?: boolean;
  /** Keep polling only while this returns true for the latest data (e.g. job still running). */
  when?: (data: TData | undefined) => boolean;
}

/**
 * `useQuery` plus a polling policy that respects the session auto-refresh
 * toggle, the page visibility (via the client default) and an optional
 * data-driven predicate.
 */
export function useLiveQuery<
  TQueryFnData = unknown,
  TError = unknown,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey
>(
  options: UseQueryOptions<TQueryFnData, TError, TData, TQueryKey>,
  live: LiveOptions<TData>
): UseQueryResult<TData, TError> {
  const { enabled: pollingEnabled } = usePolling();
  const liveEnabled = live.enabled ?? true;

  return useQuery<TQueryFnData, TError, TData, TQueryKey>({
    ...options,
    refetchInterval: (query) => {
      if (!pollingEnabled || !liveEnabled) return false;
      if (live.when && !live.when(query.state.data as TData | undefined)) return false;
      return POLL[live.tier];
    },
  });
}

export default useLiveQuery;
