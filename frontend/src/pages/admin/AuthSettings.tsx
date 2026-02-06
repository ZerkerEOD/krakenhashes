import { Box, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import AuthSettingsForm from '../../components/admin/AuthSettings';

const AuthSettingsPage = () => {
  const { t } = useTranslation('admin');

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        {t('authSettings.pageTitle') as string}
      </Typography>
      <AuthSettingsForm />
    </Box>
  );
};

export default AuthSettingsPage;
