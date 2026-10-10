import React, { useState, useCallback } from 'react';
import {
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Button,
  TextField,
  Box
} from '@mui/material';
import { useTranslation } from 'react-i18next';

interface PasswordConfirmDialogState {
  isOpen: boolean;
  title: string;
  message: string;
  resolve: ((value: string | null) => void) | null;
}

const initialState: PasswordConfirmDialogState = {
  isOpen: false,
  title: '',
  message: '',
  resolve: null,
};

export const usePasswordConfirm = () => {
  const { t } = useTranslation('common');
  const [dialogState, setDialogState] = useState<PasswordConfirmDialogState>(initialState);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');

  const handleClose = useCallback(() => {
    if (dialogState.resolve) {
      dialogState.resolve(null);
    }
    setDialogState(initialState);
    setPassword('');
    setError('');
  }, [dialogState]);

  const handleConfirm = useCallback(() => {
    if (!password.trim()) {
      setError(t('passwordConfirm.passwordRequired') as string);
      return;
    }

    if (dialogState.resolve) {
      dialogState.resolve(password);
    }
    setDialogState(initialState);
    setPassword('');
    setError('');
  }, [dialogState, password, t]);

  const showPasswordConfirm = useCallback((title: string, message: string): Promise<string | null> => {
    return new Promise<string | null>((resolve) => {
      setDialogState({
        isOpen: true,
        title,
        message,
        resolve,
      });
    });
  }, []);

  const PasswordConfirmDialog = useCallback(() => (
    <Dialog
      open={dialogState.isOpen}
      onClose={handleClose}
      aria-labelledby="password-confirm-dialog-title"
      aria-describedby="password-confirm-dialog-description"
      maxWidth="sm"
      fullWidth
    >
      <DialogTitle id="password-confirm-dialog-title">{dialogState.title}</DialogTitle>
      <DialogContent>
        <DialogContentText id="password-confirm-dialog-description" sx={{ mb: 2 }}>
          {dialogState.message}
        </DialogContentText>
        <Box>
          <TextField
            autoFocus
            fullWidth
            type="password"
            label={t('passwordConfirm.currentPasswordLabel')}
            value={password}
            onChange={(e) => {
              setPassword(e.target.value);
              setError('');
            }}
            onKeyPress={(e) => {
              if (e.key === 'Enter') {
                handleConfirm();
              }
            }}
            error={!!error}
            helperText={error}
            margin="dense"
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} color="primary">
          {t('buttons.cancel')}
        </Button>
        <Button onClick={handleConfirm} color="primary" variant="contained">
          {t('buttons.confirm')}
        </Button>
      </DialogActions>
    </Dialog>
  ), [dialogState, password, error, handleClose, handleConfirm, t]);

  return { showPasswordConfirm, PasswordConfirmDialog };
};
