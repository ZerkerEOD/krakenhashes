/**
 * Toasts. One channel for the whole app, backed by notistack.
 *
 *   const toast = useToast();
 *   toast.success(t('saved'));
 *   toast.error(err);                 // accepts unknown: axios errors are unwrapped
 *
 * `toast` (module-level) is for non-component code such as axios interceptors
 * and React Query defaults; `<ToastBridge/>` inside the provider wires it up.
 */
import React, { useEffect, useMemo } from 'react';
import { OptionsObject, SnackbarKey, SnackbarProvider, SnackbarProviderProps, useSnackbar } from 'notistack';
import { useTheme } from '@mui/material/styles';
import { getErrorMessage } from '../../utils/errors';

type Variant = 'success' | 'error' | 'warning' | 'info';
type ToastOptions = Omit<OptionsObject, 'variant'>;

export interface Toast {
  success: (message: string, options?: ToastOptions) => SnackbarKey | undefined;
  error: (error: unknown, options?: ToastOptions) => SnackbarKey | undefined;
  warning: (message: string, options?: ToastOptions) => SnackbarKey | undefined;
  info: (message: string, options?: ToastOptions) => SnackbarKey | undefined;
  dismiss: (key?: SnackbarKey) => void;
}

type Enqueue = (message: string, options?: OptionsObject) => SnackbarKey;
type Close = (key?: SnackbarKey) => void;

let bridgedEnqueue: Enqueue | null = null;
let bridgedClose: Close | null = null;

const build = (enqueue: Enqueue | null, close: Close | null): Toast => {
  const fire = (variant: Variant) => (message: string, options?: ToastOptions) => {
    if (!enqueue) {
      // eslint-disable-next-line no-console
      console[variant === 'error' ? 'error' : 'log'](`[toast:${variant}] ${message}`);
      return undefined;
    }
    return enqueue(message, { variant, ...options });
  };
  return {
    success: fire('success'),
    warning: fire('warning'),
    info: fire('info'),
    error: (error, options) => fire('error')(getErrorMessage(error), options),
    dismiss: (key) => close?.(key),
  };
};

/** Module-level toast for code outside the React tree. No-op until `<ToastBridge/>` mounts. */
export const toast: Toast = {
  success: (m, o) => build(bridgedEnqueue, bridgedClose).success(m, o),
  error: (e, o) => build(bridgedEnqueue, bridgedClose).error(e, o),
  warning: (m, o) => build(bridgedEnqueue, bridgedClose).warning(m, o),
  info: (m, o) => build(bridgedEnqueue, bridgedClose).info(m, o),
  dismiss: (k) => bridgedClose?.(k),
};

export const ToastBridge: React.FC = () => {
  const { enqueueSnackbar, closeSnackbar } = useSnackbar();
  useEffect(() => {
    bridgedEnqueue = enqueueSnackbar as Enqueue;
    bridgedClose = closeSnackbar;
    return () => {
      bridgedEnqueue = null;
      bridgedClose = null;
    };
  }, [enqueueSnackbar, closeSnackbar]);
  return null;
};

export const useToast = (): Toast => {
  const { enqueueSnackbar, closeSnackbar } = useSnackbar();
  return useMemo(() => build(enqueueSnackbar as Enqueue, closeSnackbar), [enqueueSnackbar, closeSnackbar]);
};

/**
 * App-wide snackbar provider with theme-aware colours (notistack's defaults
 * are fixed hex values that do not follow light mode).
 */
export const ToastProvider: React.FC<{ children: React.ReactNode } & Partial<SnackbarProviderProps>> = ({
  children,
  ...rest
}) => {
  const theme = useTheme();
  const base = {
    borderRadius: theme.shape.borderRadius,
    fontSize: String(theme.typography.body2.fontSize ?? '0.8125rem'),
    fontWeight: 500,
    boxShadow: theme.palette.mode === 'dark' ? '0 8px 24px rgba(0,0,0,0.55)' : '0 8px 24px rgba(16,24,40,0.18)',
  };
  const tone = (main: string, contrast: string) => ({ ...base, backgroundColor: main, color: contrast });
  return (
    <SnackbarProvider
      maxSnack={3}
      preventDuplicate
      autoHideDuration={4000}
      anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
      style={base}
      {...rest}
    >
      <style>{`
        .notistack-MuiContent-success { ${toCss(tone(theme.palette.success.main, theme.palette.success.contrastText))} }
        .notistack-MuiContent-error { ${toCss(tone(theme.palette.error.main, theme.palette.error.contrastText))} }
        .notistack-MuiContent-warning { ${toCss(tone(theme.palette.warning.main, theme.palette.warning.contrastText))} }
        .notistack-MuiContent-info { ${toCss(tone(theme.palette.info.main, theme.palette.info.contrastText))} }
        .notistack-MuiContent-default { ${toCss(tone(theme.palette.surface.raised, theme.palette.text.primary))} border: 1px solid ${theme.palette.divider}; }
      `}</style>
      <ToastBridge />
      {children}
    </SnackbarProvider>
  );
};

const toCss = (o: Record<string, string | number>) =>
  Object.entries(o)
    .map(([k, v]) => `${k.replace(/[A-Z]/g, (m) => `-${m.toLowerCase()}`)}: ${typeof v === 'number' && k !== 'fontWeight' ? `${v}px` : v} !important;`)
    .join(' ');

export default useToast;
