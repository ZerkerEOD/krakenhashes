import React, { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Link,
  Tooltip,
  Typography,
} from '@mui/material';
import { DeleteForever as DeleteForeverIcon, Warning as WarningIcon } from '@mui/icons-material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { GridColDef } from '@mui/x-data-grid';
import { DataTable, EntityLink, PageHeader, StatusChip, useToast } from '../../components/ui';
import { useTranslation } from 'react-i18next';
import {
  CloudInstance,
  CloudProviderConfig,
  CLOUD_DISCORD_URL,
  CLOUD_ISSUE_URL,
} from '../../types/cloud';
import {
  destroyCloudInstance,
  listCloudInstances,
  listCloudProviders,
  formatCents,
  formatDuration,
  ttlRemainingSeconds,
} from '../../services/cloud';

const apiError = (err: any, fallback: string): string =>
  err?.response?.data?.error || err?.message || fallback;

/**
 * Live rented GPU fleet.
 *
 * Every row here is money leaving the account, so the page refreshes on a
 * timer and the TTL column ticks locally between refreshes — a stale countdown
 * on this page reads as "plenty of time left" when it isn't.
 */
const CloudFleet: React.FC = () => {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const toast = useToast();

  const [destroyTarget, setDestroyTarget] = useState<CloudInstance | null>(null);
  // Drives the local TTL countdown between server refreshes.
  const [, setTick] = useState(0);

  const { data: instances, isLoading, error } = useQuery<CloudInstance[]>({
    queryKey: ['cloudInstances'],
    queryFn: listCloudInstances,
    refetchInterval: 15_000,
  });

  /*
   * Providers are fetched only to resolve each instance's maturity. An
   * instance row carries provider_config_id but not the provider kind, and
   * joining it server-side would mean widening the instance query, the scanner
   * and the model for a label. The provider list is small, changes rarely and
   * is already cached by the settings screen.
   *
   * No refetchInterval: unlike the fleet itself, this is configuration.
   */
  const { data: providerList } = useQuery({
    queryKey: ['cloudProviders'],
    queryFn: listCloudProviders,
    staleTime: 60_000,
  });

  const providerByConfig = useMemo(() => {
    const map = new Map<string, CloudProviderConfig>();
    (providerList?.providers ?? []).forEach((p) => map.set(p.id, p));
    return map;
  }, [providerList]);

  useEffect(() => {
    const timer = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  const destroyMutation = useMutation({
    mutationFn: (id: string) => destroyCloudInstance(id),
    onSuccess: () => {
      toast.success(t('cloud.fleet.destroyed') as string);
      queryClient.invalidateQueries({ queryKey: ['cloudInstances'] });
      setDestroyTarget(null);
    },
    onError: (err: any) => {
      // 502 means the provider refused and the instance IS STILL BILLING.
      // Surfaced verbatim rather than as a generic failure.
      toast.error(apiError(err, t('cloud.fleet.destroyFailed') as string), { persist: true });
      setDestroyTarget(null);
    },
  });

  const live = instances ?? [];
  const stuckTeardown = live.filter((i) => i.terminate_attempts > 0);
  /*
   * Instances on a provider nobody has yet proven with real money. The warning
   * belongs HERE rather than only on the settings screen: an operator ticks a
   * provider once and then lives on this page, so a caveat that only appears at
   * configuration time is a caveat nobody re-reads while money is being spent.
   *
   * Only the unproven state is marked, unlike the provider settings table: a
   * Tested chip on every AWS row would be noise on a page that is watched
   * continuously rather than read once.
   */
  const experimentalLive = live.filter(
    (i) => providerByConfig.get(i.provider_config_id)?.maturity === 'experimental'
  );
  const experimentalProviderNames = Array.from(
    new Set(
      experimentalLive.map((i) => providerByConfig.get(i.provider_config_id)?.name).filter(Boolean)
    )
  ).join(', ');
  const totalHourly = live
    .filter((i) => i.state !== 'terminated' && i.state !== 'failed')
    .reduce((sum, i) => sum + i.hourly_rate_cents, 0);
  const totalReserved = live.reduce((sum, i) => sum + i.reserved_cents, 0);

  const columns: GridColDef<CloudInstance>[] = [
    {
      field: 'label',
      headerName: t('cloud.fleet.columns.label') as string,
      flex: 1.2,
      minWidth: 200,
      renderCell: (p) => {
        const instance = p.row;
        const provider = providerByConfig.get(instance.provider_config_id);
        return (
          <Box sx={{ display: 'flex', alignItems: 'center' }}>
            {instance.label}
            {provider?.maturity === 'experimental' && (
              <Tooltip title={t('cloud.fleet.experimentalTooltip', { provider: provider.name }) as string}>
                <Chip
                  size="small"
                  variant="outlined"
                  color="warning"
                  label={t('cloud.providers.experimentalChip') as string}
                  sx={{ ml: 1, height: 18, fontSize: '0.65rem' }}
                />
              </Tooltip>
            )}
            {instance.terminate_attempts > 0 && (
              <Tooltip
                title={instance.last_terminate_error || (t('cloud.fleet.teardownFailingTooltip') as string)}
              >
                <WarningIcon fontSize="small" color="error" sx={{ ml: 1, verticalAlign: 'middle' }} />
              </Tooltip>
            )}
          </Box>
        );
      },
    },
    {
      field: 'state',
      headerName: t('cloud.fleet.columns.state') as string,
      width: 130,
      renderCell: (p) => (
        <StatusChip
          entity="cloud"
          status={p.row.state}
          label={t(`cloud.fleet.states.${p.row.state}`) as string}
        />
      ),
    },
    {
      field: 'client_name_snapshot',
      headerName: t('cloud.fleet.columns.client') as string,
      flex: 1,
      minWidth: 140,
      renderCell: (p) =>
        p.row.client_id ? (
          <EntityLink type="client" id={p.row.client_id} label={p.row.client_name_snapshot || p.row.client_id} />
        ) : (
          p.row.client_name_snapshot || '—'
        ),
    },
    {
      field: 'agent_id',
      headerName: t('cloud.fleet.columns.agent', 'Agent') as string,
      width: 110,
      renderCell: (p) =>
        p.row.agent_id ? <EntityLink type="agent" id={p.row.agent_id} /> : '—',
    },
    {
      field: 'job_execution_id',
      headerName: t('cloud.fleet.columns.job', 'Job') as string,
      width: 130,
      renderCell: (p) =>
        p.row.job_execution_id ? (
          <EntityLink type="job" id={p.row.job_execution_id} label={p.row.job_execution_id.slice(0, 8)} mono />
        ) : (
          '—'
        ),
    },
    {
      field: 'gpu_model',
      headerName: t('cloud.fleet.columns.gpu') as string,
      flex: 1,
      minWidth: 140,
      valueGetter: (_v, row) => `${row.gpu_count ? `${row.gpu_count}× ` : ''}${row.gpu_model || '—'}`,
    },
    {
      field: 'hourly_rate_cents',
      headerName: t('cloud.fleet.columns.rate') as string,
      width: 110,
      align: 'right',
      headerAlign: 'right',
      renderCell: (p) => t('cloud.fleet.perHour', { rate: formatCents(p.row.hourly_rate_cents) }) as string,
    },
    {
      field: 'cost',
      headerName: t('cloud.fleet.columns.spend') as string,
      width: 130,
      align: 'right',
      headerAlign: 'right',
      valueGetter: (_v, row) => row.actual_cost_cents ?? row.estimated_cost_cents,
      renderCell: (p) => (
        <>
          {formatCents(p.row.actual_cost_cents ?? p.row.estimated_cost_cents)}
          {p.row.actual_cost_cents === null || p.row.actual_cost_cents === undefined ? (
            <Tooltip title={t('cloud.fleet.estimatedTooltip') as string}>
              <Typography variant="caption" color="text.secondary" sx={{ ml: 0.5 }}>
                {t('cloud.fleet.estimated') as string}
              </Typography>
            </Tooltip>
          ) : null}
        </>
      ),
    },
    {
      field: 'ttl',
      headerName: t('cloud.fleet.columns.ttl') as string,
      width: 110,
      align: 'right',
      headerAlign: 'right',
      sortable: false,
      renderCell: (p) => {
        const remaining = ttlRemainingSeconds(p.row);
        return (
          <Typography
            variant="body2"
            component="span"
            color={remaining > 0 && remaining < 300 ? 'error' : 'text.primary'}
          >
            {remaining > 0 ? formatDuration(remaining) : (t('cloud.fleet.ttlExpired') as string)}
          </Typography>
        );
      },
    },
    {
      field: 'disk_gb',
      headerName: t('cloud.fleet.columns.disk') as string,
      width: 90,
      align: 'right',
      headerAlign: 'right',
      valueFormatter: (v) => (v ? `${v} GB` : '—'),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader title={t('cloud.fleet.title') as string} description={t('cloud.fleet.description') as string} />

      {stuckTeardown.length > 0 && (
        <Alert severity="error" sx={{ mb: 2 }}>
          <AlertTitle>{t('cloud.fleet.teardownFailingTitle') as string}</AlertTitle>
          {t('cloud.fleet.teardownFailingBody', { count: stuckTeardown.length }) as string}
        </Alert>
      )}

      {experimentalLive.length > 0 && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          <AlertTitle>
            {
              t('cloud.fleet.experimentalRunningTitle', {
                count: experimentalLive.length,
              }) as string
            }
          </AlertTitle>
          {
            t('cloud.fleet.experimentalRunningBody', {
              providers: experimentalProviderNames,
            }) as string
          }
          <Box sx={{ mt: 1 }}>
            <Link href={CLOUD_ISSUE_URL} target="_blank" rel="noopener noreferrer">
              {t('cloud.providers.reportIssueLink') as string}
            </Link>
            {' · '}
            <Link href={CLOUD_DISCORD_URL} target="_blank" rel="noopener noreferrer">
              {t('cloud.providers.reportDiagnosticsLink') as string}
            </Link>
          </Box>
        </Alert>
      )}

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {apiError(error, t('cloud.fleet.loadFailed') as string)}
        </Alert>
      )}

      {live.length > 0 && (
        <Alert severity="info" sx={{ mb: 2 }}>
          {t('cloud.fleet.summary', {
            count: live.length,
            hourly: formatCents(totalHourly),
            reserved: formatCents(totalReserved),
          }) as string}
        </Alert>
      )}

      <DataTable<CloudInstance>
        rows={live}
        columns={columns}
        getRowId={(r) => r.id}
        loading={isLoading}
        pagination={false}
        sorting={{ mode: 'client' }}
        rowActions={() => [
          {
            key: 'destroy',
            label: t('cloud.fleet.destroy') as string,
            icon: <DeleteForeverIcon fontSize="small" />,
            danger: true,
            placement: 'inline',
            onClick: (r) => setDestroyTarget(r),
          },
        ]}
        emptyState={
          <Alert severity="success" sx={{ m: 2 }}>
            {t('cloud.fleet.empty') as string}
          </Alert>
        }
        tableKey="cloud-fleet"
      />

      <Dialog open={Boolean(destroyTarget)} onClose={() => setDestroyTarget(null)}>
        <DialogTitle>{t('cloud.fleet.destroyTitle') as string}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t('cloud.fleet.destroyBody', { label: destroyTarget?.label }) as string}
          </DialogContentText>
          {destroyTarget?.job_execution_id && (
            <Alert severity="warning" sx={{ mt: 2 }}>
              {t('cloud.fleet.destroyJobWarning') as string}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDestroyTarget(null)}>
            {t('buttons.cancel', { ns: 'common' }) as string}
          </Button>
          <Button
            color="error"
            variant="contained"
            disabled={destroyMutation.isPending}
            onClick={() => destroyTarget && destroyMutation.mutate(destroyTarget.id)}
          >
            {t('cloud.fleet.destroy') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default CloudFleet;
