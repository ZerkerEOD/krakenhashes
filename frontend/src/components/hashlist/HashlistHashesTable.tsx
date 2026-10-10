import { useMemo, useState } from 'react';
import { Box } from '@mui/material';
import { ContentCopy as CopyIcon } from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../services/api';
import CrackedPassword from '../common/CrackedPassword';
import { DataTable, StatusChip, useToast } from '../ui';

interface HashDetail {
  id: string;
  hash_value: string;
  original_hash: string;
  username?: string;
  domain?: string;
  hash_type_id: number;
  is_cracked: boolean;
  password?: string;
  last_updated: string;
  // LM hash partial crack status (only for hash_type_id 3000)
  is_partially_lm_cracked?: boolean;
  lm_first_half_password?: string;
  lm_second_half_password?: string;
}

interface HashlistHashesTableProps {
  hashlistId: string;
  hashlistName: string;
  totalHashes: number;
  crackedHashes: number;
}

const ellipsis = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' } as const;
const mono = { fontFamily: (th: any) => th.typography.monoFamily, fontSize: '0.875rem' };

/** The hashes of one hashlist, server-paginated, with a filter over the current page. */
export default function HashlistHashesTable({ hashlistId, totalHashes, crackedHashes }: HashlistHashesTableProps) {
  const toast = useToast();
  const { t } = useTranslation('hashlists');
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(100);
  const [searchTerm, setSearchTerm] = useState('');

  const query = useQuery({
    queryKey: ['hashlists', 'detail', hashlistId, 'hashes', page, pageSize],
    queryFn: async () => {
      const response = await api.get(`/api/hashlists/${hashlistId}/hashes`, {
        params: { limit: pageSize, offset: page * pageSize },
      });
      return { hashes: (response.data.hashes || []) as HashDetail[], total: (response.data.total || 0) as number };
    },
    placeholderData: keepPreviousData,
  });
  const data = useMemo(() => query.data?.hashes ?? [], [query.data]);
  const totalCount = query.data?.total ?? 0;

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    toast.success(t('hashesTable.copiedToClipboard') as string);
  };

  const filteredData = useMemo(() => {
    if (!searchTerm) return data;
    const s = searchTerm.toLowerCase();
    return data.filter(
      (h) =>
        h.original_hash.toLowerCase().includes(s) ||
        (h.password && h.password.toLowerCase().includes(s)) ||
        (h.username && h.username.toLowerCase().includes(s)) ||
        (h.domain && h.domain.toLowerCase().includes(s))
    );
  }, [data, searchTerm]);

  const columns = useMemo<GridColDef<HashDetail>[]>(
    () => [
      {
        field: 'original_hash',
        headerName: t('hashesTable.columns.originalHash') as string,
        flex: 3,
        minWidth: 240,
        // Wrap long hashes inside their own column instead of overflowing neighbours (#50).
        renderCell: (p) => (
          <Box component="span" sx={{ ...mono, wordBreak: 'break-all', py: 0.75, lineHeight: 1.4 }}>
            {p.row.original_hash}
          </Box>
        ),
      },
      {
        field: 'username',
        headerName: t('hashesTable.columns.username') as string,
        flex: 1,
        minWidth: 110,
        renderCell: (p) => <Box component="span" sx={ellipsis}>{p.row.username || '-'}</Box>,
      },
      {
        field: 'domain',
        headerName: t('hashesTable.columns.domain') as string,
        flex: 1,
        minWidth: 110,
        renderCell: (p) => <Box component="span" sx={ellipsis}>{p.row.domain || '-'}</Box>,
      },
      {
        field: 'password',
        headerName: t('hashesTable.columns.password') as string,
        flex: 1.1,
        minWidth: 120,
        renderCell: (p) => {
          const h = p.row;
          return (
            <Box component="span" sx={{ ...mono, ...ellipsis }}>
              {h.is_cracked
                ? h.password
                  ? <CrackedPassword password={h.password} />
                  : '-'
                : h.is_partially_lm_cracked
                ? `[${h.lm_first_half_password || '?'}][${h.lm_second_half_password || '?'}]`
                : '-'}
            </Box>
          );
        },
      },
      {
        field: 'is_cracked',
        headerName: t('hashesTable.columns.status') as string,
        width: 110,
        renderCell: (p) => {
          const h = p.row;
          if (h.is_cracked) return <StatusChip entity="generic" status="success" label={t('hashesTable.status.cracked') as string} />;
          if (h.is_partially_lm_cracked) return <StatusChip entity="generic" status="warning" label={t('hashesTable.status.partial') as string} />;
          return <StatusChip entity="generic" status="pending" label={t('hashesTable.status.pending') as string} />;
        },
      },
    ],
    [t]
  );

  const pct = totalHashes > 0 ? Math.round((crackedHashes / totalHashes) * 100) : 0;

  return (
    <DataTable<HashDetail>
      rows={filteredData}
      columns={columns}
      loading={query.isLoading}
      fetching={query.isFetching && !query.isLoading}
      error={query.error ? (t('hashesTable.loadFailed') as string) : undefined}
      onRetry={() => void query.refetch()}
      pagination={{
        mode: 'server',
        page,
        pageSize,
        rowCount: totalCount,
        pageSizeOptions: [25, 50, 100],
        onChange: (m) => {
          setPage(m.pageSize !== pageSize ? 0 : m.page);
          setPageSize(m.pageSize);
        },
      }}
      sorting={false}
      toolbar={{
        title: t('hashesTable.title') as string,
        subtitle: t('hashesTable.subtitle', { cracked: crackedHashes, total: totalHashes, pct }) as string,
        search: { value: searchTerm, onChange: setSearchTerm, placeholder: t('hashesTable.searchPlaceholder') as string, debounceMs: 150 },
      }}
      rowActions={(row) => [
        {
          key: 'copy',
          label: row.is_cracked && row.password ? (t('hashesTable.copyPassword') as string) : (t('hashesTable.copyHash') as string),
          icon: <CopyIcon fontSize="small" />,
          onClick: (r) => copyToClipboard(r.is_cracked && r.password ? r.password : r.original_hash),
        },
      ]}
      aria-label={t('hashesTable.ariaLabel') as string}
      sx={{ mb: 2 }}
    />
  );
}
