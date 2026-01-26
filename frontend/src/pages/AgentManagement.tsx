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

import React, { useState, useEffect } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
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
} from '@mui/material';
import {
  Delete as DeleteIcon,
  CheckCircle as CheckCircleIcon,
  Cancel as CancelIcon,
  Clear as ClearIcon
} from '@mui/icons-material';
import { Agent, ClaimVoucher, AgentDevice } from '../types/agent';
import { api } from '../services/api';
import AgentDownloads from '../components/agent/AgentDownloads';

/**
 * AgentManagement component handles the display and management of KrakenHashes agents.
 * 
 * Features:
 *   - Register new agents
 *   - Generate claim codes
 *   - View agent status
 *   - Monitor agent health
 * 
 * @returns {JSX.Element} The rendered agent management page
 * 
 * @example
 * <AgentManagement />
 */
export default function AgentManagement() {
  const { t } = useTranslation('agents');
  const [agents, setAgents] = useState<Agent[]>([]);
  const [agentDevices, setAgentDevices] = useState<{ [key: string]: AgentDevice[] }>({});
  const [claimVouchers, setClaimVouchers] = useState<ClaimVoucher[]>([]);
  const [openDialog, setOpenDialog] = useState(false);
  const [isContinuous, setIsContinuous] = useState(false);
  const [claimCode, setClaimCode] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [clearBusyDialogOpen, setClearBusyDialogOpen] = useState(false);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  // Fetch data
  const fetchData = async () => {
    try {
      setLoading(true);
      setError(null);
      
      console.log('Fetching agents and vouchers...');
      const [agentsRes, vouchersRes] = await Promise.all([
        api.get<Agent[]>('/api/agents'),
        api.get<ClaimVoucher[]>('/api/vouchers')
      ]);
      
      console.log('Received agents:', agentsRes.data);
      console.log('Received vouchers:', vouchersRes.data);
      
      setAgents(agentsRes.data || []);
      setClaimVouchers((vouchersRes.data || []).filter(v => v.is_active));
      
      // Fetch devices for each agent
      const devicePromises = (agentsRes.data || []).map(agent => 
        api.get<AgentDevice[]>(`/api/agents/${agent.id}/devices`)
          .then(res => ({ agentId: agent.id, devices: res.data || [] }))
          .catch(() => ({ agentId: agent.id, devices: [] }))
      );
      
      const deviceResults = await Promise.all(devicePromises);
      const devicesMap: { [key: string]: AgentDevice[] } = {};
      deviceResults.forEach(result => {
        devicesMap[result.agentId] = result.devices;
      });
      setAgentDevices(devicesMap);
      
    } catch (error) {
      console.error('Failed to fetch data:', error);
      setError(t('errors.loadFailed') as string);
      setAgents([]);
      setClaimVouchers([]);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
    
    // Set up polling for updates
    const interval = setInterval(fetchData, 30000); // Poll every 30 seconds
    
    return () => clearInterval(interval);
  }, []);

  // Handle claim code generation
  const handleCreateClaimCode = async () => {
    try {
      setError(null);
      const response = await api.post<{ code: string }>('/api/vouchers/temp', {
        isContinuous: isContinuous
      });
      setClaimCode(response.data.code);
      await fetchData(); // Refresh the vouchers list
    } catch (error) {
      console.error('Failed to create claim code:', error);
      setError(t('errors.generateFailed') as string);
    }
  };

  // Handle voucher deactivation
  const handleDeactivateVoucher = async (code: string) => {
    try {
      setError(null);
      await api.delete(`/api/vouchers/${code}/disable`);
      await fetchData();
    } catch (error) {
      console.error('Failed to deactivate voucher:', error);
      setError(t('errors.deactivateFailed') as string);
    }
  };

  // Handle agent removal
  const handleRemoveAgent = async (agentId: string) => {
    try {
      setError(null);
      await api.delete(`/api/agents/${agentId}`);
      await fetchData();
    } catch (error) {
      console.error('Failed to remove agent:', error);
      setError(t('errors.removeFailed') as string);
    }
  };

  // Handle clear busy status
  const handleClearBusyStatus = async () => {
    if (!selectedAgentId) return;

    try {
      setError(null);
      setSuccessMessage(null);
      await api.post(`/api/agents/${selectedAgentId}/clear-busy-status`);
      setSuccessMessage(t('messages.busyStatusCleared') as string);
      setClearBusyDialogOpen(false);
      setSelectedAgentId(null);
      await fetchData();
    } catch (error) {
      console.error('Failed to clear busy status:', error);
      setError(t('errors.clearBusyFailed') as string);
      setClearBusyDialogOpen(false);
    }
  };

  // Check if agent is stuck (busy but no active tasks)
  const isAgentStuck = (agent: Agent): boolean => {
    const busyStatus = agent.metadata?.busy_status;
    const currentTaskId = agent.metadata?.current_task_id;
    return busyStatus === 'true' && !currentTaskId;
  };

  if (loading) {
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

        {/* Agent Downloads Section */}
        <AgentDownloads />

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
                      <Chip
                        label={voucher.is_continuous ? (t('vouchers.continuous') as string) : (t('vouchers.singleUse') as string)}
                        color={voucher.is_continuous ? "primary" : "default"}
                      />
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
                    <TableCell>{agent.createdBy?.username || (t('common.unknown') as string)}</TableCell>
                    <TableCell>{agent.version}</TableCell>
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
                      <Chip
                        label={t(`labels.${agent.status}`, { ns: 'common' }) as string}
                        color={agent.status === 'active' ? 'success' : agent.status === 'error' ? 'error' : 'default'}
                        size="small"
                      />
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
                <FormControlLabel
                  control={
                    <Switch
                      checked={isContinuous}
                      onChange={(e) => setIsContinuous(e.target.checked)}
                    />
                  }
                  label={t('dialogs.register.continuousLabel') as string}
                />
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