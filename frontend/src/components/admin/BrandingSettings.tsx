/**
 * BrandingSettings — Admin → Settings → General → Branding (GitHub issue #41).
 *
 * Application name, page title, primary / secondary accent colours and the
 * logo / favicon uploads. Text and colour fields autosave individually by
 * merging into the last loaded settings. The "powered by KrakenHashes"
 * attribution is enforced by the backend and only displayed here.
 */
import React, { useRef, useState } from 'react';
import { Alert, Box, Button, Card, CardContent, CircularProgress, Grid, Typography } from '@mui/material';
import CloudUploadIcon from '@mui/icons-material/CloudUpload';
import DeleteIcon from '@mui/icons-material/Delete';
import { useQueryClient } from '@tanstack/react-query';
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
import { DEFAULT_PRIMARY, DEFAULT_SECONDARY } from '../../styles/tokens';
import GroupSettingsProvider from '../settings/GroupSettingsProvider';
import { ColorSetting, Panel, TextSetting, useSettingsCtx } from '../settings/fields';
import { useToast } from '../ui/toast';
import { getErrorMessage } from '../../utils/errors';
import { qk } from '../../services/queryKeys';

const LOGO_MAX_BYTES = 2 * 1024 * 1024;
const FAVICON_MAX_BYTES = 512 * 1024;

type Translate = (key: string, opts?: Record<string, unknown>) => string;

type BrandingGroup = {
  app_name: string;
  page_title: string;
  primary_color: string;
  secondary_color: string;
  /** Effective (resolved) asset URLs; no field binds to this. */
  _effective: AdminBranding['effective'];
};

const QUERY_KEY = qk.admin.group('branding');

const loadBranding = async (): Promise<BrandingGroup> => {
  const admin = await getBrandingSettings();
  return {
    app_name: admin.settings.app_name ?? '',
    page_title: admin.settings.page_title ?? '',
    primary_color: admin.settings.primary_color ?? '',
    secondary_color: admin.settings.secondary_color ?? '',
    _effective: admin.effective,
  };
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
  const toast = useToast();

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    if (file.size > maxBytes) {
      toast.error(t('branding.errors.tooLarge', { max: Math.round(maxBytes / 1024) }));
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
            bgcolor: 'surface.sunken',
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

/** Name, title and colours; reads the live drafts so the title preview tracks typing. */
const IdentityFields: React.FC<{ t: Translate }> = ({ t }) => {
  const { values } = useSettingsCtx();
  const { branding } = useBranding();
  const previewName = (values.app_name ?? '').trim() || 'KrakenHashes';
  const previewBase = (values.page_title ?? '').trim() || previewName;
  // Mirrors branding.ComposePageTitle on the server; the attribution text itself
  // is server-supplied (not translatable) so the preview matches what is enforced.
  const previewTitle =
    previewBase.toLowerCase() === 'krakenhashes' ? 'KrakenHashes' : `${previewBase} · ${branding.powered_by}`;

  return (
    <Panel title={t('branding.identity')} caption={t('branding.colorHint')}>
      <Grid item xs={12} md={6}>
        <TextSetting settingKey="app_name" label={t('branding.appName')} placeholder="KrakenHashes" maxLength={64} helper={t('branding.appNameHelper')} />
      </Grid>
      <Grid item xs={12} md={6}>
        <TextSetting
          settingKey="page_title"
          label={t('branding.pageTitle')}
          placeholder={previewName}
          maxLength={120}
          helper={t('branding.pageTitleHelper', { title: previewTitle })}
        />
      </Grid>
      <Grid item xs={12} md={6}>
        <ColorSetting settingKey="primary_color" label={t('branding.primaryColor')} fallback={DEFAULT_PRIMARY} helper={t('branding.errors.invalidHex')} />
      </Grid>
      <Grid item xs={12} md={6}>
        <ColorSetting settingKey="secondary_color" label={t('branding.secondaryColor')} fallback={DEFAULT_SECONDARY} helper={t('branding.errors.invalidHex')} />
      </Grid>
    </Panel>
  );
};

const BrandingSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr: Translate = (key, opts) => t(key, opts) as string;
  const toast = useToast();
  const queryClient = useQueryClient();
  const { refresh } = useBranding();
  const [busyAsset, setBusyAsset] = useState<BrandingAssetKind | null>(null);

  const saveField = async (key: keyof BrandingGroup & string, value: BrandingGroup[keyof BrandingGroup], current: BrandingGroup) => {
    if (key === '_effective') return;
    const input: BrandingSettingsInput = {
      app_name: current.app_name,
      page_title: current.page_title,
      primary_color: current.primary_color,
      secondary_color: current.secondary_color,
      [key]: value,
    };
    await updateBrandingSettings(input);
    await refresh();
  };

  const afterAssetChange = async () => {
    await queryClient.invalidateQueries({ queryKey: QUERY_KEY });
    await refresh();
  };

  const handleUpload = (kind: BrandingAssetKind) => async (file: File) => {
    setBusyAsset(kind);
    try {
      await uploadBrandingAsset(kind, file);
      await afterAssetChange();
      toast.success(tr('branding.messages.uploaded'));
    } catch (error) {
      toast.error(getErrorMessage(error) || tr('branding.errors.uploadFailed'));
    } finally {
      setBusyAsset(null);
    }
  };

  const handleRemove = (kind: BrandingAssetKind) => async () => {
    setBusyAsset(kind);
    try {
      await deleteBrandingAsset(kind);
      await afterAssetChange();
      toast.success(tr('branding.messages.removed'));
    } catch (error) {
      toast.error(getErrorMessage(error) || tr('branding.errors.removeFailed'));
    } finally {
      setBusyAsset(null);
    }
  };

  return (
    <GroupSettingsProvider<BrandingGroup>
      queryKey={QUERY_KEY}
      load={loadBranding}
      saveField={saveField}
      render={(data) => (
        <Box>
          <Alert severity="info" sx={{ mb: 3 }}>{tr('branding.attributionNote')}</Alert>
          <Grid container spacing={3}>
            <IdentityFields t={tr} />
            <Grid item xs={12} md={6}>
              <AssetCard
                kind="logo"
                title={tr('branding.logo')}
                hint={tr('branding.logoHint')}
                currentUrl={data._effective?.logo_url ?? null}
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
                currentUrl={data._effective?.favicon_url ?? null}
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
      )}
    />
  );
};

export default BrandingSettings;
