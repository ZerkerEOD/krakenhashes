import { useEffect, useMemo, useState } from 'react';
import { Box, LinearProgress, Typography } from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import { keepPreviousData } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../services/api';
import { viewParams, DashboardView } from '../../services/dashboard';
import { qk } from '../../services/queryKeys';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { ROUTES } from '../../constants/routes';
import { DataTable, EntityLink, SectionCard, StatusChip, readTablePrefs } from '../ui';

interface Hashlist {
  id: string;
  name: string;
  status: 'uploading' | 'processing' | 'ready' | 'error';
  total_hashes: number;
  cracked_hashes: number;
  clientName?: string;
  client_id?: string;
  exclude_from_potfile?: boolean;
}

interface UserHashlistsResponse {
  data: Hashlist[];
  total_count: number;
  limit: number;
  offset: number;
}

export interface HashlistOverviewProps {
  /** Dashboard view: `mine` lists the user's own hashlists, `teams`/`all` the team-scoped list. */
  view?: DashboardView;
  /** The app-bar team (sent as `team_id` for mine/teams). */
  teamId?: string | null;
}

const TABLE_KEY = 'dashboard.hashlists';
const PAGE_SIZES = [5, 10, 25];

const crackPercentage = (h: Hashlist) => (h.total_hashes > 0 ? Math.round((h.cracked_hashes / h.total_hashes) * 100) : 0);

/** Dashboard widget: hashlists in the selected view with crack progress (server-paginated). */
export default function HashlistOverview({ view = 'mine', teamId }: HashlistOverviewProps = {}) {
  const { t } = useTranslation('dashboard');
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(() => {
    const saved = readTablePrefs(TABLE_KEY).pageSize;
    return saved && PAGE_SIZES.includes(saved) ? saved : PAGE_SIZES[0];
  });
  useEffect(() => setPage(0), [view, teamId]);

  const query = useLiveQuery<UserHashlistsResponse>(
    {
      queryKey: qk.dashboard.hashlists(view, { teamId: teamId ?? '', page, pageSize }),
      queryFn: async () => {
        const params: Record<string, string | number> = { ...viewParams(view, teamId), limit: pageSize, offset: page * pageSize };
        // Mine = hashlists I uploaded; teams/all = the team-scoped hashlist list.
        const url = view === 'mine' ? '/api/user/hashlists' : '/api/hashlists';
        const res = await api.get<UserHashlistsResponse>(url, { params });
        return res.data;
      },
      placeholderData: keepPreviousData,
    },
    { tier: 'fast' }
  );

  const hashlists = query.data?.data || [];
  const totalCount = query.data?.total_count || 0;

  const columns = useMemo<GridColDef<Hashlist>[]>(
    () => [
      {
        field: 'name',
        headerName: t('hashlistOverview.columns.name') as string,
        flex: 1.4,
        minWidth: 160,
        renderCell: (p) => <EntityLink type="hashlist" id={p.row.id} label={p.row.name} />,
      },
      {
        field: 'clientName',
        headerName: t('hashlistOverview.columns.client') as string,
        flex: 1,
        minWidth: 120,
        renderCell: (p) =>
          p.row.client_id && p.row.clientName ? (
            <EntityLink type="client" id={p.row.client_id} label={p.row.clientName} />
          ) : (
            p.row.clientName || '-'
          ),
      },
      {
        field: 'status',
        headerName: t('hashlistOverview.columns.status') as string,
        width: 120,
        renderCell: (p) => (
          <StatusChip entity="hashlist" status={p.row.status} label={t(`hashlistOverview.status.${p.row.status}`) as string} />
        ),
      },
      {
        field: 'total_hashes',
        headerName: t('hashlistOverview.columns.total') as string,
        type: 'number',
        width: 110,
        valueFormatter: (v: number) => (v ?? 0).toLocaleString(),
      },
      {
        field: 'cracked_hashes',
        headerName: t('hashlistOverview.columns.cracked') as string,
        type: 'number',
        width: 110,
        renderCell: (p) => <EntityLink type="pot_hashlist" id={p.row.id} label={(p.row.cracked_hashes ?? 0).toLocaleString()} />,
      },
      {
        field: 'progress',
        headerName: t('hashlistOverview.columns.progress') as string,
        width: 160,
        renderCell: (p) => {
          const pct = crackPercentage(p.row);
          return (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, width: '100%' }}>
              <LinearProgress variant="determinate" value={pct} sx={{ flexGrow: 1, height: 6, borderRadius: 3 }} />
              <Typography variant="caption" sx={{ minWidth: 35 }}>
                {pct}%
              </Typography>
            </Box>
          );
        },
      },
    ],
    [t]
  );

  return (
    <SectionCard title={t('hashlistOverview.title')} flush>
      <DataTable<Hashlist>
        flat
        rows={hashlists}
        columns={columns}
        loading={query.isLoading}
        fetching={query.isFetching && !query.isLoading}
        error={query.error ? (t('hashlistOverview.loadError') as string) : undefined}
        onRetry={() => void query.refetch()}
        pagination={{
          mode: 'server',
          page,
          pageSize,
          rowCount: totalCount,
          pageSizeOptions: PAGE_SIZES,
          onChange: (m) => {
            setPage(m.pageSize !== pageSize ? 0 : m.page);
            setPageSize(m.pageSize);
          },
        }}
        tableKey={TABLE_KEY}
        sorting={false}
        rowLinkTo={(row) => ROUTES.hashlist(row.id)}
        emptyState={{ title: t('hashlistOverview.noHashlists') as string }}
      />
    </SectionCard>
  );
}
