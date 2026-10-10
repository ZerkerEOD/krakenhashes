import { useCallback, useEffect, useMemo, useState } from 'react';
import { QueryKey, useQuery, useQueryClient } from '@tanstack/react-query';
import { getErrorMessage } from '../../../utils/errors';
import { useToast } from '../../ui/toast';
import { SettingsCtxValue, SettingsMap } from './context';
import { useSaveStates } from './useSaveStates';

/**
 * A SettingsCtx over a typed settings object that lives behind its own
 * endpoint (auth, MFA, WebAuthn, global webhook, branding, ...).
 *
 * Fields bind with `settingKey="<property>"`. Values are serialised to
 * strings for the shared primitives (booleans → "true"/"false", arrays →
 * JSON) and parsed back using the type of the loaded property before
 * `saveField` is called with ONLY the changed property.
 *
 * After every successful save the group is refetched, so a concurrent edit by
 * another admin is picked up on the next interaction (last writer wins for the
 * single field in between — documented limitation, no ETags on these routes).
 */
export interface GroupSettingsOptions<T extends Record<string, unknown>> {
  queryKey: QueryKey;
  load: () => Promise<T>;
  /** Persist one property. `current` is the last loaded object (for merge-from-state endpoints). */
  saveField: (key: keyof T & string, value: T[keyof T], current: T) => Promise<unknown>;
  /** Skip the refetch after save (when the endpoint's GET is expensive). */
  refetchAfterSave?: boolean;
  enabled?: boolean;
}

export const serializeValue = (v: unknown): string => {
  if (v === null || v === undefined) return '';
  if (typeof v === 'boolean') return v ? 'true' : 'false';
  if (Array.isArray(v) || typeof v === 'object') return JSON.stringify(v);
  return String(v);
};

export const parseLike = (sample: unknown, text: string): unknown => {
  if (typeof sample === 'boolean') return text === 'true';
  if (typeof sample === 'number') {
    const n = Number(text);
    return isNaN(n) ? sample : n;
  }
  if (Array.isArray(sample) || (typeof sample === 'object' && sample !== null)) {
    try {
      return JSON.parse(text);
    } catch {
      return sample;
    }
  }
  if (sample === null || sample === undefined) {
    // Unknown type: best effort.
    if (text === 'true' || text === 'false') return text === 'true';
    if (text !== '' && !isNaN(Number(text))) return Number(text);
    try {
      if (text.startsWith('[') || text.startsWith('{')) return JSON.parse(text);
    } catch {
      /* fall through */
    }
    return text;
  }
  return text;
};

export function useGroupSettings<T extends Record<string, unknown>>(
  options: GroupSettingsOptions<T>
): SettingsCtxValue & { data: T | undefined; error: unknown; reload: () => Promise<unknown> } {
  const { queryKey, load, saveField, refetchAfterSave = true, enabled = true } = options;
  const toast = useToast();
  const queryClient = useQueryClient();
  const { saveStates, setState, savingKey } = useSaveStates();
  const [values, setValues] = useState<SettingsMap>({});

  const query = useQuery({ queryKey, queryFn: load, enabled, staleTime: 10_000 });

  // Server state → string map (only when the server data changes).
  useEffect(() => {
    if (!query.data) return;
    const map: SettingsMap = {};
    Object.entries(query.data).forEach(([k, v]) => {
      map[k] = serializeValue(v);
    });
    setValues(map);
  }, [query.data]);

  const saveOne = useCallback(
    async (key: string, value: string, previous: string) => {
      const current = query.data;
      if (!current) return;
      setState(key, { state: 'saving' });
      try {
        const parsed = parseLike((current as Record<string, unknown>)[key], value) as T[keyof T];
        await saveField(key as keyof T & string, parsed, current);
        setState(key, { state: 'saved' });
        if (refetchAfterSave) await queryClient.invalidateQueries({ queryKey });
        else queryClient.setQueryData(queryKey, { ...current, [key]: parsed });
      } catch (err) {
        const message = getErrorMessage(err);
        setValues((v) => ({ ...v, [key]: previous }));
        setState(key, { state: 'error', error: message });
        toast.error(message);
      }
    },
    [query.data, queryClient, queryKey, refetchAfterSave, saveField, setState, toast]
  );

  const reload = useCallback(() => queryClient.invalidateQueries({ queryKey }), [queryClient, queryKey]);

  return useMemo(
    () => ({
      values,
      setValues,
      saveOne,
      loading: query.isLoading,
      saveStates,
      savingKey,
      data: query.data,
      error: query.error,
      reload,
    }),
    [values, saveOne, query.isLoading, query.data, query.error, saveStates, savingKey, reload]
  );
}

export default useGroupSettings;
