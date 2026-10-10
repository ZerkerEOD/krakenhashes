import { TypographyOptions } from '@mui/material/styles/createTypography';
import { fontFamily, monoFamily } from './tokens';

/**
 * Compact product scale. `h4` is the page title (rendered by `PageHeader` as
 * `component="h1"`), `h6` is the card/section title. Body text is 14px, which
 * matches table density.
 */
export const typography: TypographyOptions = {
  fontFamily,
  monoFamily,
  fontSize: 14,
  h1: { fontSize: '1.75rem', fontWeight: 600, lineHeight: 1.25, letterSpacing: '-0.01em' },
  h2: { fontSize: '1.5rem', fontWeight: 600, lineHeight: 1.3, letterSpacing: '-0.01em' },
  h3: { fontSize: '1.25rem', fontWeight: 600, lineHeight: 1.3 },
  h4: { fontSize: '1.125rem', fontWeight: 600, lineHeight: 1.35 },
  h5: { fontSize: '1rem', fontWeight: 600, lineHeight: 1.4 },
  h6: { fontSize: '0.875rem', fontWeight: 600, lineHeight: 1.45 },
  subtitle1: { fontSize: '0.875rem', fontWeight: 500, lineHeight: 1.5 },
  subtitle2: { fontSize: '0.8125rem', fontWeight: 500, lineHeight: 1.5 },
  body1: { fontSize: '0.875rem', lineHeight: 1.55 },
  body2: { fontSize: '0.8125rem', lineHeight: 1.5 },
  caption: { fontSize: '0.75rem', lineHeight: 1.45 },
  overline: { fontSize: '0.6875rem', fontWeight: 600, letterSpacing: '0.06em', lineHeight: 1.6 },
  button: { fontWeight: 600, textTransform: 'none', letterSpacing: 0 },
};
