import React from 'react';
import { Box, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import Diagnostics from '../../components/admin/Diagnostics';

const DiagnosticsPage = () => {
  const { t } = useTranslation('admin');

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        {t('diagnostics.pageTitle') as string}
      </Typography>
      <Diagnostics />
    </Box>
  );
};

export default DiagnosticsPage;
