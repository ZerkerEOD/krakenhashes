/**
 * MUI module augmentation for the KrakenHashes theme.
 *
 * Imported once from `theme.ts` so the extra palette entries and the mono font
 * family are typed everywhere `useTheme()` / `sx` is used.
 */
import type { PaletteColor, PaletteColorOptions } from '@mui/material/styles';
import '@mui/material/Chip';
import '@mui/material/Badge';
import '@mui/material/LinearProgress';
import '@mui/material/CircularProgress';
import '@mui/material/Button';
import '@mui/material/IconButton';

declare module '@mui/material/styles' {
  interface PaletteSurface {
    /** Code blocks, table headers, inputs: visually "below" paper. */
    sunken: string;
    /** Menus, popovers, drawers: visually "above" paper. */
    raised: string;
    hover: string;
    selected: string;
  }

  interface Palette {
    running: PaletteColor;
    queued: PaletteColor;
    idle: PaletteColor;
    surface: PaletteSurface;
    chart: string[];
  }

  interface PaletteOptions {
    running?: PaletteColorOptions;
    queued?: PaletteColorOptions;
    idle?: PaletteColorOptions;
    surface?: Partial<PaletteSurface>;
    chart?: string[];
  }

  interface TypographyVariants {
    monoFamily: string;
  }

  interface TypographyVariantsOptions {
    monoFamily?: string;
  }
}

declare module '@mui/material/Chip' {
  interface ChipPropsColorOverrides {
    running: true;
    queued: true;
    idle: true;
  }
}

declare module '@mui/material/Badge' {
  interface BadgePropsColorOverrides {
    running: true;
    queued: true;
    idle: true;
  }
}

declare module '@mui/material/LinearProgress' {
  interface LinearProgressPropsColorOverrides {
    running: true;
    queued: true;
    idle: true;
  }
}

declare module '@mui/material/CircularProgress' {
  interface CircularProgressPropsColorOverrides {
    running: true;
    queued: true;
    idle: true;
  }
}

declare module '@mui/material/Button' {
  interface ButtonPropsColorOverrides {
    running: true;
    queued: true;
    idle: true;
  }
}

declare module '@mui/material/IconButton' {
  interface IconButtonPropsColorOverrides {
    running: true;
    queued: true;
    idle: true;
  }
}

export {};
