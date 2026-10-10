/**
 * ThemeModeContext: the user's light/dark/system preference.
 *
 * The preference is per browser (localStorage) and syncs across tabs through
 * the `storage` event. `system` follows `prefers-color-scheme` live. The
 * resolved mode is also written onto `<html>` (`data-mode`, `color-scheme`)
 * and the `theme-color` meta so the browser chrome follows.
 *
 * `index.html` carries a tiny pre-paint script that reads the same key so the
 * first paint already has the right background.
 */
import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import useMediaQuery from '@mui/material/useMediaQuery';
import { STORAGE_KEY_THEME_MODE, ThemeMode, ThemePreference, surfaces } from '../styles/tokens';

interface ThemeModeContextValue {
  mode: ThemeMode;
  preference: ThemePreference;
  setPreference: (next: ThemePreference) => void;
  toggle: () => void;
}

const ThemeModeContext = createContext<ThemeModeContextValue | undefined>(undefined);

const isPreference = (v: unknown): v is ThemePreference => v === 'light' || v === 'dark' || v === 'system';

const readStored = (): ThemePreference => {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY_THEME_MODE);
    return isPreference(raw) ? raw : 'system';
  } catch {
    return 'system';
  }
};

const applyToDocument = (mode: ThemeMode) => {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  root.dataset.mode = mode;
  root.style.colorScheme = mode;
  root.style.backgroundColor = surfaces[mode].default;
  let meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (!meta) {
    meta = document.createElement('meta');
    meta.name = 'theme-color';
    document.head.appendChild(meta);
  }
  meta.content = surfaces[mode].paper;
};

export const ThemeModeProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [preference, setPreferenceState] = useState<ThemePreference>(readStored);
  const systemDark = useMediaQuery('(prefers-color-scheme: dark)', { noSsr: true });
  const mode: ThemeMode = preference === 'system' ? (systemDark ? 'dark' : 'light') : preference;

  const setPreference = useCallback((next: ThemePreference) => {
    setPreferenceState(next);
    try {
      window.localStorage.setItem(STORAGE_KEY_THEME_MODE, next);
    } catch {
      /* private mode / blocked storage: in-memory only */
    }
  }, []);

  const toggle = useCallback(() => setPreference(mode === 'dark' ? 'light' : 'dark'), [mode, setPreference]);

  useEffect(() => applyToDocument(mode), [mode]);

  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY_THEME_MODE) setPreferenceState(isPreference(e.newValue) ? e.newValue : 'system');
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  const value = useMemo(() => ({ mode, preference, setPreference, toggle }), [mode, preference, setPreference, toggle]);

  return <ThemeModeContext.Provider value={value}>{children}</ThemeModeContext.Provider>;
};

export const useThemeMode = (): ThemeModeContextValue => {
  const ctx = useContext(ThemeModeContext);
  if (!ctx) {
    return { mode: 'dark', preference: 'dark', setPreference: () => undefined, toggle: () => undefined };
  }
  return ctx;
};

export default ThemeModeContext;
