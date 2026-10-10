import React, { useState } from 'react';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  Switch,
  Typography,
} from '@mui/material';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../services/api';
import { useToast } from '../ui/toast';
import { getErrorMessage } from '../../utils/errors';

interface GenerateVoucherDialogProps {
  open: boolean;
  onClose: () => void;
  /** Called with the new claim code once it has been generated. */
  onGenerated?: (code: string) => void;
}

/**
 * Generates an agent claim voucher (single-use or continuous, optionally a
 * system voucher that serves every team) and shows the code to copy.
 */
const GenerateVoucherDialog: React.FC<GenerateVoucherDialogProps> = ({ open, onClose, onGenerated }) => {
  const { t } = useTranslation('agents');
  const toast = useToast();
  const queryClient = useQueryClient();
  const [isContinuous, setIsContinuous] = useState(false);
  const [isSystem, setIsSystem] = useState(false);
  const [code, setCode] = useState('');

  const generate = useMutation({
    mutationFn: () =>
      api.post<{ code: string }>('/api/vouchers/temp', { isContinuous, isSystem }).then((r) => r.data),
    onSuccess: (data) => {
      setCode(data.code);
      onGenerated?.(data.code);
      queryClient.invalidateQueries({ queryKey: ['vouchers'] });
    },
    onError: (err) => toast.error(getErrorMessage(err) || (t('errors.generateFailed') as string)),
  });

  const close = () => {
    setCode('');
    setIsContinuous(false);
    setIsSystem(false);
    onClose();
  };

  return (
    <Dialog open={open} onClose={close} maxWidth="xs" fullWidth>
      <DialogTitle>
        {code ? (t('dialogs.register.generatedTitle') as string) : (t('dialogs.register.title') as string)}
      </DialogTitle>
      <DialogContent>
        {!code ? (
          <Box sx={{ pt: 1, display: 'flex', flexDirection: 'column' }}>
            <FormControlLabel
              control={<Switch checked={isContinuous} onChange={(e) => setIsContinuous(e.target.checked)} />}
              label={t('dialogs.register.continuousLabel') as string}
            />
            <FormControlLabel
              control={<Switch checked={isSystem} onChange={(e) => setIsSystem(e.target.checked)} />}
              label={t('dialogs.register.systemLabel') as string}
            />
          </Box>
        ) : (
          <Box sx={{ mt: 1, textAlign: 'center' }}>
            <Typography variant="subtitle1">{t('dialogs.register.claimCodeLabel') as string}</Typography>
            <Typography variant="h5" sx={{ my: 2, fontFamily: (theme) => theme.typography.monoFamily, userSelect: 'all' }}>
              {code}
            </Typography>
            <Typography color="text.secondary">
              {isContinuous
                ? (t('dialogs.register.continuousDescription') as string)
                : (t('dialogs.register.singleUseDescription') as string)}
            </Typography>
          </Box>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={close}>{t('buttons.close') as string}</Button>
        {!code && (
          <Button onClick={() => generate.mutate()} variant="contained" disabled={generate.isPending}>
            {t('buttons.generateCode') as string}
          </Button>
        )}
      </DialogActions>
    </Dialog>
  );
};

export default GenerateVoucherDialog;
