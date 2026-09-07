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
  Accordion,
  AccordionDetails,
  AccordionSummary,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
} from '@mui/material';
import {
  Edit as EditIcon,
  ExpandMore as ExpandMoreIcon,
  Warning as WarningIcon,
} from '@mui/icons-material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import MoneyField from './MoneyField';
import {
  ClientCloudSettings,
  ClientCloudSettingsInput,
  CloudBudgetAssessment,
  ClientCloudDefaults,
  BudgetPeriod,
  BUDGET_PERIODS,
  CloudProviderKind,
  CLOUD_PROVIDER_KINDS,
  cloudProviderLabel,
  requiresThirdPartyAck,
} from '../../../types/cloud';
import {
  acknowledgeClientProvider,
  getClientCloudBudget,
  getClientCloudDefaults,
  updateClientCloudDefaults,
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
  // '' means inherit; the tri-state a plain checkbox cannot express.
  const [enabled, setEnabled] = useState<'' | 'on' | 'off'>('');
  const [period, setPeriod] = useState<BudgetPeriod | ''>('');
  const [allowlist, setAllowlist] = useState<CloudProviderKind[]>([]);
  const [formError, setFormError] = useState<string | null>(null);
  const [ackProvider, setAckProvider] = useState<CloudProviderKind | null>(null);

  const { data: clients, isLoading, error } = useQuery<ClientCloudSettings[]>({
    queryKey: ['cloudClientSettings'],
    queryFn: listClientCloudSettings,
  });

  const { data: defaults } = useQuery<ClientCloudDefaults>({
    queryKey: ['cloudClientDefaults'],
    queryFn: getClientCloudDefaults,
  });

  const [defaultsForm, setDefaultsForm] = useState<ClientCloudDefaults | null>(null);
  useEffect(() => {
    if (defaults && defaultsForm === null) setDefaultsForm(defaults);
  }, [defaults, defaultsForm]);

  /*
   * Which clients a given set of defaults would leave able to rent paid
   * capacity.
   *
   * Resolved exactly the way the backend does — per field, override first,
   * default second — because the whole point of showing this is that the
   * operator can trust it. A count derived from slightly different rules than
   * the ones that actually gate spending would be worse than no count at all.
   */
  const clientsEmpoweredBy = (d: ClientCloudDefaults | null): ClientCloudSettings[] => {
    if (!d) return [];
    return (clients ?? []).filter((c) => {
      const enabled = c.cloud_enabled === null ? d.cloud_enabled : c.cloud_enabled;
      const budget = c.cloud_budget_cents ?? d.cloud_budget_cents;
      const providers =
        c.cloud_provider_allowlist.length > 0
          ? c.cloud_provider_allowlist
          : d.cloud_provider_allowlist;
      return enabled && budget !== null && providers.length > 0;
    });
  };

  // The delta is what matters: clients that CANNOT spend today but could once
  // this is saved. Showing the absolute total would cry wolf on every edit.
  const newlyEmpowered = React.useMemo(() => {
    const before = new Set(clientsEmpoweredBy(defaults ?? null).map((c) => c.client_id));
    return clientsEmpoweredBy(defaultsForm).filter((c) => !before.has(c.client_id));
  }, [clients, defaults, defaultsForm]);

  const [confirmDefaults, setConfirmDefaults] = useState(false);

  const submitDefaults = () => {
    if (!defaultsForm) return;
    // Only interrupt when the change widens who can spend. Narrowing it, or
    // editing a period, saves straight through.
    if (newlyEmpowered.length > 0) {
      setConfirmDefaults(true);
      return;
    }
    defaultsMutation.mutate(defaultsForm);
  };

  const defaultsMutation = useMutation({
    mutationFn: updateClientCloudDefaults,
    onSuccess: (saved) => {
      enqueueSnackbar(t('cloud.budgets.defaults.saved') as string, { variant: 'success' });
      setDefaultsForm(saved);
      // Every inheriting client's effective values just changed.
      queryClient.invalidateQueries({ queryKey: ['cloudClientSettings'] });
      queryClient.invalidateQueries({ queryKey: ['cloudClientBudget'] });
      queryClient.invalidateQueries({ queryKey: ['cloudClientDefaults'] });
    },
    onError: (err: any) =>
      enqueueSnackbar(apiError(err, t('cloud.budgets.defaults.saveFailed') as string), {
        variant: 'error',
      }),
  });

  useEffect(() => {
    if (!editing) return;
    setBudgetDollars(centsToDollars(editing.cloud_budget_cents));
    setTtlMinutes(editing.max_instance_ttl_minutes ? String(editing.max_instance_ttl_minutes) : '');
    setEnabled(editing.cloud_enabled === null ? '' : editing.cloud_enabled ? 'on' : 'off');
    setPeriod(editing.cloud_budget_period ?? '');
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

    /*
     * Validate against what will ACTUALLY be in force, not against what was
     * typed. A client left inheriting is perfectly valid with empty fields, so
     * these checks resolve through the defaults first — otherwise "inherit
     * everything" would be rejected as "enabled without a budget".
     */
    const willBeEnabled = enabled === '' ? Boolean(defaults?.cloud_enabled) : enabled === 'on';
    const willHaveBudget = cents !== null || (defaults?.cloud_budget_cents ?? null) !== null;
    const willHaveProvider =
      allowlist.length > 0 || (defaults?.cloud_provider_allowlist?.length ?? 0) > 0;

    if (willBeEnabled && !willHaveBudget) {
      setFormError(t('cloud.budgets.errors.enabledWithoutBudget') as string);
      return;
    }
    if (willBeEnabled && !willHaveProvider) {
      setFormError(t('cloud.budgets.errors.enabledWithoutProvider') as string);
      return;
    }

    saveMutation.mutate({
      clientId: editing.client_id,
      input: {
        cloud_enabled: enabled === '' ? null : enabled === 'on',
        cloud_provider_allowlist: allowlist,
        cloud_budget_cents: cents,
        cloud_budget_period: period === '' ? null : period,
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

      {/*
        * Server defaults. Placed above the client table because it is the
        * setting that decides what every unconfigured client below is doing —
        * reading the table without it tells you nothing about what will
        * actually happen.
        */}
      <Accordion sx={{ mb: 3 }} defaultExpanded={(clients ?? []).length === 0}>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Box>
            <Typography variant="subtitle1">
              {t('cloud.budgets.defaults.title') as string}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {defaultsForm?.cloud_budget_cents == null
                ? (t('cloud.budgets.defaults.summaryUnset') as string)
                : (t('cloud.budgets.defaults.summary', {
                    amount: formatCents(defaultsForm.cloud_budget_cents),
                    period: t(
                      `cloud.budgets.periods.${defaultsForm.cloud_budget_period}`
                    ) as string,
                  }) as string)}
            </Typography>
          </Box>
        </AccordionSummary>
        <AccordionDetails>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {t('cloud.budgets.defaults.description') as string}
          </Typography>

          {defaultsForm && (
            <>
              <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', mb: 2 }}>
                <Box sx={{ flex: '1 1 220px' }}>
                  <MoneyField
                    label={t('cloud.budgets.defaults.budget') as string}
                    cents={defaultsForm.cloud_budget_cents}
                    onChange={(cents) =>
                      setDefaultsForm({ ...defaultsForm, cloud_budget_cents: cents })
                    }
                    helperText={t('cloud.budgets.defaults.budgetHelp') as string}
                  />
                </Box>
                <Box sx={{ flex: '1 1 220px' }}>
                  <FormControl fullWidth>
                    <InputLabel>{t('cloud.budgets.defaults.period') as string}</InputLabel>
                    <Select
                      label={t('cloud.budgets.defaults.period') as string}
                      value={defaultsForm.cloud_budget_period}
                      onChange={(e) =>
                        setDefaultsForm({
                          ...defaultsForm,
                          cloud_budget_period: e.target.value as BudgetPeriod,
                        })
                      }
                    >
                      {BUDGET_PERIODS.map((p) => (
                        <MenuItem key={p} value={p}>
                          {t(`cloud.budgets.periods.${p}`) as string}
                        </MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                </Box>
              </Box>

              <FormControlLabel
                control={
                  <Checkbox
                    checked={defaultsForm.cloud_enabled}
                    onChange={(e) =>
                      setDefaultsForm({ ...defaultsForm, cloud_enabled: e.target.checked })
                    }
                  />
                }
                label={t('cloud.budgets.defaults.enabled') as string}
              />

              <Typography variant="subtitle2" sx={{ mt: 2 }}>
                {t('cloud.budgets.defaults.providers') as string}
              </Typography>
              <Typography variant="caption" color="text.secondary" display="block" sx={{ mb: 1 }}>
                {t('cloud.budgets.defaults.providersHelp') as string}
              </Typography>
              <Box>
                {SELECTABLE_PROVIDERS.map((provider) => (
                  <FormControlLabel
                    key={provider}
                    control={
                      <Checkbox
                        checked={defaultsForm.cloud_provider_allowlist.includes(provider)}
                        onChange={(e) =>
                          setDefaultsForm({
                            ...defaultsForm,
                            cloud_provider_allowlist: e.target.checked
                              ? [...defaultsForm.cloud_provider_allowlist, provider]
                              : defaultsForm.cloud_provider_allowlist.filter(
                                  (p) => p !== provider
                                ),
                          })
                        }
                      />
                    }
                    label={cloudProviderLabel(provider)}
                  />
                ))}
              </Box>

              {/*
                * Peer providers still need a per-client acknowledgement even
                * when defaulted in, so say so rather than letting a default
                * look like it granted consent on everyone's behalf.
                */}
              {defaultsForm.cloud_provider_allowlist.some((p) =>
                requiresThirdPartyAck(p as CloudProviderKind)
              ) && (
                <Alert severity="warning" sx={{ mt: 1 }}>
                  {t('cloud.budgets.defaults.peerAckNote') as string}
                </Alert>
              )}

              {defaultsForm.cloud_enabled && defaultsForm.cloud_budget_cents !== null && (
                <Alert severity="info" sx={{ mt: 2 }}>
                  {t('cloud.budgets.defaults.appliesNow') as string}
                </Alert>
              )}

              {/*
                * Funding without enabling provisions nothing, and the two live
                * in different controls, so the combination is easy to leave
                * half-done and impossible to diagnose from the client table.
                */}
              {!defaultsForm.cloud_enabled && defaultsForm.cloud_budget_cents !== null && (
                <Alert severity="warning" sx={{ mt: 2 }}>
                  {t('cloud.budgets.defaults.fundedButDisabled') as string}
                </Alert>
              )}

              {defaultsForm.cloud_enabled &&
                defaultsForm.cloud_provider_allowlist.length === 0 && (
                  <Alert severity="warning" sx={{ mt: 2 }}>
                    {t('cloud.budgets.defaults.enabledWithoutProviders') as string}
                  </Alert>
                )}

              {newlyEmpowered.length > 0 && (
                <Alert severity="warning" sx={{ mt: 2 }}>
                  <AlertTitle>
                    {t('cloud.budgets.defaults.willEnableTitle', {
                      count: newlyEmpowered.length,
                    }) as string}
                  </AlertTitle>
                  {t('cloud.budgets.defaults.willEnableBody') as string}
                  <Box sx={{ mt: 1 }}>
                    {newlyEmpowered.slice(0, 12).map((c) => (
                      <Chip
                        key={c.client_id}
                        size="small"
                        sx={{ mr: 0.5, mb: 0.5 }}
                        label={c.client_name}
                      />
                    ))}
                    {newlyEmpowered.length > 12 && (
                      <Chip
                        size="small"
                        variant="outlined"
                        label={t('cloud.budgets.defaults.andMore', {
                          count: newlyEmpowered.length - 12,
                        }) as string}
                      />
                    )}
                  </Box>
                </Alert>
              )}

              <Box sx={{ mt: 2 }}>
                <Button
                  variant="contained"
                  disabled={defaultsMutation.isPending}
                  onClick={submitDefaults}
                >
                  {t('cloud.budgets.defaults.save') as string}
                </Button>
              </Box>
            </>
          )}
        </AccordionDetails>
      </Accordion>

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

          {/* Whether this client may spend at all comes FIRST: every field
              below it is meaningless until this is answered. */}
          {/*
            * Three states, not a checkbox. A checkbox can only say on or off,
            * which leaves no way to put a client back to following the server
            * default once it has been set either way.
            */}
          <FormControl fullWidth margin="normal">
            <InputLabel>{t('cloud.budgets.fields.enabled') as string}</InputLabel>
            <Select
              label={t('cloud.budgets.fields.enabled') as string}
              value={enabled}
              onChange={(e) => setEnabled(e.target.value as '' | 'on' | 'off')}
            >
              <MenuItem value="">
                {t('cloud.budgets.inheritLabel', {
                  value: defaults?.cloud_enabled
                    ? (t('cloud.budgets.on') as string)
                    : (t('cloud.budgets.off') as string),
                }) as string}
              </MenuItem>
              <MenuItem value="on">{t('cloud.budgets.on') as string}</MenuItem>
              <MenuItem value="off">{t('cloud.budgets.off') as string}</MenuItem>
            </Select>
          </FormControl>

          <FormControl fullWidth margin="normal">
            <InputLabel>{t('cloud.budgets.fields.period') as string}</InputLabel>
            <Select
              label={t('cloud.budgets.fields.period') as string}
              value={period}
              onChange={(e) => setPeriod(e.target.value as BudgetPeriod | '')}
            >
              <MenuItem value="">
                {t('cloud.budgets.inheritLabel', {
                  value: t(
                    `cloud.budgets.periods.${defaults?.cloud_budget_period ?? 'monthly'}`
                  ) as string,
                }) as string}
              </MenuItem>
              {BUDGET_PERIODS.map((p) => (
                <MenuItem key={p} value={p}>
                  {t(`cloud.budgets.periods.${p}`) as string}
                </MenuItem>
              ))}
            </Select>
          </FormControl>

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

        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditing(null)}>{t('buttons.cancel', { ns: 'common' }) as string}</Button>
          <Button variant="contained" onClick={handleSave} disabled={saveMutation.isPending}>
            {t('buttons.save', { ns: 'common' }) as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/*
        * Saving a default that widens who can spend is confirmed explicitly.
        *
        * This is the one action in the feature whose blast radius is not
        * visible from the control that triggers it: a single checkbox on a
        * defaults panel can hand paid capacity to every client that has never
        * been configured. Naming them, and requiring a second click, keeps that
        * from being something an operator discovers on an invoice.
        */}
      <Dialog open={confirmDefaults} onClose={() => setConfirmDefaults(false)} maxWidth="sm" fullWidth>
        <DialogTitle>
          {t('cloud.budgets.defaults.confirmTitle', { count: newlyEmpowered.length }) as string}
        </DialogTitle>
        <DialogContent>
          <DialogContentText sx={{ mb: 2 }}>
            {t('cloud.budgets.defaults.confirmBody', {
              count: newlyEmpowered.length,
              amount:
                defaultsForm?.cloud_budget_cents != null
                  ? formatCents(defaultsForm.cloud_budget_cents)
                  : '',
              period: t(
                `cloud.budgets.periods.${defaultsForm?.cloud_budget_period ?? 'monthly'}`
              ) as string,
            }) as string}
          </DialogContentText>
          <Box>
            {newlyEmpowered.map((c) => (
              <Chip key={c.client_id} size="small" sx={{ mr: 0.5, mb: 0.5 }} label={c.client_name} />
            ))}
          </Box>
          <Alert severity="info" sx={{ mt: 2 }}>
            {t('cloud.budgets.defaults.confirmAlternative') as string}
          </Alert>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmDefaults(false)}>
            {t('buttons.cancel', { ns: 'common' }) as string}
          </Button>
          <Button
            variant="contained"
            color="warning"
            onClick={() => {
              setConfirmDefaults(false);
              if (defaultsForm) defaultsMutation.mutate(defaultsForm);
            }}
          >
            {t('cloud.budgets.defaults.confirmAccept', { count: newlyEmpowered.length }) as string}
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
