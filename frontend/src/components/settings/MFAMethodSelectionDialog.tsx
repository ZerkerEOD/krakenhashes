import React from 'react';
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
  const methodInfo = {
    authenticator: {
      icon: <KeyIcon sx={{ fontSize: 48 }} />,
      title: 'Authenticator App',
      description: 'Use a time-based code from an authenticator app like Google Authenticator, Authy, or 1Password.',
    },
    passkey: {
      icon: <FingerprintIcon sx={{ fontSize: 48 }} />,
      title: 'Passkey',
      description: 'Use a security key, fingerprint, face recognition, or device PIN for quick and secure authentication.',
    },
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
    >
      <DialogTitle>Choose MFA Method</DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          Select how you'd like to verify your identity when signing in.
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
        <Button onClick={onClose}>Cancel</Button>
      </DialogActions>
    </Dialog>
  );
};

export default MFAMethodSelectionDialog;
