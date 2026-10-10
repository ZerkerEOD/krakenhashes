import React, { useState, useEffect } from 'react';
import {
  Card,
  CardContent,
  Typography,
  Box,
  Alert,
  CircularProgress,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  ListItemSecondaryAction,
  IconButton,
  Tooltip,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button
} from '@mui/material';
import AccountTreeIcon from '@mui/icons-material/AccountTree';
import SecurityIcon from '@mui/icons-material/Security';
import VpnKeyIcon from '@mui/icons-material/VpnKey';
import LinkOffIcon from '@mui/icons-material/LinkOff';
import { useTranslation } from 'react-i18next';
import { UserIdentity } from '../../types/sso';
import { getMyIdentities, unlinkMyIdentity, getProviderTypeLabel } from '../../services/sso';

const LinkedAccountsCard: React.FC = () => {
  const { t } = useTranslation('settings');
  const [identities, setIdentities] = useState<UserIdentity[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [unlinkDialogOpen, setUnlinkDialogOpen] = useState(false);
  const [identityToUnlink, setIdentityToUnlink] = useState<UserIdentity | null>(null);
  const [unlinking, setUnlinking] = useState(false);

  const fetchIdentities = async () => {
    try {
      const data = await getMyIdentities();
      setIdentities(data);
      setError(null);
    } catch (err: any) {
      console.error('Failed to fetch linked accounts:', err);
      setError(err.message || (t('linkedAccounts.errors.loadFailed') as string));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchIdentities();
  }, []);

  const handleUnlinkClick = (identity: UserIdentity) => {
    setIdentityToUnlink(identity);
    setUnlinkDialogOpen(true);
  };

  const handleUnlinkConfirm = async () => {
    if (!identityToUnlink) return;

    setUnlinking(true);
    try {
      await unlinkMyIdentity(identityToUnlink.id);
      setIdentities(identities.filter(i => i.id !== identityToUnlink.id));
      setUnlinkDialogOpen(false);
      setIdentityToUnlink(null);
    } catch (err: any) {
      setError(err.message || (t('linkedAccounts.errors.unlinkFailed') as string));
    } finally {
      setUnlinking(false);
    }
  };

  const getProviderIcon = (type: string) => {
    switch (type) {
      case 'ldap':
        return <AccountTreeIcon />;
      case 'saml':
        return <SecurityIcon />;
      case 'oidc':
      case 'oauth2':
        return <VpnKeyIcon />;
      default:
        return <VpnKeyIcon />;
    }
  };

  const formatDate = (dateString?: string) => {
    if (!dateString) return t('common.never') as string;
    try {
      return new Date(dateString).toLocaleString();
    } catch {
      return t('common.invalidDate') as string;
    }
  };

  return (
    <Card sx={{ mb: 3 }}>
      <CardContent>
        <Typography variant="h6" gutterBottom>
          {t('linkedAccounts.title')}
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t('linkedAccounts.description')}
        </Typography>

        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
            <CircularProgress />
          </Box>
        ) : identities.length === 0 ? (
          <Alert severity="info">
            {t('linkedAccounts.noAccountsMessage')}
          </Alert>
        ) : (
          <List>
            {identities.map((identity) => (
              <ListItem
                key={identity.id}
                sx={{
                  bgcolor: 'background.paper',
                  borderRadius: 1,
                  mb: 1,
                  border: 1,
                  borderColor: 'divider'
                }}
              >
                <ListItemIcon>
                  {getProviderIcon(identity.provider_type)}
                </ListItemIcon>
                <ListItemText
                  primary={
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <Typography variant="subtitle1">
                        {identity.provider_name || getProviderTypeLabel(identity.provider_type)}
                      </Typography>
                      <Chip
                        size="small"
                        label={getProviderTypeLabel(identity.provider_type)}
                        variant="outlined"
                      />
                    </Box>
                  }
                  secondary={
                    <Box>
                      {identity.external_email && (
                        <Typography variant="body2" color="text.secondary">
                          {t('linkedAccounts.emailLabel', { email: identity.external_email })}
                        </Typography>
                      )}
                      {identity.external_username && (
                        <Typography variant="body2" color="text.secondary">
                          {t('linkedAccounts.usernameLabel', { username: identity.external_username })}
                        </Typography>
                      )}
                      <Typography variant="caption" color="text.secondary">
                        {t('linkedAccounts.lastLoginLinked', { lastLogin: formatDate(identity.last_login_at), linked: formatDate(identity.created_at) })}
                      </Typography>
                    </Box>
                  }
                />
                <ListItemSecondaryAction>
                  <Tooltip title={t('linkedAccounts.unlinkTooltip') as string}>
                    <IconButton
                      edge="end"
                      onClick={() => handleUnlinkClick(identity)}
                      color="error"
                    >
                      <LinkOffIcon />
                    </IconButton>
                  </Tooltip>
                </ListItemSecondaryAction>
              </ListItem>
            ))}
          </List>
        )}
      </CardContent>

      {/* Unlink Confirmation Dialog */}
      <Dialog
        open={unlinkDialogOpen}
        onClose={() => setUnlinkDialogOpen(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('linkedAccounts.unlinkDialog.title')}</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>
            {t('linkedAccounts.unlinkDialog.warning')}
          </Alert>
          <Typography>
            {t('linkedAccounts.unlinkDialog.confirmation', { provider: identityToUnlink?.provider_name || getProviderTypeLabel(identityToUnlink?.provider_type || '') })}
          </Typography>
          {identityToUnlink?.external_email && (
            <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
              ({identityToUnlink.external_email})
            </Typography>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setUnlinkDialogOpen(false)} disabled={unlinking}>
            {t('common.cancel')}
          </Button>
          <Button
            onClick={handleUnlinkConfirm}
            color="error"
            variant="contained"
            disabled={unlinking}
          >
            {unlinking ? <CircularProgress size={24} /> : t('linkedAccounts.unlink')}
          </Button>
        </DialogActions>
      </Dialog>
    </Card>
  );
};

export default LinkedAccountsCard;
