import React from 'react';
import { Box, Typography } from '@mui/material';
import { useAuth } from '../../contexts/AuthContext';
import { Navigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import BinaryManagement from '../../components/admin/BinaryManagement';

const BinaryManagementPage: React.FC = () => {
  const { t } = useTranslation('admin');
  const { userRole } = useAuth();

  // Redirect if not admin
  if (userRole !== 'admin') {
    return <Navigate to="/" replace />;
  }

  return (
    <Box sx={{ width: '100%', p: 3 }}>
      <Typography variant="h4" gutterBottom>
        {t('binaryManagement.pageTitle') as string}
      </Typography>
      <BinaryManagement />
    </Box>
  );
};

export default BinaryManagementPage; 