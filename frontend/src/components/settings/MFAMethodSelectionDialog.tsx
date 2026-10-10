import React from 'react';
import { useTranslation } from 'react-i18next';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Typography,
  Card,
  CardActionArea,
  CardContent,
} from '@mui/material';
import {
  Key as KeyIcon,
  Fingerprint as FingerprintIcon,
} from '@mui/icons-material';

interface MFAMethodSelectionDialogProps {
  open: boolean;
  onClose: () => void;
  onSelectMethod: (method: 'authenticator' | 'passkey') => void;
  availableMethods: ('authenticator' | 'passkey')[];
}

const MFAMethodSelectionDialog: React.FC<MFAMethodSelectionDialogProps> = ({
  open,
  onClose,
  onSelectMethod,
  availableMethods,
}) => {
  const { t } = useTranslation('settings');
  const methodInfo = {
    authenticator: {
      icon: <KeyIcon sx={{ fontSize: 48 }} />,
      title: t('mfa.methods.totp') as string,
      description: t('mfa.methodSelection.authenticatorDescription') as string,
    },
    passkey: {
      icon: <FingerprintIcon sx={{ fontSize: 48 }} />,
      title: t('mfa.methods.passkey') as string,
      description: t('mfa.methodSelection.passkeyDescription') as string,
    },
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
    >
      <DialogTitle>{t('mfa.methodSelection.title')}</DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          {t('mfa.methodSelection.description')}
        </Typography>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          {availableMethods.map((method) => {
            const info = methodInfo[method];
            return (
              <Card
                key={method}
                variant="outlined"
                sx={{
                  '&:hover': {
                    borderColor: 'primary.main',
                    backgroundColor: 'action.hover',
                  },
                }}
              >
                <CardActionArea onClick={() => onSelectMethod(method)}>
                  <CardContent>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
                      <Box
                        sx={{
                          color: 'primary.main',
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                        }}
                      >
                        {info.icon}
                      </Box>
                      <Box sx={{ flex: 1 }}>
                        <Typography variant="subtitle1" fontWeight="medium">
                          {info.title}
                        </Typography>
                        <Typography variant="body2" color="text.secondary">
                          {info.description}
                        </Typography>
                      </Box>
                    </Box>
                  </CardContent>
                </CardActionArea>
              </Card>
            );
          })}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('mfa.dialogs.cancel')}</Button>
      </DialogActions>
    </Dialog>
  );
};

export default MFAMethodSelectionDialog;
