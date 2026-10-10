/**
 * Agent Management page component for KrakenHashes frontend.
 * 
 * Features:
 *   - Agent registration with claim code generation
 *   - Agent list display and management
 *   - Real-time status monitoring
 *   - Team assignment
 * 
 * @packageDocumentation
 */

import React, { useState } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient, keepPreviousData } from '@tanstack/react-query';
import {
  Box,
  Button,
  Typography,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Tooltip,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  CheckCircle as CheckCircleIcon,
  Cancel as CancelIcon,
  Clear as ClearIcon
} from '@mui/icons-material';
import type { GridColDef } from '@mui/x-data-grid';
import { Agent, ClaimVoucher, AgentDevice } from '../types/agent';
import { ROUTES } from '../constants/routes';
import { useAuth } from '../contexts/AuthContext';
import GenerateVoucherDialog from '../components/vouchers/GenerateVoucherDialog';
import ConfirmationNumberIcon from '@mui/icons-material/ConfirmationNumber';
import { api } from '../services/api';
import AgentInstall from '../components/agent/AgentInstall';
import SanFailureBanner from '../components/admin/certificates/SanFailureBanner';
import { formatAgentVersion, agentVersionStatus } from '../utils/agentVersion';
import { DataTable, EntityLink, PageHeader, StatusChip, useConfirm, useToast } from '../components/ui';
import { useLiveQuery } from '../hooks/useLiveQuery';
import { getErrorMessage } from '../utils/errors';

const NIL_UUID = '00000000-0000-0000-0000-000000000000';

/** System/ownerless agents have no owner (empty, nil or the all-zero UUID). */
const isSystemOwned = (agent: Agent): boolean =>
  Boolean((agent as any).isSystemAgent) || !agent.ownerId || agent.ownerId === NIL_UUID;

/**
 * Render the Agent Management page for viewing and managing agents, active claim vouchers, and device/status details.
 *
 * The component provides UI and controls to generate and deactivate claim vouchers, remove agents, clear stuck/busy agent status, and launch the per-OS installation wizard.
 *
 * @returns The Agent Management page as a JSX element
 */
export default function AgentManagement() {
  const { t } = useTranslation('agents');
  const tr = (k: string, o?: any) => t(k, o) as string;
  const { userRole } = useAuth();
  const queryClient = useQueryClient();
  const toast = useToast();
  const confirm = useConfirm();
  const [openDialog, setOpenDialog] = useState(false);
  const [claimCode, setClaimCode] = useState<string>('');
  const [clearBusyDialogOpen, setClearBusyDialogOpen] = useState(false);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);

  // --- Queries (React Query keeps previous data on refetch, so background
  // polling updates the tables in place without blanking the page) ---
  const {
    data: agents = [],
    isLoading,
    isFetching,
    error: agentsError,
    refetch: refetchAgents,
  } = useLiveQuery(
    {
      queryKey: ['agents'],
      queryFn: async () => (await api.get<Agent[]>('/api/agents')).data || [],
      placeholderData: keepPreviousData,
    },
    { tier: 'list' }
  );

  const { data: vouchersRaw = [] } = useLiveQuery(
    {
      queryKey: ['vouchers'],
      queryFn: async () => (await api.get<ClaimVoucher[]>('/api/vouchers')).data || [],
      placeholderData: keepPreviousData,
    },
    { tier: 'list' }
  );
  const claimVouchers = vouchersRaw.filter(v => v.is_active);

  // Devices keyed on the agent-id SET (only refetches when agents are
  // added/removed, not on every status poll).
  const agentIdsKey = agents.map(a => a.id).sort().join(',');
  const { data: agentDevices = {} } = useQuery({
    queryKey: ['agent-devices', agentIdsKey],
    enabled: agents.length > 0,
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const results = await Promise.all(agents.map(agent =>
        api.get<AgentDevice[]>(`/api/agents/${agent.id}/devices`)
          .then(res => ({ id: agent.id, devices: res.data || [] }))
          .catch(() => ({ id: agent.id, devices: [] as AgentDevice[] }))
      ));
      const map: { [key: string]: AgentDevice[] } = {};
      results.forEach(r => { map[r.id] = r.devices; });
      return map;
    },
  });

  // Expected cluster agent version (clean release, e.g. "2.1.0"). Shared cache
  // key with the install panel. Used to badge agents that are behind.
  const { data: agentPlatforms } = useQuery({
    queryKey: ['agent-platforms'],
    queryFn: async () => (await api.get<{ version: string }>('/api/public/agent/platforms')).data,
    staleTime: 5 * 60 * 1000,
  });
  const expectedVersion = agentPlatforms?.version || '';

  // --- Mutations (invalidate the affected query; no full-page refetch) ---
  const generateCodeMutation = useMutation({
    mutationFn: (vars: { isContinuous: boolean; isSystem: boolean }) =>
      api.post<{ code: string }>('/api/vouchers/temp', { isContinuous: vars.isContinuous, isSystem: vars.isSystem })
        .then(r => r.data),
    onSuccess: (data) => {
      setClaimCode(data.code);
      queryClient.invalidateQueries({ queryKey: ['vouchers'] });
    },
    onError: () => toast.error(tr('errors.generateFailed')),
  });

  const clearBusyMutation = useMutation({
    mutationFn: (id: string) => api.post(`/api/agents/${id}/clear-busy-status`),
    onSuccess: () => {
      toast.success(tr('messages.busyStatusCleared'));
      queryClient.invalidateQueries({ queryKey: ['agents'] });
    },
    onError: () => toast.error(tr('errors.clearBusyFailed')),
  });

  const handleRemoveAgent = async (agent: Agent) => {
    await confirm({
      title: tr('actions.removeAgent'),
      message: agent.name || agent.id,
      severity: 'danger',
      confirmLabel: tr('actions.removeAgent'),
      action: async () => {
        try {
          await api.delete(`/api/agents/${agent.id}`);
        } catch (err) {
          throw new Error(getErrorMessage(err) || tr('errors.removeFailed'));
        }
        queryClient.invalidateQueries({ queryKey: ['agents'] });
      },
    });
  };

  const handleClearBusyStatus = () => {
    if (!selectedAgentId) return;
    clearBusyMutation.mutate(selectedAgentId);
    setClearBusyDialogOpen(false);
    setSelectedAgentId(null);
  };

  // Generate a code for the install wizard; returns the new code.
  const handleWizardGenerateCode = async (): Promise<string | null> => {
    try {
      const data = await generateCodeMutation.mutateAsync({ isContinuous: false, isSystem: false });
      return data.code;
    } catch {
      return null;
    }
  };

  // Check if agent is stuck (busy but no active tasks)
  const isAgentStuck = (agent: Agent): boolean => {
    const busyStatus = agent.metadata?.busy_status;
    const currentTaskId = agent.metadata?.current_task_id;
    return busyStatus === 'true' && !currentTaskId;
  };

  const columns: GridColDef<Agent>[] = [
    {
      field: 'id',
      headerName: tr('table.columns.agentId'),
      width: 100,
      type: 'number',
      align: 'left',
      headerAlign: 'left',
      valueGetter: (_v, row) => Number(row.id),
    },
    {
      field: 'name',
      headerName: tr('table.columns.name'),
      flex: 1,
      minWidth: 150,
      renderCell: (p) => <EntityLink type="agent" id={p.row.id} label={p.row.name} />,
    },
    {
      field: 'isEnabled',
      headerName: tr('table.columns.enabled'),
      width: 110,
      valueGetter: (_v, row) => row.isEnabled !== false,
      renderCell: (p) => (
        <StatusChip entity="generic" status={p.value ? 'enabled' : 'disabled'} label={p.value ? tr('status.enabled') : tr('status.disabled')} />
      ),
    },
    {
      field: 'owner',
      headerName: tr('table.columns.owner'),
      flex: 0.8,
      minWidth: 130,
      valueGetter: (_v, row) => (isSystemOwned(row) ? tr('fields.systemOwner') : row.ownerUsername || row.createdBy?.username || ''),
      renderCell: (p) =>
        isSystemOwned(p.row) ? (
          <Chip label={tr('fields.systemOwner')} color="secondary" size="small" />
        ) : (
          <EntityLink
            type="user"
            id={p.row.ownerId}
            label={p.row.ownerUsername || p.row.createdBy?.username || tr('common.unknown')}
          />
        ),
    },
    {
      field: 'version',
      headerName: tr('table.columns.version'),
      width: 160,
      renderCell: (p) => (
        <Box sx={{ py: 1 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap' }}>
            <Typography variant="body2">{formatAgentVersion(p.row.version)}</Typography>
            {agentVersionStatus(p.row.version, expectedVersion) === 'update-available' && (
              <Chip label={tr('version.updateAvailable')} color="warning" size="small" variant="outlined" />
            )}
          </Box>
          {p.row.status === 'updating' && p.row.targetVersion && (
            <Typography variant="caption" color="info.main" sx={{ display: 'block' }}>
              → {formatAgentVersion(p.row.targetVersion)}
            </Typography>
          )}
        </Box>
      ),
    },
    {
      field: 'hardware',
      headerName: tr('table.columns.hardware'),
      flex: 1.6,
      minWidth: 240,
      sortable: false,
      valueGetter: (_v, row) => (agentDevices[row.id] || []).map((d) => d.device_name).join(', '),
      renderCell: (p) => {
        const devices = agentDevices[p.row.id] || [];
        return devices.length > 0 ? (
          <Box sx={{ py: 1 }}>
            {devices.map((device) => (
              <Box key={device.id} sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 0.5 }}>
                {device.enabled ? (
                  <CheckCircleIcon sx={{ fontSize: 18, color: 'success.main' }} />
                ) : (
                  <CancelIcon sx={{ fontSize: 18, color: 'error.main' }} />
                )}
                <Typography variant="body2">
                  {device.device_type || tr('hardware.defaultDeviceType')} {device.device_id}: {device.device_name}
                </Typography>
              </Box>
            ))}
          </Box>
        ) : (
          <Typography variant="body2" color="text.secondary">
            {tr('messages.noDevices')}
          </Typography>
        );
      },
    },
    {
      field: 'status',
      headerName: tr('table.columns.status'),
      width: 150,
      renderCell: (p) => (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, alignItems: 'flex-start', py: 1 }}>
          <StatusChip entity="agent" status={p.row.status} />
          {p.row.updatePending && p.row.status !== 'updating' && (
            <Chip label={tr('status.updatePending')} color="warning" size="small" variant="outlined" />
          )}
          {p.row.updateError && (
            <Tooltip title={p.row.updateError}>
              <Chip label={tr('status.updateFailed')} color="error" size="small" variant="outlined" />
            </Tooltip>
          )}
        </Box>
      ),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={tr('page.title')}
        description={tr('page.description')}
        actions={
          <Button variant="contained" color="primary" onClick={() => setOpenDialog(true)}>
            {tr('buttons.registerAgent')}
          </Button>
        }
      />

      {/* An agent that cannot verify the server certificate never appears in
          the list below, so the explanation has to be here rather than on a
          row. */}
      <SanFailureBanner />

      {/* Agent Install Section (collapsed accordion + per-OS wizard) */}
      <AgentInstall
        vouchers={claimVouchers}
        defaultCode={claimCode || undefined}
        onGenerateCode={handleWizardGenerateCode}
      />

      {userRole === 'admin' && (
        <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 2, mb: 3 }}>
          <Button component={RouterLink} to={ROUTES.admin.vouchers} startIcon={<ConfirmationNumberIcon />}>
            {tr('vouchers.manage')}
          </Button>
        </Box>
      )}

      {/* Active Agents Table */}
      <Box sx={{ mt: 4 }}>
        <DataTable<Agent>
          rows={agents}
          columns={columns}
          getRowId={(r) => r.id}
          loading={isLoading}
          fetching={isFetching && !isLoading}
          error={agentsError}
          onRetry={() => refetchAgents()}
          pagination={{ mode: 'client', initialPageSize: 25 }}
          sorting={{ mode: 'client', initial: [{ field: 'id', sort: 'asc' }] }}
          toolbar={{ title: tr('sections.activeAgents') }}
          rowActions={(agent) => [
            {
              key: 'clear-busy',
              label: tr('actions.clearBusyStatus'),
              icon: <ClearIcon fontSize="small" />,
              hidden: !isAgentStuck(agent),
              placement: 'inline',
              onClick: (a) => {
                setSelectedAgentId(a.id);
                setClearBusyDialogOpen(true);
              },
            },
            {
              key: 'remove',
              label: tr('actions.removeAgent'),
              icon: <DeleteIcon fontSize="small" />,
              danger: true,
              placement: 'inline',
              onClick: (a) => handleRemoveAgent(a),
            },
          ]}
          emptyState={{ title: tr('messages.noAgents') }}
          tableKey="agents"
        />
      </Box>

      {/* Clear Busy Status Confirmation Dialog */}
      <Dialog
        open={clearBusyDialogOpen}
        onClose={() => {
          setClearBusyDialogOpen(false);
          setSelectedAgentId(null);
        }}
      >
        <DialogTitle>{tr('dialogs.clearStatus.title')}</DialogTitle>
        <DialogContent>
          <Typography>
            {tr('dialogs.clearStatus.description')}
          </Typography>
          <Typography sx={{ mt: 2, fontWeight: 'bold', color: 'warning.main' }}>
            {tr('dialogs.clearStatus.confirmation')}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button
            onClick={() => {
              setClearBusyDialogOpen(false);
              setSelectedAgentId(null);
            }}
          >
            {tr('buttons.cancel')}
          </Button>
          <Button
            onClick={handleClearBusyStatus}
            variant="contained"
            color="warning"
          >
            {tr('buttons.clearStatus')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Registration Dialog */}
      <GenerateVoucherDialog
        open={openDialog}
        onClose={() => setOpenDialog(false)}
        onGenerated={(code) => setClaimCode(code)}
      />
    </Box>
  );
}
