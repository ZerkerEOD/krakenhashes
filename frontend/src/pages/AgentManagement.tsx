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
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  FormControlLabel,
  Switch,
  CircularProgress,
  Alert,
  Link,
  Tooltip,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  CheckCircle as CheckCircleIcon,
  Cancel as CancelIcon,
  Clear as ClearIcon
} from '@mui/icons-material';
import { Agent, ClaimVoucher, AgentDevice } from '../types/agent';
import { api } from '../services/api';
import AgentInstall from '../components/agent/AgentInstall';

/**
 * Render the Agent Management page for viewing and managing agents, active claim vouchers, and device/status details.
 *
 * The component provides UI and controls to generate and deactivate claim vouchers, remove agents, clear stuck/busy agent status, and launch the per-OS installation wizard.
 *
 * @returns The Agent Management page as a JSX element
 */
export default function AgentManagement() {
  const { t } = useTranslation('agents');
  const queryClient = useQueryClient();
  const [openDialog, setOpenDialog] = useState(false);
  const [isContinuous, setIsContinuous] = useState(false);
  const [isSystemVoucher, setIsSystemVoucher] = useState(false);
  const [claimCode, setClaimCode] = useState<string>('');
  const [error, setError] = useState<string | null>(null);
  const [clearBusyDialogOpen, setClearBusyDialogOpen] = useState(false);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  // --- Queries (React Query keeps previous data on refetch, so background
  // polling updates the tables in place without blanking the page) ---
  const { data: agents = [], isLoading } = useQuery({
    queryKey: ['agents'],
    queryFn: async () => (await api.get<Agent[]>('/api/agents')).data || [],
    refetchInterval: 15000,
    placeholderData: keepPreviousData,
  });

  const { data: vouchersRaw = [] } = useQuery({
    queryKey: ['vouchers'],
    queryFn: async () => (await api.get<ClaimVoucher[]>('/api/vouchers')).data || [],
    refetchInterval: 15000,
    placeholderData: keepPreviousData,
  });
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

  // --- Mutations (invalidate the affected query; no full-page refetch) ---
  const generateCodeMutation = useMutation({
    mutationFn: (vars: { isContinuous: boolean; isSystem: boolean }) =>
      api.post<{ code: string }>('/api/vouchers/temp', { isContinuous: vars.isContinuous, isSystem: vars.isSystem })
        .then(r => r.data),
    onSuccess: (data) => {
      setClaimCode(data.code);
      queryClient.invalidateQueries({ queryKey: ['vouchers'] });
    },
    onError: () => setError(t('errors.generateFailed') as string),
  });

  const deactivateVoucherMutation = useMutation({
    mutationFn: (code: string) => api.delete(`/api/vouchers/${code}/disable`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['vouchers'] }),
    onError: () => setError(t('errors.deactivateFailed') as string),
  });

  const removeAgentMutation = useMutation({
    mutationFn: (id: string) => api.delete(`/api/agents/${id}`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['agents'] }),
    onError: () => setError(t('errors.removeFailed') as string),
  });

  const clearBusyMutation = useMutation({
    mutationFn: (id: string) => api.post(`/api/agents/${id}/clear-busy-status`),
    onSuccess: () => {
      setSuccessMessage(t('messages.busyStatusCleared') as string);
      queryClient.invalidateQueries({ queryKey: ['agents'] });
    },
    onError: () => setError(t('errors.clearBusyFailed') as string),
  });

  const handleCreateClaimCode = () => {
    setError(null);
    generateCodeMutation.mutate({ isContinuous, isSystem: isSystemVoucher });
  };

  const handleDeactivateVoucher = (code: string) => {
    setError(null);
    deactivateVoucherMutation.mutate(code);
  };

  const handleRemoveAgent = (agentId: string) => {
    setError(null);
    removeAgentMutation.mutate(agentId);
  };

  const handleClearBusyStatus = () => {
    if (!selectedAgentId) return;
    setError(null);
    setSuccessMessage(null);
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

  // Spinner only on the very first load (RQ keeps data on background refetch).
  if (isLoading) {
    return (
      <Box sx={{ p: 3, display: 'flex', justifyContent: 'center', alignItems: 'center', height: '50vh' }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 3 }}>
          <Box>
            <Typography variant="h4" component="h1" gutterBottom>
              {t('page.title') as string}
            </Typography>
            <Typography variant="body1" color="text.secondary">
              {t('page.description') as string}
            </Typography>
          </Box>
          <Button
            variant="contained"
            color="primary"
            onClick={() => setOpenDialog(true)}
          >
            {t('buttons.registerAgent') as string}
          </Button>
        </Box>

        {error && (
          <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
            {error}
          </Alert>
        )}

        {successMessage && (
          <Alert severity="success" sx={{ mb: 2 }} onClose={() => setSuccessMessage(null)}>
            {successMessage}
          </Alert>
        )}

        {/* Agent Install Section (collapsed accordion + per-OS wizard) */}
        <AgentInstall
          vouchers={claimVouchers}
          defaultCode={claimCode || undefined}
          onGenerateCode={handleWizardGenerateCode}
        />

        {/* Active Claim Vouchers Table */}
        <Typography variant="h5" sx={{ mt: 4, mb: 2 }}>
          {t('sections.activeVouchers') as string}
        </Typography>
        <TableContainer component={Paper} sx={{ mb: 4 }}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>{t('table.columns.claimCode') as string}</TableCell>
                <TableCell>{t('table.columns.createdBy') as string}</TableCell>
                <TableCell>{t('table.columns.createdAt') as string}</TableCell>
                <TableCell>{t('table.columns.type') as string}</TableCell>
                <TableCell>{t('table.columns.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {claimVouchers.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} align="center">
                    {t('messages.noVouchers') as string}
                  </TableCell>
                </TableRow>
              ) : (
                claimVouchers.map((voucher) => (
                  <TableRow key={voucher.code}>
                    <TableCell>{voucher.code}</TableCell>
                    <TableCell>{voucher.created_by?.username || (t('common.unknown') as string)}</TableCell>
                    <TableCell>{new Date(voucher.created_at).toLocaleString()}</TableCell>
                    <TableCell>
                      <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                        <Chip
                          label={voucher.is_continuous ? (t('vouchers.continuous') as string) : (t('vouchers.singleUse') as string)}
                          color={voucher.is_continuous ? "primary" : "default"}
                          size="small"
                        />
                        {voucher.created_by_id === '00000000-0000-0000-0000-000000000000' && (
                          <Chip label="System" color="secondary" size="small" />
                        )}
                      </Box>
                    </TableCell>
                    <TableCell>
                      <IconButton
                        onClick={() => handleDeactivateVoucher(voucher.code)}
                        color="error"
                        title={t('actions.deactivateVoucher') as string}
                      >
                        <DeleteIcon />
                      </IconButton>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableContainer>

        {/* Active Agents Table */}
        <Typography variant="h5" sx={{ mt: 4, mb: 2 }}>
          {t('sections.activeAgents') as string}
        </Typography>
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>{t('table.columns.agentId') as string}</TableCell>
                <TableCell>{t('table.columns.name') as string}</TableCell>
                <TableCell>{t('table.columns.enabled') as string}</TableCell>
                <TableCell>{t('table.columns.owner') as string}</TableCell>
                <TableCell>{t('table.columns.version') as string}</TableCell>
                <TableCell>{t('table.columns.hardware') as string}</TableCell>
                <TableCell>{t('table.columns.status') as string}</TableCell>
                <TableCell>{t('table.columns.actions') as string}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {agents.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} align="center">
                    {t('messages.noAgents') as string}
                  </TableCell>
                </TableRow>
              ) : (
                agents.map((agent) => (
                  <TableRow key={agent.id}>
                    <TableCell>{agent.id}</TableCell>
                    <TableCell>
                      <Link
                        component={RouterLink}
                        to={`/agents/${agent.id}`}
                        color="primary"
                        underline="hover"
                        sx={{ fontWeight: 'medium' }}
                      >
                        {agent.name}
                      </Link>
                    </TableCell>
                    <TableCell>
                      <Chip
                        label={agent.isEnabled !== false ? (t('status.enabled') as string) : (t('status.disabled') as string)}
                        color={agent.isEnabled !== false ? 'success' : 'default'}
                        size="small"
                      />
                    </TableCell>
                    <TableCell>
                      {(agent as any).isSystemAgent ? (
                        <Chip label="System" color="secondary" size="small" />
                      ) : (
                        agent.createdBy?.username || (t('common.unknown') as string)
                      )}
                    </TableCell>
                    <TableCell>
                      {agent.version}
                      {agent.status === 'updating' && agent.targetVersion && (
                        <Typography variant="caption" color="info.main" sx={{ display: 'block' }}>
                          → {agent.targetVersion}
                        </Typography>
                      )}
                    </TableCell>
                    <TableCell>
                      {agentDevices[agent.id]?.length > 0 ? (
                        agentDevices[agent.id].map((device) => (
                          <Box key={device.id} sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 0.5 }}>
                            {device.enabled ? (
                              <CheckCircleIcon sx={{ fontSize: 18, color: 'success.main' }} />
                            ) : (
                              <CancelIcon sx={{ fontSize: 18, color: 'error.main' }} />
                            )}
                            <Typography variant="body2">
                              {device.device_type || 'GPU'} {device.device_id}: {device.device_name}
                            </Typography>
                          </Box>
                        ))
                      ) : (
                        <Typography variant="body2" color="text.secondary">
                          {t('messages.noDevices') as string}
                        </Typography>
                      )}
                    </TableCell>
                    <TableCell>
                      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, alignItems: 'flex-start' }}>
                        <Chip
                          label={t(`labels.${agent.status}`, { ns: 'common' }) as string}
                          color={
                            agent.status === 'active'
                              ? 'success'
                              : agent.status === 'error'
                                ? 'error'
                                : agent.status === 'updating'
                                  ? 'info'
                                  : 'default'
                          }
                          icon={agent.status === 'updating' ? <CircularProgress size={12} color="inherit" /> : undefined}
                          size="small"
                        />
                        {agent.updatePending && agent.status !== 'updating' && (
                          <Chip label={t('status.updatePending') as string} color="warning" size="small" variant="outlined" />
                        )}
                        {agent.updateError && (
                          <Tooltip title={agent.updateError}>
                            <Chip label={t('status.updateFailed') as string} color="error" size="small" variant="outlined" />
                          </Tooltip>
                        )}
                      </Box>
                    </TableCell>
                    <TableCell>
                      {isAgentStuck(agent) && (
                        <IconButton
                          onClick={() => {
                            setSelectedAgentId(agent.id);
                            setClearBusyDialogOpen(true);
                          }}
                          color="warning"
                          title={t('actions.clearBusyStatus') as string}
                          sx={{ mr: 1 }}
                        >
                          <ClearIcon />
                        </IconButton>
                      )}
                      <IconButton
                        onClick={() => handleRemoveAgent(agent.id)}
                        color="error"
                        title={t('actions.removeAgent') as string}
                      >
                        <DeleteIcon />
                      </IconButton>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableContainer>

        {/* Clear Busy Status Confirmation Dialog */}
        <Dialog
          open={clearBusyDialogOpen}
          onClose={() => {
            setClearBusyDialogOpen(false);
            setSelectedAgentId(null);
          }}
        >
          <DialogTitle>{t('dialogs.clearStatus.title') as string}</DialogTitle>
          <DialogContent>
            <Typography>
              {t('dialogs.clearStatus.description') as string}
            </Typography>
            <Typography sx={{ mt: 2, fontWeight: 'bold', color: 'warning.main' }}>
              {t('dialogs.clearStatus.confirmation') as string}
            </Typography>
          </DialogContent>
          <DialogActions>
            <Button
              onClick={() => {
                setClearBusyDialogOpen(false);
                setSelectedAgentId(null);
              }}
            >
              {t('buttons.cancel') as string}
            </Button>
            <Button
              onClick={handleClearBusyStatus}
              variant="contained"
              color="warning"
            >
              {t('buttons.clearStatus') as string}
            </Button>
          </DialogActions>
        </Dialog>

        {/* Registration Dialog */}
        <Dialog
          open={openDialog}
          onClose={() => {
            setOpenDialog(false);
            setClaimCode('');
            setIsContinuous(false);
            setError(null);
          }}
        >
          <DialogTitle>{claimCode ? (t('dialogs.register.generatedTitle') as string) : (t('dialogs.register.title') as string)}</DialogTitle>
          <DialogContent>
            <Box sx={{ pt: 2 }}>
              {!claimCode && (
                <>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={isContinuous}
                        onChange={(e) => setIsContinuous(e.target.checked)}
                      />
                    }
                    label={t('dialogs.register.continuousLabel') as string}
                  />
                  <FormControlLabel
                    control={
                      <Switch
                        checked={isSystemVoucher}
                        onChange={(e) => setIsSystemVoucher(e.target.checked)}
                      />
                    }
                    label="System Agent (serves all teams)"
                  />
                </>
              )}
              {claimCode && (
                <Box sx={{ mt: 2, textAlign: 'center' }}>
                  <Typography variant="subtitle1">{t('dialogs.register.claimCodeLabel') as string}</Typography>
                  <Typography variant="h5" sx={{ mt: 1, mb: 2 }}>
                    {claimCode}
                  </Typography>
                  <Typography color="text.secondary">
                    {isContinuous
                      ? (t('dialogs.register.continuousDescription') as string)
                      : (t('dialogs.register.singleUseDescription') as string)}
                  </Typography>
                </Box>
              )}
            </Box>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => {
              setOpenDialog(false);
              setClaimCode('');
              setIsContinuous(false);
              setIsSystemVoucher(false);
              setError(null);
            }}>
              {t('buttons.close') as string}
            </Button>
            {!claimCode && (
              <Button onClick={handleCreateClaimCode} variant="contained">
                {t('buttons.generateCode') as string}
              </Button>
            )}
          </DialogActions>
        </Dialog>

    </Box>
  );
} 