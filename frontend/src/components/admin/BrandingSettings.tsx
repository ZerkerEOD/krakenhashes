/**
 * BrandingSettings — Admin → Settings → System Settings → Branding (GitHub issue #41).
 *
 * Application name, page title, primary / secondary accent colours and the
 * logo / favicon uploads. The "powered by KrakenHashes" attribution is enforced
 * by the backend and only displayed here; it cannot be edited.
 */
import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CircularProgress,
  Grid,
  IconButton,
  InputAdornment,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import CloudUploadIcon from '@mui/icons-material/CloudUpload';
import DeleteIcon from '@mui/icons-material/Delete';
import ClearIcon from '@mui/icons-material/Clear';
import SaveIcon from '@mui/icons-material/Save';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';
import {
  AdminBranding,
  BrandingAssetKind,
  BrandingSettingsInput,
  deleteBrandingAsset,
  getBrandingSettings,
  updateBrandingSettings,
  uploadBrandingAsset,
} from '../../services/branding';
import { useBranding } from '../../contexts/BrandingContext';
import { DEFAULT_PRIMARY, isHexColor } from '../../styles/theme';

const LOGO_MAX_BYTES = 2 * 1024 * 1024;
const FAVICON_MAX_BYTES = 512 * 1024;

const EMPTY_INPUT: BrandingSettingsInput = {
  app_name: '',
  page_title: '',
  primary_color: '',
  secondary_color: '',
};

type Translate = (key: string, opts?: Record<string, unknown>) => string;

const errorMessage = (error: unknown, fallback: string): string => {
  const data = (error as { response?: { data?: { error?: string; message?: string } } })?.response?.data;
  return data?.error || data?.message || fallback;
};

interface ColorFieldProps {
  label: string;
  value: string;
  fallback: string;
  helper: string;
  onChange: (value: string) => void;
}

/** Text field with a native colour swatch and a clear button. */
const ColorField: React.FC<ColorFieldProps> = ({ label, value, fallback, helper, onChange }) => {
  const invalid = value !== '' && !isHexColor(value);
  return (
    <TextField
      fullWidth
      label={label}
      value={value}
      placeholder={fallback}
      onChange={(e) => onChange(e.target.value.trim())}
      error={invalid}
      helperText={invalid ? helper : undefined}
      inputProps={{ maxLength: 7, spellCheck: false }}
      InputProps={{
        startAdornment: (
          <InputAdornment position="start">
            <input
              type="color"
              aria-label={label}
              value={isHexColor(value) ? value : fallback}
              onChange={(e) => onChange(e.target.value)}
              style={{
                width: 28,
                height: 28,
                padding: 0,
                border: 'none',
                background: 'transparent',
                cursor: 'pointer',
              }}
            />
          </InputAdornment>
        ),
        endAdornment: value ? (
          <InputAdornment position="end">
            <IconButton size="small" aria-label="clear" onClick={() => onChange('')}>
              <ClearIcon fontSize="small" />
            </IconButton>
          </InputAdornment>
        ) : undefined,
      }}
    />
  );
};

interface AssetCardProps {
  kind: BrandingAssetKind;
  title: string;
  hint: string;
  currentUrl: string | null;
  fallbackUrl: string;
  accept: string;
  maxBytes: number;
  busy: boolean;
  onUpload: (file: File) => void;
  onRemove: () => void;
  t: Translate;
}

const AssetCard: React.FC<AssetCardProps> = ({
  kind, title, hint, currentUrl, fallbackUrl, accept, maxBytes, busy, onUpload, onRemove, t,
}) => {
  const inputRef = useRef<HTMLInputElement>(null);
  const { enqueueSnackbar } = useSnackbar();

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    if (file.size > maxBytes) {
      enqueueSnackbar(t('branding.errors.tooLarge', { max: Math.round(maxBytes / 1024) }), { variant: 'error' });
      return;
    }
    onUpload(file);
  };

  return (
    <Card variant="outlined" sx={{ height: '100%' }}>
      <CardContent>
        <Typography variant="subtitle1" gutterBottom>{title}</Typography>
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            height: kind === 'logo' ? 96 : 64,
            mb: 2,
            p: 1,
            borderRadius: 1,
            bgcolor: 'background.default',
            border: '1px dashed',
            borderColor: 'divider',
          }}
        >
          <img
            src={currentUrl ?? fallbackUrl}
            alt={title}
            style={{ maxHeight: '100%', maxWidth: '100%', objectFit: 'contain' }}
          />
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>{hint}</Typography>
        <input ref={inputRef} type="file" accept={accept} hidden onChange={handleFile} />
        <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
          <Button
            variant="outlined"
            startIcon={busy ? <CircularProgress size={16} /> : <CloudUploadIcon />}
            disabled={busy}
            onClick={() => inputRef.current?.click()}
          >
            {t(currentUrl ? 'branding.replace' : 'branding.upload')}
          </Button>
          {currentUrl && (
            <Button variant="text" color="error" startIcon={<DeleteIcon />} disabled={busy} onClick={onRemove}>
              {t('branding.remove')}
            </Button>
          )}
        </Box>
      </CardContent>
    </Card>
  );
};

const BrandingSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = useCallback<Translate>((key, opts) => t(key, opts) as string, [t]);
  const { enqueueSnackbar } = useSnackbar();
  const { branding, refresh } = useBranding();

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [busyAsset, setBusyAsset] = useState<BrandingAssetKind | null>(null);
  const [form, setForm] = useState<BrandingSettingsInput>(EMPTY_INPUT);
  const [state, setState] = useState<AdminBranding | null>(null);

  /** Replace everything, including the form (after load or a text-settings save). */
  const apply = useCallback((next: AdminBranding) => {
    setState(next);
    setForm({ ...EMPTY_INPUT, ...next.settings });
  }, []);

  /** Asset uploads/removals must not discard unsaved name/colour edits. */
  const applyAssets = useCallback((next: AdminBranding) => {
    setState(next);
  }, []);

  const load = useCallback(async () => {
    try {
      apply(await getBrandingSettings());
    } catch (error) {
      console.error('Failed to load branding settings:', error);
      enqueueSnackbar(tr('branding.errors.loadFailed'), { variant: 'error' });
    } finally {
      setLoading(false);
    }
  }, [apply, enqueueSnackbar, tr]);

  useEffect(() => {
    void load();
  }, [load]);

  const colorsValid =
    (form.primary_color === '' || isHexColor(form.primary_color)) &&
    (form.secondary_color === '' || isHexColor(form.secondary_color));

  const dirty =
    state !== null &&
    (form.app_name !== state.settings.app_name ||
      form.page_title !== state.settings.page_title ||
      form.primary_color !== state.settings.primary_color ||
      form.secondary_color !== state.settings.secondary_color);

  const handleSave = async () => {
    if (!colorsValid) return;
    setSaving(true);
    try {
      apply(await updateBrandingSettings(form));
      await refresh();
      enqueueSnackbar(tr('branding.messages.saved'), { variant: 'success' });
    } catch (error) {
      console.error('Failed to save branding settings:', error);
      enqueueSnackbar(errorMessage(error, tr('branding.errors.saveFailed')), { variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  const handleUpload = (kind: BrandingAssetKind) => async (file: File) => {
    setBusyAsset(kind);
    try {
      applyAssets(await uploadBrandingAsset(kind, file));
      await refresh();
      enqueueSnackbar(tr('branding.messages.uploaded'), { variant: 'success' });
    } catch (error) {
      console.error(`Failed to upload branding ${kind}:`, error);
      enqueueSnackbar(errorMessage(error, tr('branding.errors.uploadFailed')), { variant: 'error' });
    } finally {
      setBusyAsset(null);
    }
  };

  const handleRemove = (kind: BrandingAssetKind) => async () => {
    setBusyAsset(kind);
    try {
      applyAssets(await deleteBrandingAsset(kind));
      await refresh();
      enqueueSnackbar(tr('branding.messages.removed'), { variant: 'success' });
    } catch (error) {
      console.error(`Failed to remove branding ${kind}:`, error);
      enqueueSnackbar(errorMessage(error, tr('branding.errors.removeFailed')), { variant: 'error' });
    } finally {
      setBusyAsset(null);
    }
  };

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  const effective = state?.effective;
  const previewName = form.app_name.trim() || 'KrakenHashes';
  const previewBase = form.page_title.trim() || previewName;
  // Mirrors branding.ComposePageTitle on the server; the attribution text itself
  // is server-supplied (not translatable) so the preview matches what is enforced.
  const previewTitle =
    previewBase.toLowerCase() === 'krakenhashes' ? 'KrakenHashes' : `${previewBase} · ${branding.powered_by}`;

  return (
    <Box>
      <Typography variant="h6" gutterBottom>{tr('branding.title')}</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {tr('branding.description')}
      </Typography>
      <Alert severity="info" sx={{ mb: 3 }}>{tr('branding.attributionNote')}</Alert>

      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="subtitle1" gutterBottom>{tr('branding.identity')}</Typography>
          <Grid container spacing={2}>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label={tr('branding.appName')}
                value={form.app_name}
                placeholder="KrakenHashes"
                inputProps={{ maxLength: 64 }}
                helperText={tr('branding.appNameHelper')}
                onChange={(e) => setForm({ ...form, app_name: e.target.value })}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label={tr('branding.pageTitle')}
                value={form.page_title}
                placeholder={previewName}
                inputProps={{ maxLength: 120 }}
                helperText={tr('branding.pageTitleHelper', { title: previewTitle })}
                onChange={(e) => setForm({ ...form, page_title: e.target.value })}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <ColorField
                label={tr('branding.primaryColor')}
                value={form.primary_color}
                fallback={DEFAULT_PRIMARY}
                helper={tr('branding.errors.invalidHex')}
                onChange={(v) => setForm({ ...form, primary_color: v })}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <ColorField
                label={tr('branding.secondaryColor')}
                value={form.secondary_color}
                fallback="#9c27b0"
                helper={tr('branding.errors.invalidHex')}
                onChange={(v) => setForm({ ...form, secondary_color: v })}
              />
            </Grid>
            <Grid item xs={12}>
              <Typography variant="caption" color="text.secondary">{tr('branding.colorHint')}</Typography>
            </Grid>
          </Grid>
          <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 2 }}>
            <Tooltip title={!colorsValid ? tr('branding.errors.invalidHex') : ''}>
              <span>
                <Button
                  variant="contained"
                  startIcon={saving ? <CircularProgress size={16} color="inherit" /> : <SaveIcon />}
                  disabled={saving || !dirty || !colorsValid}
                  onClick={handleSave}
                >
                  {tr('branding.save')}
                </Button>
              </span>
            </Tooltip>
          </Box>
        </CardContent>
      </Card>

      <Grid container spacing={2}>
        <Grid item xs={12} md={6}>
          <AssetCard
            kind="logo"
            title={tr('branding.logo')}
            hint={tr('branding.logoHint')}
            currentUrl={effective?.logo_url ?? null}
            fallbackUrl="/logo.png"
            accept="image/png,image/jpeg"
            maxBytes={LOGO_MAX_BYTES}
            busy={busyAsset === 'logo'}
            onUpload={handleUpload('logo')}
            onRemove={handleRemove('logo')}
            t={tr}
          />
        </Grid>
        <Grid item xs={12} md={6}>
          <AssetCard
            kind="favicon"
            title={tr('branding.favicon')}
            hint={tr('branding.faviconHint')}
            currentUrl={effective?.favicon_url ?? null}
            fallbackUrl="/favicon-32x32.png"
            accept="image/png,image/x-icon,image/vnd.microsoft.icon,.ico"
            maxBytes={FAVICON_MAX_BYTES}
            busy={busyAsset === 'favicon'}
            onUpload={handleUpload('favicon')}
            onRemove={handleRemove('favicon')}
            t={tr}
          />
        </Grid>
      </Grid>
    </Box>
  );
};

export default BrandingSettings;
