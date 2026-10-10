/**
 * Promise-based confirmation dialog.
 *
 *   const confirm = useConfirm();
 *   if (await confirm({ title, message, severity: 'danger' })) { ... }
 *
 * With `action`, the dialog stays open with a spinner while the action runs,
 * shows the error inline on failure, and resolves `true` only on success.
 * `requireText` adds a type-to-confirm field for destructive operations.
 */
import React, { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react';
import {
  Alert,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
  Typography,
} from '@mui/material';
import { LoadingButton } from '@mui/lab';
import { useTranslation } from 'react-i18next';
import { getErrorMessage } from '../../utils/errors';

export interface ConfirmOptions {
  title: React.ReactNode;
  message?: React.ReactNode;
  confirmLabel?: React.ReactNode;
  cancelLabel?: React.ReactNode;
  severity?: 'default' | 'danger';
  /** The user must type this exact text before Confirm is enabled. */
  requireText?: string;
  /** Run inside the dialog; resolves true only if it succeeds. */
  action?: () => Promise<unknown>;
}

type ConfirmFn = (options: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<ConfirmFn | undefined>(undefined);

interface Pending {
  options: ConfirmOptions;
  resolve: (ok: boolean) => void;
}

export const ConfirmProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { t } = useTranslation('common');
  const [pending, setPending] = useState<Pending | null>(null);
  const [typed, setTyped] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const resolvedRef = useRef(false);

  const confirm = useCallback<ConfirmFn>((options) => {
    return new Promise<boolean>((resolve) => {
      resolvedRef.current = false;
      setTyped('');
      setBusy(false);
      setError(null);
      setPending({ options, resolve });
    });
  }, []);

  const finish = (ok: boolean) => {
    if (!pending || resolvedRef.current) return;
    resolvedRef.current = true;
    pending.resolve(ok);
    setPending(null);
  };

  const handleConfirm = async () => {
    if (!pending) return;
    if (!pending.options.action) {
      finish(true);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await pending.options.action();
      setBusy(false);
      finish(true);
    } catch (err) {
      setBusy(false);
      setError(getErrorMessage(err));
    }
  };

  const value = useMemo(() => confirm, [confirm]);
  const opts = pending?.options;
  const danger = opts?.severity === 'danger';
  const textOk = !opts?.requireText || typed === opts.requireText;

  return (
    <ConfirmContext.Provider value={value}>
      {children}
      <Dialog
        open={Boolean(pending)}
        onClose={busy ? undefined : () => finish(false)}
        maxWidth="xs"
        fullWidth
        aria-labelledby="kh-confirm-title"
      >
        {opts && (
          <>
            <DialogTitle id="kh-confirm-title">{opts.title}</DialogTitle>
            <DialogContent>
              {opts.message && (
                <DialogContentText component="div" sx={{ color: 'text.primary' }}>
                  {opts.message}
                </DialogContentText>
              )}
              {opts.requireText && (
                <>
                  <Typography variant="body2" color="text.secondary" sx={{ mt: 2, mb: 1 }}>
                    {t('confirmations.typeToConfirm', { text: opts.requireText }) as string}
                  </Typography>
                  <TextField
                    autoFocus
                    fullWidth
                    value={typed}
                    onChange={(e) => setTyped(e.target.value)}
                    disabled={busy}
                    inputProps={{ 'aria-label': opts.requireText }}
                  />
                </>
              )}
              {error && (
                <Alert severity="error" sx={{ mt: 2 }}>
                  {error}
                </Alert>
              )}
            </DialogContent>
            <DialogActions>
              <Button onClick={() => finish(false)} disabled={busy} color="inherit">
                {opts.cancelLabel ?? (t('buttons.cancel') as string)}
              </Button>
              <LoadingButton
                onClick={handleConfirm}
                loading={busy}
                disabled={!textOk}
                variant="contained"
                color={danger ? 'error' : 'primary'}
                autoFocus={!opts.requireText}
              >
                {opts.confirmLabel ?? (t(danger ? 'buttons.delete' : 'buttons.confirm') as string)}
              </LoadingButton>
            </DialogActions>
          </>
        )}
      </Dialog>
    </ConfirmContext.Provider>
  );
};

export const useConfirm = (): ConfirmFn => {
  const ctx = useContext(ConfirmContext);
  if (!ctx) {
    // Outside the provider (tests): fall back to the browser dialog.
    return async (options) => window.confirm(String(options.title));
  }
  return ctx;
};

export default ConfirmProvider;
