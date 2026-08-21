import React, { useState } from 'react';
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
  FormControl,
  FormControlLabel,
  InputLabel,
  List,
  ListItem,
  ListItemText,
  MenuItem,
  Paper,
  Select,
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
import {
  Add as AddIcon,
  Delete as DeleteIcon,
  Edit as EditIcon,
  NetworkCheck as NetworkCheckIcon,
  Warning as WarningIcon,
} from '@mui/icons-material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import {
  CloudPreflightReport,
  CloudProviderConfig,
  CloudProviderConfigInput,
  CloudProviderKind,
  CLOUD_PROVIDER_KINDS,
  requiresThirdPartyAck,
  VPNCredentialKind,
  VPNProviderKind,
} from '../../../types/cloud';
import {
  acknowledgeCloudProvider,
  createCloudProvider,
  deleteCloudProvider,
  listCloudProviders,
  runCloudPreflight,
  updateCloudProvider,
  formatCents,
} from '../../../services/cloud';

const apiError = (err: any, fallback: string): string =>
  err?.response?.data?.error || err?.message || fallback;

const emptyForm = (): CloudProviderConfigInput => ({
  provider: 'aws',
  name: '',
  enabled: false,
  credentials: '',
  settings: {},
  max_concurrent_instances: 2,
  max_instance_hourly_cents: 200,
  vpn_provider: 'tailscale',
  vpn_credential_kind: 'oauth',
  vpn_credential: '',
  vpn_tag_or_group: '',
  backend_vpn_host: '',
});

/** Which credential kinds each VPN provider actually supports. */
const CREDENTIAL_KINDS: Record<VPNProviderKind, VPNCredentialKind[]> = {
  tailscale: ['oauth', 'reusable_key'],
  netbird: ['pat', 'reusable_key'],
  wireguard: ['static_config'],
};

const CloudProviderSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const { enqueueSnackbar } = useSnackbar();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<CloudProviderConfig | null>(null);
  const [form, setForm] = useState<CloudProviderConfigInput>(emptyForm());
  const [formError, setFormError] = useState<string | null>(null);
  const [ackTarget, setAckTarget] = useState<CloudProviderConfig | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<CloudProviderConfig | null>(null);
  const [preflight, setPreflight] = useState<CloudPreflightReport | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ['cloudProviders'],
    queryFn: listCloudProviders,
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['cloudProviders'] });

  const saveMutation = useMutation({
    mutationFn: (input: CloudProviderConfigInput) =>
      editing ? updateCloudProvider(editing.id, input) : createCloudProvider(input),
    onSuccess: () => {
      enqueueSnackbar(t('cloud.providers.saved') as string, { variant: 'success' });
      invalidate();
      setDialogOpen(false);
    },
    onError: (err: any) => setFormError(apiError(err, t('cloud.providers.saveFailed') as string)),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteCloudProvider(id),
    onSuccess: () => {
      enqueueSnackbar(t('cloud.providers.deleted') as string, { variant: 'success' });
      invalidate();
      setDeleteTarget(null);
    },
    onError: (err: any) => {
      // 409: rented hardware still references this config.
      enqueueSnackbar(apiError(err, t('cloud.providers.deleteFailed') as string), { variant: 'error' });
      setDeleteTarget(null);
    },
  });

  const ackMutation = useMutation({
    mutationFn: (id: string) => acknowledgeCloudProvider(id),
    onSuccess: () => {
      enqueueSnackbar(t('cloud.providers.acknowledged') as string, { variant: 'success' });
      invalidate();
      setAckTarget(null);
    },
    onError: (err: any) => {
      enqueueSnackbar(apiError(err, t('cloud.providers.ackFailed') as string), { variant: 'error' });
      setAckTarget(null);
    },
  });

  const preflightMutation = useMutation({
    mutationFn: (id: string) => runCloudPreflight(id),
    onSuccess: (report) => setPreflight(report),
    onError: (err: any) =>
      enqueueSnackbar(apiError(err, t('cloud.providers.preflightFailed') as string), { variant: 'error' }),
  });

  const openCreate = () => {
    setEditing(null);
    setForm(emptyForm());
    setFormError(null);
    setDialogOpen(true);
  };

  const openEdit = (cfg: CloudProviderConfig) => {
    setEditing(cfg);
    setForm({
      provider: cfg.provider,
      name: cfg.name,
      enabled: cfg.enabled,
      // Secrets are never returned. Left blank means "keep what is stored".
      credentials: '',
      settings: cfg.settings ?? {},
      max_concurrent_instances: cfg.max_concurrent_instances,
      max_instance_hourly_cents: cfg.max_instance_hourly_cents,
      vpn_provider: cfg.vpn_provider ?? 'tailscale',
      vpn_credential_kind: cfg.vpn_credential_kind ?? 'oauth',
      vpn_credential: '',
      vpn_tag_or_group: cfg.vpn_tag_or_group ?? '',
      backend_vpn_host: cfg.backend_vpn_host ?? '',
    });
    setFormError(null);
    setDialogOpen(true);
  };

  const handleVPNProviderChange = (provider: VPNProviderKind) => {
    const kinds = CREDENTIAL_KINDS[provider];
    setForm((f) => ({
      ...f,
      vpn_provider: provider,
      // Carry the kind over only if the new provider supports it.
      vpn_credential_kind: kinds.includes(f.vpn_credential_kind as VPNCredentialKind)
        ? f.vpn_credential_kind
        : kinds[0],
    }));
  };

  const handleSubmit = () => {
    setFormError(null);
    const payload: CloudProviderConfigInput = { ...form };
    // Omit blank secrets entirely so the backend keeps the stored ciphertext.
    if (!payload.credentials) delete payload.credentials;
    if (!payload.vpn_credential) delete payload.vpn_credential;
    saveMutation.mutate(payload);
  };

  const providers = data?.providers ?? [];
  const credentialKinds = CREDENTIAL_KINDS[(form.vpn_provider ?? 'tailscale') as VPNProviderKind];
  const secretsRequired = !editing && form.provider !== 'mock';
  const daysUntil = (iso: string | null): number | null => {
    if (!iso) return null;
    return Math.floor((new Date(iso).getTime() - Date.now()) / 86_400_000);
  };

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h6" gutterBottom>
            {t('cloud.providers.title') as string}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {t('cloud.providers.description') as string}
          </Typography>
        </Box>
        <Button variant="contained" startIcon={<AddIcon />} onClick={openCreate}>
          {t('cloud.providers.add') as string}
        </Button>
      </Box>

      {data?.encryptionKeyEphemeral && (
        <Alert severity="error" sx={{ mb: 2 }}>
          <AlertTitle>{t('cloud.providers.ephemeralKeyTitle') as string}</AlertTitle>
          {t('cloud.providers.ephemeralKeyBody') as string}
        </Alert>
      )}

      {error && <Alert severity="error" sx={{ mb: 2 }}>{apiError(error, t('cloud.providers.loadFailed') as string)}</Alert>}

      {isLoading ? (
        <CircularProgress />
      ) : providers.length === 0 ? (
        <Alert severity="info">{t('cloud.providers.empty') as string}</Alert>
      ) : (
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>{t('cloud.providers.columns.name') as string}</TableCell>
                <TableCell>{t('cloud.providers.columns.provider') as string}</TableCell>
                <TableCell>{t('cloud.providers.columns.status') as string}</TableCell>
                <TableCell>{t('cloud.providers.columns.vpn') as string}</TableCell>
                <TableCell align="right">{t('cloud.providers.columns.limits') as string}</TableCell>
                <TableCell align="right">{t('cloud.providers.columns.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {providers.map((cfg) => {
                const expiryDays = daysUntil(cfg.vpn_credential_expires_at);
                return (
                  <TableRow key={cfg.id}>
                    <TableCell>{cfg.name}</TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        label={cfg.provider}
                        color={requiresThirdPartyAck(cfg.provider) ? 'warning' : 'default'}
                      />
                      {requiresThirdPartyAck(cfg.provider) && (
                        <Tooltip title={t('cloud.providers.thirdPartyBadge') as string}>
                          <WarningIcon fontSize="small" color="warning" sx={{ ml: 1, verticalAlign: 'middle' }} />
                        </Tooltip>
                      )}
                    </TableCell>
                    <TableCell>
                      <Chip
                        size="small"
                        label={
                          cfg.enabled
                            ? (t('cloud.providers.enabled') as string)
                            : (t('cloud.providers.disabled') as string)
                        }
                        color={cfg.enabled ? 'success' : 'default'}
                      />
                      {!cfg.has_credentials && cfg.provider !== 'mock' && (
                        <Chip
                          size="small"
                          sx={{ ml: 1 }}
                          color="error"
                          label={t('cloud.providers.noCredentials') as string}
                        />
                      )}
                    </TableCell>
                    <TableCell>
                      {cfg.vpn_provider ? (
                        <>
                          {cfg.vpn_provider}
                          {cfg.vpn_credential_kind && ` (${cfg.vpn_credential_kind})`}
                          {/* A lapsed reusable key strands every future launch,
                              so this warns well before it expires. */}
                          {expiryDays !== null && (
                            <Chip
                              size="small"
                              sx={{ ml: 1 }}
                              color={expiryDays <= 3 ? 'error' : expiryDays <= 14 ? 'warning' : 'default'}
                              label={
                                expiryDays <= 0
                                  ? (t('cloud.providers.credentialExpired') as string)
                                  : (t('cloud.providers.credentialExpiresIn', { days: expiryDays }) as string)
                              }
                            />
                          )}
                        </>
                      ) : (
                        <Chip size="small" color="error" label={t('cloud.providers.noVpn') as string} />
                      )}
                    </TableCell>
                    <TableCell align="right">
                      {t('cloud.providers.limitsValue', {
                        instances: cfg.max_concurrent_instances,
                        rate: formatCents(cfg.max_instance_hourly_cents),
                      }) as string}
                    </TableCell>
                    <TableCell align="right">
                      <Tooltip title={t('cloud.providers.preflight') as string}>
                        <span>
                          <Button
                            size="small"
                            startIcon={<NetworkCheckIcon />}
                            disabled={preflightMutation.isPending}
                            onClick={() => preflightMutation.mutate(cfg.id)}
                          >
                            {t('cloud.providers.preflight') as string}
                          </Button>
                        </span>
                      </Tooltip>
                      {requiresThirdPartyAck(cfg.provider) && !cfg.third_party_ack_at && (
                        <Button size="small" color="warning" onClick={() => setAckTarget(cfg)}>
                          {t('cloud.providers.acknowledge') as string}
                        </Button>
                      )}
                      <Button size="small" startIcon={<EditIcon />} onClick={() => openEdit(cfg)}>
                        {t('cloud.providers.edit') as string}
                      </Button>
                      <Button
                        size="small"
                        color="error"
                        startIcon={<DeleteIcon />}
                        onClick={() => setDeleteTarget(cfg)}
                      >
                        {t('cloud.providers.delete') as string}
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </TableContainer>
      )}

      {/* --- Create / edit --- */}
      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>
          {editing
            ? (t('cloud.providers.editTitle') as string)
            : (t('cloud.providers.addTitle') as string)}
        </DialogTitle>
        <DialogContent>
          {formError && <Alert severity="error" sx={{ mb: 2 }}>{formError}</Alert>}

          {/*
            * Peer-hardware warning. Keyed off requiresThirdPartyAck rather
            * than a provider name so AWS and RunPod Secure render with NO
            * warning surface at all — they are the operator's own account and
            * RunPod's own SOC 2 datacentres respectively.
            */}
          {requiresThirdPartyAck(form.provider) && (
            <Alert severity="warning" sx={{ mb: 2 }}>
              <AlertTitle>{t('cloud.providers.peerWarningTitle') as string}</AlertTitle>
              {t(`cloud.providers.peerWarningBody.${form.provider}`) as string}
            </Alert>
          )}

          <TextField
            fullWidth
            margin="normal"
            label={t('cloud.providers.fields.name') as string}
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />

          <FormControl fullWidth margin="normal">
            <InputLabel>{t('cloud.providers.fields.provider') as string}</InputLabel>
            <Select
              label={t('cloud.providers.fields.provider') as string}
              value={form.provider}
              // Provider is immutable after creation: the stored credentials
              // and settings are provider-shaped.
              disabled={Boolean(editing)}
              onChange={(e) => setForm({ ...form, provider: e.target.value as CloudProviderKind })}
            >
              {CLOUD_PROVIDER_KINDS.map((kind) => (
                <MenuItem key={kind} value={kind}>
                  {t(`cloud.providers.kinds.${kind}`) as string}
                </MenuItem>
              ))}
            </Select>
          </FormControl>

          <TextField
            fullWidth
            margin="normal"
            type="password"
            autoComplete="new-password"
            label={t('cloud.providers.fields.credentials') as string}
            value={form.credentials ?? ''}
            onChange={(e) => setForm({ ...form, credentials: e.target.value })}
            helperText={
              editing
                ? (t('cloud.providers.fields.credentialsKeepHelp') as string)
                : (t('cloud.providers.fields.credentialsHelp', { provider: form.provider }) as string)
            }
            required={secretsRequired}
          />

          <TextField
            fullWidth
            margin="normal"
            type="number"
            label={t('cloud.providers.fields.maxConcurrent') as string}
            value={form.max_concurrent_instances}
            onChange={(e) =>
              setForm({ ...form, max_concurrent_instances: parseInt(e.target.value, 10) || 0 })
            }
            inputProps={{ min: 0 }}
          />

          <TextField
            fullWidth
            margin="normal"
            type="number"
            label={t('cloud.providers.fields.maxHourlyCents') as string}
            value={form.max_instance_hourly_cents}
            onChange={(e) =>
              setForm({ ...form, max_instance_hourly_cents: parseInt(e.target.value, 10) || 0 })
            }
            helperText={t('cloud.providers.fields.maxHourlyHelp') as string}
            inputProps={{ min: 0 }}
          />

          <Typography variant="subtitle2" sx={{ mt: 3 }}>
            {t('cloud.providers.vpnSection') as string}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {t('cloud.providers.vpnSectionHelp') as string}
          </Typography>

          <FormControl fullWidth margin="normal">
            <InputLabel>{t('cloud.providers.fields.vpnProvider') as string}</InputLabel>
            <Select
              label={t('cloud.providers.fields.vpnProvider') as string}
              value={form.vpn_provider ?? 'tailscale'}
              onChange={(e) => handleVPNProviderChange(e.target.value as VPNProviderKind)}
            >
              <MenuItem value="tailscale">Tailscale</MenuItem>
              <MenuItem value="netbird">NetBird</MenuItem>
              <MenuItem value="wireguard">WireGuard</MenuItem>
            </Select>
          </FormControl>

          <FormControl fullWidth margin="normal">
            <InputLabel>{t('cloud.providers.fields.credentialKind') as string}</InputLabel>
            <Select
              label={t('cloud.providers.fields.credentialKind') as string}
              value={form.vpn_credential_kind ?? credentialKinds[0]}
              onChange={(e) =>
                setForm({ ...form, vpn_credential_kind: e.target.value as VPNCredentialKind })
              }
            >
              {credentialKinds.map((kind) => (
                <MenuItem key={kind} value={kind}>
                  {t(`cloud.providers.credentialKinds.${kind}`) as string}
                </MenuItem>
              ))}
            </Select>
          </FormControl>

          {(form.vpn_credential_kind === 'reusable_key' ||
            form.vpn_credential_kind === 'static_config') && (
            <Alert severity="warning" sx={{ mt: 1 }}>
              {form.vpn_credential_kind === 'reusable_key'
                ? (t('cloud.providers.reusableKeyWarning') as string)
                : (t('cloud.providers.staticConfigWarning') as string)}
            </Alert>
          )}

          <TextField
            fullWidth
            margin="normal"
            type="password"
            autoComplete="new-password"
            label={t('cloud.providers.fields.vpnCredential') as string}
            value={form.vpn_credential ?? ''}
            onChange={(e) => setForm({ ...form, vpn_credential: e.target.value })}
            helperText={
              editing
                ? (t('cloud.providers.fields.credentialsKeepHelp') as string)
                : (t(`cloud.providers.vpnCredentialHelp.${form.vpn_credential_kind ?? 'oauth'}`) as string)
            }
          />

          {form.vpn_credential_kind === 'reusable_key' && (
            <TextField
              fullWidth
              margin="normal"
              type="datetime-local"
              label={t('cloud.providers.fields.credentialExpiry') as string}
              InputLabelProps={{ shrink: true }}
              value={(form.settings?.vpn_credential_expires_at ?? '').slice(0, 16)}
              onChange={(e) =>
                setForm({
                  ...form,
                  settings: {
                    ...form.settings,
                    vpn_credential_expires_at: e.target.value
                      ? new Date(e.target.value).toISOString()
                      : '',
                  },
                })
              }
              helperText={t('cloud.providers.fields.credentialExpiryHelp') as string}
            />
          )}

          {form.vpn_provider !== 'wireguard' && (
            <TextField
              fullWidth
              margin="normal"
              label={t('cloud.providers.fields.vpnTag') as string}
              value={form.vpn_tag_or_group ?? ''}
              onChange={(e) => setForm({ ...form, vpn_tag_or_group: e.target.value })}
              helperText={t('cloud.providers.fields.vpnTagHelp') as string}
            />
          )}

          <TextField
            fullWidth
            margin="normal"
            label={t('cloud.providers.fields.backendVpnHost') as string}
            value={form.backend_vpn_host ?? ''}
            onChange={(e) => setForm({ ...form, backend_vpn_host: e.target.value })}
            helperText={t('cloud.providers.fields.backendVpnHostHelp') as string}
          />

          <FormControlLabel
            sx={{ mt: 2 }}
            control={
              <Checkbox
                checked={form.enabled}
                onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
              />
            }
            label={t('cloud.providers.fields.enabled') as string}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialogOpen(false)}>{t('buttons.cancel', { ns: 'common' }) as string}</Button>
          <Button
            variant="contained"
            onClick={handleSubmit}
            disabled={saveMutation.isPending || !form.name}
          >
            {t('cloud.providers.save') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* --- Third-party acknowledgement --- */}
      <Dialog open={Boolean(ackTarget)} onClose={() => setAckTarget(null)} maxWidth="sm" fullWidth>
        <DialogTitle>{t('cloud.providers.ackTitle') as string}</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>
            <AlertTitle>{t('cloud.providers.vastWarningTitle') as string}</AlertTitle>
            {/* Keyed by tier: naming the wrong provider in a data-exposure
                consent dialog is the whole risk this dialog exists to manage. */}
            {ackTarget && (t(`cloud.providers.ackBody.${ackTarget.provider}`) as string)}
          </Alert>
          <DialogContentText>{t('cloud.providers.ackConfirm') as string}</DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAckTarget(null)}>{t('buttons.cancel', { ns: 'common' }) as string}</Button>
          <Button
            color="warning"
            variant="contained"
            disabled={ackMutation.isPending}
            onClick={() => ackTarget && ackMutation.mutate(ackTarget.id)}
          >
            {t('cloud.providers.ackAccept') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* --- Delete --- */}
      <Dialog open={Boolean(deleteTarget)} onClose={() => setDeleteTarget(null)}>
        <DialogTitle>{t('cloud.providers.deleteTitle') as string}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t('cloud.providers.deleteBody', { name: deleteTarget?.name }) as string}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteTarget(null)}>{t('buttons.cancel', { ns: 'common' }) as string}</Button>
          <Button
            color="error"
            variant="contained"
            disabled={deleteMutation.isPending}
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
          >
            {t('cloud.providers.delete') as string}
          </Button>
        </DialogActions>
      </Dialog>

      {/* --- Preflight report --- */}
      <Dialog open={Boolean(preflight)} onClose={() => setPreflight(null)} maxWidth="sm" fullWidth>
        <DialogTitle>{t('cloud.providers.preflightTitle') as string}</DialogTitle>
        <DialogContent>
          {preflight && (
            <>
              <Alert severity={preflight.ok ? 'success' : 'error'} sx={{ mb: 2 }}>
                {preflight.ok
                  ? (t('cloud.providers.preflightOk') as string)
                  : (t('cloud.providers.preflightFailedBody') as string)}
              </Alert>
              {preflight.identity && (
                <Typography variant="body2" sx={{ mb: 1 }}>
                  {t('cloud.providers.preflightIdentity', { identity: preflight.identity }) as string}
                </Typography>
              )}
              {typeof preflight.quota_limit === 'number' && (
                <Typography variant="body2" sx={{ mb: 1 }}>
                  {t('cloud.providers.preflightQuota', {
                    used: preflight.quota_used ?? 0,
                    limit: preflight.quota_limit,
                    source: preflight.quota_source ?? '',
                  }) as string}
                </Typography>
              )}
              {[
                { key: 'missing_permissions', severity: 'error' as const },
                // Inconclusive checks are failures, not passes: an unknown is
                // not permission to spend money.
                { key: 'inconclusive', severity: 'warning' as const },
                { key: 'errors', severity: 'error' as const },
                { key: 'warnings', severity: 'warning' as const },
              ].map(({ key, severity }) => {
                const items = (preflight as any)[key] as string[] | undefined;
                if (!items || items.length === 0) return null;
                return (
                  <Alert key={key} severity={severity} sx={{ mb: 1 }}>
                    <AlertTitle>{t(`cloud.providers.preflightSections.${key}`) as string}</AlertTitle>
                    <List dense disablePadding>
                      {items.map((item) => (
                        <ListItem key={item} disableGutters sx={{ py: 0 }}>
                          <ListItemText primary={item} />
                        </ListItem>
                      ))}
                    </List>
                  </Alert>
                );
              })}
            </>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setPreflight(null)}>{t('buttons.close', { ns: 'common' }) as string}</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default CloudProviderSettings;
