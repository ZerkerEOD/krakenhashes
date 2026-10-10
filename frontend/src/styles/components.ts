import { alpha, Theme, ThemeOptions } from '@mui/material/styles';
import type {} from '@mui/x-data-grid/themeAugmentation';
import { radius } from './tokens';

/**
 * Component overrides. Every override is a callback that reads the live
 * palette, so the same file serves light and dark.
 *
 * Deliberate choices:
 *  - Paper/Card drop MUI's dark-mode elevation overlay (`backgroundImage`) so
 *    the surface tiers in `palette.ts` are the only thing that lifts a surface.
 *  - AppBar defaults to `color="default"`: with `color="primary"` the admin's
 *    brand colour (pure red by default) would become a solid header in light mode.
 *  - Table cells get a themed header/divider so the pages not yet migrated to
 *    `DataTable` still look like the rest of the app.
 */
export const buildComponents = (theme: Theme): ThemeOptions['components'] => {
  const { palette } = theme;
  const isDark = palette.mode === 'dark';

  return {
    MuiCssBaseline: {
      styleOverrides: {
        ':root': {
          colorScheme: palette.mode,
          '--kh-bg': palette.background.default,
          '--kh-paper': palette.background.paper,
          '--kh-surface-sunken': palette.surface.sunken,
          '--kh-surface-raised': palette.surface.raised,
          '--kh-divider': palette.divider,
          '--kh-text-primary': palette.text.primary,
          '--kh-text-secondary': palette.text.secondary,
          ...Object.fromEntries(palette.chart.map((c, i) => [`--kh-chart-${i}`, c])),
        },
        body: {
          backgroundColor: palette.background.default,
          color: palette.text.primary,
          scrollbarColor: `${alpha(palette.text.primary, 0.25)} transparent`,
        },
        '*::-webkit-scrollbar': { width: 10, height: 10 },
        '*::-webkit-scrollbar-thumb': {
          backgroundColor: alpha(palette.text.primary, 0.2),
          borderRadius: 8,
          border: '2px solid transparent',
          backgroundClip: 'content-box',
        },
        '*::-webkit-scrollbar-thumb:hover': { backgroundColor: alpha(palette.text.primary, 0.35) },
        '@media (prefers-reduced-motion: reduce)': {
          '*, *::before, *::after': {
            animationDuration: '0.01ms !important',
            animationIterationCount: '1 !important',
            transitionDuration: '0.01ms !important',
            scrollBehavior: 'auto !important',
          },
        },
      },
    },

    MuiPaper: {
      defaultProps: { elevation: 0 },
      styleOverrides: {
        root: {
          backgroundImage: 'none',
          borderRadius: radius.md,
        },
        outlined: { borderColor: palette.divider },
        elevation1: { boxShadow: isDark ? '0 1px 2px rgba(0,0,0,0.5)' : '0 1px 2px rgba(16,24,40,0.08)' },
        elevation8: {
          backgroundColor: palette.surface.raised,
          border: `1px solid ${palette.divider}`,
          boxShadow: isDark ? '0 8px 24px rgba(0,0,0,0.55)' : '0 8px 24px rgba(16,24,40,0.14)',
        },
      },
    },
    MuiCard: {
      defaultProps: { elevation: 0, variant: 'outlined' },
      styleOverrides: { root: { borderRadius: radius.md } },
    },
    MuiCardHeader: {
      styleOverrides: {
        root: { padding: theme.spacing(2, 2.5, 1) },
        title: { fontSize: theme.typography.h6.fontSize, fontWeight: 600 },
        subheader: { fontSize: theme.typography.caption.fontSize },
      },
    },
    MuiCardContent: {
      styleOverrides: { root: { padding: theme.spacing(2, 2.5), '&:last-child': { paddingBottom: theme.spacing(2.5) } } },
    },

    MuiAppBar: {
      defaultProps: { color: 'default', elevation: 0 },
      styleOverrides: {
        root: {
          backgroundColor: palette.background.paper,
          color: palette.text.primary,
          borderBottom: `1px solid ${palette.divider}`,
          backgroundImage: 'none',
        },
      },
    },
    MuiDrawer: {
      styleOverrides: {
        paper: {
          backgroundColor: palette.background.paper,
          backgroundImage: 'none',
          borderRight: `1px solid ${palette.divider}`,
        },
      },
    },
    MuiToolbar: {
      styleOverrides: { root: { minHeight: 56, '@media (min-width:600px)': { minHeight: 56 } } },
    },

    MuiButton: {
      defaultProps: { disableElevation: true },
      styleOverrides: {
        root: { borderRadius: radius.sm, textTransform: 'none', fontWeight: 600 },
        sizeSmall: { minHeight: 32, padding: theme.spacing(0.5, 1.5) },
        sizeMedium: { minHeight: 36 },
        outlined: { borderColor: alpha(palette.text.primary, 0.23) },
      },
    },
    MuiIconButton: {
      styleOverrides: { root: { borderRadius: radius.sm } },
    },
    MuiToggleButton: {
      styleOverrides: { root: { textTransform: 'none', borderRadius: radius.sm } },
    },

    MuiChip: {
      styleOverrides: {
        root: { fontWeight: 500, borderRadius: radius.sm },
        sizeSmall: { height: 22, fontSize: '0.75rem' },
        outlined: { borderColor: alpha(palette.text.primary, 0.23) },
        label: { paddingLeft: 10, paddingRight: 10 },
      },
    },

    MuiTableContainer: {
      styleOverrides: { root: { borderRadius: radius.md } },
    },
    MuiTableHead: {
      styleOverrides: { root: { backgroundColor: palette.surface.sunken } },
    },
    MuiTableCell: {
      styleOverrides: {
        root: { borderBottom: `1px solid ${palette.divider}` },
        head: {
          fontWeight: 600,
          fontSize: '0.75rem',
          color: palette.text.secondary,
          backgroundColor: palette.surface.sunken,
          whiteSpace: 'nowrap',
        },
        sizeSmall: { padding: theme.spacing(0.75, 1.5) },
      },
    },
    MuiTableRow: {
      styleOverrides: {
        root: {
          '&:hover': { backgroundColor: palette.surface.hover },
          '&.Mui-selected, &.Mui-selected:hover': { backgroundColor: palette.surface.selected },
          '&:last-child td': { borderBottom: 0 },
        },
      },
    },
    MuiTablePagination: {
      styleOverrides: { root: { borderTop: `1px solid ${palette.divider}` } },
    },

    MuiTabs: {
      styleOverrides: {
        root: { minHeight: 40 },
        indicator: { height: 2 },
      },
    },
    MuiTab: {
      styleOverrides: {
        root: { textTransform: 'none', minHeight: 40, fontWeight: 500, padding: theme.spacing(1, 2) },
      },
    },

    MuiDialog: {
      styleOverrides: { paper: { borderRadius: radius.lg, border: `1px solid ${palette.divider}` } },
    },
    MuiDialogTitle: {
      styleOverrides: {
        root: { fontSize: '1rem', fontWeight: 600, padding: theme.spacing(2, 3), borderBottom: `1px solid ${palette.divider}` },
      },
    },
    MuiDialogContent: {
      styleOverrides: { root: { padding: theme.spacing(2.5, 3), '&.MuiDialogContent-dividers': { borderColor: palette.divider } } },
    },
    MuiDialogActions: {
      styleOverrides: { root: { padding: theme.spacing(1.5, 3, 2), borderTop: `1px solid ${palette.divider}` } },
    },

    MuiTooltip: {
      styleOverrides: {
        tooltip: {
          backgroundColor: palette.surface.raised,
          color: palette.text.primary,
          border: `1px solid ${palette.divider}`,
          fontSize: '0.75rem',
          boxShadow: isDark ? '0 4px 12px rgba(0,0,0,0.5)' : '0 4px 12px rgba(16,24,40,0.12)',
        },
        arrow: { color: palette.surface.raised, '&::before': { border: `1px solid ${palette.divider}` } },
      },
    },
    MuiMenu: {
      defaultProps: { elevation: 8 },
      styleOverrides: { paper: { borderRadius: radius.md }, list: { padding: theme.spacing(0.5) } },
    },
    MuiMenuItem: {
      styleOverrides: { root: { borderRadius: radius.sm, margin: theme.spacing(0, 0.5), minHeight: 36 } },
    },
    MuiPopover: {
      defaultProps: { elevation: 8 },
    },

    MuiTextField: {
      defaultProps: { size: 'small' },
    },
    MuiFormControl: {
      defaultProps: { size: 'small' },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: {
          borderRadius: radius.sm,
          backgroundColor: palette.background.paper,
          '& .MuiOutlinedInput-notchedOutline': { borderColor: alpha(palette.text.primary, 0.2) },
          '&:hover .MuiOutlinedInput-notchedOutline': { borderColor: alpha(palette.text.primary, 0.45) },
        },
      },
    },
    MuiInputLabel: {
      styleOverrides: { root: { fontSize: '0.875rem' } },
    },

    MuiAlert: {
      styleOverrides: {
        root: { borderRadius: radius.md },
        standardSuccess: { backgroundColor: alpha(palette.success.main, 0.12), color: palette.text.primary },
        standardWarning: { backgroundColor: alpha(palette.warning.main, 0.12), color: palette.text.primary },
        standardError: { backgroundColor: alpha(palette.error.main, 0.12), color: palette.text.primary },
        standardInfo: { backgroundColor: alpha(palette.info.main, 0.12), color: palette.text.primary },
      },
    },

    MuiLinearProgress: {
      styleOverrides: {
        root: { borderRadius: 4, backgroundColor: palette.surface.sunken },
        bar: { borderRadius: 4 },
      },
    },

    MuiListSubheader: {
      styleOverrides: {
        root: {
          backgroundColor: 'transparent',
          color: palette.text.secondary,
          fontSize: '0.6875rem',
          fontWeight: 600,
          letterSpacing: '0.06em',
          textTransform: 'uppercase',
          lineHeight: '32px',
        },
      },
    },
    MuiListItemButton: {
      styleOverrides: {
        root: {
          borderRadius: radius.sm,
          '&.Mui-selected': {
            backgroundColor: palette.surface.selected,
            '&:hover': { backgroundColor: palette.surface.selected },
            '& .MuiListItemIcon-root': { color: palette.primary.main },
          },
        },
      },
    },
    MuiListItemIcon: {
      styleOverrides: { root: { minWidth: 36, color: palette.text.secondary } },
    },

    MuiAccordion: {
      defaultProps: { disableGutters: true, elevation: 0 },
      styleOverrides: {
        root: {
          border: `1px solid ${palette.divider}`,
          borderRadius: radius.md,
          '&:before': { display: 'none' },
          '&.Mui-expanded': { margin: 0 },
        },
      },
    },

    MuiSkeleton: {
      styleOverrides: { root: { backgroundColor: alpha(palette.text.primary, isDark ? 0.08 : 0.08) } },
    },

    MuiDataGrid: {
      defaultProps: {
        density: 'compact',
        disableColumnMenu: true,
        rowHeight: 40,
        columnHeaderHeight: 40,
      },
      styleOverrides: {
        root: {
          border: `1px solid ${palette.divider}`,
          borderRadius: radius.md,
          backgroundColor: palette.background.paper,
          color: palette.text.primary,
          '--DataGrid-rowBorderColor': palette.divider,
          '& .MuiDataGrid-columnHeaders, & .MuiDataGrid-columnHeader': {
            backgroundColor: palette.surface.sunken,
          },
          '& .MuiDataGrid-columnHeaderTitle': {
            fontWeight: 600,
            fontSize: '0.75rem',
            color: palette.text.secondary,
          },
          '& .MuiDataGrid-columnSeparator': { color: palette.divider },
          '& .MuiDataGrid-cell': { borderTopColor: palette.divider, display: 'flex', alignItems: 'center' },
          '& .MuiDataGrid-row--dynamicHeight > .MuiDataGrid-cell': { paddingTop: 6, paddingBottom: 6, minHeight: 40 },
          '& .MuiDataGrid-cell:focus, & .MuiDataGrid-cell:focus-within, & .MuiDataGrid-columnHeader:focus, & .MuiDataGrid-columnHeader:focus-within':
            { outline: 'none' },
          '& .MuiDataGrid-row:hover': { backgroundColor: palette.surface.hover },
          '& .MuiDataGrid-row.Mui-selected, & .MuiDataGrid-row.Mui-selected:hover': {
            backgroundColor: palette.surface.selected,
          },
          '& .MuiDataGrid-footerContainer': { borderTop: `1px solid ${palette.divider}` },
          '& .MuiDataGrid-overlay': { backgroundColor: 'transparent' },
          '& .MuiDataGrid-row.kh-detail-row': {
            backgroundColor: palette.surface.sunken,
            '&:hover': { backgroundColor: palette.surface.sunken },
          },
          '& .MuiDataGrid-row.kh-detail-row .MuiDataGrid-cell': { padding: 0 },
          '& .MuiDataGrid-row.kh-row-clickable': { cursor: 'pointer' },
          '& .MuiDataGrid-row.kh-row-muted': { opacity: 0.7 },
        },
      },
    },
  };
};
