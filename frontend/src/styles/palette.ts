import { alpha, createTheme, darken, getContrastRatio, PaletteOptions } from '@mui/material/styles';
import {
  DEFAULT_PRIMARY,
  ThemeMode,
  chartSeries,
  status,
  surfaces,
  text,
} from './tokens';

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;

export const isHexColor = (value: string | null | undefined): value is string =>
  typeof value === 'string' && HEX_COLOR.test(value);

/**
 * Semantic tones a status can map to. `default` renders as a neutral chip.
 * Everything else is a real palette entry so chips, badges and progress bars
 * can take it as their `color` prop.
 */
export type StatusTone = 'success' | 'warning' | 'error' | 'info' | 'running' | 'queued' | 'idle' | 'default';

/**
 * In light mode a saturated admin primary (the default is pure red) can fall
 * under 4.5:1 as link/text colour on a white page. Darken it just enough for
 * `primary.main`; `light`/`dark` accents keep the admin's exact hue.
 */
const readablePrimary = (mode: ThemeMode, hex: string, background: string): string => {
  if (mode === 'dark') return hex;
  let candidate = hex;
  for (let i = 0; i < 6 && getContrastRatio(candidate, background) < 4.5; i += 1) {
    candidate = darken(candidate, 0.15);
  }
  return candidate;
};

export const buildPalette = (
  mode: ThemeMode,
  primary: string = DEFAULT_PRIMARY,
  secondary?: string | null
): PaletteOptions => {
  const surface = surfaces[mode];
  const primaryHex = isHexColor(primary) ? primary : DEFAULT_PRIMARY;
  const primaryMain = readablePrimary(mode, primaryHex, surface.default);

  // augmentColor needs a palette instance; a throwaway theme of the right mode
  // gives us correct contrastText / light / dark for the custom tones.
  const scratch = createTheme({ palette: { mode } });
  const tone = (name: string, main: string) => scratch.palette.augmentColor({ color: { main }, name });

  const isDark = mode === 'dark';
  const overlay = isDark ? '#ffffff' : '#000000';

  return {
    mode,
    primary: {
      main: primaryMain,
      ...(primaryMain !== primaryHex ? { light: primaryHex } : {}),
    },
    ...(secondary && isHexColor(secondary) ? { secondary: { main: secondary } } : {}),
    success: tone('success', status.success[mode]),
    warning: tone('warning', status.warning[mode]),
    error: tone('error', status.error[mode]),
    info: tone('info', status.info[mode]),
    running: tone('running', status.running[mode]),
    queued: tone('queued', status.queued[mode]),
    idle: tone('idle', status.idle[mode]),
    background: {
      default: surface.default,
      paper: surface.paper,
    },
    surface: {
      sunken: surface.sunken,
      raised: surface.raised,
      hover: alpha(overlay, isDark ? 0.06 : 0.04),
      selected: alpha(primaryMain, isDark ? 0.16 : 0.1),
    },
    divider: alpha(overlay, 0.12),
    text: text[mode],
    action: {
      hover: alpha(overlay, isDark ? 0.06 : 0.04),
      selected: alpha(primaryMain, isDark ? 0.16 : 0.1),
      disabledBackground: alpha(overlay, 0.08),
      focus: alpha(overlay, 0.12),
    },
    chart: chartSeries[mode],
  };
};
