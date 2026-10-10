import React from 'react';
import { Chip, Paper, Stack, Tooltip, Typography } from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import AddIcon from '@mui/icons-material/Add';
import CloseIcon from '@mui/icons-material/Close';
import { useTranslation } from 'react-i18next';
import { DataTable, EntityLink } from '../../ui';
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

  const columns: GridColDef<DiscoveredAddress>[] = [
    {
      field: 'address',
      headerName: t('serverCertificate.discovered.address') as string,
      flex: 1,
      minWidth: 180,
      renderCell: (p) => (
        <Stack direction="row" spacing={1} alignItems="center">
          <Typography variant="body2" sx={{ fontFamily: (theme) => theme.typography.monoFamily }}>
            {p.row.address}
          </Typography>
          {p.row.status === 'pending_reissue' && (
            <Chip size="small" color="info" label={t('serverCertificate.discovered.pending') as string} />
          )}
        </Stack>
      ),
    },
    {
      field: 'sources',
      headerName: t('serverCertificate.discovered.source') as string,
      flex: 1,
      minWidth: 180,
      sortable: false,
      renderCell: (p) => (
        <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', gap: 0.5, py: 0.5 }}>
          {p.row.sources.map((source) => (
            <Tooltip key={source} title={t(`serverCertificate.discovered.sourceHelp.${source}`) as string}>
              <Chip
                size="small"
                color={sourceColor(source)}
                label={t(`serverCertificate.discovered.sources.${source}`) as string}
              />
            </Tooltip>
          ))}
        </Stack>
      ),
    },
    {
      field: 'last_agent_name',
      headerName: t('serverCertificate.discovered.reportedBy') as string,
      flex: 1,
      minWidth: 160,
      renderCell: (p) =>
        p.row.last_agent_id ? (
          <EntityLink type="agent" id={p.row.last_agent_id} label={p.row.last_agent_name ?? `#${p.row.last_agent_id}`} />
        ) : (
          <Typography variant="body2" color="text.secondary" noWrap>
            {p.row.last_agent_name ?? p.row.last_user_agent ?? '—'}
          </Typography>
        ),
    },
    {
      field: 'last_seen_at',
      headerName: t('serverCertificate.discovered.lastSeen') as string,
      width: 110,
      renderCell: (p) => (
        <Typography variant="body2" color="text.secondary">
          {relativeTime(p.row.last_seen_at)}
        </Typography>
      ),
    },
    {
      field: 'hit_count',
      headerName: t('serverCertificate.discovered.hits') as string,
      width: 80,
      align: 'right',
      headerAlign: 'right',
    },
  ];

  return (
    <Paper sx={{ p: 3 }}>
      <Typography variant="subtitle1" gutterBottom>
        {t('serverCertificate.discovered.title') as string}
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('serverCertificate.discovered.help') as string}
      </Typography>

      <DataTable<DiscoveredAddress>
        flat
        rows={visible}
        columns={columns}
        getRowId={(r) => r.id}
        pagination={false}
        sorting={false}
        rowActions={(item) => [
          {
            key: 'add',
            label: t('serverCertificate.discovered.add') as string,
            icon: <AddIcon fontSize="small" />,
            disabled: disabled || !item.allowed || item.status === 'pending_reissue',
            tooltip: item.allowed ? undefined : item.rejection_reason ?? undefined,
            placement: 'inline',
            onClick: (r) => onAdd(r),
          },
          {
            key: 'dismiss',
            label: t('serverCertificate.discovered.dismiss') as string,
            icon: <CloseIcon fontSize="small" />,
            disabled,
            placement: 'inline',
            onClick: (r) => onDismiss(r.id),
          },
        ]}
        emptyState={{ title: t('serverCertificate.discovered.empty') as string }}
      />
    </Paper>
  );
};

export default DiscoveredAddressesPanel;
