import React, { useState, useEffect } from 'react';
import { Typography, Box, Autocomplete, TextField } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import PotTable from '../components/pot/PotTable';
import { potService } from '../services/pot';
import { api } from '../services/api';

interface HashlistOption {
  id: number;
  name: string;
  cracked_hashes: number;
}

export default function Pot() {
  const { t } = useTranslation('pot');
  const navigate = useNavigate();
  const [hashlists, setHashlists] = useState<HashlistOption[]>([]);
  const [hashlistsLoading, setHashlistsLoading] = useState(false);

  useEffect(() => {
    const loadHashlists = async () => {
      try {
        setHashlistsLoading(true);
        const response = await api.get('/api/hashlists');
        const withCracks = (response.data.hashlists || [])
          .filter((h: any) => h.cracked_hashes > 0)
          .map((h: any) => ({ id: h.id, name: h.name, cracked_hashes: h.cracked_hashes }));
        setHashlists(withCracks);
      } catch (err) {
        console.error('Error loading hashlists:', err);
      } finally {
        setHashlistsLoading(false);
      }
    };
    loadHashlists();
  }, []);

  const fetchData = async (limit: number, offset: number, search?: string) => {
    return await potService.getPot({ limit, offset, search });
  };

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            {t('page.title') as string}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {t('page.description') as string}
          </Typography>
        </Box>
        <Autocomplete
          options={hashlists}
          getOptionLabel={(option) => `${option.name} (${option.cracked_hashes.toLocaleString()} cracked)`}
          loading={hashlistsLoading}
          onChange={(_, value) => {
            if (value) {
              navigate(`/pot/hashlist/${value.id}`);
            }
          }}
          renderInput={(params) => (
            <TextField
              {...params}
              label={t('page.filterByHashlist') as string}
              placeholder={t('page.hashlistPlaceholder') as string}
              size="small"
            />
          )}
          sx={{ minWidth: 300 }}
          size="small"
        />
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
