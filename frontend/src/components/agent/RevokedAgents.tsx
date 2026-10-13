/**
 * Read-only table of revoked agents.
 *
 * Revoke is a hard, permanent action distinct from "disable": it invalidates
 * the agent's client certificate, severs its connection, and moves it out of
 * the active agent list (retaining history) into this view. The data comes
 * from `GET /api/agents/revoked`, which is admin only, so this component is
 * only rendered for admins.
 *
 * @packageDocumentation
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { Box, Typography } from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import { RevokedAgent, NullString, NullTime } from '../../types/agent';
import { getRevokedAgents } from '../../services/api';
import { DataTable } from '../ui';
import { formatAgentVersion } from '../../utils/agentVersion';
import { formatDateTime, formatRelativeTime } from '../../utils/formatters';

/** Unwrap a Go `sql.NullString` JSON shape to its value, or null when absent. */
const nullStr = (v?: NullString): string | null => (v && v.Valid ? v.String : null);
/** Unwrap a Go `sql.NullTime` JSON shape to its ISO value, or null when absent. */
const nullTime = (v?: NullTime): string | null => (v && v.Valid ? v.Time : null);

/**
 * Render the read-only Revoked Agents table. Admin-only (the backend endpoint
 * returns 403 otherwise), so callers should only mount this for admins.
 *
 * @returns The revoked agents table as a JSX element.
 */
export default function RevokedAgents() {
  const { t } = useTranslation('agents');
  const tr = (k: string, o?: any) => t(k, o) as string;

  const {
    data: agents = [],
    isLoading,
    isFetching,
    error,
    refetch,
  } = useQuery({
    queryKey: ['revoked-agents'],
    queryFn: getRevokedAgents,
    placeholderData: keepPreviousData,
  });

  const dash = tr('common.none');

  const columns: GridColDef<RevokedAgent>[] = [
    {
      field: 'id',
      headerName: tr('table.columns.agentId'),
      width: 100,
      type: 'number',
      align: 'left',
      headerAlign: 'left',
    },
    {
      field: 'name',
      headerName: tr('table.columns.name'),
      flex: 1,
      minWidth: 150,
      renderCell: (p) => <Typography variant="body2">{p.row.name || dash}</Typography>,
    },
    {
      field: 'version',
      headerName: tr('table.columns.version'),
      width: 150,
      valueGetter: (_v, row) => formatAgentVersion(row.version),
    },
    {
      field: 'revokedAt',
      headerName: tr('revoked.columns.revokedAt'),
      width: 180,
      valueGetter: (_v, row) => nullTime(row.revokedAt),
      renderCell: (p) => <Typography variant="body2">{formatDateTime(p.value as string | null)}</Typography>,
    },
    {
      field: 'revokedByUsername',
      headerName: tr('revoked.columns.revokedBy'),
      flex: 0.8,
      minWidth: 140,
      valueGetter: (_v, row) => nullStr(row.revokedByUsername) ?? dash,
    },
    {
      field: 'lastHeartbeat',
      headerName: tr('revoked.columns.lastSeen'),
      width: 160,
      valueGetter: (_v, row) => row.lastHeartbeat,
      renderCell: (p) => (
        <Typography variant="body2">{formatRelativeTime(p.row.lastHeartbeat)}</Typography>
      ),
    },
    {
      field: 'clientCertSerial',
      headerName: tr('revoked.columns.certSerial'),
      flex: 1,
      minWidth: 180,
      sortable: false,
      valueGetter: (_v, row) => nullStr(row.clientCertSerial) ?? dash,
      renderCell: (p) => (
        <Typography
          variant="body2"
          sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}
        >
          {p.value as string}
        </Typography>
      ),
    },
  ];

  return (
    <Box>
      <DataTable<RevokedAgent>
        rows={agents}
        columns={columns}
        getRowId={(r) => r.id}
        loading={isLoading}
        fetching={isFetching && !isLoading}
        error={error}
        onRetry={() => refetch()}
        pagination={{ mode: 'client', initialPageSize: 25 }}
        sorting={{ mode: 'client', initial: [{ field: 'revokedAt', sort: 'desc' }] }}
        toolbar={{ title: tr('sections.revokedAgents') }}
        emptyState={{ title: tr('messages.noRevokedAgents') }}
        tableKey="revoked-agents"
      />
    </Box>
  );
}
