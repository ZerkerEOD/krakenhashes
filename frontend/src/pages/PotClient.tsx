import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Box, Button, Alert, CircularProgress } from '@mui/material';
import { ArrowBack as ArrowBackIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { EntityLink, PageHeader } from '../components/ui';
import { ROUTES } from '../constants/routes';
import PotTable from '../components/pot/PotTable';
import { potService } from '../services/pot';
import { api } from '../services/api';

interface Client {
  id: string;
  name: string;
}

export default function PotClient() {
  const { t } = useTranslation('pot');
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [clientName, setClientName] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadClientInfo = async () => {
      if (!id) return;
      
      try {
        setLoading(true);
        const response = await api.get<{ data: Client }>(`/api/clients/${id}`);
        setClientName(response.data.data.name);
      } catch (err) {
        console.error('Error loading client info:', err);
        setError(t('errors.loadClientFailed') as string);
      } finally {
        setLoading(false);
      }
    };

    loadClientInfo();
  }, [id, t]);

  const fetchData = async (limit: number, offset: number, search?: string) => {
    if (!id) throw new Error('No client ID provided');
    return await potService.getPotByClient(id, { limit, offset, search });
  };

  const handleBack = () => {
    navigate(id ? ROUTES.client(id!) : ROUTES.pot);
  };

  if (loading) {
    return (
      <Box sx={{ p: 3 }}>
        <Box display="flex" justifyContent="center" alignItems="center" minHeight={400}>
          <CircularProgress />
        </Box>
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">{error}</Alert>
        <Box sx={{ mt: 2 }}>
          <Button startIcon={<ArrowBackIcon />} onClick={handleBack}>
            {t('navigation.backToSource') as string}
          </Button>
        </Box>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('client.title') as string}
        description={
          <>
            {t('sourceLabel.client', 'Client') as string}:{' '}
            <EntityLink type="client" id={id} label={clientName || id} />
          </>
        }
        backTo={ROUTES.client(id!)}
        breadcrumbs={[
          { label: t('breadcrumb', 'Cracked hashes') as string, to: ROUTES.pot },
          { label: clientName || id, to: ROUTES.client(id!) },
        ]}
      />

      <PotTable
        title={t('client.tableTitle', { clientName }) as string}
        fetchData={fetchData}
        filterParam="client"
        filterValue={clientName}
        contextType="client"
        contextName={clientName}
        contextId={id}
      />
    </Box>
  );
}