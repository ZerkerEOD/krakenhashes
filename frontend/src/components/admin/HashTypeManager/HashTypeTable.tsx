import React, { useMemo, useState } from 'react';
import { Box, Chip, Tooltip, Typography } from '@mui/material';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/Delete';
import WarningIcon from '@mui/icons-material/Warning';
import WaterDropIcon from '@mui/icons-material/WaterDrop';
import BoltIcon from '@mui/icons-material/Bolt';
import type { GridColDef } from '@mui/x-data-grid';
import { useTranslation } from 'react-i18next';
import { DataTable } from '../../ui';
import { HashType } from '../../../types/hashType';

interface HashTypeTableProps {
  hashTypes: HashType[];
  onEdit: (hashType: HashType) => void;
  onDelete: (hashType: HashType) => void;
  loading?: boolean;
  fetching?: boolean;
  error?: unknown;
  onRetry?: () => void;
}

const truncateText = (text: string | null | undefined, maxLength: number = 50): string => {
  if (!text) return '';
  return text.length > maxLength ? text.substring(0, maxLength) + '...' : text;
};

/** Hash type catalogue with client-side search, sort and pagination. */
const HashTypeTable: React.FC<HashTypeTableProps> = ({
  hashTypes,
  onEdit,
  onDelete,
  loading = false,
  fetching,
  error,
  onRetry,
}) => {
  const { t } = useTranslation('admin');
  const [searchTerm, setSearchTerm] = useState('');

  const filteredHashTypes = useMemo(() => {
    const search = searchTerm.toLowerCase();
    return hashTypes.filter(
      (ht) =>
        ht.id.toString().includes(search) ||
        ht.name.toLowerCase().includes(search) ||
        (ht.description && ht.description.toLowerCase().includes(search))
    );
  }, [hashTypes, searchTerm]);

  const columns: GridColDef<HashType>[] = [
    {
      field: 'id',
      headerName: t('hashTypes.table.id') as string,
      type: 'number',
      width: 100,
      align: 'left',
      headerAlign: 'left',
      renderCell: (p) => (
        <Typography variant="body2" sx={{ fontWeight: 'bold' }}>
          {p.row.id}
        </Typography>
      ),
    },
    {
      field: 'name',
      headerName: t('hashTypes.table.name') as string,
      flex: 1.2,
      minWidth: 200,
      renderCell: (p) => (
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Typography variant="body2">{p.row.name}</Typography>
          {p.row.id === 1000 && (
            <Tooltip title={t('hashTypes.tooltips.requiresProcessing') as string}>
              <Chip label={t('hashTypes.tooltips.processing') as string} size="small" color="info" icon={<BoltIcon />} />
            </Tooltip>
          )}
        </Box>
      ),
    },
    {
      field: 'description',
      headerName: t('hashTypes.table.description') as string,
      flex: 1.5,
      minWidth: 200,
      renderCell: (p) => (
        <Tooltip title={p.row.description || ''} arrow>
          <Typography variant="body2" noWrap sx={{ cursor: p.row.description ? 'help' : 'default' }}>
            {truncateText(p.row.description)}
          </Typography>
        </Tooltip>
      ),
    },
    {
      field: 'example',
      headerName: t('hashTypes.table.example') as string,
      flex: 1,
      minWidth: 180,
      sortable: false,
      renderCell: (p) => (
        <Tooltip title={p.row.example || ''} arrow>
          <Typography
            variant="body2"
            noWrap
            sx={{
              fontFamily: (theme) => theme.typography.monoFamily,
              fontSize: '0.85rem',
              cursor: p.row.example ? 'help' : 'default',
            }}
          >
            {truncateText(p.row.example, 30)}
          </Typography>
        </Tooltip>
      ),
    },
    {
      field: 'slow',
      headerName: t('hashTypes.table.slow') as string,
      type: 'boolean',
      width: 80,
      align: 'center',
      headerAlign: 'center',
      renderCell: (p) =>
        p.row.slow ? (
          <Tooltip title={t('hashTypes.tooltips.slowAlgorithm') as string}>
            <WarningIcon color="warning" fontSize="small" />
          </Tooltip>
        ) : null,
    },
    {
      field: 'is_salted',
      headerName: t('hashTypes.table.salted') as string,
      type: 'boolean',
      width: 80,
      align: 'center',
      headerAlign: 'center',
      renderCell: (p) =>
        p.row.is_salted ? (
          <Tooltip title={t('hashTypes.tooltips.salted') as string}>
            <WaterDropIcon color="info" fontSize="small" />
          </Tooltip>
        ) : null,
    },
  ];

  return (
    <DataTable<HashType>
      rows={filteredHashTypes}
      columns={columns}
      getRowId={(r) => r.id}
      loading={loading}
      fetching={fetching}
      error={error}
      onRetry={onRetry}
      pagination={{ mode: 'client', initialPageSize: 25 }}
      sorting={{ mode: 'client', initial: [{ field: 'id', sort: 'asc' }] }}
      toolbar={{
        search: {
          value: searchTerm,
          onChange: setSearchTerm,
          placeholder: t('hashTypes.searchPlaceholder') as string,
          debounceMs: 150,
        },
        filters: (
          <Typography variant="body2" color="text.secondary">
            {t('hashTypes.hashTypesFound', { count: filteredHashTypes.length }) as string}
          </Typography>
        ),
      }}
      rowActions={() => [
        {
          key: 'edit',
          label: t('common.edit') as string,
          icon: <EditIcon fontSize="small" />,
          onClick: (ht) => onEdit(ht),
        },
        {
          key: 'delete',
          label: t('common.delete') as string,
          icon: <DeleteIcon fontSize="small" />,
          danger: true,
          onClick: (ht) => onDelete(ht),
        },
      ]}
      emptyState={{ title: t('hashTypes.table.noResults') as string }}
      tableKey="admin-hash-types"
    />
  );
};

export default HashTypeTable;
