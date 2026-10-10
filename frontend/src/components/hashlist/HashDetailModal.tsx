import React from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Typography,
  Box,
  Chip,
  Divider,
  Grid,
  Paper,
  IconButton,
  Tooltip
} from '@mui/material';
import {
  Close as CloseIcon,
  ContentCopy as CopyIcon,
  Check as CheckIcon,
  TextFields as TextFieldsIcon
} from '@mui/icons-material';
import { useToast } from '../ui/toast';
import { useTranslation } from 'react-i18next';
import CrackedPassword from '../common/CrackedPassword';
import { hexPlainPreview, parseHexPlain } from '../../utils/hexPlain';

interface HashDetail {
  id: string;
  hash_value: string;
  original_hash: string;
  username?: string;
  hash_type_id: number;
  is_cracked: boolean;
  password?: string;
  last_updated: string;
  // Frontend enriched fields
  hash?: string;
  isCracked?: boolean;
  crackedText?: string;
  hashlistName?: string;
  hashType?: string;
  crackedAt?: string;
  addedAt?: string;
}

interface HashDetailModalProps {
  open: boolean;
  onClose: () => void;
  hash: HashDetail | null;
}

export default function HashDetailModal({ open, onClose, hash }: HashDetailModalProps) {
  const toast = useToast();
  const { t } = useTranslation('common');
  const [copied, setCopied] = React.useState(false);

  const handleCopyHash = () => {
    if (hash?.hash_value) {
      navigator.clipboard.writeText(hash.hash_value).then(() => {
        setCopied(true);
        toast.success(t('hashDetailModal.hashCopied') as string);
        setTimeout(() => setCopied(false), 2000);
      });
    }
  };

  const handleCopyCrackedText = () => {
    if (hash?.password) {
      navigator.clipboard.writeText(hash.password).then(() => {
        toast.success(t('hashDetailModal.crackedTextCopied') as string);
      });
    }
  };

  // Only set for $HEX[...] passwords: a readable best guess, which is not the
  // exact password (the $HEX value copied above is).
  const hexBytes = hash?.password ? parseHexPlain(hash.password) : null;
  const handleCopyBestGuess = () => {
    if (hexBytes) {
      navigator.clipboard.writeText(hexPlainPreview(hexBytes)).then(() => {
        toast.success(t('clipboard.copied') as string);
      });
    }
  };

  if (!hash) return null;

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>
        <Box display="flex" justifyContent="space-between" alignItems="center">
          <Typography variant="h6">{t('hashDetailModal.title') as string}</Typography>
          <IconButton onClick={onClose} size="small">
            <CloseIcon />
          </IconButton>
        </Box>
      </DialogTitle>

      <DialogContent dividers>
        <Grid container spacing={3}>
          {/* Hash Value Section */}
          <Grid item xs={12}>
            <Paper sx={{ p: 2, bgcolor: 'grey.50' }}>
              <Box display="flex" justifyContent="space-between" alignItems="center" mb={1}>
                <Typography variant="subtitle2" color="text.secondary">
                  {t('hashDetailModal.hashValue') as string}
                </Typography>
                <Tooltip title={copied ? (t('hashDetailModal.copied') as string) : (t('hashDetailModal.copyHash') as string)}>
                  <IconButton size="small" onClick={handleCopyHash}>
                    {copied ? <CheckIcon fontSize="small" /> : <CopyIcon fontSize="small" />}
                  </IconButton>
                </Tooltip>
              </Box>
              <Typography 
                variant="body2" 
                sx={{ 
                  fontFamily: 'monospace', 
                  wordBreak: 'break-all',
                  fontSize: '0.875rem'
                }}
              >
                {hash.hash_value}
              </Typography>
            </Paper>
          </Grid>

          {/* Original Hash Section (if different from hash value) */}
          {hash.original_hash && hash.original_hash !== hash.hash_value && (
            <Grid item xs={12}>
              <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                {t('hashDetailModal.originalHash') as string}
              </Typography>
              <Typography 
                variant="body2" 
                sx={{ 
                  fontFamily: 'monospace', 
                  wordBreak: 'break-all',
                  fontSize: '0.75rem',
                  color: 'text.secondary'
                }}
              >
                {hash.original_hash}
              </Typography>
            </Grid>
          )}

          {/* Status Section */}
          <Grid item xs={12} sm={6}>
            <Typography variant="subtitle2" color="text.secondary" gutterBottom>
              {t('hashDetailModal.status') as string}
            </Typography>
            <Chip
              label={hash.is_cracked ? (t('hashDetailModal.cracked') as string) : (t('hashDetailModal.notCracked') as string)}
              color={hash.is_cracked ? 'success' : 'default'}
              size="medium"
            />
          </Grid>

          {/* Hash Type Section */}
          {(hash.hashType || hash.hash_type_id) && (
            <Grid item xs={12} sm={6}>
              <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                {t('hashDetailModal.hashType') as string}
              </Typography>
              <Typography variant="body1">
                {hash.hashType || (t('hashDetailModal.typeId', { id: hash.hash_type_id }) as string)}
              </Typography>
            </Grid>
          )}

          {/* Username Section */}
          {hash.username && (
            <Grid item xs={12} sm={6}>
              <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                {t('hashDetailModal.username') as string}
              </Typography>
              <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                {hash.username}
              </Typography>
            </Grid>
          )}

          {/* Cracked Text Section */}
          {hash.is_cracked && hash.password && (
            <Grid item xs={12}>
              <Paper sx={{ p: 2, bgcolor: 'success.50' }}>
                <Box display="flex" justifyContent="space-between" alignItems="center" mb={1}>
                  <Typography variant="subtitle2" color="text.secondary">
                    {t('hashDetailModal.crackedText') as string}
                  </Typography>
                  <Box>
                    {hexBytes && (
                      <Tooltip title={t('hexPassword.copyGuess') as string}>
                        <IconButton size="small" onClick={handleCopyBestGuess}>
                          <TextFieldsIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    )}
                    <Tooltip title={hexBytes ? (t('hexPassword.copyExact') as string) : (t('hashDetailModal.copyPlaintext') as string)}>
                      <IconButton size="small" onClick={handleCopyCrackedText}>
                        <CopyIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </Box>
                </Box>
                <Typography 
                  variant="body1" 
                  sx={{ 
                    fontFamily: 'monospace',
                    fontWeight: 'bold',
                    color: 'success.main'
                  }}
                >
                  <CrackedPassword password={hash.password} />
                </Typography>
              </Paper>
            </Grid>
          )}

          <Grid item xs={12}>
            <Divider />
          </Grid>

          {/* Metadata Section */}
          <Grid item xs={12}>
            <Grid container spacing={2}>
              {hash.hashlistName && (
                <Grid item xs={12} sm={6}>
                  <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                    {t('hashDetailModal.hashlist') as string}
                  </Typography>
                  <Typography variant="body2">
                    {hash.hashlistName}
                  </Typography>
                </Grid>
              )}

              {hash.last_updated && (
                <Grid item xs={12} sm={6}>
                  <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                    {t('hashDetailModal.lastUpdated') as string}
                  </Typography>
                  <Typography variant="body2">
                    {new Date(hash.last_updated).toLocaleString()}
                  </Typography>
                </Grid>
              )}

              {hash.is_cracked && hash.last_updated && (
                <Grid item xs={12} sm={6}>
                  <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                    {t('hashDetailModal.crackedAt') as string}
                  </Typography>
                  <Typography variant="body2">
                    {new Date(hash.last_updated).toLocaleString()}
                  </Typography>
                </Grid>
              )}
            </Grid>
          </Grid>
        </Grid>
      </DialogContent>

      <DialogActions>
        <Button onClick={onClose}>{t('buttons.close') as string}</Button>
      </DialogActions>
    </Dialog>
  );
}