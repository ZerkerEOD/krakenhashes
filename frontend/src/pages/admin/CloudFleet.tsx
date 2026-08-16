import React, { useEffect, useState } from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material';
import { DeleteForever as DeleteForeverIcon, Warning as WarningIcon } from '@mui/icons-material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { CloudInstance, CloudInstanceState } from '../../types/cloud';
import {
  destroyCloudInstance,
  listCloudInstances,
  formatCents,
  formatDuration,
  ttlRemainingSeconds,
} from '../../services/cloud';

const apiError = (err: any, fallback: string): string =>
  err?.response?.data?.error || err?.message || fallback;

const STATE_COLOR: Record<CloudInstanceState, 'default' | 'info' | 'success' | 'warning' | 'error'> = {
  requested: 'default',
  launching: 'info',
  provisioning: 'info',
  syncing: 'info',
  running: 'success',
  draining: 'warning',
  terminating: 'warning',
  terminated: 'default',
  failed: 'error',
};

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
  const { enqueueSnackbar } = useSnackbar();

  const [destroyTarget, setDestroyTarget] = useState<CloudInstance | null>(null);
  // Drives the local TTL countdown between server refreshes.
  const [, setTick] = useState(0);

  const { data: instances, isLoading, error } = useQuery<CloudInstance[]>({
    queryKey: ['cloudInstances'],
    queryFn: listCloudInstances,
    refetchInterval: 15_000,
  });

  useEffect(() => {
    const timer = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  const destroyMutation = useMutation({
    mutationFn: (id: string) => destroyCloudInstance(id),
    onSuccess: () => {
      enqueueSnackbar(t('cloud.fleet.destroyed') as string, { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['cloudInstances'] });
      setDestroyTarget(null);
    },
    onError: (err: any) => {
      // 502 means the provider refused and the instance IS STILL BILLING.
      // Surfaced verbatim rather than as a generic failure.
      enqueueSnackbar(apiError(err, t('cloud.fleet.destroyFailed') as string), {
        variant: 'error',
        persist: true,
      });
      setDestroyTarget(null);
    },
  });

  const live = instances ?? [];
  const stuckTeardown = live.filter((i) => i.terminate_attempts > 0);
  const totalHourly = live
    .filter((i) => i.state !== 'terminated' && i.state !== 'failed')
    .reduce((sum, i) => sum + i.hourly_rate_cents, 0);
  const totalReserved = live.reduce((sum, i) => sum + i.reserved_cents, 0);

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            {t('cloud.fleet.title') as string}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {t('cloud.fleet.description') as string}
          </Typography>
        </Box>
      </Box>

      {stuckTeardown.length > 0 && (
        <Alert severity="error" sx={{ mb: 2 }}>
          <AlertTitle>{t('cloud.fleet.teardownFailingTitle') as string}</AlertTitle>
          {t('cloud.fleet.teardownFailingBody', { count: stuckTeardown.length }) as string}
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

      {isLoading ? (
        <CircularProgress />
      ) : live.length === 0 ? (
        <Alert severity="success">{t('cloud.fleet.empty') as string}</Alert>
      ) : (
        <TableContainer component={Paper}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('cloud.fleet.columns.label') as string}</TableCell>
                <TableCell>{t('cloud.fleet.columns.state') as string}</TableCell>
                <TableCell>{t('cloud.fleet.columns.client') as string}</TableCell>
                <TableCell>{t('cloud.fleet.columns.gpu') as string}</TableCell>
                <TableCell align="right">{t('cloud.fleet.columns.rate') as string}</TableCell>
                <TableCell align="right">{t('cloud.fleet.columns.spend') as string}</TableCell>
                <TableCell align="right">{t('cloud.fleet.columns.ttl') as string}</TableCell>
                <TableCell align="right">{t('cloud.fleet.columns.disk') as string}</TableCell>
                <TableCell align="right">{t('cloud.fleet.columns.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {live.map((instance) => {
                const remaining = ttlRemainingSeconds(instance);
                const cost = instance.actual_cost_cents ?? instance.estimated_cost_cents;
                return (
                  <TableRow key={instance.id}>
                    <TableCell>
                      {instance.label}
                      {instance.terminate_attempts > 0 && (
                        <Tooltip
                          title={
                            instance.last_terminate_error ||
                            (t('cloud.fleet.teardownFailingTooltip') as string)
                          }
                        >
                          <WarningIcon fontSize="small" color="error" sx={{ ml: 1, verticalAlign: 'middle' }} />
                        </Tooltip>
                      )}
                    </TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        color={STATE_COLOR[instance.state] ?? 'default'}
                        label={t(`cloud.fleet.states.${instance.state}`) as string}
                      />
                    </TableCell>
                    <TableCell>{instance.client_name_snapshot || '—'}</TableCell>
                    <TableCell>
                      {instance.gpu_count ? `${instance.gpu_count}× ` : ''}
                      {instance.gpu_model || '—'}
                    </TableCell>
                    <TableCell align="right">
                      {t('cloud.fleet.perHour', { rate: formatCents(instance.hourly_rate_cents) }) as string}
                    </TableCell>
                    <TableCell align="right">
                      {formatCents(cost)}
                      {instance.actual_cost_cents === null ||
                      instance.actual_cost_cents === undefined ? (
                        <Tooltip title={t('cloud.fleet.estimatedTooltip') as string}>
                          <Typography variant="caption" color="text.secondary" sx={{ ml: 0.5 }}>
                            {t('cloud.fleet.estimated') as string}
                          </Typography>
                        </Tooltip>
                      ) : null}
                    </TableCell>
                    <TableCell align="right">
                      <Typography
                        variant="body2"
                        color={remaining > 0 && remaining < 300 ? 'error' : 'text.primary'}
                      >
                        {remaining > 0
                          ? formatDuration(remaining)
                          : (t('cloud.fleet.ttlExpired') as string)}
                      </Typography>
                    </TableCell>
                    <TableCell align="right">
                      {instance.disk_gb ? `${instance.disk_gb} GB` : '—'}
                    </TableCell>
                    <TableCell align="right">
                      <Button
                        size="small"
                        color="error"
                        startIcon={<DeleteForeverIcon />}
                        onClick={() => setDestroyTarget(instance)}
                      >
                        {t('cloud.fleet.destroy') as string}
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </TableContainer>
      )}

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
