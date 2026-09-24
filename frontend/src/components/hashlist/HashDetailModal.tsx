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
import { useSnackbar } from 'notistack';
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
  const { enqueueSnackbar } = useSnackbar();
  const { t } = useTranslation('common');
  const [copied, setCopied] = React.useState(false);

  const handleCopyHash = () => {
    if (hash?.hash_value) {
      navigator.clipboard.writeText(hash.hash_value).then(() => {
        setCopied(true);
        enqueueSnackbar('Hash copied to clipboard', { variant: 'success' });
        setTimeout(() => setCopied(false), 2000);
      });
    }
  };

  const handleCopyCrackedText = () => {
    if (hash?.password) {
      navigator.clipboard.writeText(hash.password).then(() => {
        enqueueSnackbar('Cracked text copied to clipboard', { variant: 'success' });
      });
    }
  };

  // Only set for $HEX[...] passwords: a readable best guess, which is not the
  // exact password (the $HEX value copied above is).
  const hexBytes = hash?.password ? parseHexPlain(hash.password) : null;
  const handleCopyBestGuess = () => {
    if (hexBytes) {
      navigator.clipboard.writeText(hexPlainPreview(hexBytes)).then(() => {
        enqueueSnackbar(t('clipboard.copied') as string, { variant: 'success' });
      });
    }
  };

  if (!hash) return null;

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>
        <Box display="flex" justifyContent="space-between" alignItems="center">
          <Typography variant="h6">Hash Details</Typography>
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
                  Hash Value
                </Typography>
                <Tooltip title={copied ? "Copied!" : "Copy hash"}>
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
                Original Hash (from file)
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
              Status
            </Typography>
            <Chip 
              label={hash.is_cracked ? 'Cracked' : 'Not Cracked'}
              color={hash.is_cracked ? 'success' : 'default'}
              size="medium"
            />
          </Grid>

          {/* Hash Type Section */}
          {(hash.hashType || hash.hash_type_id) && (
            <Grid item xs={12} sm={6}>
              <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                Hash Type
              </Typography>
              <Typography variant="body1">
                {hash.hashType || `Type ID: ${hash.hash_type_id}`}
              </Typography>
            </Grid>
          )}

          {/* Username Section */}
          {hash.username && (
            <Grid item xs={12} sm={6}>
              <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                Username
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
                    Cracked Text
                  </Typography>
                  <Box>
                    {hexBytes && (
                      <Tooltip title={t('hexPassword.copyGuess') as string}>
                        <IconButton size="small" onClick={handleCopyBestGuess}>
                          <TextFieldsIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    )}
                    <Tooltip title={hexBytes ? (t('hexPassword.copyExact') as string) : 'Copy plaintext'}>
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
                    Hashlist
                  </Typography>
                  <Typography variant="body2">
                    {hash.hashlistName}
                  </Typography>
                </Grid>
              )}

              {hash.last_updated && (
                <Grid item xs={12} sm={6}>
                  <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                    Last Updated
                  </Typography>
                  <Typography variant="body2">
                    {new Date(hash.last_updated).toLocaleString()}
                  </Typography>
                </Grid>
              )}

              {hash.is_cracked && hash.last_updated && (
                <Grid item xs={12} sm={6}>
                  <Typography variant="subtitle2" color="text.secondary" gutterBottom>
                    Cracked At
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
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}