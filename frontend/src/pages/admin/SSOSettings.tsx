import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Typography,
  Paper,
  CircularProgress,
  Alert,
  Button,
  Switch,
  FormControlLabel,
  Divider,
  IconButton,
  Tooltip,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormHelperText,
  Accordion,
  AccordionSummary,
  AccordionDetails,
  Grid,
  Card,
  CardContent,
  CardActions
} from '@mui/material';
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid';
import AddIcon from '@mui/icons-material/Add';
import EditIcon from '@mui/icons-material/Edit';
import DeleteIcon from '@mui/icons-material/Delete';
import PlayArrowIcon from '@mui/icons-material/PlayArrow';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import SecurityIcon from '@mui/icons-material/Security';
import VpnKeyIcon from '@mui/icons-material/VpnKey';
import AccountTreeIcon from '@mui/icons-material/AccountTree';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import CancelIcon from '@mui/icons-material/Cancel';
import DownloadIcon from '@mui/icons-material/Download';
import { useSnackbar } from 'notistack';
import { useTranslation } from 'react-i18next';

import {
  SSOSettings,
  SSOSettingsUpdate,
  SSOProvider,
  SSOProviderType,
  CreateSSOProviderRequest,
  UpdateSSOProviderRequest,
  LDAPConfig,
  SAMLConfig,
  OAuthConfig,
  SSOProviderWithConfig
} from '../../types/sso';
import {
  getSSOSettings,
  updateSSOSettings,
  listSSOProviders,
  getSSOProvider,
  createSSOProvider,
  updateSSOProvider,
  deleteSSOProvider,
  testSSOProvider,
  getProviderTypeLabel
} from '../../services/sso';

const SSOSettingsPage: React.FC = () => {
  const { t } = useTranslation('admin');
  const [settings, setSettings] = useState<SSOSettings | null>(null);
  const [providers, setProviders] = useState<SSOProvider[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [providerDialogOpen, setProviderDialogOpen] = useState(false);
  const [editingProvider, setEditingProvider] = useState<SSOProviderWithConfig | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [providerToDelete, setProviderToDelete] = useState<SSOProvider | null>(null);
  const [testingProvider, setTestingProvider] = useState<string | null>(null);

  const { enqueueSnackbar } = useSnackbar();

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [settingsData, providersData] = await Promise.all([
        getSSOSettings(),
        listSSOProviders()
      ]);
      setSettings(settingsData);
      setProviders(providersData);
    } catch (err: any) {
      console.error('Failed to fetch SSO data:', err);
      setError(err.message || t('ssoSettings.messages.loadFailed') as string);
      enqueueSnackbar(t('ssoSettings.messages.loadFailed') as string, { variant: 'error' });
    } finally {
      setLoading(false);
    }
  }, [enqueueSnackbar, t]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleSettingsChange = async (field: keyof SSOSettings, value: boolean) => {
    if (!settings) return;

    setSaving(true);
    try {
      const update: SSOSettingsUpdate = { [field]: value };
      await updateSSOSettings(update);
      setSettings({ ...settings, [field]: value });
      enqueueSnackbar(t('ssoSettings.messages.settingsUpdated') as string, { variant: 'success' });
    } catch (err: any) {
      enqueueSnackbar(err.message || t('ssoSettings.messages.updateFailed') as string, { variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  const handleAddProvider = () => {
    setEditingProvider(null);
    setProviderDialogOpen(true);
  };

  const handleEditProvider = async (provider: SSOProvider) => {
    try {
      const fullProvider = await getSSOProvider(provider.id);
      setEditingProvider(fullProvider);
      setProviderDialogOpen(true);
    } catch (err: any) {
      enqueueSnackbar(err.message || t('ssoSettings.messages.loadProviderFailed') as string, { variant: 'error' });
    }
  };

  const handleDeleteClick = (provider: SSOProvider) => {
    setProviderToDelete(provider);
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!providerToDelete) return;

    try {
      await deleteSSOProvider(providerToDelete.id);
      setProviders(providers.filter(p => p.id !== providerToDelete.id));
      enqueueSnackbar(t('ssoSettings.messages.providerDeleted') as string, { variant: 'success' });
    } catch (err: any) {
      enqueueSnackbar(err.message || t('ssoSettings.messages.deleteFailed') as string, { variant: 'error' });
    } finally {
      setDeleteDialogOpen(false);
      setProviderToDelete(null);
    }
  };

  const handleTestProvider = async (providerId: string) => {
    setTestingProvider(providerId);
    try {
      const result = await testSSOProvider(providerId);
      if (result.success) {
        enqueueSnackbar(t('ssoSettings.messages.testSuccess') as string, { variant: 'success' });
      } else {
        enqueueSnackbar(result.message || t('ssoSettings.messages.testFailed') as string, { variant: 'error' });
      }
    } catch (err: any) {
      enqueueSnackbar(err.message || t('ssoSettings.messages.testFailed') as string, { variant: 'error' });
    } finally {
      setTestingProvider(null);
    }
  };

  const handleDownloadCertificate = async (providerId: string) => {
    try {
      const response = await fetch(`/api/auth/saml/${providerId}/metadata`);
      const xml = await response.text();

      const parser = new DOMParser();
      const doc = parser.parseFromString(xml, 'text/xml');
      const certElement = doc.querySelector('KeyDescriptor[use="signing"] X509Certificate');

      if (certElement) {
        const certBase64 = certElement.textContent;
        const pem = `-----BEGIN CERTIFICATE-----\n${certBase64}\n-----END CERTIFICATE-----`;

        const blob = new Blob([pem], { type: 'application/x-pem-file' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `sp-certificate-${providerId}.pem`;
        a.click();
        URL.revokeObjectURL(url);
        enqueueSnackbar(t('ssoSettings.messages.certificateDownloaded') as string, { variant: 'success' });
      } else {
        enqueueSnackbar(t('ssoSettings.messages.certificateNotFound') as string, { variant: 'error' });
      }
    } catch (error) {
      console.error('Failed to download certificate:', error);
      enqueueSnackbar(t('ssoSettings.messages.certificateDownloadFailed') as string, { variant: 'error' });
    }
  };

  const handleProviderSave = async (data: CreateSSOProviderRequest | UpdateSSOProviderRequest) => {
    try {
      if (editingProvider) {
        await updateSSOProvider(editingProvider.id, data as UpdateSSOProviderRequest);
        enqueueSnackbar(t('ssoSettings.messages.providerUpdated') as string, { variant: 'success' });
      } else {
        await createSSOProvider(data as CreateSSOProviderRequest);
        enqueueSnackbar(t('ssoSettings.messages.providerCreated') as string, { variant: 'success' });
      }
      setProviderDialogOpen(false);
      fetchData();
    } catch (err: any) {
      enqueueSnackbar(err.message || t('ssoSettings.messages.saveProviderFailed') as string, { variant: 'error' });
    }
  };

  const getProviderIcon = (type: SSOProviderType) => {
    switch (type) {
      case 'ldap':
        return <AccountTreeIcon />;
      case 'saml':
        return <SecurityIcon />;
      case 'oidc':
      case 'oauth2':
        return <VpnKeyIcon />;
      default:
        return <SecurityIcon />;
    }
  };

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">{error}</Alert>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            {t('ssoSettings.pageTitle') as string}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {t('ssoSettings.pageDescription') as string}
          </Typography>
        </Box>
      </Box>

      {/* Global Settings */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" gutterBottom>
          {t('ssoSettings.authMethods.title') as string}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('ssoSettings.authMethods.description') as string}
        </Typography>

        <Grid container spacing={3}>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              control={
                <Switch
                  checked={settings?.local_auth_enabled ?? true}
                  onChange={(e) => handleSettingsChange('local_auth_enabled', e.target.checked)}
                  disabled={saving}
                />
              }
              label={t('ssoSettings.authMethods.localAuth') as string}
            />
            <Typography variant="caption" display="block" color="text.secondary">
              {t('ssoSettings.authMethods.localAuthDescription') as string}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              control={
                <Switch
                  checked={settings?.ldap_auth_enabled ?? false}
                  onChange={(e) => handleSettingsChange('ldap_auth_enabled', e.target.checked)}
                  disabled={saving}
                />
              }
              label={t('ssoSettings.authMethods.ldapAuth') as string}
            />
            <Typography variant="caption" display="block" color="text.secondary">
              {t('ssoSettings.authMethods.ldapAuthDescription') as string}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              control={
                <Switch
                  checked={settings?.saml_auth_enabled ?? false}
                  onChange={(e) => handleSettingsChange('saml_auth_enabled', e.target.checked)}
                  disabled={saving}
                />
              }
              label={t('ssoSettings.authMethods.samlAuth') as string}
            />
            <Typography variant="caption" display="block" color="text.secondary">
              {t('ssoSettings.authMethods.samlAuthDescription') as string}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              control={
                <Switch
                  checked={settings?.oauth_auth_enabled ?? false}
                  onChange={(e) => handleSettingsChange('oauth_auth_enabled', e.target.checked)}
                  disabled={saving}
                />
              }
              label={t('ssoSettings.authMethods.oauthAuth') as string}
            />
            <Typography variant="caption" display="block" color="text.secondary">
              {t('ssoSettings.authMethods.oauthAuthDescription') as string}
            </Typography>
          </Grid>
        </Grid>

        <Divider sx={{ my: 3 }} />

        <Typography variant="h6" gutterBottom>
          {t('ssoSettings.provisioning.title') as string}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('ssoSettings.provisioning.description') as string}
        </Typography>

        <Grid container spacing={3}>
          <Grid item xs={12} sm={6}>
            <FormControlLabel
              control={
                <Switch
                  checked={settings?.sso_auto_create_users ?? true}
                  onChange={(e) => handleSettingsChange('sso_auto_create_users', e.target.checked)}
                  disabled={saving}
                />
              }
              label={t('ssoSettings.provisioning.autoCreateUsers') as string}
            />
            <Typography variant="caption" display="block" color="text.secondary">
              {t('ssoSettings.provisioning.autoCreateUsersDescription') as string}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6}>
            <FormControlLabel
              control={
                <Switch
                  checked={settings?.sso_auto_enable_users ?? false}
                  onChange={(e) => handleSettingsChange('sso_auto_enable_users', e.target.checked)}
                  disabled={saving}
                />
              }
              label={t('ssoSettings.provisioning.autoEnableUsers') as string}
            />
            <Typography variant="caption" display="block" color="text.secondary">
              {t('ssoSettings.provisioning.autoEnableUsersDescription') as string}
            </Typography>
          </Grid>
        </Grid>
      </Paper>

      {/* Providers List */}
      <Paper sx={{ p: 3 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
          <Typography variant="h6">
            {t('ssoSettings.providers.title') as string}
          </Typography>
          <Button
            variant="contained"
            startIcon={<AddIcon />}
            onClick={handleAddProvider}
          >
            {t('ssoSettings.providers.addProvider') as string}
          </Button>
        </Box>

        {providers.length === 0 ? (
          <Alert severity="info">
            {t('ssoSettings.providers.noProviders') as string}
          </Alert>
        ) : (
          <Grid container spacing={2}>
            {providers.map((provider) => (
              <Grid item xs={12} md={6} lg={4} key={provider.id}>
                <Card variant="outlined">
                  <CardContent>
                    <Box sx={{ display: 'flex', alignItems: 'center', mb: 1 }}>
                      {getProviderIcon(provider.provider_type)}
                      <Typography variant="h6" sx={{ ml: 1 }}>
                        {provider.name}
                      </Typography>
                    </Box>
                    <Typography variant="body2" color="text.secondary" gutterBottom>
                      {getProviderTypeLabel(provider.provider_type)}
                    </Typography>
                    <Box sx={{ mt: 1 }}>
                      <Chip
                        size="small"
                        icon={provider.enabled ? <CheckCircleIcon /> : <CancelIcon />}
                        label={provider.enabled ? t('ssoSettings.providers.enabled') as string : t('ssoSettings.providers.disabled') as string}
                        color={provider.enabled ? 'success' : 'default'}
                      />
                    </Box>
                    {/* Show callback URL for OAuth/OIDC providers */}
                    {(provider.provider_type === 'oidc' || provider.provider_type === 'oauth2') && (
                      <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block', wordBreak: 'break-all' }}>
                        {t('ssoSettings.providers.callbackUrl') as string}: {window.location.origin}/api/auth/oauth/{provider.id}/callback
                      </Typography>
                    )}
                    {/* Show ACS URL and Metadata for SAML providers */}
                    {provider.provider_type === 'saml' && (
                      <>
                        <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block', wordBreak: 'break-all' }}>
                          {t('ssoSettings.providers.acsUrl') as string}: {window.location.origin}/api/auth/saml/{provider.id}/acs
                        </Typography>
                        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', wordBreak: 'break-all' }}>
                          {t('ssoSettings.providers.metadata') as string}: <a href={`${window.location.origin}/api/auth/saml/${provider.id}/metadata`} target="_blank" rel="noopener noreferrer" style={{ color: 'inherit' }}>{t('ssoSettings.providers.viewXml') as string}</a>
                        </Typography>
                      </>
                    )}
                  </CardContent>
                  <CardActions>
                    <Tooltip title={t('common.edit') as string}>
                      <IconButton size="small" onClick={() => handleEditProvider(provider)}>
                        <EditIcon />
                      </IconButton>
                    </Tooltip>
                    <Tooltip title={t('ssoSettings.providers.testConnection') as string}>
                      <IconButton
                        size="small"
                        onClick={() => handleTestProvider(provider.id)}
                        disabled={testingProvider === provider.id}
                      >
                        {testingProvider === provider.id ? (
                          <CircularProgress size={20} />
                        ) : (
                          <PlayArrowIcon />
                        )}
                      </IconButton>
                    </Tooltip>
                    {provider.provider_type === 'saml' && (
                      <Tooltip title={t('ssoSettings.providers.downloadCertificate') as string}>
                        <IconButton
                          size="small"
                          onClick={() => handleDownloadCertificate(provider.id)}
                        >
                          <DownloadIcon />
                        </IconButton>
                      </Tooltip>
                    )}
                    <Tooltip title={t('common.delete') as string}>
                      <IconButton
                        size="small"
                        color="error"
                        onClick={() => handleDeleteClick(provider)}
                      >
                        <DeleteIcon />
                      </IconButton>
                    </Tooltip>
                  </CardActions>
                </Card>
              </Grid>
            ))}
          </Grid>
        )}
      </Paper>

      {/* Provider Dialog */}
      <ProviderDialog
        open={providerDialogOpen}
        provider={editingProvider}
        onClose={() => setProviderDialogOpen(false)}
        onSave={handleProviderSave}
      />

      {/* Delete Confirmation Dialog */}
      <Dialog open={deleteDialogOpen} onClose={() => setDeleteDialogOpen(false)}>
        <DialogTitle>{t('ssoSettings.dialogs.deleteProvider.title') as string}</DialogTitle>
        <DialogContent>
          <Typography>
            {t('ssoSettings.dialogs.deleteProvider.confirmation', { name: providerToDelete?.name }) as string}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)}>{t('common.cancel') as string}</Button>
          <Button onClick={handleDeleteConfirm} color="error" variant="contained">
            {t('common.delete') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

// Provider Dialog Component
interface ProviderDialogProps {
  open: boolean;
  provider: SSOProviderWithConfig | null;
  onClose: () => void;
  onSave: (data: CreateSSOProviderRequest | UpdateSSOProviderRequest) => Promise<void>;
}

const ProviderDialog: React.FC<ProviderDialogProps> = ({ open, provider, onClose, onSave }) => {
  const { t } = useTranslation('admin');
  const [saving, setSaving] = useState(false);
  const [providerType, setProviderType] = useState<SSOProviderType>('ldap');
  const [name, setName] = useState('');
  const [enabled, setEnabled] = useState(false);
  const [autoCreateUsers, setAutoCreateUsers] = useState<boolean | undefined>(undefined);
  const [autoEnableUsers, setAutoEnableUsers] = useState<boolean | undefined>(undefined);

  // LDAP fields
  const [ldapConfig, setLdapConfig] = useState<LDAPConfig>({
    server_url: '',
    base_dn: '',
    user_search_filter: '(sAMAccountName={{username}})',
    use_start_tls: false,
    skip_cert_verify: false,
    email_attribute: 'mail',
    connection_timeout_seconds: 10
  });

  // SAML fields (sign_requests is auto-enabled with auto-generated keys)
  const [samlConfig, setSamlConfig] = useState<SAMLConfig>({
    sp_entity_id: '',
    idp_entity_id: '',
    idp_sso_url: '',
    idp_certificate: '',
    require_signed_assertions: true,
    require_encrypted_assertions: false,
    email_attribute: 'email'
  });

  // OAuth fields
  const [oauthConfig, setOauthConfig] = useState<OAuthConfig>({
    is_oidc: true,
    client_id: '',
    scopes: ['openid', 'email', 'profile'],
    email_attribute: 'email',
    external_id_attribute: 'sub'
  });

  useEffect(() => {
    if (provider) {
      setName(provider.name);
      setProviderType(provider.provider_type);
      setEnabled(provider.enabled);
      setAutoCreateUsers(provider.auto_create_users);
      setAutoEnableUsers(provider.auto_enable_users);

      if (provider.ldap_config) {
        setLdapConfig({ ...ldapConfig, ...provider.ldap_config });
      }
      if (provider.saml_config) {
        setSamlConfig({ ...samlConfig, ...provider.saml_config });
      }
      if (provider.oauth_config) {
        setOauthConfig({ ...oauthConfig, ...provider.oauth_config });
      }
    } else {
      // Reset form
      setName('');
      setProviderType('ldap');
      setEnabled(false);
      setAutoCreateUsers(undefined);
      setAutoEnableUsers(undefined);
      setLdapConfig({
        server_url: '',
        base_dn: '',
        user_search_filter: '(sAMAccountName={{username}})',
        use_start_tls: false,
        skip_cert_verify: false,
        email_attribute: 'mail',
        connection_timeout_seconds: 10
      });
      setSamlConfig({
        sp_entity_id: '',
        idp_entity_id: '',
        idp_sso_url: '',
        idp_certificate: '',
        require_signed_assertions: true,
        require_encrypted_assertions: false,
        email_attribute: 'email'
      });
      setOauthConfig({
        is_oidc: true,
        client_id: '',
        scopes: ['openid', 'email', 'profile'],
        email_attribute: 'email',
        external_id_attribute: 'sub'
      });
    }
  }, [provider, open]);

  const handleSave = async () => {
    setSaving(true);
    try {
      const data: CreateSSOProviderRequest = {
        name,
        provider_type: providerType,
        enabled,
        auto_create_users: autoCreateUsers,
        auto_enable_users: autoEnableUsers
      };

      if (providerType === 'ldap') {
        data.ldap_config = ldapConfig;
      } else if (providerType === 'saml') {
        data.saml_config = samlConfig;
      } else {
        data.oauth_config = oauthConfig;
      }

      await onSave(data);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>
        {provider ? t('ssoSettings.dialogs.providerForm.editTitle') as string : t('ssoSettings.dialogs.providerForm.addTitle') as string}
      </DialogTitle>
      <DialogContent>
        <Grid container spacing={2} sx={{ mt: 1 }}>
          <Grid item xs={12} sm={6}>
            <TextField
              fullWidth
              label={t('ssoSettings.dialogs.providerForm.providerName') as string}
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </Grid>
          <Grid item xs={12} sm={6}>
            <FormControl fullWidth disabled={!!provider}>
              <InputLabel>{t('ssoSettings.dialogs.providerForm.providerType') as string}</InputLabel>
              <Select
                value={providerType}
                label={t('ssoSettings.dialogs.providerForm.providerType') as string}
                onChange={(e) => setProviderType(e.target.value as SSOProviderType)}
              >
                <MenuItem value="ldap">{t('ssoSettings.providerTypes.ldap') as string}</MenuItem>
                <MenuItem value="saml">{t('ssoSettings.providerTypes.saml') as string}</MenuItem>
                <MenuItem value="oidc">{t('ssoSettings.providerTypes.oidc') as string}</MenuItem>
                <MenuItem value="oauth2">{t('ssoSettings.providerTypes.oauth2') as string}</MenuItem>
              </Select>
            </FormControl>
          </Grid>
          <Grid item xs={12}>
            <FormControlLabel
              control={
                <Switch
                  checked={enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                />
              }
              label={t('ssoSettings.dialogs.providerForm.enabled') as string}
            />
          </Grid>

          {/* Provider-specific configuration */}
          {providerType === 'ldap' && (
            <LDAPConfigForm config={ldapConfig} onChange={setLdapConfig} />
          )}
          {providerType === 'saml' && (
            <SAMLConfigForm config={samlConfig} onChange={setSamlConfig} />
          )}
          {(providerType === 'oidc' || providerType === 'oauth2') && (
            <OAuthConfigForm config={oauthConfig} onChange={setOauthConfig} />
          )}
        </Grid>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common.cancel') as string}</Button>
        <Button
          onClick={handleSave}
          variant="contained"
          disabled={saving || !name}
        >
          {saving ? <CircularProgress size={24} /> : t('common.save') as string}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

// LDAP Config Form
interface LDAPConfigFormProps {
  config: LDAPConfig;
  onChange: (config: LDAPConfig) => void;
}

const LDAPConfigForm: React.FC<LDAPConfigFormProps> = ({ config, onChange }) => {
  const { t } = useTranslation('admin');
  return (
    <>
      <Grid item xs={12}>
        <Typography variant="subtitle2" gutterBottom>
          {t('ssoSettings.ldapConfig.title') as string}
        </Typography>
      </Grid>
      <Grid item xs={12}>
        <TextField
          fullWidth
          label={t('ssoSettings.ldapConfig.serverUrl') as string}
          value={config.server_url}
          onChange={(e) => onChange({ ...config, server_url: e.target.value })}
          placeholder="ldaps://ldap.example.com:636"
          required
          helperText={t('ssoSettings.ldapConfig.serverUrlHelper') as string}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.ldapConfig.baseDn') as string}
          value={config.base_dn}
          onChange={(e) => onChange({ ...config, base_dn: e.target.value })}
          placeholder="dc=example,dc=com"
          required
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.ldapConfig.userSearchFilter') as string}
          value={config.user_search_filter}
          onChange={(e) => onChange({ ...config, user_search_filter: e.target.value })}
          helperText={t('ssoSettings.ldapConfig.userSearchFilterHelper') as string}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.ldapConfig.bindDn') as string}
          value={config.bind_dn || ''}
          onChange={(e) => onChange({ ...config, bind_dn: e.target.value })}
          placeholder="cn=admin,dc=example,dc=com"
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.ldapConfig.bindPassword') as string}
          type="password"
          value={config.bind_password || ''}
          onChange={(e) => onChange({ ...config, bind_password: e.target.value })}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.ldapConfig.emailAttribute') as string}
          value={config.email_attribute}
          onChange={(e) => onChange({ ...config, email_attribute: e.target.value })}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.ldapConfig.usernameAttribute') as string}
          value={config.username_attribute || ''}
          onChange={(e) => onChange({ ...config, username_attribute: e.target.value })}
          placeholder="sAMAccountName"
          helperText={t('ssoSettings.ldapConfig.usernameAttributeHelper') as string}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.ldapConfig.connectionTimeout') as string}
          type="number"
          value={config.connection_timeout_seconds}
          onChange={(e) => onChange({ ...config, connection_timeout_seconds: parseInt(e.target.value) || 10 })}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <FormControlLabel
          control={
            <Switch
              checked={config.use_start_tls}
              onChange={(e) => onChange({ ...config, use_start_tls: e.target.checked })}
            />
          }
          label={t('ssoSettings.ldapConfig.useStartTls') as string}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <FormControlLabel
          control={
            <Switch
              checked={config.skip_cert_verify}
              onChange={(e) => onChange({ ...config, skip_cert_verify: e.target.checked })}
            />
          }
          label={t('ssoSettings.ldapConfig.skipCertVerify') as string}
        />
      </Grid>
    </>
  );
};

// SAML Config Form
interface SAMLConfigFormProps {
  config: SAMLConfig;
  onChange: (config: SAMLConfig) => void;
}

const SAMLConfigForm: React.FC<SAMLConfigFormProps> = ({ config, onChange }) => {
  const { t } = useTranslation('admin');
  return (
    <>
      <Grid item xs={12}>
        <Typography variant="subtitle2" gutterBottom>
          {t('ssoSettings.samlConfig.title') as string}
        </Typography>
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.samlConfig.spEntityId') as string}
          value={config.sp_entity_id}
          onChange={(e) => onChange({ ...config, sp_entity_id: e.target.value })}
          required
          helperText={t('ssoSettings.samlConfig.spEntityIdHelper') as string}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.samlConfig.idpEntityId') as string}
          value={config.idp_entity_id}
          onChange={(e) => onChange({ ...config, idp_entity_id: e.target.value })}
          required
        />
      </Grid>
      <Grid item xs={12}>
        <TextField
          fullWidth
          label={t('ssoSettings.samlConfig.idpSsoUrl') as string}
          value={config.idp_sso_url}
          onChange={(e) => onChange({ ...config, idp_sso_url: e.target.value })}
          required
          placeholder="https://idp.example.com/sso"
        />
      </Grid>
      <Grid item xs={12}>
        <TextField
          fullWidth
          multiline
          rows={4}
          label={t('ssoSettings.samlConfig.idpCertificate') as string}
          value={config.idp_certificate}
          onChange={(e) => onChange({ ...config, idp_certificate: e.target.value })}
          required
          placeholder="-----BEGIN CERTIFICATE-----..."
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.samlConfig.emailAttribute') as string}
          value={config.email_attribute}
          onChange={(e) => onChange({ ...config, email_attribute: e.target.value })}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.samlConfig.usernameAttribute') as string}
          value={config.username_attribute || ''}
          onChange={(e) => onChange({ ...config, username_attribute: e.target.value })}
          placeholder="uid"
          helperText={t('ssoSettings.samlConfig.usernameAttributeHelper') as string}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <FormControlLabel
          control={
            <Switch
              checked={config.require_signed_assertions}
              onChange={(e) => onChange({ ...config, require_signed_assertions: e.target.checked })}
            />
          }
          label={t('ssoSettings.samlConfig.requireSignedAssertions') as string}
        />
      </Grid>
    </>
  );
};

// OAuth Config Form
interface OAuthConfigFormProps {
  config: OAuthConfig;
  onChange: (config: OAuthConfig) => void;
}

const OAuthConfigForm: React.FC<OAuthConfigFormProps> = ({ config, onChange }) => {
  const { t } = useTranslation('admin');
  return (
    <>
      <Grid item xs={12}>
        <Typography variant="subtitle2" gutterBottom>
          {t('ssoSettings.oauthConfig.title') as string}
        </Typography>
      </Grid>
      <Grid item xs={12}>
        <FormControlLabel
          control={
            <Switch
              checked={config.is_oidc}
              onChange={(e) => onChange({ ...config, is_oidc: e.target.checked })}
            />
          }
          label={t('ssoSettings.oauthConfig.isOidc') as string}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.oauthConfig.clientId') as string}
          value={config.client_id}
          onChange={(e) => onChange({ ...config, client_id: e.target.value })}
          required
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.oauthConfig.clientSecret') as string}
          type="password"
          value={config.client_secret || ''}
          onChange={(e) => onChange({ ...config, client_secret: e.target.value })}
        />
      </Grid>
      {config.is_oidc && (
        <Grid item xs={12}>
          <TextField
            fullWidth
            label={t('ssoSettings.oauthConfig.discoveryUrl') as string}
            value={config.discovery_url || ''}
            onChange={(e) => onChange({ ...config, discovery_url: e.target.value })}
            placeholder="https://idp.example.com/.well-known/openid-configuration"
            helperText={t('ssoSettings.oauthConfig.discoveryUrlHelper') as string}
          />
        </Grid>
      )}
      {!config.is_oidc && (
        <>
          <Grid item xs={12} sm={6}>
            <TextField
              fullWidth
              label={t('ssoSettings.oauthConfig.authorizationUrl') as string}
              value={config.authorization_url || ''}
              onChange={(e) => onChange({ ...config, authorization_url: e.target.value })}
            />
          </Grid>
          <Grid item xs={12} sm={6}>
            <TextField
              fullWidth
              label={t('ssoSettings.oauthConfig.tokenUrl') as string}
              value={config.token_url || ''}
              onChange={(e) => onChange({ ...config, token_url: e.target.value })}
            />
          </Grid>
          <Grid item xs={12}>
            <TextField
              fullWidth
              label={t('ssoSettings.oauthConfig.userInfoUrl') as string}
              value={config.userinfo_url || ''}
              onChange={(e) => onChange({ ...config, userinfo_url: e.target.value })}
            />
          </Grid>
        </>
      )}
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.oauthConfig.scopes') as string}
          value={config.scopes.join(' ')}
          onChange={(e) => onChange({ ...config, scopes: e.target.value.split(' ').filter(s => s) })}
          helperText={t('ssoSettings.oauthConfig.scopesHelper') as string}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.oauthConfig.emailAttribute') as string}
          value={config.email_attribute}
          onChange={(e) => onChange({ ...config, email_attribute: e.target.value })}
        />
      </Grid>
      <Grid item xs={12} sm={6}>
        <TextField
          fullWidth
          label={t('ssoSettings.oauthConfig.usernameAttribute') as string}
          value={config.username_attribute || ''}
          onChange={(e) => onChange({ ...config, username_attribute: e.target.value })}
          placeholder="preferred_username"
          helperText={t('ssoSettings.oauthConfig.usernameAttributeHelper') as string}
        />
      </Grid>
    </>
  );
};

export default SSOSettingsPage;
