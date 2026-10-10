/**
 * Design tokens: the mode-independent primitives every other style file builds
 * on. Nothing in here references MUI; `palette.ts` turns these into a MUI
 * palette per mode and `components.ts` reads them for radii and motion.
 *
 * Status hues are chosen so that the same semantic (success/warning/error/
 * info/running/queued/idle) stays recognisable in both modes: the dark values
 * are lighter and less saturated, the light values darker, so each clears
 * 4.5:1 against its mode's `background.default`.
 */

export type ThemeMode = 'light' | 'dark';
export type ThemePreference = ThemeMode | 'system';

export const STORAGE_KEY_THEME_MODE = 'kh.theme-mode';

/** Admin branding defaults (issue #41). */
export const DEFAULT_PRIMARY = '#ff0000';
export const DEFAULT_SECONDARY = '#9c27b0';

export const neutral = {
  0: '#ffffff',
  50: '#f5f6f8',
  100: '#eceef2',
  200: '#d9dde4',
  300: '#b9c0cb',
  400: '#8f99a8',
  500: '#7d8590',
  600: '#57606a',
  700: '#3a414b',
  800: '#262b34',
  850: '#1e222b',
  900: '#171a21',
  950: '#0f1115',
  1000: '#0a0c10',
} as const;

interface ModePair {
  dark: string;
  light: string;
}

export const status: Record<
  'success' | 'warning' | 'error' | 'info' | 'running' | 'queued' | 'idle',
  ModePair
> = {
  success: { dark: '#3fb950', light: '#1f883d' },
  warning: { dark: '#d29922', light: '#9a6700' },
  error: { dark: '#f85149', light: '#cf222e' },
  info: { dark: '#58a6ff', light: '#0969da' },
  running: { dark: '#2f9bff', light: '#0b6bcb' },
  queued: { dark: '#c69026', light: '#7d5a00' },
  idle: { dark: '#7d8590', light: '#57606a' },
};

export const surfaces: Record<ThemeMode, { default: string; paper: string; sunken: string; raised: string }> = {
  dark: { default: neutral[950], paper: neutral[900], sunken: neutral[1000], raised: neutral[850] },
  light: { default: neutral[50], paper: neutral[0], sunken: neutral[100], raised: neutral[0] },
};

export const text: Record<ThemeMode, { primary: string; secondary: string; disabled: string }> = {
  dark: { primary: '#e6e8ee', secondary: 'rgba(255,255,255,0.68)', disabled: 'rgba(255,255,255,0.38)' },
  light: { primary: '#15181e', secondary: 'rgba(0,0,0,0.62)', disabled: 'rgba(0,0,0,0.38)' },
};

/** Categorical chart series, 8 per mode, ordered for maximum adjacent contrast. */
export const chartSeries: Record<ThemeMode, string[]> = {
  dark: ['#58a6ff', '#3fb950', '#d29922', '#f778ba', '#a371f7', '#f85149', '#39c5cf', '#e3b341'],
  light: ['#0969da', '#1a7f37', '#9a6700', '#bf3989', '#8250df', '#cf222e', '#1b7c83', '#7d4e00'],
};

export const radius = { xs: 4, sm: 6, md: 8, lg: 12 } as const;

export const spacingUnit = 8;

export const fontFamily =
  '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Inter, "Helvetica Neue", Arial, sans-serif';
export const monoFamily =
  '"JetBrains Mono", "SFMono-Regular", Menlo, Consolas, "Liberation Mono", monospace';

export const motion = {
  duration: { fast: 120, base: 200, slow: 320 },
  easing: {
    standard: 'cubic-bezier(0.4, 0, 0.2, 1)',
    decelerate: 'cubic-bezier(0, 0, 0.2, 1)',
    accelerate: 'cubic-bezier(0.4, 0, 1, 1)',
  },
} as const;
