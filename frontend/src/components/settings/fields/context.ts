import React from 'react';

/**
 * Shared state for autosaving settings fields.
 *
 * A field is bound to ONE key. `values` is server state (strings, exactly as
 * the API returned them); a field keeps its in-progress edit in local draft
 * state and only writes to `values` when it saves, so nothing can be shown
 * that was not persisted. `saveOne` writes only that key.
 *
 * See `useSystemSettingsForm` (system_settings keys) and `useGroupSettings`
 * (typed group endpoints) for the two providers of this context.
 */

export type SettingsMap = Record<string, string>;

export type SaveState = 'idle' | 'saving' | 'saved' | 'error';

export interface FieldSaveStatus {
  state: SaveState;
  error?: string;
}

export interface SettingsCtxValue {
  values: SettingsMap;
  setValues: React.Dispatch<React.SetStateAction<SettingsMap>>;
  /** Persist one key. Resolves when the server answered (success or failure); never throws. */
  saveOne: (key: string, value: string, previous: string) => Promise<void>;
  loading: boolean;
  saveStates: Record<string, FieldSaveStatus>;
  /** Key currently being saved, if any (kept for older callers). */
  savingKey: string | null;
  /** When false (e.g. a dependent toggle is off) fields render disabled. */
  enabled?: boolean;
}

export const SettingsCtx = React.createContext<SettingsCtxValue | null>(null);

export const useSettingsCtx = (): SettingsCtxValue => {
  const ctx = React.useContext(SettingsCtx);
  if (!ctx) throw new Error('setting field rendered outside a SettingsCtx provider');
  return ctx;
};

export const numberValueOf = (values: SettingsMap, key: string, fallback = 0): number => {
  const parsed = parseFloat(values[key] ?? '');
  return isNaN(parsed) ? fallback : parsed;
};

export const boolValueOf = (values: SettingsMap, key: string): boolean =>
  (values[key] ?? '').trim().toLowerCase() === 'true';

/** How long the green tick stays before the field returns to idle. */
export const SAVED_DECAY_MS = 2000;
