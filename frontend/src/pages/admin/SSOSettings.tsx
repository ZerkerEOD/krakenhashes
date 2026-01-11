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
import { useSnackbar } from 'notistack';

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
      setError(err.message || 'Failed to load SSO settings');
      enqueueSnackbar('Failed to load SSO settings', { variant: 'error' });
    } finally {
      setLoading(false);
    }
  }, [enqueueSnackbar]);

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
      enqueueSnackbar('Settings updated successfully', { variant: 'success' });
    } catch (err: any) {
      enqueueSnackbar(err.message || 'Failed to update settings', { variant: 'error' });
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
      enqueueSnackbar(err.message || 'Failed to load provider details', { variant: 'error' });
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
      enqueueSnackbar('Provider deleted successfully', { variant: 'success' });
    } catch (err: any) {
      enqueueSnackbar(err.message || 'Failed to delete provider', { variant: 'error' });
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
        enqueueSnackbar('Connection test successful', { variant: 'success' });
      } else {
        enqueueSnackbar(result.message || 'Connection test failed', { variant: 'error' });
      }
    } catch (err: any) {
      enqueueSnackbar(err.message || 'Connection test failed', { variant: 'error' });
    } finally {
      setTestingProvider(null);
    }
  };

  const handleProviderSave = async (data: CreateSSOProviderRequest | UpdateSSOProviderRequest) => {
    try {
      if (editingProvider) {
        await updateSSOProvider(editingProvider.id, data as UpdateSSOProviderRequest);
        enqueueSnackbar('Provider updated successfully', { variant: 'success' });
      } else {
        await createSSOProvider(data as CreateSSOProviderRequest);
        enqueueSnackbar('Provider created successfully', { variant: 'success' });
      }
      setProviderDialogOpen(false);
      fetchData();
    } catch (err: any) {
      enqueueSnackbar(err.message || 'Failed to save provider', { variant: 'error' });
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
            SSO Settings
          </Typography>
          <Typography variant="body1" color="text.secondary">
            Configure Single Sign-On providers and authentication options
          </Typography>
        </Box>
      </Box>

      {/* Global Settings */}
      <Paper sx={{ p: 3, mb: 3 }}>
        <Typography variant="h6" gutterBottom>
          Authentication Methods
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Enable or disable authentication methods globally
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
              label="Local Authentication"
            />
            <Typography variant="caption" display="block" color="text.secondary">
              Username/password login
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
              label="LDAP Authentication"
            />
            <Typography variant="caption" display="block" color="text.secondary">
              Active Directory / LDAP
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
              label="SAML Authentication"
            />
            <Typography variant="caption" display="block" color="text.secondary">
              SAML 2.0 SSO
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
              label="OAuth/OIDC Authentication"
            />
            <Typography variant="caption" display="block" color="text.secondary">
              OpenID Connect / OAuth 2.0
            </Typography>
          </Grid>
        </Grid>

        <Divider sx={{ my: 3 }} />

        <Typography variant="h6" gutterBottom>
          User Provisioning
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          Configure automatic user creation from SSO logins
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
              label="Auto-create Users"
            />
            <Typography variant="caption" display="block" color="text.secondary">
              Automatically create user accounts for new SSO logins
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
              label="Auto-enable Users"
            />
            <Typography variant="caption" display="block" color="text.secondary">
              Automatically enable newly created SSO users (otherwise admin approval required)
            </Typography>
          </Grid>
        </Grid>
      </Paper>

      {/* Providers List */}
      <Paper sx={{ p: 3 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
          <Typography variant="h6">
            SSO Providers
          </Typography>
          <Button
            variant="contained"
            startIcon={<AddIcon />}
            onClick={handleAddProvider}
          >
            Add Provider
          </Button>
        </Box>

        {providers.length === 0 ? (
          <Alert severity="info">
            No SSO providers configured. Click "Add Provider" to create one.
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
                        label={provider.enabled ? 'Enabled' : 'Disabled'}
                        color={provider.enabled ? 'success' : 'default'}
                      />
                    </Box>
                    {/* Show callback URL for OAuth/OIDC providers */}
                    {(provider.provider_type === 'oidc' || provider.provider_type === 'oauth2') && (
                      <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block', wordBreak: 'break-all' }}>
                        Callback URL: {window.location.origin}/api/auth/oauth/{provider.id}/callback
                      </Typography>
                    )}
                    {/* Show ACS URL for SAML providers */}
                    {provider.provider_type === 'saml' && (
                      <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block', wordBreak: 'break-all' }}>
                        ACS URL: {window.location.origin}/api/auth/saml/{provider.id}/acs
                      </Typography>
                    )}
                  </CardContent>
                  <CardActions>
                    <Tooltip title="Edit">
                      <IconButton size="small" onClick={() => handleEditProvider(provider)}>
                        <EditIcon />
                      </IconButton>
                    </Tooltip>
                    <Tooltip title="Test Connection">
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
                    <Tooltip title="Delete">
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
        <DialogTitle>Delete Provider</DialogTitle>
        <DialogContent>
          <Typography>
            Are you sure you want to delete the provider "{providerToDelete?.name}"?
            This action cannot be undone and will unlink all associated user identities.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteDialogOpen(false)}>Cancel</Button>
          <Button onClick={handleDeleteConfirm} color="error" variant="contained">
            Delete
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

  // SAML fields
  const [samlConfig, setSamlConfig] = useState<SAMLConfig>({
    sp_entity_id: '',
    idp_entity_id: '',
    idp_sso_url: '',
    idp_certificate: '',
    sign_requests: false,
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
        sign_requests: false,
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
        {provider ? 'Edit Provider' : 'Add Provider'}
      </DialogTitle>
      <DialogContent>
        <Grid container spacing={2} sx={{ mt: 1 }}>
          <Grid item xs={12} sm={6}>
            <TextField
              fullWidth
              label="Provider Name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </Grid>
          <Grid item xs={12} sm={6}>
            <FormControl fullWidth disabled={!!provider}>
              <InputLabel>Provider Type</InputLabel>
              <Select
                value={providerType}
                label="Provider Type"
                onChange={(e) => setProviderType(e.target.value as SSOProviderType)}
              >
                <MenuItem value="ldap">LDAP / Active Directory</MenuItem>
                <MenuItem value="saml">SAML 2.0</MenuItem>
                <MenuItem value="oidc">OpenID Connect</MenuItem>
                <MenuItem value="oauth2">OAuth 2.0</MenuItem>
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
              label="Enabled"
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
        <Button onClick={onClose}>Cancel</Button>
        <Button
          onClick={handleSave}
          variant="contained"
          disabled={saving || !name}
        >
          {saving ? <CircularProgress size={24} /> : 'Save'}
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

const LDAPConfigForm: React.FC<LDAPConfigFormProps> = ({ config, onChange }) => (
  <>
    <Grid item xs={12}>
      <Typography variant="subtitle2" gutterBottom>
        LDAP Configuration
      </Typography>
    </Grid>
    <Grid item xs={12}>
      <TextField
        fullWidth
        label="Server URL"
        value={config.server_url}
        onChange={(e) => onChange({ ...config, server_url: e.target.value })}
        placeholder="ldaps://ldap.example.com:636"
        required
        helperText="Use ldaps:// for secure connections"
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Base DN"
        value={config.base_dn}
        onChange={(e) => onChange({ ...config, base_dn: e.target.value })}
        placeholder="dc=example,dc=com"
        required
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="User Search Filter"
        value={config.user_search_filter}
        onChange={(e) => onChange({ ...config, user_search_filter: e.target.value })}
        helperText="Use {{username}} as placeholder"
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Bind DN (optional)"
        value={config.bind_dn || ''}
        onChange={(e) => onChange({ ...config, bind_dn: e.target.value })}
        placeholder="cn=admin,dc=example,dc=com"
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Bind Password"
        type="password"
        value={config.bind_password || ''}
        onChange={(e) => onChange({ ...config, bind_password: e.target.value })}
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Email Attribute"
        value={config.email_attribute}
        onChange={(e) => onChange({ ...config, email_attribute: e.target.value })}
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Connection Timeout (seconds)"
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
        label="Use StartTLS"
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
        label="Skip Certificate Verification (testing only)"
      />
    </Grid>
  </>
);

// SAML Config Form
interface SAMLConfigFormProps {
  config: SAMLConfig;
  onChange: (config: SAMLConfig) => void;
}

const SAMLConfigForm: React.FC<SAMLConfigFormProps> = ({ config, onChange }) => (
  <>
    <Grid item xs={12}>
      <Typography variant="subtitle2" gutterBottom>
        SAML Configuration
      </Typography>
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="SP Entity ID"
        value={config.sp_entity_id}
        onChange={(e) => onChange({ ...config, sp_entity_id: e.target.value })}
        required
        helperText="Your application's SAML identifier"
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="IdP Entity ID"
        value={config.idp_entity_id}
        onChange={(e) => onChange({ ...config, idp_entity_id: e.target.value })}
        required
      />
    </Grid>
    <Grid item xs={12}>
      <TextField
        fullWidth
        label="IdP SSO URL"
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
        label="IdP Certificate"
        value={config.idp_certificate}
        onChange={(e) => onChange({ ...config, idp_certificate: e.target.value })}
        required
        placeholder="-----BEGIN CERTIFICATE-----..."
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Email Attribute"
        value={config.email_attribute}
        onChange={(e) => onChange({ ...config, email_attribute: e.target.value })}
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
        label="Require Signed Assertions"
      />
    </Grid>
  </>
);

// OAuth Config Form
interface OAuthConfigFormProps {
  config: OAuthConfig;
  onChange: (config: OAuthConfig) => void;
}

const OAuthConfigForm: React.FC<OAuthConfigFormProps> = ({ config, onChange }) => (
  <>
    <Grid item xs={12}>
      <Typography variant="subtitle2" gutterBottom>
        OAuth/OIDC Configuration
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
        label="OpenID Connect (OIDC)"
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Client ID"
        value={config.client_id}
        onChange={(e) => onChange({ ...config, client_id: e.target.value })}
        required
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Client Secret"
        type="password"
        value={config.client_secret || ''}
        onChange={(e) => onChange({ ...config, client_secret: e.target.value })}
      />
    </Grid>
    {config.is_oidc && (
      <Grid item xs={12}>
        <TextField
          fullWidth
          label="Discovery URL"
          value={config.discovery_url || ''}
          onChange={(e) => onChange({ ...config, discovery_url: e.target.value })}
          placeholder="https://idp.example.com/.well-known/openid-configuration"
          helperText="OIDC discovery endpoint (auto-configures other URLs)"
        />
      </Grid>
    )}
    {!config.is_oidc && (
      <>
        <Grid item xs={12} sm={6}>
          <TextField
            fullWidth
            label="Authorization URL"
            value={config.authorization_url || ''}
            onChange={(e) => onChange({ ...config, authorization_url: e.target.value })}
          />
        </Grid>
        <Grid item xs={12} sm={6}>
          <TextField
            fullWidth
            label="Token URL"
            value={config.token_url || ''}
            onChange={(e) => onChange({ ...config, token_url: e.target.value })}
          />
        </Grid>
        <Grid item xs={12}>
          <TextField
            fullWidth
            label="User Info URL"
            value={config.userinfo_url || ''}
            onChange={(e) => onChange({ ...config, userinfo_url: e.target.value })}
          />
        </Grid>
      </>
    )}
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Scopes"
        value={config.scopes.join(' ')}
        onChange={(e) => onChange({ ...config, scopes: e.target.value.split(' ').filter(s => s) })}
        helperText="Space-separated list"
      />
    </Grid>
    <Grid item xs={12} sm={6}>
      <TextField
        fullWidth
        label="Email Attribute"
        value={config.email_attribute}
        onChange={(e) => onChange({ ...config, email_attribute: e.target.value })}
      />
    </Grid>
  </>
);

export default SSOSettingsPage;
