import React, { useState } from 'react';
import { Box, Button } from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import { useTranslation } from 'react-i18next';
import { keepPreviousData } from '@tanstack/react-query';
import { PageHeader } from '../../components/ui';
import VoucherTable from '../../components/vouchers/VoucherTable';
import GenerateVoucherDialog from '../../components/vouchers/GenerateVoucherDialog';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { api } from '../../services/api';
import type { ClaimVoucher } from '../../types/agent';

/** Admin → People & Access → Vouchers: agent claim codes. */
const Vouchers: React.FC = () => {
  const { t } = useTranslation('agents');
  const [dialogOpen, setDialogOpen] = useState(false);
  const [includeInactive, setIncludeInactive] = useState(false);

  const query = useLiveQuery<ClaimVoucher[]>(
    {
      queryKey: ['vouchers'],
      queryFn: async () => (await api.get<ClaimVoucher[]>('/api/vouchers')).data || [],
      placeholderData: keepPreviousData,
    },
    { tier: 'list' }
  );

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={t('vouchers.pageTitle') as string}
        description={t('vouchers.pageDescription') as string}
        actions={
          <Button variant="contained" startIcon={<AddIcon />} onClick={() => setDialogOpen(true)}>
            {t('vouchers.generate') as string}
          </Button>
        }
      />
      <VoucherTable
        vouchers={query.data ?? []}
        loading={query.isLoading}
        error={query.error}
        onRetry={() => void query.refetch()}
        includeInactive={includeInactive}
        onIncludeInactiveChange={setIncludeInactive}
      />
      <GenerateVoucherDialog open={dialogOpen} onClose={() => setDialogOpen(false)} />
    </Box>
  );
};

export default Vouchers;
