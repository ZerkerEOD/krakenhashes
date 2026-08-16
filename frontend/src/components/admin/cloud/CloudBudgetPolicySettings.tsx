import React, { useEffect, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  FormControlLabel,
  Grid,
  TextField,
  Typography,
} from '@mui/material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import { CloudBudgetPolicy } from '../../../types/cloud';
import { getDefaultBudgetPolicy, updateDefaultBudgetPolicy } from '../../../services/cloud';

const apiError = (err: any, fallback: string): string =>
  err?.response?.data?.error || err?.message || fallback;

/**
 * The system-default spend threshold ladder.
 *
 * Every threshold is configurable because there is no universally right
 * answer: "stop at 99%, hard stop at 100%, never notify me" is as legitimate
 * as "warn me at 50%". Note the ladder only decides how gracefully the cap is
 * reached — what actually prevents an overrun is reservation accounting, which
 * commits an instance's whole TTL cost before it boots.
 */
const CloudBudgetPolicySettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

  const [policy, setPolicy] = useState<CloudBudgetPolicy | null>(null);
  const [notifyEnabled, setNotifyEnabled] = useState(true);
  const [formError, setFormError] = useState<string | null>(null);

  const { data, isLoading, error } = useQuery<CloudBudgetPolicy>({
    queryKey: ['cloudDefaultPolicy'],
    queryFn: getDefaultBudgetPolicy,
  });

  useEffect(() => {
    if (!data) return;
    setPolicy(data);
    setNotifyEnabled(data.notify_pct !== null && data.notify_pct !== undefined);
  }, [data]);

  const saveMutation = useMutation({
    mutationFn: (p: CloudBudgetPolicy) => updateDefaultBudgetPolicy(p),
    onSuccess: () => {
      enqueueSnackbar(t('cloud.policy.saved') as string, { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['cloudDefaultPolicy'] });
    },
    onError: (err: any) => setFormError(apiError(err, t('cloud.policy.saveFailed') as string)),
  });

  const handleSave = () => {
    if (!policy) return;
    setFormError(null);

    const notify = notifyEnabled ? policy.notify_pct ?? 0 : null;
    // Mirrors the DB CHECK, so the admin gets a readable message instead of a
    // constraint-violation string.
    if (notify !== null && (notify <= 0 || notify > policy.stop_provision_pct)) {
      setFormError(t('cloud.policy.errors.notifyRange') as string);
      return;
    }
    if (policy.stop_provision_pct > policy.drain_pct) {
      setFormError(t('cloud.policy.errors.stopBeforeDrain') as string);
      return;
    }
    if (policy.drain_pct > policy.hard_stop_pct) {
      setFormError(t('cloud.policy.errors.drainBeforeHardStop') as string);
      return;
    }
    if (policy.drain_timeout_seconds < 0) {
      setFormError(t('cloud.policy.errors.negativeTimeout') as string);
      return;
    }

    saveMutation.mutate({ ...policy, notify_pct: notify });
  };

  const numberField = (
    field: keyof CloudBudgetPolicy,
    labelKey: string,
    helpKey: string,
    max = 500
  ) => (
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        type="number"
        label={t(labelKey) as string}
        value={(policy?.[field] as number) ?? 0}
        onChange={(e) =>
          setPolicy((p) => (p ? { ...p, [field]: parseInt(e.target.value, 10) || 0 } : p))
        }
        helperText={t(helpKey) as string}
        inputProps={{ min: 0, max }}
      />
    </Grid>
  );

  if (isLoading) return <CircularProgress />;
  if (error) {
    return <Alert severity="error">{apiError(error, t('cloud.policy.loadFailed') as string)}</Alert>;
  }
  if (!policy) return null;

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('cloud.policy.title') as string}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        {t('cloud.policy.description') as string}
      </Typography>

      {formError && <Alert severity="error" sx={{ mb: 2 }}>{formError}</Alert>}

      <Grid container spacing={2}>
        <Grid item xs={12}>
          <FormControlLabel
            control={
              <Checkbox
                checked={notifyEnabled}
                onChange={(e) => setNotifyEnabled(e.target.checked)}
              />
            }
            label={t('cloud.policy.fields.notifyEnabled') as string}
          />
        </Grid>

        {notifyEnabled && (
          <Grid item xs={12} sm={6}>
            <TextField
              fullWidth
              type="number"
              label={t('cloud.policy.fields.notifyPct') as string}
              value={policy.notify_pct ?? 0}
              onChange={(e) =>
                setPolicy({ ...policy, notify_pct: parseInt(e.target.value, 10) || 0 })
              }
              helperText={t('cloud.policy.fields.notifyPctHelp') as string}
              inputProps={{ min: 1, max: 100 }}
            />
          </Grid>
        )}

        {numberField(
          'stop_provision_pct',
          'cloud.policy.fields.stopProvisionPct',
          'cloud.policy.fields.stopProvisionPctHelp',
          100
        )}
        {numberField(
          'drain_pct',
          'cloud.policy.fields.drainPct',
          'cloud.policy.fields.drainPctHelp',
          100
        )}
        {numberField(
          'hard_stop_pct',
          'cloud.policy.fields.hardStopPct',
          'cloud.policy.fields.hardStopPctHelp',
          500
        )}
        {numberField(
          'drain_timeout_seconds',
          'cloud.policy.fields.drainTimeout',
          'cloud.policy.fields.drainTimeoutHelp',
          86400
        )}

        <Grid item xs={12}>
          <FormControlLabel
            control={
              <Checkbox
                checked={policy.allow_overage}
                onChange={(e) => setPolicy({ ...policy, allow_overage: e.target.checked })}
              />
            }
            label={t('cloud.policy.fields.allowOverage') as string}
          />
          <Typography variant="body2" color="text.secondary">
            {t('cloud.policy.fields.allowOverageHelp') as string}
          </Typography>
        </Grid>

        {policy.allow_overage && (
          <Grid item xs={12}>
            <Alert severity="warning">{t('cloud.policy.overageWarning') as string}</Alert>
          </Grid>
        )}

        {policy.hard_stop_pct > 100 && !policy.allow_overage && (
          <Grid item xs={12}>
            <Alert severity="info">
              {t('cloud.policy.hardStopAboveCapNote', { pct: policy.hard_stop_pct }) as string}
            </Alert>
          </Grid>
        )}
      </Grid>

      <Box sx={{ mt: 3 }}>
        <Button variant="contained" onClick={handleSave} disabled={saveMutation.isPending}>
          {t('buttons.save', { ns: 'common' }) as string}
        </Button>
      </Box>
    </Box>
  );
};

export default CloudBudgetPolicySettings;
