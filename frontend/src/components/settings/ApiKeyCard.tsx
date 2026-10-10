import React, { useEffect, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Link,
  TextField,
  Typography,
} from '@mui/material';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import VpnKeyIcon from '@mui/icons-material/VpnKey';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import { useTranslation } from 'react-i18next';
import { generateApiKey, getApiKeyInfo, revokeApiKey } from '../../services/api';
import { ApiKeyInfo } from '../../types/user';
import { useToast } from '../ui/toast';
import { useConfirm } from '../ui/ConfirmProvider';
import SectionCard from '../ui/SectionCard';
import { getErrorMessage } from '../../utils/errors';

/** Personal API key: generate, regenerate, revoke; the key is shown once. */
const ApiKeyCard: React.FC = () => {
  const { t } = useTranslation('settings');
  const toast = useToast();
  const confirm = useConfirm();
  const [info, setInfo] = useState<ApiKeyInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [generated, setGenerated] = useState('');

  const reload = async () => {
    try {
      setInfo((await getApiKeyInfo()).data.data);
    } catch (err) {
      console.error('Failed to load API key info:', err);
    }
  };

  useEffect(() => {
    void reload();
  }, []);

  const generate = async () => {
    if (info?.hasKey) {
      const ok = await confirm({
        title: t('apiKey.confirmRegenerate.title') as string,
        message: `${t('apiKey.confirmRegenerate.warning')} ${t('apiKey.confirmRegenerate.confirm')}`,
        severity: 'danger',
        confirmLabel: t('apiKey.regenerate') as string,
      });
      if (!ok) return;
    }
    setLoading(true);
    try {
      const res = await generateApiKey();
      setGenerated(res.data.data.apiKey);
      await reload();
    } catch (err) {
      toast.error(getErrorMessage(err) || (t('apiKey.errors.generateFailed') as string));
    } finally {
      setLoading(false);
    }
  };

  const revoke = async () => {
    const ok = await confirm({
      title: t('apiKey.confirmRevoke.title') as string,
      message: `${t('apiKey.confirmRevoke.warning')} ${t('apiKey.confirmRevoke.confirm')}`,
      severity: 'danger',
      confirmLabel: t('apiKey.revoke') as string,
    });
    if (!ok) return;
    setLoading(true);
    try {
      await revokeApiKey();
      toast.success(t('apiKey.success.revoked') as string);
      await reload();
    } catch (err) {
      toast.error(getErrorMessage(err) || (t('apiKey.errors.revokeFailed') as string));
    } finally {
      setLoading(false);
    }
  };

  const formatDate = (value?: string) => {
    if (!value) return t('common.never') as string;
    const d = new Date(value);
    return isNaN(d.getTime()) ? (t('common.invalidDate') as string) : d.toLocaleString();
  };

  return (
    <SectionCard title={t('apiKey.title') as string}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
        <VpnKeyIcon color={info?.hasKey ? 'success' : 'disabled'} />
        <Typography variant="body1" component="span">
          {t('apiKey.status') as string}:
        </Typography>
        {info?.hasKey ? (
          <Chip label={t('apiKey.active') as string} color="success" size="small" icon={<CheckCircleIcon />} />
        ) : (
          <Chip label={t('apiKey.noKeyGenerated') as string} size="small" />
        )}
      </Box>
      {info?.hasKey && (
        <Box sx={{ ml: 4, mb: 2 }}>
          <Typography variant="body2" color="text.secondary">
            {t('apiKey.created') as string}: {formatDate(info.createdAt)}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {t('apiKey.lastUsed') as string}: {formatDate(info.lastUsed)}
          </Typography>
        </Box>
      )}
      <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', mt: 2 }}>
        <Button variant="contained" startIcon={<VpnKeyIcon />} onClick={generate} disabled={loading}>
          {info?.hasKey ? (t('apiKey.regenerate') as string) : (t('apiKey.generate') as string)}
        </Button>
        {info?.hasKey && (
          <Button variant="outlined" color="error" onClick={revoke} disabled={loading}>
            {t('apiKey.revoke') as string}
          </Button>
        )}
        <Button variant="text" component={Link} href="https://zerkereod.github.io/krakenhashes/user-api/" target="_blank" rel="noopener noreferrer">
          {t('apiKey.viewDocs') as string}
        </Button>
      </Box>

      <Dialog open={Boolean(generated)} onClose={() => setGenerated('')} maxWidth="md" fullWidth>
        <DialogTitle>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <VpnKeyIcon color="primary" />
            {t('apiKey.dialog.title') as string}
          </Box>
        </DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 3 }}>
            <strong>{t('apiKey.dialog.saveNow') as string}</strong> {t('apiKey.dialog.cantSeeAgain') as string}
          </Alert>
          <TextField
            fullWidth
            multiline
            rows={3}
            value={generated}
            InputProps={{
              readOnly: true,
              sx: { fontFamily: (theme) => theme.typography.monoFamily, fontSize: '0.9rem' },
              endAdornment: (
                <IconButton
                  aria-label="copy"
                  onClick={() => {
                    void navigator.clipboard.writeText(generated);
                    toast.success(t('apiKey.success.copied') as string);
                  }}
                  sx={{ position: 'absolute', right: 8, top: 8 }}
                >
                  <ContentCopyIcon />
                </IconButton>
              ),
            }}
          />
          <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
            {t('apiKey.dialog.instructions') as string}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button variant="contained" onClick={() => setGenerated('')}>
            {t('apiKey.dialog.saved') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </SectionCard>
  );
};

export default ApiKeyCard;
