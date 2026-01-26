import React, { useState } from 'react';
import { Box, Typography, Button, CircularProgress } from '@mui/material';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { getPasswordPolicy, getAccountSecurity, updateAuthSettings } from '../../services/auth';
import { PasswordPolicy, AccountSecurity, AuthSettingsUpdate } from '../../types/auth';
import AuthSettingsForm from '../../components/admin/AuthSettings';

const AuthSettingsPage = () => {
  const { t } = useTranslation('admin');
  const [loading, setLoading] = useState(false);
  const { enqueueSnackbar } = useSnackbar();

  const handleSave = async (settings: AuthSettingsUpdate) => {
    setLoading(true);
    try {
      await updateAuthSettings(settings);
      enqueueSnackbar(t('authSettings.messages.updateSuccess') as string, { variant: 'success' });
    } catch (error) {
      enqueueSnackbar(t('authSettings.messages.updateFailed') as string, { variant: 'error' });
      console.error('Failed to update settings:', error);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        {t('authSettings.pageTitle') as string}
      </Typography>
      <AuthSettingsForm onSave={handleSave} loading={loading} />
    </Box>
  );
};

export default AuthSettingsPage; 