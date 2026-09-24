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
  Link,
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
  AWSZoneSelection,
  CloudPreflightReport,
  CloudProviderConfig,
  CloudProviderConfigInput,
  CloudProviderKind,
  CLOUD_DISCORD_URL,
  CLOUD_ISSUE_URL,
  CLOUD_PROVIDER_KINDS,
  providerMaturity,
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
import ProviderCapacityPicker, { PlacementSelection } from './ProviderCapacityPicker';

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

/**
 * Which providers the backend can actually build a client for.
 *
 * Kept in step with buildProvider in backend/internal/services/cloud/service.go.
 * A kind offered here but missing there lets an admin configure a provider,
 * enable it, allowlist it for a client and get silence — nothing fails until
 * the first launch attempt, three screens away from the mistake.
 *
 * All five are now implemented. Maturity, not implementation, is what separates
 * them — see providerMaturity and the Beta warning below.
 */
const IMPLEMENTED_PROVIDERS: CloudProviderKind[] = [
  'aws',
  'vastai',
  'runpod',
  'runpod_community',
  'mock',
];

/**
 * The settings key holding the VPN control-plane URL, per VPN provider.
 *
 * Self-hosted deployments are the norm for NetBird and common for Tailscale
 * (Headscale), and without this the client is pointed at the vendor's SaaS.
 * Read back by netbirdManagementURL/tailscaleLoginServer in
 * backend/internal/services/cloud/vpn.go.
 */
const VPN_URL_SETTING: Partial<Record<VPNProviderKind, string>> = {
  netbird: 'netbird_management_url',
  tailscale: 'tailscale_login_server',
};

/**
 * One row of the AWS instance-type table.
 *
 * AWSSettings splits this across two maps keyed by instance type
 * (instance_type_rates and instance_type_gpus). That is the right shape on the
 * wire and the wrong shape for a form, so the UI flattens it and re-splits on
 * write — an operator should never have to keep two maps in agreement by hand.
 */
interface AWSInstanceRow {
  instance_type: string;
  hourly_cents: number;
  gpu_model: string;
  gpu_count: number;
  vram_gb_per_gpu: number;
}

const emptyAWSRow = (): AWSInstanceRow => ({
  instance_type: '',
  hourly_cents: 0,
  gpu_model: '',
  gpu_count: 1,
  vram_gb_per_gpu: 0,
});

const awsRowsFromSettings = (settings: Record<string, any>): AWSInstanceRow[] => {
  const rates = (settings?.instance_type_rates ?? {}) as Record<string, number>;
  const gpus = (settings?.instance_type_gpus ?? {}) as Record<string, any>;
  const types = Array.from(new Set([...Object.keys(rates), ...Object.keys(gpus)]));
  return types.map((instance_type) => ({
    instance_type,
    hourly_cents: rates[instance_type] ?? 0,
    gpu_model: gpus[instance_type]?.gpu_model ?? '',
    gpu_count: gpus[instance_type]?.gpu_count ?? 1,
    vram_gb_per_gpu: gpus[instance_type]?.vram_gb_per_gpu ?? 0,
  }));
};

const awsRowsToSettings = (rows: AWSInstanceRow[]) => {
  const instance_type_rates: Record<string, number> = {};
  const instance_type_gpus: Record<string, any> = {};
  rows.forEach((row) => {
    const key = row.instance_type.trim();
    if (!key) return;
    instance_type_rates[key] = row.hourly_cents;
    /*
     * The GPU declaration is optional and must stay genuinely absent when the
     * model is blank. Writing {gpu_model: ""} instead would match no entry in
     * the class table while still looking declared, and cost-per-work ranking
     * would quietly collapse to price-per-hour with nothing to show for it.
     */
    if (row.gpu_model.trim()) {
      instance_type_gpus[key] = {
        gpu_model: row.gpu_model.trim(),
        gpu_count: row.gpu_count || 1,
        ...(row.vram_gb_per_gpu > 0 ? { vram_gb_per_gpu: row.vram_gb_per_gpu } : {}),
      };
    }
  });
  return { instance_type_rates, instance_type_gpus };
};

/*
 * AWS zone settings <-> the picker's generic placement selection.
 *
 * Both directions preserve the "empty list means ALL" convention exactly:
 * instance_types and hardware are the same field wearing different names, and
 * translating an empty list into an explicit one would silently freeze the
 * selection so that a newly priced instance type never joins it.
 */
const awsZonesToSelection = (zones: AWSZoneSelection[]): PlacementSelection[] =>
  zones.map((z) => ({
    // The picker keys on the STABLE id. Falling back to the alias keeps a
    // config written before zone_id was recorded from vanishing off the screen.
    id: z.zone_id || z.zone,
    name: z.zone || z.zone_id,
    ref: z.subnet_id,
    hardware: z.instance_types ?? [],
  }));

const selectionToAWSZones = (sel: PlacementSelection[]): AWSZoneSelection[] =>
  sel.map((s) => ({
    zone: s.name,
    zone_id: s.id,
    subnet_id: s.ref,
    instance_types: s.hardware,
  }));

/** Comma-or-newline separated free text into a clean list. */
const splitList = (raw: string): string[] =>
  raw
    .split(/[,\n]/)
    .map((s) => s.trim())
    .filter(Boolean);

/*
 * Vast.ai settings <-> the picker's placement selection, in 'axes' mode.
 *
 * Vast stores two INDEPENDENT lists rather than a per-country matrix, so every
 * placement carries the same hardware list. The picker is told this via
 * mode="axes" and behaves accordingly; flattening on save without saying so
 * would silently discard a per-country choice the screen appeared to offer.
 */
const vastToSelection = (settings: Record<string, any>): PlacementSelection[] => {
  const countries = (settings?.countries ?? []) as string[];
  const models = (settings?.gpu_models ?? []) as string[];
  return countries.map((c) => ({ id: c, name: c, ref: c, hardware: models }));
};

const selectionToVast = (sel: PlacementSelection[]) => ({
  countries: sel.map((s) => s.id),
  // Any entry answers for all of them in axes mode; empty means every card.
  gpu_models: sel[0]?.hardware ?? [],
});

/*
 * RunPod settings <-> the picker's placement selection, in 'axes' mode.
 *
 * Like Vast.ai, RunPod stores two independent lists rather than a matrix. The
 * "anywhere" row the explorer synthesises has id "any" and is NOT a real data
 * centre, so it must never be written back as one — doing so would send
 * dataCenterIds:["any"] on create and fail every launch.
 */
const runpodToSelection = (settings: Record<string, any>): PlacementSelection[] => {
  const dcs = (settings?.data_center_ids ?? []) as string[];
  const types = (settings?.gpu_type_ids ?? []) as string[];
  if (dcs.length === 0) {
    // Unpinned is RunPod's WIDEST setting, not its narrowest: their scheduler
    // may place a pod anywhere. Shown as the synthetic "any" row.
    return [{ id: 'any', name: 'Anywhere (RunPod chooses)', ref: '', hardware: types }];
  }
  return dcs.map((d) => ({ id: d, name: d, ref: d, hardware: types }));
};

const selectionToRunPod = (sel: PlacementSelection[]) => ({
  data_center_ids: sel.map((s) => s.id).filter((id) => id !== 'any'),
  gpu_type_ids: sel[0]?.hardware ?? [],
});

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
  /*
   * AWS instance types are held here rather than derived from form.settings on
   * every render, because awsRowsToSettings drops rows with a blank instance
   * type — deriving would delete a half-typed row the moment it was created.
   */
  const [awsRows, setAwsRows] = useState<AWSInstanceRow[]>([]);
  // AWS credentials are a JSON blob on the wire but two fields to a human.
  const [awsKeyID, setAwsKeyID] = useState('');
  const [awsSecret, setAwsSecret] = useState('');

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
    setAwsRows([emptyAWSRow()]);
    setAwsKeyID('');
    setAwsSecret('');
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
    const rows = awsRowsFromSettings(cfg.settings ?? {});
    setAwsRows(rows.length ? rows : [emptyAWSRow()]);
    setAwsKeyID('');
    setAwsSecret('');
    setFormError(null);
    setDialogOpen(true);
  };

  /** Write one provider-settings key without disturbing the others. */
  const setSetting = (key: string, value: any) =>
    setForm((f) => ({ ...f, settings: { ...f.settings, [key]: value } }));

  /** Update the instance-type table and keep both settings maps in step. */
  const updateAwsRows = (rows: AWSInstanceRow[]) => {
    setAwsRows(rows);
    setForm((f) => ({ ...f, settings: { ...f.settings, ...awsRowsToSettings(rows) } }));
  };

  const patchAwsRow = (index: number, patch: Partial<AWSInstanceRow>) =>
    updateAwsRows(awsRows.map((row, i) => (i === index ? { ...row, ...patch } : row)));

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

    /*
     * AWS credentials go over the wire as a JSON document, because that is
     * what buildProvider unmarshals into AWSCredentials. Serialised here so
     * the operator types an access key and a secret rather than JSON.
     *
     * Only sent when something was typed: blank means "keep the stored
     * credential", and writing {"access_key_id":"","secret_access_key":""}
     * would overwrite a working secret with an empty one.
     */
    if (payload.provider === 'aws') {
      if (awsKeyID.trim() || awsSecret.trim()) {
        payload.credentials = JSON.stringify({
          access_key_id: awsKeyID.trim(),
          secret_access_key: awsSecret.trim(),
        });
      } else {
        payload.credentials = '';
      }
      payload.settings = { ...payload.settings, ...awsRowsToSettings(awsRows) };
    }

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
                      {/*
                        * A SEPARATE badge from the third-party one, deliberately.
                        * "Nobody has proven this works" and "this runs on hardware
                        * you do not control" are different risks that happen to
                        * overlap on Vast: RunPod Secure is experimental but
                        * first-party, and collapsing them would hide that.
                        *
                        * BOTH states are rendered, unlike the Fleet page. This
                        * table is where providers are compared side by side, and
                        * "proven" inferred from the absence of a warning is not a
                        * claim anyone reads. The Fleet page stays warning-only
                        * because a Tested chip on every AWS instance row is noise
                        * on a page an operator watches continuously.
                        */}
                      {cfg.maturity === 'experimental' ? (
                        <Tooltip title={t('cloud.providers.experimentalTooltip') as string}>
                          <Chip
                            size="small"
                            variant="outlined"
                            color="warning"
                            label={t('cloud.providers.experimentalChip') as string}
                            sx={{ ml: 1, height: 20, fontSize: '0.7rem' }}
                          />
                        </Tooltip>
                      ) : (
                        <Tooltip title={t('cloud.providers.testedTooltip') as string}>
                          <Chip
                            size="small"
                            variant="outlined"
                            color="success"
                            label={t('cloud.providers.testedChip') as string}
                            sx={{ ml: 1, height: 20, fontSize: '0.7rem' }}
                          />
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
      {/* md, not sm: the AWS section carries a five-column instance-type table. */}
      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)} maxWidth="md" fullWidth>
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

          {!IMPLEMENTED_PROVIDERS.includes(form.provider) && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {t('cloud.providers.providerNotImplemented') as string}
            </Alert>
          )}

          {/*
            * Server value first, mirror second. A saved config carries a
            * server-computed maturity; the mirror only covers the create form,
            * where nothing has been saved to ask the server about.
            */}
          {(editing?.maturity ?? providerMaturity(form.provider)) === 'experimental' && (
            <Alert severity="warning" sx={{ mb: 2 }}>
              <AlertTitle>{t('cloud.providers.experimentalTitle') as string}</AlertTitle>
              {t('cloud.providers.experimentalBody') as string}
              {/*
                * The two destinations are deliberately separate links rather
                * than one "get help" pointer: the bug report is public and the
                * diagnostic bundle must not be.
                */}
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

          {/* AWS authenticates with an access key pair, not a single token. */}
          {form.provider === 'aws' ? (
            <>
              <TextField
                fullWidth
                margin="normal"
                autoComplete="off"
                label={t('cloud.providers.fields.awsAccessKeyId') as string}
                value={awsKeyID}
                onChange={(e) => setAwsKeyID(e.target.value)}
                helperText={
                  editing
                    ? (t('cloud.providers.fields.credentialsKeepHelp') as string)
                    : (t('cloud.providers.fields.awsAccessKeyIdHelp') as string)
                }
                required={secretsRequired}
              />
              <TextField
                fullWidth
                margin="normal"
                type="password"
                autoComplete="new-password"
                label={t('cloud.providers.fields.awsSecretAccessKey') as string}
                value={awsSecret}
                onChange={(e) => setAwsSecret(e.target.value)}
                required={secretsRequired}
              />
            </>
          ) : (
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
                  : (t('cloud.providers.fields.credentialsHelp', {
                      provider: form.provider,
                    }) as string)
              }
              required={secretsRequired}
            />
          )}

          {form.provider === 'aws' && (
            <>
              <Typography variant="subtitle2" sx={{ mt: 3 }}>
                {t('cloud.providers.awsSection') as string}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {t('cloud.providers.awsSectionHelp') as string}
              </Typography>

              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.awsRegion') as string}
                value={form.settings?.region ?? ''}
                onChange={(e) => setSetting('region', e.target.value.trim())}
                helperText={t('cloud.providers.fields.awsRegionHelp') as string}
                required
              />
              {/*
                * THE SUBNET ID FIELD IS DELIBERATELY GONE.
                *
                * It was a second, contradictory way to express the same thing
                * the zone picker below expresses: placement. Showing both
                * invited editing a value the backend ignores the moment any
                * zone is selected, which is exactly how it read to an operator.
                *
                * Nothing is lost. settings.subnet_id is preserved untouched on
                * save (the form spreads existing settings), the backend's
                * placements() still falls back to it when no zones are
                * configured, and the picker pre-selects that subnet's zone and
                * labels it as legacy — so opening the picker once and saving
                * converts the config across.
                */}
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.awsSecurityGroups') as string}
                value={(form.settings?.security_group_ids ?? []).join(', ')}
                onChange={(e) =>
                  setSetting(
                    'security_group_ids',
                    e.target.value
                      .split(',')
                      .map((s) => s.trim())
                      .filter(Boolean)
                  )
                }
                helperText={t('cloud.providers.fields.awsSecurityGroupsHelp') as string}
                required
              />
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.awsAmiSsm') as string}
                value={form.settings?.ami_ssm_parameter ?? ''}
                onChange={(e) => setSetting('ami_ssm_parameter', e.target.value.trim())}
                helperText={t('cloud.providers.fields.awsAmiSsmHelp') as string}
              />
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.awsAmiId') as string}
                value={form.settings?.ami_id ?? ''}
                onChange={(e) => setSetting('ami_id', e.target.value.trim())}
                helperText={t('cloud.providers.fields.awsAmiIdHelp') as string}
              />
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.awsInstanceProfile') as string}
                value={form.settings?.iam_instance_profile_arn ?? ''}
                onChange={(e) => setSetting('iam_instance_profile_arn', e.target.value.trim())}
                helperText={t('cloud.providers.fields.awsInstanceProfileHelp') as string}
              />
              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  fullWidth
                  margin="normal"
                  type="number"
                  label={t('cloud.providers.fields.awsRootVolume') as string}
                  value={form.settings?.root_volume_gb ?? 100}
                  onChange={(e) =>
                    setSetting('root_volume_gb', parseInt(e.target.value, 10) || 0)
                  }
                  helperText={t('cloud.providers.fields.awsRootVolumeHelp') as string}
                  inputProps={{ min: 0 }}
                />
                <TextField
                  fullWidth
                  margin="normal"
                  type="number"
                  label={t('cloud.providers.fields.awsEbsRate') as string}
                  value={form.settings?.ebs_cents_per_gb_month ?? 8}
                  onChange={(e) =>
                    setSetting('ebs_cents_per_gb_month', parseFloat(e.target.value) || 0)
                  }
                  helperText={t('cloud.providers.fields.awsEbsRateHelp') as string}
                  inputProps={{ min: 0, step: 0.1 }}
                />
              </Box>
              <FormControlLabel
                sx={{ mt: 1 }}
                control={
                  <Checkbox
                    checked={Boolean(form.settings?.use_spot)}
                    onChange={(e) => setSetting('use_spot', e.target.checked)}
                  />
                }
                label={t('cloud.providers.fields.awsUseSpot') as string}
              />
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {t('cloud.providers.fields.awsUseSpotHelp') as string}
              </Typography>

              <Typography variant="subtitle2" sx={{ mt: 2 }}>
                {t('cloud.providers.awsInstanceTypes') as string}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {t('cloud.providers.awsInstanceTypesHelp') as string}
              </Typography>
              {awsRows.map((row, i) => (
                <Box key={i} sx={{ display: 'flex', gap: 1, alignItems: 'flex-start', mb: 1 }}>
                  <TextField
                    sx={{ flex: 2 }}
                    size="small"
                    label={t('cloud.providers.fields.awsInstanceType') as string}
                    value={row.instance_type}
                    onChange={(e) => patchAwsRow(i, { instance_type: e.target.value })}
                  />
                  <TextField
                    sx={{ flex: 1 }}
                    size="small"
                    type="number"
                    label={t('cloud.providers.fields.awsHourlyCents') as string}
                    value={row.hourly_cents}
                    onChange={(e) =>
                      patchAwsRow(i, { hourly_cents: parseInt(e.target.value, 10) || 0 })
                    }
                    inputProps={{ min: 0 }}
                  />
                  <TextField
                    sx={{ flex: 2 }}
                    size="small"
                    label={t('cloud.providers.fields.awsGpuModel') as string}
                    value={row.gpu_model}
                    onChange={(e) => patchAwsRow(i, { gpu_model: e.target.value })}
                  />
                  <TextField
                    sx={{ flex: 1 }}
                    size="small"
                    type="number"
                    label={t('cloud.providers.fields.awsGpuCount') as string}
                    value={row.gpu_count}
                    onChange={(e) =>
                      patchAwsRow(i, { gpu_count: parseInt(e.target.value, 10) || 0 })
                    }
                    inputProps={{ min: 0 }}
                  />
                  <TextField
                    sx={{ flex: 1 }}
                    size="small"
                    type="number"
                    label={t('cloud.providers.fields.awsVram') as string}
                    value={row.vram_gb_per_gpu}
                    onChange={(e) =>
                      patchAwsRow(i, { vram_gb_per_gpu: parseInt(e.target.value, 10) || 0 })
                    }
                    inputProps={{ min: 0 }}
                  />
                  <Tooltip title={t('cloud.providers.awsRemoveInstanceType') as string}>
                    <span>
                      <Button
                        size="small"
                        color="error"
                        disabled={awsRows.length <= 1}
                        onClick={() => updateAwsRows(awsRows.filter((_, j) => j !== i))}
                        sx={{ mt: 0.5, minWidth: 0 }}
                      >
                        <DeleteIcon fontSize="small" />
                      </Button>
                    </span>
                  </Tooltip>
                </Box>
              ))}
              <Button
                size="small"
                startIcon={<AddIcon />}
                onClick={() => updateAwsRows([...awsRows, emptyAWSRow()])}
              >
                {t('cloud.providers.awsAddInstanceType') as string}
              </Button>

              {/*
                * The picker speaks placement/hardware; AWS settings speak
                * zone/instance-type. Adapting here rather than teaching the
                * picker AWS's vocabulary is what lets Vast and RunPod reuse it
                * unchanged — they store different keys for the same idea.
                */}
              <ProviderCapacityPicker
                providerConfigId={editing?.id ?? null}
                selection={awsZonesToSelection((form.settings?.zones ?? []) as AWSZoneSelection[])}
                fallbackHardware={awsRows.map((r) => r.instance_type.trim()).filter(Boolean)}
                legacyRefNote={form.settings?.subnet_id ?? ''}
                onChange={(sel) => setSetting('zones', selectionToAWSZones(sel))}
              />
            </>
          )}

          {form.provider === 'vastai' && (
            <>
              <Typography variant="subtitle2" sx={{ mt: 3 }}>
                {t('cloud.providers.vastSection') as string}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {t('cloud.providers.vastSectionHelp') as string}
              </Typography>

              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.vastCountries') as string}
                value={(form.settings?.countries ?? []).join(', ')}
                onChange={(e) => setSetting('countries', splitList(e.target.value))}
                helperText={t('cloud.providers.fields.vastCountriesHelp') as string}
              />
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.vastGpuModels') as string}
                value={(form.settings?.gpu_models ?? []).join(', ')}
                onChange={(e) => setSetting('gpu_models', splitList(e.target.value))}
                helperText={t('cloud.providers.fields.vastGpuModelsHelp') as string}
              />
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.vastDeniedGpuModels') as string}
                value={(form.settings?.denied_gpu_models ?? []).join(', ')}
                onChange={(e) => setSetting('denied_gpu_models', splitList(e.target.value))}
                helperText={t('cloud.providers.fields.vastDeniedGpuModelsHelp') as string}
              />
              <TextField
                fullWidth
                margin="normal"
                type="number"
                label={t('cloud.providers.fields.vastMinReliability') as string}
                value={form.settings?.min_reliability ?? 0}
                onChange={(e) => setSetting('min_reliability', parseFloat(e.target.value) || 0)}
                helperText={t('cloud.providers.fields.vastMinReliabilityHelp') as string}
                inputProps={{ min: 0, max: 1, step: 0.01 }}
              />

              {/*
                * The two tier toggles are grouped and warned about together
                * because they are the same decision twice, and it is the only
                * decision on this screen that moves client hash material onto
                * machines nobody has vetted.
                */}
              <FormControlLabel
                sx={{ mt: 1 }}
                control={
                  <Checkbox
                    checked={Boolean(form.settings?.allow_unverified)}
                    onChange={(e) => setSetting('allow_unverified', e.target.checked)}
                  />
                }
                label={t('cloud.providers.fields.vastAllowUnverified') as string}
              />
              <FormControlLabel
                control={
                  <Checkbox
                    checked={Boolean(form.settings?.allow_residential)}
                    onChange={(e) => setSetting('allow_residential', e.target.checked)}
                  />
                }
                label={t('cloud.providers.fields.vastAllowResidential') as string}
              />
              {(form.settings?.allow_unverified || form.settings?.allow_residential) && (
                <Alert severity="warning" sx={{ mt: 1, mb: 1 }}>
                  <AlertTitle>{t('cloud.providers.vastTierWarningTitle') as string}</AlertTitle>
                  {t('cloud.providers.vastTierWarningBody') as string}
                </Alert>
              )}

              <ProviderCapacityPicker
                providerConfigId={editing?.id ?? null}
                mode="axes"
                selection={vastToSelection(form.settings ?? {})}
                fallbackHardware={(form.settings?.gpu_models ?? []) as string[]}
                onChange={(sel) => {
                  const { countries, gpu_models } = selectionToVast(sel);
                  setForm((f) => ({
                    ...f,
                    settings: { ...f.settings, countries, gpu_models },
                  }));
                }}
              />
            </>
          )}

          {(form.provider === 'runpod' || form.provider === 'runpod_community') && (
            <>
              <Typography variant="subtitle2" sx={{ mt: 3 }}>
                {t('cloud.providers.runpodSection') as string}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {t('cloud.providers.runpodSectionHelp') as string}
              </Typography>

              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.runpodDataCenters') as string}
                value={(form.settings?.data_center_ids ?? []).join(', ')}
                onChange={(e) => setSetting('data_center_ids', splitList(e.target.value))}
                helperText={t('cloud.providers.fields.runpodDataCentersHelp') as string}
              />
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.runpodGpuTypes') as string}
                value={(form.settings?.gpu_type_ids ?? []).join(', ')}
                onChange={(e) => setSetting('gpu_type_ids', splitList(e.target.value))}
                helperText={t('cloud.providers.fields.runpodGpuTypesHelp') as string}
              />
              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  fullWidth
                  margin="normal"
                  type="number"
                  label={t('cloud.providers.fields.runpodDiskGb') as string}
                  value={form.settings?.container_disk_gb ?? 0}
                  onChange={(e) => setSetting('container_disk_gb', parseInt(e.target.value, 10) || 0)}
                  helperText={t('cloud.providers.fields.runpodDiskGbHelp') as string}
                  inputProps={{ min: 0 }}
                />
                <TextField
                  fullWidth
                  margin="normal"
                  type="number"
                  label={t('cloud.providers.fields.runpodDiskRate') as string}
                  value={form.settings?.container_disk_cents_per_gb_month ?? 10}
                  onChange={(e) =>
                    setSetting('container_disk_cents_per_gb_month', parseFloat(e.target.value) || 0)
                  }
                  helperText={t('cloud.providers.fields.runpodDiskRateHelp') as string}
                  inputProps={{ min: 0, step: 0.1 }}
                />
              </Box>

              {/*
                * Community teardown is materially weaker than Secure and the
                * operator has to know before they enable it, not after a pod
                * has been billing overnight.
                */}
              {form.provider === 'runpod_community' ? (
                <Alert severity="warning" sx={{ mt: 2, mb: 1 }}>
                  <AlertTitle>{t('cloud.providers.runpodCommunityTeardownTitle') as string}</AlertTitle>
                  {t('cloud.providers.runpodCommunityTeardownBody') as string}
                </Alert>
              ) : (
                <>
                  <FormControlLabel
                    sx={{ mt: 1 }}
                    control={
                      <Checkbox
                        checked={Boolean(form.settings?.allow_in_guest_self_destruct)}
                        onChange={(e) =>
                          setSetting('allow_in_guest_self_destruct', e.target.checked)
                        }
                      />
                    }
                    label={t('cloud.providers.fields.runpodSelfDestruct') as string}
                  />
                  <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                    {t('cloud.providers.fields.runpodSelfDestructHelp') as string}
                  </Typography>
                </>
              )}

              <ProviderCapacityPicker
                providerConfigId={editing?.id ?? null}
                mode="axes"
                selection={runpodToSelection(form.settings ?? {})}
                fallbackHardware={(form.settings?.gpu_type_ids ?? []) as string[]}
                onChange={(sel) => {
                  const { data_center_ids, gpu_type_ids } = selectionToRunPod(sel);
                  setForm((f) => ({
                    ...f,
                    settings: { ...f.settings, data_center_ids, gpu_type_ids },
                  }));
                }}
              />
            </>
          )}

          {form.provider === 'mock' && (
            <>
              <Typography variant="subtitle2" sx={{ mt: 3 }}>
                {t('cloud.providers.mockSection') as string}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {t('cloud.providers.mockSectionHelp') as string}
              </Typography>
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.mockAgentBinary') as string}
                value={form.settings?.agent_binary ?? ''}
                onChange={(e) => setSetting('agent_binary', e.target.value.trim())}
                helperText={t('cloud.providers.fields.mockAgentBinaryHelp') as string}
              />
              <TextField
                fullWidth
                margin="normal"
                label={t('cloud.providers.fields.mockBackendHost') as string}
                value={form.settings?.backend_host ?? ''}
                onChange={(e) => setSetting('backend_host', e.target.value.trim())}
                helperText={t('cloud.providers.fields.mockBackendHostHelp') as string}
              />
              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  fullWidth
                  margin="normal"
                  type="number"
                  label={t('cloud.providers.fields.mockHourlyRate') as string}
                  value={form.settings?.hourly_rate_cents ?? 0}
                  onChange={(e) =>
                    setSetting('hourly_rate_cents', parseInt(e.target.value, 10) || 0)
                  }
                  inputProps={{ min: 0 }}
                />
                <TextField
                  fullWidth
                  margin="normal"
                  type="number"
                  label={t('cloud.providers.fields.mockBootDelay') as string}
                  value={form.settings?.boot_delay_seconds ?? 0}
                  onChange={(e) =>
                    setSetting('boot_delay_seconds', parseInt(e.target.value, 10) || 0)
                  }
                  inputProps={{ min: 0 }}
                />
              </Box>
              {(
                [
                  ['fail_launch', 'mockFailLaunch'],
                  ['drop_launch_response', 'mockDropLaunchResponse'],
                  ['never_register', 'mockNeverRegister'],
                  ['fail_destroy', 'mockFailDestroy'],
                ] as const
              ).map(([key, label]) => (
                <FormControlLabel
                  key={key}
                  control={
                    <Checkbox
                      checked={Boolean(form.settings?.[key])}
                      onChange={(e) => setSetting(key, e.target.checked)}
                    />
                  }
                  label={t(`cloud.providers.fields.${label}`) as string}
                />
              ))}
            </>
          )}

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
                ? // Keyed by provider: the caveats are not shared. Tailscale's
                  // 90-day auth-key cap does not apply to NetBird, and the
                  // escape hatch is an OAuth client on one and a PAT on the
                  // other, so a single string is wrong for whichever provider
                  // it was not written for.
                  (t(
                    `cloud.providers.reusableKeyWarning.${
                      form.vpn_provider === 'netbird' ? 'netbird' : 'tailscale'
                    }`
                  ) as string)
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

          {/*
            * Control-plane URL. Required for a self-hosted NetBird (the mint
            * call posts to {url}/api/setup-keys and the client is started with
            * --management-url) and for Headscale. Blank means the vendor SaaS.
            */}
          {VPN_URL_SETTING[form.vpn_provider ?? 'tailscale'] && (
            <TextField
              fullWidth
              margin="normal"
              label={t(`cloud.providers.fields.vpnUrl.${form.vpn_provider}`) as string}
              value={form.settings?.[VPN_URL_SETTING[form.vpn_provider!]!] ?? ''}
              onChange={(e) =>
                setSetting(VPN_URL_SETTING[form.vpn_provider!]!, e.target.value.trim())
              }
              helperText={t(`cloud.providers.fields.vpnUrlHelp.${form.vpn_provider}`) as string}
            />
          )}

          {form.vpn_provider !== 'wireguard' && (
            <TextField
              fullWidth
              margin="normal"
              label={t('cloud.providers.fields.vpnTag') as string}
              value={form.vpn_tag_or_group ?? ''}
              onChange={(e) => setForm({ ...form, vpn_tag_or_group: e.target.value })}
              /*
               * NetBird only consumes this on the PAT path, as auto_groups on
               * the key it mints. start_netbird passes --setup-key and
               * --management-url and never reads KH_VPN_TAG, so with a reusable
               * key the field does nothing and the help must say so.
               */
              helperText={
                form.vpn_provider === 'netbird' && form.vpn_credential_kind === 'reusable_key'
                  ? (t('cloud.providers.fields.vpnTagIgnoredHelp') as string)
                  : (t('cloud.providers.fields.vpnTagHelp') as string)
              }
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
            <AlertTitle>{t('cloud.providers.peerWarningTitle') as string}</AlertTitle>
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
