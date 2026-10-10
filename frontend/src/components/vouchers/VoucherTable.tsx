import React, { useMemo } from 'react';
import { Box, Chip, FormControlLabel, Switch } from '@mui/material';
import BlockIcon from '@mui/icons-material/Block';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { GridColDef } from '@mui/x-data-grid';
import { DataTable, EntityLink, StatusChip } from '../ui';
import { useToast } from '../ui/toast';
import { useConfirm } from '../ui/ConfirmProvider';
import { api } from '../../services/api';
import { getErrorMessage } from '../../utils/errors';
import type { ClaimVoucher } from '../../types/agent';

const SYSTEM_USER_ID = '00000000-0000-0000-0000-000000000000';

interface VoucherTableProps {
  vouchers: ClaimVoucher[];
  loading?: boolean;
  error?: unknown;
  onRetry?: () => void;
  includeInactive: boolean;
  onIncludeInactiveChange: (v: boolean) => void;
}

/** Claim vouchers with a deactivate action; inactive ones can be shown on demand. */
const VoucherTable: React.FC<VoucherTableProps> = ({
  vouchers,
  loading,
  error,
  onRetry,
  includeInactive,
  onIncludeInactiveChange,
}) => {
  const { t } = useTranslation('agents');
  const tr = (k: string) => t(k) as string;
  const toast = useToast();
  const confirm = useConfirm();
  const queryClient = useQueryClient();

  const deactivate = useMutation({
    mutationFn: (code: string) => api.delete(`/api/vouchers/${code}/disable`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['vouchers'] }),
    onError: (err) => toast.error(getErrorMessage(err) || tr('errors.deactivateFailed')),
  });

  const rows = useMemo(
    () => (includeInactive ? vouchers : vouchers.filter((v) => v.is_active)),
    [vouchers, includeInactive]
  );

  const columns: GridColDef<ClaimVoucher>[] = [
    {
      field: 'code',
      headerName: tr('table.columns.claimCode'),
      flex: 1.2,
      minWidth: 180,
      renderCell: (p) => (
        <Box component="span" sx={{ fontFamily: (theme) => theme.typography.monoFamily }}>
          {p.value}
        </Box>
      ),
    },
    {
      field: 'created_by',
      headerName: tr('table.columns.createdBy'),
      flex: 1,
      minWidth: 140,
      valueGetter: (_v, row) => row.created_by?.username ?? '',
      renderCell: (p) =>
        p.row.created_by_id === SYSTEM_USER_ID || !p.row.created_by ? (
          <span>{p.row.created_by?.username || tr('vouchers.systemLabel')}</span>
        ) : (
          <EntityLink type="user" id={p.row.created_by_id} label={p.row.created_by.username} />
        ),
    },
    {
      field: 'created_at',
      headerName: tr('table.columns.createdAt'),
      width: 180,
      valueFormatter: (v) => (v ? new Date(v as string).toLocaleString() : ''),
    },
    {
      field: 'is_continuous',
      headerName: tr('table.columns.type'),
      width: 200,
      sortable: false,
      renderCell: (p) => (
        <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
          <Chip
            size="small"
            color={p.value ? 'primary' : 'default'}
            label={p.value ? tr('vouchers.continuous') : tr('vouchers.singleUse')}
          />
          {p.row.created_by_id === SYSTEM_USER_ID && <Chip size="small" color="secondary" label={tr('vouchers.systemLabel')} />}
        </Box>
      ),
    },
    {
      field: 'is_active',
      headerName: tr('vouchers.status'),
      width: 120,
      renderCell: (p) => (
        <StatusChip entity="generic" status={p.value ? 'enabled' : 'disabled'} label={p.value ? tr('vouchers.active') : tr('vouchers.inactive')} />
      ),
    },
  ];

  return (
    <DataTable<ClaimVoucher>
      rows={rows}
      columns={columns}
      getRowId={(r) => r.code}
      loading={loading}
      error={error}
      onRetry={onRetry}
      pagination={{ mode: 'client', initialPageSize: 25 }}
      sorting={{ mode: 'client', initial: [{ field: 'created_at', sort: 'desc' }] }}
      toolbar={{
        filters: (
          <FormControlLabel
            control={<Switch size="small" checked={includeInactive} onChange={(e) => onIncludeInactiveChange(e.target.checked)} />}
            label={tr('vouchers.includeInactive')}
          />
        ),
      }}
      rowActions={(row) => [
        {
          key: 'deactivate',
          label: tr('actions.deactivateVoucher'),
          icon: <BlockIcon fontSize="small" />,
          danger: true,
          hidden: !row.is_active,
          onClick: async (r) => {
            const ok = await confirm({
              title: tr('actions.deactivateVoucher'),
              message: r.code,
              severity: 'danger',
              confirmLabel: tr('actions.deactivateVoucher'),
            });
            if (ok) deactivate.mutate(r.code);
          },
        },
      ]}
      emptyState={{ title: tr('messages.noVouchers') }}
      tableKey="vouchers"
      height="auto"
    />
  );
};

export default VoucherTable;
