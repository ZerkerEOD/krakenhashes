import React from 'react';
import { Typography, Box } from '@mui/material';
import { useTranslation } from 'react-i18next';
import PotTable from '../components/pot/PotTable';
import { potService } from '../services/pot';

export default function Pot() {
  const { t } = useTranslation('pot');

  const fetchData = async (limit: number, offset: number, search?: string) => {
    return await potService.getPot({ limit, offset, search });
  };

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ mb: 3 }}>
        <Typography variant="h4" component="h1" gutterBottom>
          {t('page.title') as string}
        </Typography>
        <Typography variant="body1" color="text.secondary">
          {t('page.description') as string}
        </Typography>
      </Box>

      <PotTable
        title={t('table.allCrackedHashes') as string}
        fetchData={fetchData}
        contextType="master"
        contextName="master"
      />
    </Box>
  );
}