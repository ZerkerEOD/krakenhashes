import React from 'react';
import { Box, Typography } from '@mui/material';
import Diagnostics from '../../components/admin/Diagnostics';

const DiagnosticsPage = () => {
  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        Diagnostics
      </Typography>
      <Diagnostics />
    </Box>
  );
};

export default DiagnosticsPage;
