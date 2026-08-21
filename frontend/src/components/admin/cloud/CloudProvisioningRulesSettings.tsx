import React, { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  CircularProgress,
  Divider,
  FormControlLabel,
  Grid,
  Slider,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import {
  getDefaultProvisioningRules,
  updateDefaultProvisioningRules,
} from '../../../services/cloud';
import { getMaxPriority } from '../../../services/systemSettings';
import { CloudProvisioningRules } from '../../../types/cloud';

/**
 * The system-default provisioning rules: WHEN the autoscaler may spend, as
 * opposed to the budget ladder's HOW MUCH.
 *
 * Edits the UNMERGED default deliberately. Per-client overrides are edited on
 * the client screen, where the merged view is the useful one.
 */

/** Dollars in the form, integer cents on the wire. */
const centsToDollars = (cents?: number | null): string =>
  cents === null || cents === undefined || cents === 0 ? '' : (cents / 100).toFixed(2);

const dollarsToCents = (value: string): number => {
  const parsed = parseFloat(value);
  return Number.isFinite(parsed) && parsed > 0 ? Math.round(parsed * 100) : 0;
};

/** Minutes in the form, seconds on the wire — nobody thinks in 900s. */
const secondsToMinutes = (seconds?: number | null): string =>
  seconds === null || seconds === undefined ? '' : String(Math.round(seconds / 60));

const minutesToSeconds = (value: string): number => {
  const parsed = parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed * 60 : 0;
};

/**
 * "HH:MM" for <input type="time"> from the backend's "HH:MM:SS", and back.
 * An empty pair means no window at all, which is distinct from start === end
 * ("always open") — both read as unrestricted, but only the first inherits.
 */
const toTimeInput = (value?: string | null): string => (value ? value.slice(0, 5) : '');
const fromTimeInput = (value: string): string | null => (value ? `${value}:00` : null);

const CloudProvisioningRulesSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const { enqueueSnackbar } = useSnackbar();
  const queryClient = useQueryClient();

  const { data: rules, isLoading, error } = useQuery({
    queryKey: ['cloud', 'rules', 'default'],
    queryFn: getDefaultProvisioningRules,
  });

  /*
   * The live priority ceiling. The floor below is stored as an ABSOLUTE
   * integer, so a value typed against the wrong ceiling silently matches
   * nothing — the marks are scaled from this rather than hardcoded to 0-100.
   */
  const { data: maxPriority } = useQuery({
    queryKey: ['settings', 'max-priority'],
    queryFn: getMaxPriority,
  });
  const ceiling = maxPriority?.max_priority ?? 1000;

  const [form, setForm] = useState<Partial<CloudProvisioningRules>>({});
  const [windowEnabled, setWindowEnabled] = useState(false);

  useEffect(() => {
    if (!rules) return;
    setForm(rules);
    setWindowEnabled(
      Boolean(rules.provisioning_window_start && rules.provisioning_window_end)
    );
  }, [rules]);

  const saveMutation = useMutation({
    mutationFn: updateDefaultProvisioningRules,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cloud', 'rules'] });
      enqueueSnackbar(t('cloud.rules.saved') as string, { variant: 'success' });
    },
    onError: (err: any) =>
      enqueueSnackbar(
        err?.response?.data?.error || (t('cloud.rules.saveFailed') as string),
        { variant: 'error' }
      ),
  });

  /**
   * Band marks scaled to the live ceiling. At 1000 "High" lands on 700; at 100
   * it lands on 70. Rendering fixed 0-100 marks against a 1000 ceiling is
   * exactly how an admin ends up setting a floor that disables everything.
   */
  const marks = useMemo(
    () =>
      [
        { fraction: 0, key: 'minimal' },
        { fraction: 0.1, key: 'low' },
        { fraction: 0.4, key: 'normal' },
        { fraction: 0.7, key: 'high' },
        { fraction: 0.9, key: 'critical' },
      ].map(({ fraction, key }) => ({
        value: Math.round(ceiling * fraction),
        label: t(`cloud.rules.bands.${key}`) as string,
      })),
    [ceiling, t]
  );

  if (isLoading) return <CircularProgress />;
  if (error) {
    return <Alert severity="error">{t('cloud.rules.loadFailed') as string}</Alert>;
  }

  const floor = form.min_job_priority ?? 0;

  const handleSave = () => {
    saveMutation.mutate({
      ...form,
      // A window is stored as a pair or not at all; the DB enforces that too.
      provisioning_window_start: windowEnabled
        ? fromTimeInput(toTimeInput(form.provisioning_window_start) || '00:00')
        : null,
      provisioning_window_end: windowEnabled
        ? fromTimeInput(toTimeInput(form.provisioning_window_end) || '00:00')
        : null,
    });
  };

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('cloud.rules.title') as string}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        {t('cloud.rules.description') as string}
      </Typography>

      <Alert severity="info" sx={{ mb: 3 }}>
        <AlertTitle>{t('cloud.rules.inheritTitle') as string}</AlertTitle>
        {t('cloud.rules.inheritBody') as string}
      </Alert>

      <Grid container spacing={3}>
        {/* Priority floor */}
        <Grid item xs={12}>
          <Typography gutterBottom>
            {t('cloud.rules.fields.minJobPriority') as string}
          </Typography>
          <Box sx={{ px: 2 }}>
            <Slider
              value={floor}
              min={0}
              max={ceiling}
              step={Math.max(1, Math.round(ceiling / 100))}
              marks={marks}
              valueLabelDisplay="on"
              onChange={(_e, v) =>
                setForm((prev) => ({ ...prev, min_job_priority: v as number }))
              }
            />
          </Box>
          <Typography variant="caption" color="text.secondary" display="block">
            {t('cloud.rules.helperText.minJobPriority', { ceiling }) as string}
          </Typography>
          {floor === 0 && (
            <Typography variant="caption" color="text.secondary" display="block">
              {t('cloud.rules.helperText.floorOff') as string}
            </Typography>
          )}
        </Grid>

        <Grid item xs={12}>
          <Divider />
        </Grid>

        {/* Minimum starvation */}
        <Grid item xs={12} sm={6}>
          <TextField
            fullWidth
            type="number"
            label={t('cloud.rules.fields.minStarvation') as string}
            value={secondsToMinutes(form.min_starvation_seconds)}
            onChange={(e) =>
              setForm((prev) => ({
                ...prev,
                min_starvation_seconds: minutesToSeconds(e.target.value),
              }))
            }
            inputProps={{ min: 0 }}
            helperText={t('cloud.rules.helperText.minStarvation') as string}
          />
        </Grid>

        {/* Skip if finishing soon */}
        <Grid item xs={12} sm={6}>
          <TextField
            fullWidth
            type="number"
            label={t('cloud.rules.fields.skipIfFinishing') as string}
            value={secondsToMinutes(form.skip_if_finishing_within_seconds)}
            onChange={(e) =>
              setForm((prev) => ({
                ...prev,
                skip_if_finishing_within_seconds: minutesToSeconds(e.target.value),
              }))
            }
            inputProps={{ min: 0 }}
            helperText={t('cloud.rules.helperText.skipIfFinishing') as string}
          />
        </Grid>

        {/* Per-job spend cap */}
        <Grid item xs={12} sm={6}>
          <TextField
            fullWidth
            label={t('cloud.rules.fields.maxSpendPerJob') as string}
            value={centsToDollars(form.max_spend_per_job_cents)}
            onChange={(e) =>
              setForm((prev) => ({
                ...prev,
                max_spend_per_job_cents: dollarsToCents(e.target.value),
              }))
            }
            InputProps={{ startAdornment: <Box sx={{ mr: 1 }}>$</Box> }}
            helperText={t('cloud.rules.helperText.maxSpendPerJob') as string}
          />
        </Grid>

        <Grid item xs={12}>
          <Divider />
        </Grid>

        {/* Provisioning window */}
        <Grid item xs={12}>
          <FormControlLabel
            control={
              <Switch
                checked={windowEnabled}
                onChange={(e) => setWindowEnabled(e.target.checked)}
              />
            }
            label={t('cloud.rules.fields.windowEnabled') as string}
          />
          <Typography variant="caption" color="text.secondary" display="block">
            {t('cloud.rules.helperText.windowEnabled') as string}
          </Typography>
        </Grid>

        {windowEnabled && (
          <>
            <Grid item xs={12} sm={4}>
              <TextField
                fullWidth
                type="time"
                label={t('cloud.rules.fields.windowStart') as string}
                value={toTimeInput(form.provisioning_window_start)}
                onChange={(e) =>
                  setForm((prev) => ({
                    ...prev,
                    provisioning_window_start: fromTimeInput(e.target.value),
                  }))
                }
                InputLabelProps={{ shrink: true }}
              />
            </Grid>
            <Grid item xs={12} sm={4}>
              <TextField
                fullWidth
                type="time"
                label={t('cloud.rules.fields.windowEnd') as string}
                value={toTimeInput(form.provisioning_window_end)}
                onChange={(e) =>
                  setForm((prev) => ({
                    ...prev,
                    provisioning_window_end: fromTimeInput(e.target.value),
                  }))
                }
                InputLabelProps={{ shrink: true }}
              />
            </Grid>
            <Grid item xs={12} sm={4}>
              <TextField
                fullWidth
                label={t('cloud.rules.fields.windowTz') as string}
                value={form.provisioning_window_tz ?? ''}
                onChange={(e) =>
                  setForm((prev) => ({
                    ...prev,
                    provisioning_window_tz: e.target.value || null,
                  }))
                }
                placeholder="UTC"
                helperText={t('cloud.rules.helperText.windowTz') as string}
              />
            </Grid>
            <Grid item xs={12}>
              <Alert severity="info">
                {t('cloud.rules.helperText.windowWrap') as string}
              </Alert>
            </Grid>
          </>
        )}

        <Grid item xs={12}>
          <Button
            variant="contained"
            onClick={handleSave}
            disabled={saveMutation.isPending}
          >
            {t('cloud.rules.save') as string}
          </Button>
        </Grid>
      </Grid>
    </Box>
  );
};

export default CloudProvisioningRulesSettings;
