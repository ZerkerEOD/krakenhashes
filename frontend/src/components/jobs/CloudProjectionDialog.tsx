import React, { useState } from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  LinearProgress,
  Typography,
} from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { CloudProjection } from '../../types/cloud';
import { getJobProjection, formatCents, formatDuration } from '../../services/cloud';
import { SimpleTable } from '../ui';

interface ProjectionRow {
  key: string;
  label: string;
  value: React.ReactNode;
}

interface Props {
  open: boolean;
  jobId: string;
  clientId?: string;
  /** Speed and price of the capacity being considered, for a what-if. */
  cloudSpeed?: number;
  hourlyRateCents?: number;
  onClose: () => void;
  onConfirm: () => void;
}

const apiError = (err: any, fallback: string): string =>
  err?.response?.data?.error || err?.message || fallback;

/**
 * Pre-launch projection: will this job finish before the budget runs out?
 *
 * The coverage bar is the single number that matters — the percentage of the
 * remaining work the budget can pay to complete. Below 100 the operator must
 * tick an explicit acknowledgement before launching. NPK, which this borrows
 * from, had the equivalent warning commented out, which turned their spend cap
 * into a silent killer; partial progress is still worth buying, but only when
 * it is chosen deliberately.
 */
const CloudProjectionDialog: React.FC<Props> = ({
  open,
  jobId,
  clientId,
  cloudSpeed,
  hourlyRateCents,
  onClose,
  onConfirm,
}) => {
  const { t } = useTranslation('jobs');
  const [acknowledged, setAcknowledged] = useState(false);

  const { data, isLoading, error } = useQuery<CloudProjection>({
    queryKey: ['cloudProjection', jobId, cloudSpeed, hourlyRateCents, clientId],
    queryFn: () => getJobProjection(jobId, { cloudSpeed, hourlyRateCents, clientId }),
    enabled: open,
  });

  const coverage = data?.coverage_pct ?? 0;
  const shortfall = Boolean(data) && !data!.will_finish;
  const canConfirm = Boolean(data) && (!shortfall || acknowledged);

  const barColor = coverage >= 100 ? 'success' : coverage >= 50 ? 'warning' : 'error';

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{t('cloud.projection.title') as string}</DialogTitle>
      <DialogContent>
        {isLoading && <CircularProgress />}

        {error && (
          <Alert severity="error">
            {apiError(error, t('cloud.projection.loadFailed') as string)}
          </Alert>
        )}

        {data && (
          <>
            <Box sx={{ mb: 3 }}>
              <Typography variant="subtitle2" gutterBottom>
                {t('cloud.projection.coverageLabel') as string}
              </Typography>
              <LinearProgress
                variant="determinate"
                value={Math.min(coverage, 100)}
                color={barColor}
                sx={{ height: 12, borderRadius: 1 }}
              />
              <Typography variant="caption" color="text.secondary">
                {t('cloud.projection.coverageValue', { pct: coverage.toFixed(1) }) as string}
              </Typography>
            </Box>

            <SimpleTable<ProjectionRow>
              hideHeader
              getRowKey={(r) => r.key}
              columns={[
                { field: 'label', headerName: t('cloud.projection.metric', 'Metric') as string },
                {
                  field: 'value',
                  headerName: t('cloud.projection.value', 'Value') as string,
                  align: 'right',
                  render: (r) => r.value,
                },
              ]}
              rows={[
                {
                  key: 'time',
                  label: t('cloud.projection.timeToFinish') as string,
                  // Zero throughput means unknown, never "instant".
                  value:
                    data.time_to_finish_seconds > 0
                      ? formatDuration(data.time_to_finish_seconds)
                      : (t('cloud.projection.unknown') as string),
                },
                {
                  key: 'cost',
                  label: t('cloud.projection.projectedCost') as string,
                  value: formatCents(data.projected_cost_cents),
                },
                {
                  key: 'budget',
                  label: t('cloud.projection.availableBudget') as string,
                  value: formatCents(data.available_cents),
                },
                {
                  key: 'onprem',
                  label: t('cloud.projection.onpremSpeed') as string,
                  value: `${data.onprem_speed.toLocaleString()} H/s`,
                },
                {
                  key: 'cloud',
                  label: t('cloud.projection.cloudSpeed') as string,
                  value: `${data.cloud_speed.toLocaleString()} H/s`,
                },
              ]}
            />

            {data.pessimistic && (
              <Alert severity="info" sx={{ mt: 2 }}>
                {t('cloud.projection.pessimistic') as string}
              </Alert>
            )}

            {data.note && (
              <Alert severity="info" sx={{ mt: 2 }}>
                {data.note}
              </Alert>
            )}

            {shortfall && (
              <Alert severity="warning" sx={{ mt: 2 }}>
                <AlertTitle>{t('cloud.projection.shortfallTitle') as string}</AlertTitle>
                {t('cloud.projection.shortfallBody', { pct: coverage.toFixed(1) }) as string}
                <FormControlLabel
                  sx={{ mt: 1, display: 'block' }}
                  control={
                    <Checkbox
                      checked={acknowledged}
                      onChange={(e) => setAcknowledged(e.target.checked)}
                    />
                  }
                  label={t('cloud.projection.shortfallAcknowledge') as string}
                />
              </Alert>
            )}
          </>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('buttons.cancel', { ns: 'common' }) as string}</Button>
        <Button variant="contained" disabled={!canConfirm} onClick={onConfirm}>
          {t('cloud.projection.confirm') as string}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

export default CloudProjectionDialog;
