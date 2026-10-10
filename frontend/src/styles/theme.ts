/**
 * Application theme.
 *
 * `buildTheme(mode, primary, secondary)` is the single entry point. The mode
 * comes from `ThemeModeContext` (user preference, defaults to the OS), the two
 * accent colours from the admin branding (issue #41). Everything else lives in
 * `tokens.ts` / `palette.ts` / `typography.ts` / `components.ts`.
 */
import { createTheme, Theme } from '@mui/material/styles';
import './augmentation';
import { buildComponents } from './components';
import { buildPalette, isHexColor } from './palette';
import { typography } from './typography';
import { DEFAULT_PRIMARY, ThemeMode, motion, radius, spacingUnit } from './tokens';

export { DEFAULT_PRIMARY, isHexColor };
export type { ThemeMode };
export type { StatusTone } from './palette';

export const buildTheme = (
  mode: ThemeMode = 'dark',
  primary: string = DEFAULT_PRIMARY,
  secondary?: string | null,
  /** MUI core component text for the UI language (`@mui/material/locale`). */
  locale?: object
): Theme => {
  const base = createTheme({
    palette: buildPalette(mode, primary, secondary),
    typography,
    shape: { borderRadius: radius.md },
    spacing: spacingUnit,
    transitions: {
      duration: {
        shortest: motion.duration.fast,
        shorter: motion.duration.fast + 30,
        short: motion.duration.base,
        standard: motion.duration.base + 50,
        complex: motion.duration.slow,
        enteringScreen: motion.duration.base,
        leavingScreen: motion.duration.fast + 60,
      },
    },
  });
  return createTheme(base, { components: buildComponents(base) }, locale ?? {});
};

export default buildTheme;
