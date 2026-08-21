import React, { useEffect, useState } from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Checkbox,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControlLabel,
  LinearProgress,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import { Edit as EditIcon, Warning as WarningIcon } from '@mui/icons-material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import {
  ClientCloudSettings,
  ClientCloudSettingsInput,
  CloudBudgetAssessment,
  CloudProviderKind,
  CLOUD_PROVIDER_KINDS,
  cloudProviderLabel,
  requiresThirdPartyAck,
} from '../../../types/cloud';
import {
  acknowledgeClientProvider,
  getClientCloudBudget,
  listClientCloudSettings,
  updateClientCloudSettings,
  formatCents,
} from '../../../services/cloud';

const apiError = (err: any, fallback: string): string =>
  err?.response?.data?.error || err?.message || fallback;

const SELECTABLE_PROVIDERS: CloudProviderKind[] = CLOUD_PROVIDER_KINDS;

/** Dollars in the form, integer cents on the wire. */
const centsToDollars = (cents: number | null): string =>
  cents === null || cents === undefined ? '' : (cents / 100).toFixed(2);

const dollarsToCents = (value: string): number | null => {
  const trimmed = value.trim();
  if (trimmed === '') return null;
  const parsed = Number(trimmed);
  if (Number.isNaN(parsed)) return null;
  return Math.round(parsed * 100);
};

/** One client's live spend bar, fetched on demand when the row expands. */
const BudgetBar: React.FC<{ clientId: string }> = ({ clientId }) => {
  const { t } = useTranslation('admin');
  const { data, isLoading } = useQuery<CloudBudgetAssessment>({
    queryKey: ['cloudClientBudget', clientId],
    queryFn: () => getClientCloudBudget(clientId),
    // Spend moves as instances run; the reaper accrues about once a minute.
    refetchInterval: 60_000,
  });

  if (isLoading) return <CircularProgress size={16} />;
  if (!data) return null;

  const { state, action } = data;
  if (state.cap_cents === null) {
    return <Chip size="small" color="default" label={t('cloud.budgets.unfunded') as string} />;
  }

  const pct = Math.min(state.used_pct, 100);
  const color = state.used_pct >= 99 ? 'error' : state.used_pct >= 90 ? 'warning' : 'primary';

  return (
    <Box sx={{ minWidth: 180 }}>
      <LinearProgress variant="determinate" value={pct} color={color} sx={{ height: 8, borderRadius: 1 }} />
      <Typography variant="caption" color="text.secondary">
        {t('cloud.budgets.spendSummary', {
          used: formatCents(state.incurred_cents + state.reserved_cents),
          cap: formatCents(state.cap_cents),
          pct: state.used_pct.toFixed(1),
        }) as string}
      </Typography>
      {action !== 'none' && (
        <Chip
          size="small"
          sx={{ ml: 1 }}
          color={action === 'notify' ? 'warning' : 'error'}
          label={t(`cloud.budgets.actions.${action}`) as string}
        />
      )}
    </Box>
  );
};

const CloudClientBudgets: React.FC = () => {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

  const [editing, setEditing] = useState<ClientCloudSettings | null>(null);
  const [budgetDollars, setBudgetDollars] = useState('');
  const [ttlMinutes, setTtlMinutes] = useState('');
  const [enabled, setEnabled] = useState(false);
  const [allowlist, setAllowlist] = useState<CloudProviderKind[]>([]);
  const [formError, setFormError] = useState<string | null>(null);
  const [ackProvider, setAckProvider] = useState<CloudProviderKind | null>(null);

  const { data: clients, isLoading, error } = useQuery<ClientCloudSettings[]>({
    queryKey: ['cloudClientSettings'],
    queryFn: listClientCloudSettings,
  });

  useEffect(() => {
    if (!editing) return;
    setBudgetDollars(centsToDollars(editing.cloud_budget_cents));
    setTtlMinutes(editing.max_instance_ttl_minutes ? String(editing.max_instance_ttl_minutes) : '');
    setEnabled(editing.cloud_enabled);
    setAllowlist(editing.cloud_provider_allowlist ?? []);
    setFormError(null);
  }, [editing]);

  const saveMutation = useMutation({
    mutationFn: ({ clientId, input }: { clientId: string; input: ClientCloudSettingsInput }) =>
      updateClientCloudSettings(clientId, input),
    onSuccess: () => {
      enqueueSnackbar(t('cloud.budgets.saved') as string, { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['cloudClientSettings'] });
      queryClient.invalidateQueries({ queryKey: ['cloudClientBudget'] });
      setEditing(null);
    },
    onError: (err: any) => setFormError(apiError(err, t('cloud.budgets.saveFailed') as string)),
  });

  const ackMutation = useMutation({
    mutationFn: ({ clientId, provider }: { clientId: string; provider: CloudProviderKind }) =>
      acknowledgeClientProvider(clientId, provider),
    onSuccess: (_data, variables) => {
      enqueueSnackbar(t('cloud.budgets.acknowledged') as string, { variant: 'success' });
      queryClient.invalidateQueries({ queryKey: ['cloudClientSettings'] });
      // Reflect the acknowledgement in the open dialog without a refetch race.
      setEditing((prev) =>
        prev
          ? {
              ...prev,
              provider_ack: {
                ...prev.provider_ack,
                [variables.provider]: { at: new Date().toISOString(), by: '' },
              },
            }
          : prev
      );
      setAllowlist((prev) =>
        prev.includes(variables.provider) ? prev : [...prev, variables.provider]
      );
      setAckProvider(null);
    },
    onError: (err: any) => {
      setFormError(apiError(err, t('cloud.budgets.ackFailed') as string));
      setAckProvider(null);
    },
  });

  const toggleProvider = (provider: CloudProviderKind, checked: boolean) => {
    if (!checked) {
      setAllowlist((prev) => prev.filter((p) => p !== provider));
      return;
    }
    // Peer providers put this client's hashes on machines whose owners have
    // root. That consent is per-client and must be recorded before the
    // allowlist can include it; the backend rejects the save otherwise.
    //
    // Predicate, not a provider name: AWS and RunPod Secure must go straight
    // into the allowlist with no consent step.
    if (requiresThirdPartyAck(provider) && editing && !editing.provider_ack?.[provider]) {
      setAckProvider(provider);
      return;
    }
    setAllowlist((prev) => (prev.includes(provider) ? prev : [...prev, provider]));
  };

  const handleSave = () => {
    if (!editing) return;
    setFormError(null);

    const cents = dollarsToCents(budgetDollars);
    if (budgetDollars.trim() !== '' && cents === null) {
      setFormError(t('cloud.budgets.errors.invalidBudget') as string);
      return;
    }
    if (cents !== null && cents < 0) {
      setFormError(t('cloud.budgets.errors.negativeBudget') as string);
      return;
    }

    let ttl: number | null = null;
    if (ttlMinutes.trim() !== '') {
      ttl = parseInt(ttlMinutes, 10);
      if (Number.isNaN(ttl) || ttl <= 0) {
        setFormError(t('cloud.budgets.errors.invalidTtl') as string);
        return;
      }
    }

    // Enabled with no funded budget can never provision anything; say so here
    // rather than letting the operator discover it as a silent no-op.
    if (enabled && cents === null) {
      setFormError(t('cloud.budgets.errors.enabledWithoutBudget') as string);
      return;
    }
    if (enabled && allowlist.length === 0) {
      setFormError(t('cloud.budgets.errors.enabledWithoutProvider') as string);
      return;
    }

    saveMutation.mutate({
      clientId: editing.client_id,
      input: {
        cloud_enabled: enabled,
        cloud_provider_allowlist: allowlist,
        cloud_budget_cents: cents,
        max_instance_ttl_minutes: ttl,
      },
    });
  };

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('cloud.budgets.title') as string}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        {t('cloud.budgets.description') as string}
      </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {apiError(error, t('cloud.budgets.loadFailed') as string)}
        </Alert>
      )}

      {isLoading ? (
        <CircularProgress />
      ) : (clients ?? []).length === 0 ? (
        <Alert severity="info">{t('cloud.budgets.empty') as string}</Alert>
      ) : (
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>{t('cloud.budgets.columns.client') as string}</TableCell>
                <TableCell>{t('cloud.budgets.columns.status') as string}</TableCell>
                <TableCell>{t('cloud.budgets.columns.providers') as string}</TableCell>
                <TableCell>{t('cloud.budgets.columns.spend') as string}</TableCell>
                <TableCell align="right">{t('cloud.budgets.columns.ttl') as string}</TableCell>
                <TableCell align="right">{t('cloud.budgets.columns.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {(clients ?? []).map((client) => (
                <TableRow key={client.client_id}>
                  <TableCell>{client.client_name}</TableCell>
                  <TableCell>
                    <Chip
                      size="small"
                      color={client.cloud_enabled ? 'success' : 'default'}
                      label={
                        client.cloud_enabled
                          ? (t('cloud.budgets.enabled') as string)
                          : (t('cloud.budgets.disabled') as string)
                      }
                    />
                  </TableCell>
                  <TableCell>
                    {client.cloud_provider_allowlist.length === 0 ? (
                      <Typography variant="caption" color="text.secondary">
                        {t('cloud.budgets.noProviders') as string}
                      </Typography>
                    ) : (
                      client.cloud_provider_allowlist.map((provider) => (
                        <Chip
                          key={provider}
                          size="small"
                          sx={{ mr: 0.5 }}
                          color={
                            requiresThirdPartyAck(provider as CloudProviderKind)
                              ? 'warning'
                              : 'default'
                          }
                          icon={
                            requiresThirdPartyAck(provider as CloudProviderKind) ? (
                              <WarningIcon />
                            ) : undefined
                          }
                          label={cloudProviderLabel(provider as CloudProviderKind)}
                        />
                      ))
                    )}
                  </TableCell>
                  <TableCell>
                    <BudgetBar clientId={client.client_id} />
                  </TableCell>
                  <TableCell align="right">
                    {client.max_instance_ttl_minutes
                      ? (t('cloud.budgets.ttlValue', { minutes: client.max_instance_ttl_minutes }) as string)
                      : '—'}
                  </TableCell>
                  <TableCell align="right">
                    <Button size="small" startIcon={<EditIcon />} onClick={() => setEditing(client)}>
                      {t('cloud.budgets.edit') as string}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      <Dialog open={Boolean(editing)} onClose={() => setEditing(null)} maxWidth="sm" fullWidth>
        <DialogTitle>
          {t('cloud.budgets.editTitle', { name: editing?.client_name }) as string}
        </DialogTitle>
        <DialogContent>
          {formError && <Alert severity="error" sx={{ mb: 2 }}>{formError}</Alert>}

          <TextField
            fullWidth
            margin="normal"
            label={t('cloud.budgets.fields.budget') as string}
            value={budgetDollars}
            onChange={(e) => setBudgetDollars(e.target.value)}
            helperText={t('cloud.budgets.fields.budgetHelp') as string}
            InputProps={{ startAdornment: <Box sx={{ mr: 1 }}>$</Box> }}
          />

          <TextField
            fullWidth
            margin="normal"
            type="number"
            label={t('cloud.budgets.fields.ttl') as string}
            value={ttlMinutes}
            onChange={(e) => setTtlMinutes(e.target.value)}
            helperText={t('cloud.budgets.fields.ttlHelp') as string}
            inputProps={{ min: 1 }}
          />

          <Typography variant="subtitle2" sx={{ mt: 3 }}>
            {t('cloud.budgets.fields.providers') as string}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {t('cloud.budgets.fields.providersHelp') as string}
          </Typography>

          {SELECTABLE_PROVIDERS.map((provider) => (
            <Box key={provider} sx={{ display: 'flex', alignItems: 'center' }}>
              <FormControlLabel
                control={
                  <Checkbox
                    checked={allowlist.includes(provider)}
                    onChange={(e) => toggleProvider(provider, e.target.checked)}
                  />
                }
                label={cloudProviderLabel(provider)}
              />
              {requiresThirdPartyAck(provider) && (
                <Tooltip title={t('cloud.budgets.peerTooltip') as string}>
                  <WarningIcon fontSize="small" color="warning" />
                </Tooltip>
              )}
              {editing?.provider_ack?.[provider] && (
                <Chip
                  size="small"
                  sx={{ ml: 1 }}
                  color="success"
                  label={t('cloud.budgets.acknowledgedChip') as string}
                />
              )}
            </Box>
          ))}

          <FormControlLabel
            sx={{ mt: 2 }}
            control={<Checkbox checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />}
            label={t('cloud.budgets.fields.enabled') as string}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditing(null)}>{t('buttons.cancel', { ns: 'common' }) as string}</Button>
          <Button variant="contained" onClick={handleSave} disabled={saveMutation.isPending}>
            {t('buttons.save', { ns: 'common' }) as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Per-client Vast.ai acknowledgement. The provider-level one says the
          operator understands Vast.ai; this one says this engagement's data
          may go there. */}
      <Dialog open={Boolean(ackProvider)} onClose={() => setAckProvider(null)} maxWidth="sm" fullWidth>
        <DialogTitle>{t('cloud.budgets.ackTitle') as string}</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>
            <AlertTitle>{t('cloud.budgets.ackWarningTitle') as string}</AlertTitle>
            {t('cloud.budgets.ackWarningBody', { name: editing?.client_name }) as string}
          </Alert>
          <DialogContentText>{t('cloud.budgets.ackConfirm') as string}</DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAckProvider(null)}>{t('buttons.cancel', { ns: 'common' }) as string}</Button>
          <Button
            color="warning"
            variant="contained"
            disabled={ackMutation.isPending}
            onClick={() =>
              editing &&
              ackProvider &&
              ackMutation.mutate({ clientId: editing.client_id, provider: ackProvider })
            }
          >
            {t('cloud.budgets.ackAccept') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default CloudClientBudgets;
