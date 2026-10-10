import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Button,
  Tabs,
  Tab,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  CircularProgress,
  Autocomplete,
  Alert,
} from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import PersonAddIcon from '@mui/icons-material/PersonAdd';
import LinkIcon from '@mui/icons-material/Link';
import SecurityIcon from '@mui/icons-material/Security';
import { useParams } from 'react-router-dom';
import { useTranslation, Trans } from 'react-i18next';
import { DataTable, EntityLink, PageHeader, StatusChip, useToast, useConfirm } from '../../components/ui';
import { Team, TeamMember, TeamRole, UserSearchResult, TeamAgentTrust, TeamNameOnly, TeamAgent } from '../../types/team';
import { Client } from '../../types/client';
import { teamsService } from '../../services/teams';
import { listClients } from '../../services/api';
import { useAuth } from '../../contexts/AuthContext';
import { formatAgentVersion } from '../../utils/agentVersion';

interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

const SYSTEM_USER_ID = '00000000-0000-0000-0000-000000000000';

const TabPanel: React.FC<TabPanelProps> = ({ children, value, index }) => (
  <div hidden={value !== index}>{value === index && <Box sx={{ pt: 2 }}>{children}</Box>}</div>
);

export const TeamDetail: React.FC = () => {
  const { teamId } = useParams<{ teamId: string }>();
  const { t } = useTranslation('admin');
  const tr = (k: string, o?: any) => t(k, o) as string;
  const toast = useToast();
  const confirm = useConfirm();
  const { userRole } = useAuth();
  const [team, setTeam] = useState<Team | null>(null);
  const [members, setMembers] = useState<TeamMember[]>([]);
  const [clients, setClients] = useState<Client[]>([]);
  const [loading, setLoading] = useState(true);
  const [tabValue, setTabValue] = useState(0);

  // Member management state
  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<UserSearchResult[]>([]);
  const [selectedUser, setSelectedUser] = useState<UserSearchResult | null>(null);
  const [newMemberRole, setNewMemberRole] = useState<TeamRole>('member');

  // Edit team state
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [editName, setEditName] = useState('');
  const [editDescription, setEditDescription] = useState('');
  const [saving, setSaving] = useState(false);

  // Client assignment state
  const [assignClientOpen, setAssignClientOpen] = useState(false);
  const [allClients, setAllClients] = useState<Client[]>([]);
  const [selectedClient, setSelectedClient] = useState<Client | null>(null);
  const [loadingClients, setLoadingClients] = useState(false);

  // Agent visibility state
  const [agents, setAgents] = useState<TeamAgent[]>([]);

  // Trust management state
  const [trustedTeams, setTrustedTeams] = useState<TeamAgentTrust[]>([]);
  const [allTeamNames, setAllTeamNames] = useState<TeamNameOnly[]>([]);
  const [addTrustOpen, setAddTrustOpen] = useState(false);
  const [selectedTrustTeam, setSelectedTrustTeam] = useState<TeamNameOnly | null>(null);

  const showConfirm = async (message: string, action: () => void) => {
    const ok = await confirm({
      title: tr('teams.detail.confirmActionTitle'),
      message,
      severity: 'danger',
      confirmLabel: tr('common.confirm'),
    });
    if (ok) action();
  };

  // Permission checks
  const isTeamAdmin = team?.user_role === 'admin';
  const isSystemAdmin = userRole === 'admin';
  const canManageMembers = isTeamAdmin || isSystemAdmin;
  const canManageClients = isSystemAdmin;
  const canEditTeam = isTeamAdmin || isSystemAdmin;
  const canManageTrust = isTeamAdmin || isSystemAdmin;

  const loadTeamData = async () => {
    if (!teamId) return;

    try {
      setLoading(true);
      const [teamData, membersData, clientsData, trustData, agentsData] = await Promise.all([
        teamsService.getTeam(teamId),
        teamsService.getTeamMembers(teamId),
        teamsService.getTeamClients(teamId),
        teamsService.getTrustedTeams(teamId).catch(() => []),
        teamsService.getTeamAgents(teamId).catch(() => []),
      ]);
      setTeam(teamData);
      setMembers(membersData || []);
      setClients(clientsData || []);
      setTrustedTeams(trustData || []);
      setAgents(agentsData || []);
    } catch (error) {
      console.error('Failed to load team data:', error);
      toast.error(tr('teams.detail.errors.loadFailed'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTeamData();
  }, [teamId]);

  // Search users for adding
  useEffect(() => {
    const searchUsers = async () => {
      if (searchQuery.length < 2 || !teamId) {
        setSearchResults([]);
        return;
      }

      try {
        const results = await teamsService.searchUsers(teamId, searchQuery);
        setSearchResults(results);
      } catch (error) {
        console.error('Failed to search users:', error);
      }
    };

    const debounce = setTimeout(searchUsers, 300);
    return () => clearTimeout(debounce);
  }, [searchQuery, teamId]);

  // Member management handlers
  const handleAddMember = async () => {
    if (!teamId || !selectedUser) return;

    try {
      await teamsService.addMember(teamId, {
        user_id: selectedUser.id,
        role: newMemberRole,
      });
      setAddMemberOpen(false);
      setSelectedUser(null);
      setSearchQuery('');
      setNewMemberRole('member');
      await loadTeamData();
      toast.success(tr('teams.detail.messages.memberAdded'));
    } catch (error) {
      console.error('Failed to add member:', error);
      toast.error(tr('teams.detail.errors.addMemberFailed'));
    }
  };

  const handleRemoveMember = (userId: string) => {
    if (!teamId) return;
    showConfirm(tr('teams.detail.confirm.removeMember'), async () => {
      try {
        await teamsService.removeMember(teamId, userId);
        await loadTeamData();
        toast.success(tr('teams.detail.messages.memberRemoved'));
      } catch (error) {
        console.error('Failed to remove member:', error);
        toast.error(tr('teams.detail.errors.removeMemberFailed'));
      }
    });
  };

  const handleUpdateRole = async (userId: string, newRole: TeamRole) => {
    if (!teamId) return;

    try {
      await teamsService.updateMemberRole(teamId, userId, { role: newRole });
      await loadTeamData();
      toast.success(tr('teams.detail.messages.roleUpdated'));
    } catch (error) {
      console.error('Failed to update role:', error);
      toast.error(tr('teams.detail.errors.updateRoleFailed'));
    }
  };

  // Edit team handlers
  const handleEditOpen = () => {
    if (!team) return;
    setEditName(team.name);
    setEditDescription(team.description || '');
    setEditDialogOpen(true);
  };

  const handleEditSave = async () => {
    if (!teamId || !editName.trim()) return;

    try {
      setSaving(true);
      await teamsService.updateTeam(teamId, { name: editName.trim(), description: editDescription.trim() });
      setEditDialogOpen(false);
      await loadTeamData();
      toast.success(tr('teams.detail.messages.teamUpdated'));
    } catch (error) {
      console.error('Failed to update team:', error);
      toast.error(tr('teams.detail.errors.updateTeamFailed'));
    } finally {
      setSaving(false);
    }
  };

  // Client assignment handlers
  const handleAssignClientOpen = async () => {
    setAssignClientOpen(true);
    setSelectedClient(null);
    setLoadingClients(true);
    try {
      const response = await listClients();
      const allClientsData = response.data.data || [];
      const assignedIds = new Set(clients.map((c) => c.id));
      setAllClients(allClientsData.filter((c: Client) => !assignedIds.has(c.id)));
    } catch (error) {
      console.error('Failed to load clients:', error);
      toast.error(tr('teams.detail.errors.loadClientsFailed'));
    } finally {
      setLoadingClients(false);
    }
  };

  const handleAssignClient = async () => {
    if (!teamId || !selectedClient) return;

    try {
      await teamsService.assignClient(teamId, selectedClient.id);
      setAssignClientOpen(false);
      setSelectedClient(null);
      await loadTeamData();
      toast.success(tr('teams.detail.messages.clientAssigned'));
    } catch (error) {
      console.error('Failed to assign client:', error);
      toast.error(tr('teams.detail.errors.assignClientFailed'));
    }
  };

  const handleRemoveClient = (clientId: string, clientName: string) => {
    if (!teamId) return;
    showConfirm(
      tr('teams.detail.confirm.removeClient', { name: clientName }),
      async () => {
        try {
          await teamsService.removeClient(teamId, clientId);
          await loadTeamData();
          toast.success(tr('teams.detail.messages.clientRemoved'));
        } catch (error) {
          console.error('Failed to remove client:', error);
          toast.error(tr('teams.detail.errors.removeClientFailed'));
        }
      }
    );
  };

  // Trust management handlers
  const handleAddTrustOpen = async () => {
    setAddTrustOpen(true);
    setSelectedTrustTeam(null);
    try {
      const names = await teamsService.listAllTeamNames();
      // Filter out current team and already trusted teams
      const trustedIds = new Set(trustedTeams.map(t => t.trusted_team_id));
      setAllTeamNames(names.filter(nm => nm.id !== teamId && !trustedIds.has(nm.id)));
    } catch (error) {
      console.error('Failed to load team names:', error);
      toast.error(tr('teams.detail.errors.loadTeamNamesFailed'));
    }
  };

  const handleAddTrust = async () => {
    if (!teamId || !selectedTrustTeam) return;

    try {
      await teamsService.addTrust(teamId, selectedTrustTeam.id);
      setAddTrustOpen(false);
      setSelectedTrustTeam(null);
      await loadTeamData();
      toast.success(tr('teams.detail.messages.trustAdded'));
    } catch (error) {
      console.error('Failed to add trust:', error);
      toast.error(tr('teams.detail.errors.addTrustFailed'));
    }
  };

  const handleRemoveTrust = (trustedTeamId: string) => {
    if (!teamId) return;
    showConfirm(
      tr('teams.detail.confirm.removeTrust'),
      async () => {
        try {
          await teamsService.removeTrust(teamId, trustedTeamId);
          await loadTeamData();
          toast.success(tr('teams.detail.messages.trustRemoved'));
        } catch (error) {
          console.error('Failed to remove trust:', error);
          toast.error(tr('teams.detail.errors.removeTrustFailed'));
        }
      }
    );
  };

  const memberColumns: GridColDef<TeamMember>[] = [
    {
      field: 'username',
      headerName: tr('teams.detail.columns.username'),
      flex: 1,
      minWidth: 140,
      renderCell: (p) => <EntityLink type="user" id={p.row.user_id} label={p.row.username} />,
    },
    { field: 'email', headerName: tr('teams.detail.columns.email'), flex: 1.2, minWidth: 180 },
    {
      field: 'role',
      headerName: tr('teams.detail.columns.role'),
      width: 150,
      renderCell: (p) =>
        canManageMembers ? (
          <Box onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
            <Select
              size="small"
              value={p.row.role}
              onChange={(e) => handleUpdateRole(p.row.user_id, e.target.value as TeamRole)}
            >
              <MenuItem value="member">{tr('teams.detail.roles.member')}</MenuItem>
              <MenuItem value="admin">{tr('teams.detail.roles.admin')}</MenuItem>
            </Select>
          </Box>
        ) : (
          <Chip
            label={p.row.role === 'admin' ? tr('teams.detail.roles.admin') : tr('teams.detail.roles.member')}
            color={p.row.role === 'admin' ? 'primary' : 'default'}
            size="small"
          />
        ),
    },
    {
      field: 'joined_at',
      headerName: tr('teams.detail.columns.joined'),
      width: 130,
      valueFormatter: (v) => (v ? new Date(v as string).toLocaleDateString() : ''),
    },
  ];

  const clientColumns: GridColDef<Client>[] = [
    {
      field: 'name',
      headerName: tr('teams.detail.columns.clientName'),
      flex: 1,
      minWidth: 160,
      renderCell: (p) => <EntityLink type="client" id={p.row.id} label={p.row.name} />,
    },
    {
      field: 'description',
      headerName: tr('teams.detail.columns.description'),
      flex: 2,
      minWidth: 200,
      valueFormatter: (v) => (v as string) || '-',
    },
  ];

  const trustColumns: GridColDef<TeamAgentTrust>[] = [
    {
      field: 'trusted_name',
      headerName: tr('teams.detail.columns.trustedTeam'),
      flex: 1,
      minWidth: 180,
      valueGetter: (_v, row) => row.trusted_name || row.trusted_team_id,
      renderCell: (p) => (
        <EntityLink type="team" id={p.row.trusted_team_id} label={p.row.trusted_name || p.row.trusted_team_id} />
      ),
    },
    {
      field: 'created_at',
      headerName: tr('teams.detail.columns.trustedSince'),
      width: 150,
      valueFormatter: (v) => (v ? new Date(v as string).toLocaleDateString() : ''),
    },
  ];

  const agentColumns: GridColDef<TeamAgent>[] = [
    {
      field: 'name',
      headerName: tr('teams.detail.columns.agentName'),
      flex: 1,
      minWidth: 160,
      renderCell: (p) => <EntityLink type="agent" id={p.row.id} label={p.row.name} />,
    },
    {
      field: 'status',
      headerName: tr('teams.detail.columns.status'),
      width: 130,
      renderCell: (p) => <StatusChip entity="agent" status={p.row.status} />,
    },
    {
      field: 'version',
      headerName: tr('teams.detail.columns.version'),
      width: 130,
      valueFormatter: (v) => (v ? formatAgentVersion(v as string) : '-'),
    },
    {
      field: 'owner_username',
      headerName: tr('teams.detail.columns.owner'),
      flex: 1,
      minWidth: 140,
      valueGetter: (_v, row) => row.owner_username || tr('teams.detail.systemOwner'),
      renderCell: (p) =>
        p.row.owner_id && p.row.owner_id !== SYSTEM_USER_ID ? (
          <EntityLink type="user" id={p.row.owner_id} label={p.row.owner_username || p.row.owner_id} />
        ) : (
          <span>{p.row.owner_username || tr('teams.detail.systemOwner')}</span>
        ),
    },
    {
      field: 'source',
      headerName: tr('teams.detail.columns.source'),
      flex: 1,
      minWidth: 200,
      valueGetter: (_v, row) =>
        row.source === 'direct' ? tr('teams.detail.source.direct') : tr('teams.detail.source.trusted', { team: row.source_team_name || '' }),
      renderCell: (p) =>
        p.row.source === 'direct' ? (
          <Chip label={tr('teams.detail.source.direct')} color="primary" size="small" variant="outlined" />
        ) : (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Chip label={tr('teams.detail.source.trustedBadge')} color="secondary" size="small" variant="outlined" />
            {p.row.source_team_id ? (
              <EntityLink type="team" id={p.row.source_team_id} label={p.row.source_team_name || tr('teams.detail.source.unknown')} />
            ) : (
              <span>{p.row.source_team_name || tr('teams.detail.source.unknown')}</span>
            )}
          </Box>
        ),
    },
  ];

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (!team) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography>{tr('teams.detail.notFound')}</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={team.name}
        description={team.description || tr('teams.detail.noDescription')}
        backTo="/teams"
        breadcrumbs={[{ label: tr('teams.detail.breadcrumb'), to: '/teams' }, { label: team.name }]}
        actions={
          canEditTeam && (
            <Button variant="outlined" startIcon={<EditIcon />} onClick={handleEditOpen}>
              {tr('teams.detail.editTeam')}
            </Button>
          )
        }
      />

      <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
        <Tabs value={tabValue} onChange={(_, v) => setTabValue(v)}>
          <Tab label={tr('teams.detail.tabs.members', { count: members.length })} />
          <Tab label={tr('teams.detail.tabs.clients', { count: clients.length })} />
          <Tab label={tr('teams.detail.tabs.trustedTeams', { count: trustedTeams.length })} />
          <Tab label={tr('teams.detail.tabs.agents', { count: agents.length })} />
        </Tabs>
      </Box>

      {/* Members Tab */}
      <TabPanel value={tabValue} index={0}>
        {canManageMembers && (
          <Box sx={{ mb: 2 }}>
            <Button
              variant="contained"
              startIcon={<PersonAddIcon />}
              onClick={() => setAddMemberOpen(true)}
            >
              {tr('teams.detail.addMember')}
            </Button>
          </Box>
        )}

        <DataTable<TeamMember>
          rows={members}
          columns={memberColumns}
          getRowId={(r) => r.user_id}
          pagination={{ mode: 'client', initialPageSize: 25 }}
          sorting={{ mode: 'client', initial: [{ field: 'username', sort: 'asc' }] }}
          rowActions={
            canManageMembers
              ? () => [
                  {
                    key: 'remove',
                    label: tr('teams.detail.rowActions.removeMember'),
                    icon: <DeleteIcon fontSize="small" />,
                    danger: true,
                    onClick: (r) => handleRemoveMember(r.user_id),
                  },
                ]
              : undefined
          }
          emptyState={{ title: tr('teams.detail.empty.noMembers') }}
          tableKey="team-members"
        />
      </TabPanel>

      {/* Clients Tab */}
      <TabPanel value={tabValue} index={1}>
        {canManageClients && (
          <Box sx={{ mb: 2 }}>
            <Button
              variant="contained"
              startIcon={<LinkIcon />}
              onClick={handleAssignClientOpen}
            >
              {tr('teams.detail.assignClient')}
            </Button>
          </Box>
        )}

        <DataTable<Client>
          rows={clients}
          columns={clientColumns}
          getRowId={(r) => r.id}
          pagination={{ mode: 'client', initialPageSize: 25 }}
          sorting={{ mode: 'client', initial: [{ field: 'name', sort: 'asc' }] }}
          rowActions={
            canManageClients
              ? () => [
                  {
                    key: 'remove',
                    label: tr('teams.detail.rowActions.removeClientFromTeam'),
                    icon: <DeleteIcon fontSize="small" />,
                    danger: true,
                    onClick: (r) => handleRemoveClient(r.id, r.name),
                  },
                ]
              : undefined
          }
          emptyState={{ title: tr('teams.detail.empty.noClients') }}
          tableKey="team-clients"
        />
      </TabPanel>

      {/* Trusted Teams Tab */}
      <TabPanel value={tabValue} index={2}>
        <Alert severity="info" sx={{ mb: 2 }}>
          {tr('teams.detail.trustInfo')}
        </Alert>
        {canManageTrust && (
          <Box sx={{ mb: 2 }}>
            <Button
              variant="contained"
              startIcon={<SecurityIcon />}
              onClick={handleAddTrustOpen}
            >
              {tr('teams.detail.addTrust')}
            </Button>
          </Box>
        )}

        <DataTable<TeamAgentTrust>
          rows={trustedTeams}
          columns={trustColumns}
          getRowId={(r) => r.trusted_team_id}
          pagination={false}
          sorting={{ mode: 'client' }}
          rowActions={
            canManageTrust
              ? () => [
                  {
                    key: 'remove',
                    label: tr('teams.detail.rowActions.removeTrust'),
                    icon: <DeleteIcon fontSize="small" />,
                    danger: true,
                    onClick: (r) => handleRemoveTrust(r.trusted_team_id),
                  },
                ]
              : undefined
          }
          emptyState={{ title: tr('teams.detail.empty.noTrustedTeams') }}
          tableKey="team-trust"
        />
      </TabPanel>

      {/* Agents Tab */}
      <TabPanel value={tabValue} index={3}>
        <Alert severity="info" sx={{ mb: 2 }}>
          <Trans
            t={t}
            i18nKey="teams.detail.agentsInfo"
            components={{ strong: <strong /> }}
          />
        </Alert>

        <DataTable<TeamAgent>
          rows={agents}
          columns={agentColumns}
          getRowId={(r) => r.id}
          pagination={{ mode: 'client', initialPageSize: 25 }}
          sorting={{ mode: 'client', initial: [{ field: 'name', sort: 'asc' }] }}
          emptyState={{
            title: tr('teams.detail.empty.noAgents'),
            description: tr('teams.detail.empty.noAgentsHint'),
          }}
          tableKey="team-agents"
        />
      </TabPanel>

      {/* Edit Team Dialog */}
      <Dialog open={editDialogOpen} onClose={() => setEditDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{tr('teams.detail.dialogs.editTeam.title')}</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label={tr('teams.detail.dialogs.editTeam.nameLabel')}
            fullWidth
            required
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
          />
          <TextField
            margin="dense"
            label={tr('teams.detail.dialogs.editTeam.descriptionLabel')}
            fullWidth
            multiline
            rows={3}
            value={editDescription}
            onChange={(e) => setEditDescription(e.target.value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditDialogOpen(false)}>{tr('teams.detail.dialogs.cancel')}</Button>
          <Button onClick={handleEditSave} variant="contained" disabled={!editName.trim() || saving}>
            {saving ? tr('teams.detail.dialogs.editTeam.savingButton') : tr('teams.detail.dialogs.editTeam.saveButton')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Add Member Dialog */}
      <Dialog open={addMemberOpen} onClose={() => setAddMemberOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{tr('teams.detail.dialogs.addMember.title')}</DialogTitle>
        <DialogContent>
          <Autocomplete
            options={searchResults}
            getOptionLabel={(option) => `${option.username} (${option.email})`}
            value={selectedUser}
            onChange={(_, value) => setSelectedUser(value)}
            inputValue={searchQuery}
            onInputChange={(_, value) => setSearchQuery(value)}
            renderInput={(params) => (
              <TextField
                {...params}
                label={tr('teams.detail.dialogs.addMember.searchLabel')}
                margin="dense"
                placeholder={tr('teams.detail.dialogs.addMember.searchPlaceholder')}
              />
            )}
            noOptionsText={searchQuery.length < 2 ? tr('teams.detail.dialogs.addMember.typeToSearch') : tr('teams.detail.dialogs.addMember.noUsersFound')}
          />
          <FormControl fullWidth margin="dense">
            <InputLabel>{tr('teams.detail.dialogs.addMember.roleLabel')}</InputLabel>
            <Select
              value={newMemberRole}
              label={tr('teams.detail.dialogs.addMember.roleLabel')}
              onChange={(e) => setNewMemberRole(e.target.value as TeamRole)}
            >
              <MenuItem value="member">{tr('teams.detail.roles.member')}</MenuItem>
              <MenuItem value="admin">{tr('teams.detail.roles.adminManager')}</MenuItem>
            </Select>
          </FormControl>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAddMemberOpen(false)}>{tr('teams.detail.dialogs.cancel')}</Button>
          <Button onClick={handleAddMember} variant="contained" disabled={!selectedUser}>
            {tr('teams.detail.dialogs.addMember.addButton')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Add Trust Dialog */}
      <Dialog open={addTrustOpen} onClose={() => setAddTrustOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{tr('teams.detail.dialogs.addTrust.title')}</DialogTitle>
        <DialogContent>
          <Alert severity="info" sx={{ mb: 2 }}>
            {tr('teams.detail.dialogs.addTrust.info')}
          </Alert>
          <Autocomplete
            options={allTeamNames}
            getOptionLabel={(option) => tr('teams.detail.dialogs.addTrust.optionLabel', { name: option.name, count: option.agent_count })}
            value={selectedTrustTeam}
            onChange={(_, value) => setSelectedTrustTeam(value)}
            renderInput={(params) => (
              <TextField
                {...params}
                label={tr('teams.detail.dialogs.addTrust.selectLabel')}
                margin="dense"
                placeholder={tr('teams.detail.dialogs.addTrust.searchPlaceholder')}
              />
            )}
            noOptionsText={tr('teams.detail.dialogs.addTrust.noTeamsAvailable')}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAddTrustOpen(false)}>{tr('teams.detail.dialogs.cancel')}</Button>
          <Button onClick={handleAddTrust} variant="contained" disabled={!selectedTrustTeam}>
            {tr('teams.detail.addTrust')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Assign Client Dialog */}
      <Dialog open={assignClientOpen} onClose={() => setAssignClientOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{tr('teams.detail.dialogs.assignClient.title')}</DialogTitle>
        <DialogContent>
          <Alert severity="info" sx={{ mb: 2 }}>
            {tr('teams.detail.dialogs.assignClient.info')}
          </Alert>
          {loadingClients ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
              <CircularProgress size={24} />
            </Box>
          ) : (
            <Autocomplete
              options={allClients}
              getOptionLabel={(option) => option.name}
              value={selectedClient}
              onChange={(_, value) => setSelectedClient(value)}
              renderOption={(props, option) => (
                <li {...props} key={option.id}>
                  <Box>
                    <Typography variant="body1">{option.name}</Typography>
                    {option.description && (
                      <Typography variant="caption" color="text.secondary">
                        {option.description}
                      </Typography>
                    )}
                  </Box>
                </li>
              )}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={tr('teams.detail.dialogs.assignClient.selectLabel')}
                  margin="dense"
                  placeholder={tr('teams.detail.dialogs.assignClient.searchPlaceholder')}
                />
              )}
              noOptionsText={tr('teams.detail.dialogs.assignClient.noClientsAvailable')}
            />
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAssignClientOpen(false)}>{tr('teams.detail.dialogs.cancel')}</Button>
          <Button onClick={handleAssignClient} variant="contained" disabled={!selectedClient}>
            {tr('teams.detail.assignClient')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default TeamDetail;
