import React from 'react';
import {
  Box,
  Button,
  Chip,
  IconButton,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import CloseIcon from '@mui/icons-material/Close';
import { useTranslation } from 'react-i18next';
import { DiscoveredAddress, DiscoverySource } from '../../../types/serverCertificate';

interface DiscoveredAddressesPanelProps {
  items: DiscoveredAddress[];
  onAdd: (item: DiscoveredAddress) => void;
  onDismiss: (id: number) => void;
  disabled?: boolean;
}

/** Agent-reported failures are the actionable signal, so they sort first. */
const sourceRank = (sources: DiscoverySource[]): number =>
  sources.includes('agent_tls_failure') ? 0 : 1;

const sourceColor = (source: DiscoverySource): 'error' | 'info' | 'default' => {
  if (source === 'agent_tls_failure') return 'error';
  if (source === 'tls_sni') return 'info';
  return 'default';
};

const relativeTime = (iso: string): string => {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
};

const DiscoveredAddressesPanel: React.FC<DiscoveredAddressesPanelProps> = ({
  items,
  onAdd,
  onDismiss,
  disabled = false,
}) => {
  const { t } = useTranslation('admin');

  // Already-covered addresses are filtered out server-side; anything still
  // pending is kept and badged, because that is what tells an admin they saved
  // a change but have not applied it yet.
  const visible = [...items]
    .filter((item) => item.status !== 'in_certificate')
    .sort((a, b) => {
      const rank = sourceRank(a.sources) - sourceRank(b.sources);
      if (rank !== 0) return rank;
      return new Date(b.last_seen_at).getTime() - new Date(a.last_seen_at).getTime();
    });

  return (
    <Paper sx={{ p: 3 }}>
      <Typography variant="subtitle1" gutterBottom>
        {t('serverCertificate.discovered.title') as string}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('serverCertificate.discovered.help') as string}
      </Typography>

      {visible.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          {t('serverCertificate.discovered.empty') as string}
        </Typography>
      ) : (
        <Box sx={{ overflowX: 'auto' }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t('serverCertificate.discovered.address') as string}</TableCell>
                <TableCell>{t('serverCertificate.discovered.source') as string}</TableCell>
                <TableCell>{t('serverCertificate.discovered.reportedBy') as string}</TableCell>
                <TableCell>{t('serverCertificate.discovered.lastSeen') as string}</TableCell>
                <TableCell align="right">{t('serverCertificate.discovered.hits') as string}</TableCell>
                <TableCell align="right">{t('serverCertificate.discovered.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {visible.map((item) => (
                <TableRow key={item.id} hover>
                  <TableCell>
                    <Stack direction="row" spacing={1} alignItems="center">
                      <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                        {item.address}
                      </Typography>
                      {item.status === 'pending_reissue' && (
                        <Chip
                          size="small"
                          color="info"
                          label={t('serverCertificate.discovered.pending') as string}
                        />
                      )}
                    </Stack>
                  </TableCell>
                  <TableCell>
                    <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', gap: 0.5 }}>
                      {item.sources.map((source) => (
                        <Tooltip
                          key={source}
                          title={t(`serverCertificate.discovered.sourceHelp.${source}`) as string}
                        >
                          <Chip
                            size="small"
                            color={sourceColor(source)}
                            label={t(`serverCertificate.discovered.sources.${source}`) as string}
                          />
                        </Tooltip>
                      ))}
                    </Stack>
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2" color="text.secondary" noWrap sx={{ maxWidth: 220 }}>
                      {item.last_agent_name ?? item.last_user_agent ?? '—'}
                    </Typography>
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2" color="text.secondary">
                      {relativeTime(item.last_seen_at)}
                    </Typography>
                  </TableCell>
                  <TableCell align="right">{item.hit_count}</TableCell>
                  <TableCell align="right">
                    <Stack direction="row" spacing={0.5} justifyContent="flex-end">
                      <Tooltip title={item.allowed ? '' : item.rejection_reason ?? ''}>
                        <span>
                          <Button
                            size="small"
                            startIcon={<AddIcon />}
                            disabled={disabled || !item.allowed || item.status === 'pending_reissue'}
                            onClick={() => onAdd(item)}
                          >
                            {t('serverCertificate.discovered.add') as string}
                          </Button>
                        </span>
                      </Tooltip>
                      <Tooltip title={t('serverCertificate.discovered.dismiss') as string}>
                        <span>
                          <IconButton
                            size="small"
                            disabled={disabled}
                            onClick={() => onDismiss(item.id)}
                          >
                            <CloseIcon fontSize="small" />
                          </IconButton>
                        </span>
                      </Tooltip>
                    </Stack>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
    </Paper>
  );
};

export default DiscoveredAddressesPanel;
