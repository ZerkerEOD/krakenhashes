import React, { useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Box, Button, Chip, Grid, LinearProgress, Stack, Typography } from '@mui/material';
import ListAltIcon from '@mui/icons-material/ListAlt';
import LockIcon from '@mui/icons-material/Lock';
import WorkIcon from '@mui/icons-material/Work';
import DescriptionIcon from '@mui/icons-material/Description';
import GroupsIcon from '@mui/icons-material/Groups';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { GridColDef } from '@mui/x-data-grid';
import { DataTable, EmptyState, EntityLink, ErrorState, PageHeader, SectionCard, StatTile, StatusChip } from '../../components/ui';
import PageSkeleton from '../../components/ui/PageSkeleton';
import JobsDataTable from '../../components/jobs/JobsDataTable';
import ClientWordlistManagementDialog from '../../components/admin/ClientWordlistManagementDialog';
import { useLiveQuery } from '../../hooks/useLiveQuery';
import { useAuth } from '../../contexts/AuthContext';
import { api } from '../../services/api';
import { qk } from '../../services/queryKeys';
import { ROUTES } from '../../constants/routes';
import { isNotFound } from '../../utils/errors';
import { formatDateTime, formatRelativeTime } from '../../utils/formatters';
import type { Client } from '../../types/client';
import type { JobSummary, PaginationInfo } from '../../types/jobs';

interface ClientOverview {
  client: Client;
  teams: { id: string; name: string }[];
  stats: {
    hashlist_count: number;
    total_hashes: number;
    cracked_hashes: number;
    job_counts: Record<string, number>;
    wordlist_count: number;
    has_potfile: boolean;
    last_activity_at?: string;
  };
}

interface HashlistRow {
  id: string;
  name: string;
  status: string;
  total_hashes: number;
  cracked_hashes: number;
  createdAt: string;
  archived_at?: string | null;
}

const ClientDetail: React.FC = () => {
  const { id = '' } = useParams<{ id: string }>();
  const { t } = useTranslation('admin');
  const { userRole } = useAuth();
  const isAdmin = userRole === 'admin';
  const queryClient = useQueryClient();
  const [wordlistsOpen, setWordlistsOpen] = useState(false);
  const [hlPage, setHlPage] = useState({ page: 0, pageSize: 10 });
  const [jobPage, setJobPage] = useState({ page: 0, pageSize: 10 });

  const overview = useQuery({
    queryKey: qk.clients.overview(id),
    queryFn: async () => (await api.get<{ data: ClientOverview }>(`/api/clients/${id}/overview`)).data.data,
    enabled: Boolean(id),
  });

  const hlParams = { client_id: id, limit: hlPage.pageSize, offset: hlPage.page * hlPage.pageSize, sort_by: 'createdAt', order: 'desc', include_archived: 'true' };
  const hashlists = useQuery({
    queryKey: qk.hashlists.list(hlParams),
    queryFn: async () => (await api.get<{ data: HashlistRow[]; total_count: number }>('/api/hashlists', { params: hlParams })).data,
    enabled: Boolean(id) && overview.isSuccess,
    placeholderData: keepPreviousData,
  });

  const jobParams = { client_id: id, page: String(jobPage.page + 1), page_size: String(jobPage.pageSize), include_archived: 'true' };
  const jobs = useLiveQuery<{ jobs: JobSummary[]; pagination: PaginationInfo }>(
    {
      queryKey: qk.jobs.list(jobParams),
      queryFn: async () => (await api.get('/api/jobs', { params: jobParams })).data,
      enabled: Boolean(id) && overview.isSuccess,
      placeholderData: keepPreviousData,
    },
    { tier: 'list', when: (d) => Boolean(d?.jobs.some((j) => ['running', 'pending', 'preparing', 'paused'].includes(j.status))) }
  );

  const hashlistColumns = useMemo<GridColDef<HashlistRow>[]>(
    () => [
      { field: 'name', headerName: t('clients.detail.columns.name', 'Name') as string, flex: 1.5, minWidth: 180, renderCell: (p) => <EntityLink type="hashlist" id={p.row.id} label={p.row.name} /> },
      { field: 'status', headerName: t('clients.detail.columns.status', 'Status') as string, width: 150, renderCell: (p) => <StatusChip entity="hashlist" status={p.row.status} /> },
      { field: 'total_hashes', headerName: t('clients.detail.columns.total', 'Hashes') as string, width: 110, type: 'number', valueFormatter: (v: number) => (v ?? 0).toLocaleString() },
      {
        field: 'cracked_hashes',
        headerName: t('clients.detail.columns.cracked', 'Cracked') as string,
        width: 170,
        renderCell: (p) => {
          const pct = p.row.total_hashes > 0 ? Math.round((p.row.cracked_hashes / p.row.total_hashes) * 100) : 0;
          return (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, width: '100%' }}>
              <EntityLink type="pot_hashlist" id={p.row.id} label={(p.row.cracked_hashes ?? 0).toLocaleString()} />
              <LinearProgress variant="determinate" value={pct} color="success" sx={{ flexGrow: 1, height: 6, borderRadius: 3 }} />
              <Typography variant="caption" color="text.secondary">{pct}%</Typography>
            </Box>
          );
        },
      },
      { field: 'createdAt', headerName: t('clients.detail.columns.created', 'Created') as string, width: 160, valueFormatter: (v: string) => formatDateTime(v) },
    ],
    [t]
  );

  if (overview.isLoading) return <PageSkeleton variant="detail" />;
  if (overview.error) {
    return (
      <Box sx={{ p: 3 }}>
        {isNotFound(overview.error) ? (
          <EmptyState
            title={t('clients.detail.notFound', 'Client not found') as string}
            description={t('clients.detail.notFoundHint', 'It may have been deleted, or it belongs to a team you are not a member of.') as string}
            action={{ label: t('clients.detail.backToClients', 'Back to clients') as string, to: ROUTES.clients }}
          />
        ) : (
          <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
        )}
      </Box>
    );
  }

  const data = overview.data!;
  const { client, stats, teams } = data;
  const crackPct = stats.total_hashes > 0 ? (stats.cracked_hashes / stats.total_hashes) * 100 : 0;
  const runningJobs = (stats.job_counts.running ?? 0) + (stats.job_counts.pending ?? 0);
  const totalJobs = Object.values(stats.job_counts).reduce((a, b) => a + b, 0);

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={client.name}
        description={client.description || undefined}
        backTo={ROUTES.clients}
        breadcrumbs={[{ label: t('clients.detail.breadcrumb', 'Clients') as string, to: ROUTES.clients }, { label: client.name }]}
        actions={
          isAdmin ? (
            <Button variant="outlined" startIcon={<DescriptionIcon />} onClick={() => setWordlistsOpen(true)}>
              {t('clients.detail.manageWordlists', 'Wordlists & potfile') as string}
            </Button>
          ) : undefined
        }
      />

      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={6} md={3}>
          <StatTile label={t('clients.detail.stats.hashlists', 'Hashlists') as string} value={stats.hashlist_count.toLocaleString()} icon={<ListAltIcon />} />
        </Grid>
        <Grid item xs={6} md={3}>
          <StatTile
            label={t('clients.detail.stats.cracked', 'Cracked') as string}
            value={`${stats.cracked_hashes.toLocaleString()} / ${stats.total_hashes.toLocaleString()}`}
            caption={`${crackPct.toFixed(1)}%`}
            icon={<LockIcon />}
            tone="success"
            to={stats.cracked_hashes > 0 ? ROUTES.potClient(client.id) : undefined}
          />
        </Grid>
        <Grid item xs={6} md={3}>
          <StatTile
            label={t('clients.detail.stats.jobs', 'Jobs') as string}
            value={totalJobs.toLocaleString()}
            caption={runningJobs > 0 ? (t('clients.detail.stats.active', '{{count}} active', { count: runningJobs }) as string) : undefined}
            icon={<WorkIcon />}
            tone={runningJobs > 0 ? 'running' : 'neutral'}
          />
        </Grid>
        <Grid item xs={6} md={3}>
          <StatTile
            label={t('clients.detail.stats.wordlists', 'Wordlists') as string}
            value={stats.wordlist_count.toLocaleString()}
            caption={stats.has_potfile ? (t('clients.detail.stats.hasPotfile', 'Client potfile present') as string) : undefined}
            icon={<DescriptionIcon />}
          />
        </Grid>
      </Grid>

      <Grid container spacing={3}>
        <Grid item xs={12} lg={8}>
          <Stack spacing={3}>
            <SectionCard title={t('clients.detail.hashlists', 'Hashlists') as string} flush>
              <DataTable<HashlistRow>
                flat
                rows={hashlists.data?.data ?? []}
                columns={hashlistColumns}
                loading={hashlists.isLoading}
                error={hashlists.error}
                onRetry={() => void hashlists.refetch()}
                pagination={{ mode: 'server', page: hlPage.page, pageSize: hlPage.pageSize, rowCount: hashlists.data?.total_count ?? 0, pageSizeOptions: [10, 25, 50], onChange: (m) => setHlPage(m) }}
                sorting={false}
                rowClassName={(r) => (r.archived_at ? 'kh-row-muted' : undefined)}
                emptyState={{ title: t('clients.detail.noHashlists', 'No hashlists for this client yet') as string }}
              />
            </SectionCard>
            <SectionCard title={t('clients.detail.jobs', 'Jobs') as string} flush>
              <JobsDataTable
                jobs={jobs.data?.jobs ?? []}
                loading={jobs.isLoading}
                fetching={jobs.isFetching && !jobs.isLoading}
                error={jobs.error}
                onRetry={() => void jobs.refetch()}
                onChanged={() => void queryClient.invalidateQueries({ queryKey: qk.jobs.all })}
                compact
        flat
                pagination={{ mode: 'server', page: jobPage.page, pageSize: jobPage.pageSize, rowCount: jobs.data?.pagination.total ?? 0, pageSizeOptions: [10, 25, 50], onChange: (m) => setJobPage(m) }}
                emptyState={{ title: t('clients.detail.noJobs', 'No jobs have run for this client') as string }}
              />
            </SectionCard>
          </Stack>
        </Grid>
        <Grid item xs={12} lg={4}>
          <Stack spacing={3}>
            <SectionCard title={t('clients.detail.teams', 'Teams') as string} icon={<GroupsIcon fontSize="small" />}>
              {teams.length === 0 ? (
                <Typography variant="body2" color="text.secondary">{t('clients.detail.noTeams', 'Not assigned to any team') as string}</Typography>
              ) : (
                <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                  {teams.map((team) => (
                    <Chip key={team.id} variant="outlined" label={<EntityLink type="team" id={team.id} label={team.name} />} />
                  ))}
                </Box>
              )}
            </SectionCard>
            <SectionCard title={t('clients.detail.policy', 'Retention & potfile') as string}>
              <Stack spacing={1}>
                <Row label={t('clients.detail.retention', 'Data retention') as string}
                  value={client.dataRetentionMonths == null ? (t('clients.detail.retentionDefault', 'System default') as string) : client.dataRetentionMonths === 0 ? (t('clients.detail.retentionForever', 'Keep forever') as string) : (t('clients.detail.retentionMonths', '{{count}} months', { count: client.dataRetentionMonths }) as string)} />
                <Row label={t('clients.detail.globalPotfile', 'Global potfile') as string} value={client.exclude_from_potfile ? (t('clients.detail.excluded', 'Excluded') as string) : (t('clients.detail.included', 'Included') as string)} />
                <Row label={t('clients.detail.clientPotfile', 'Client potfile') as string} value={client.exclude_from_client_potfile ? (t('clients.detail.excluded', 'Excluded') as string) : (t('clients.detail.included', 'Included') as string)} />
                {client.contactInfo && <Row label={t('clients.detail.contact', 'Contact') as string} value={client.contactInfo} />}
                {stats.last_activity_at && (
                  <Row label={t('clients.detail.lastActivity', 'Last activity') as string} value={formatRelativeTime(stats.last_activity_at)} />
                )}
              </Stack>
            </SectionCard>
          </Stack>
        </Grid>
      </Grid>

      {isAdmin && (
        <ClientWordlistManagementDialog
          open={wordlistsOpen}
          client={client}
          onClose={() => {
            setWordlistsOpen(false);
            void queryClient.invalidateQueries({ queryKey: qk.clients.overview(id) });
          }}
        />
      )}
    </Box>
  );
};

const Row: React.FC<{ label: string; value: React.ReactNode }> = ({ label, value }) => (
  <Box sx={{ display: 'flex', justifyContent: 'space-between', gap: 2 }}>
    <Typography variant="body2" color="text.secondary">{label}</Typography>
    <Typography variant="body2" sx={{ textAlign: 'right' }}>{value}</Typography>
  </Box>
);

export default ClientDetail;
