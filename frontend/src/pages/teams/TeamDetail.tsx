import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Button,
  Tabs,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  IconButton,
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
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import PersonAddIcon from '@mui/icons-material/PersonAdd';
import LinkIcon from '@mui/icons-material/Link';
import SecurityIcon from '@mui/icons-material/Security';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import { useParams, useNavigate } from 'react-router-dom';
import { useSnackbar } from 'notistack';
import { Team, TeamMember, TeamRole, UserSearchResult, TeamAgentTrust, TeamNameOnly } from '../../types/team';
import { Client } from '../../types/client';
import { teamsService } from '../../services/teams';
import { listClients } from '../../services/api';
import { useAuth } from '../../contexts/AuthContext';

interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

const TabPanel: React.FC<TabPanelProps> = ({ children, value, index }) => (
  <div hidden={value !== index}>{value === index && <Box sx={{ pt: 2 }}>{children}</Box>}</div>
);

export const TeamDetail: React.FC = () => {
  const { teamId } = useParams<{ teamId: string }>();
  const navigate = useNavigate();
  const { enqueueSnackbar } = useSnackbar();
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

  // Trust management state
  const [trustedTeams, setTrustedTeams] = useState<TeamAgentTrust[]>([]);
  const [allTeamNames, setAllTeamNames] = useState<TeamNameOnly[]>([]);
  const [addTrustOpen, setAddTrustOpen] = useState(false);
  const [selectedTrustTeam, setSelectedTrustTeam] = useState<TeamNameOnly | null>(null);

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
      const [teamData, membersData, clientsData, trustData] = await Promise.all([
        teamsService.getTeam(teamId),
        teamsService.getTeamMembers(teamId),
        teamsService.getTeamClients(teamId),
        teamsService.getTrustedTeams(teamId).catch(() => []),
      ]);
      setTeam(teamData);
      setMembers(membersData || []);
      setClients(clientsData || []);
      setTrustedTeams(trustData || []);
    } catch (error) {
      console.error('Failed to load team data:', error);
      enqueueSnackbar('Failed to load team data', { variant: 'error' });
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
      enqueueSnackbar('Member added successfully', { variant: 'success' });
    } catch (error) {
      console.error('Failed to add member:', error);
      enqueueSnackbar('Failed to add member', { variant: 'error' });
    }
  };

  const handleRemoveMember = async (userId: string) => {
    if (!teamId || !window.confirm('Are you sure you want to remove this member?')) return;

    try {
      await teamsService.removeMember(teamId, userId);
      await loadTeamData();
      enqueueSnackbar('Member removed', { variant: 'success' });
    } catch (error) {
      console.error('Failed to remove member:', error);
      enqueueSnackbar('Failed to remove member', { variant: 'error' });
    }
  };

  const handleUpdateRole = async (userId: string, newRole: TeamRole) => {
    if (!teamId) return;

    try {
      await teamsService.updateMemberRole(teamId, userId, { role: newRole });
      await loadTeamData();
      enqueueSnackbar('Role updated', { variant: 'success' });
    } catch (error) {
      console.error('Failed to update role:', error);
      enqueueSnackbar('Failed to update role', { variant: 'error' });
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
      enqueueSnackbar('Team updated successfully', { variant: 'success' });
    } catch (error) {
      console.error('Failed to update team:', error);
      enqueueSnackbar('Failed to update team', { variant: 'error' });
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
      enqueueSnackbar('Failed to load clients', { variant: 'error' });
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
      enqueueSnackbar('Client assigned to team', { variant: 'success' });
    } catch (error) {
      console.error('Failed to assign client:', error);
      enqueueSnackbar('Failed to assign client', { variant: 'error' });
    }
  };

  const handleRemoveClient = async (clientId: string, clientName: string) => {
    if (
      !teamId ||
      !window.confirm(
        `Are you sure you want to remove "${clientName}" from this team? Team members will lose access to this client's data.`
      )
    )
      return;

    try {
      await teamsService.removeClient(teamId, clientId);
      await loadTeamData();
      enqueueSnackbar('Client removed from team', { variant: 'success' });
    } catch (error) {
      console.error('Failed to remove client:', error);
      enqueueSnackbar('Failed to remove client', { variant: 'error' });
    }
  };

  // Trust management handlers
  const handleAddTrustOpen = async () => {
    setAddTrustOpen(true);
    setSelectedTrustTeam(null);
    try {
      const names = await teamsService.listAllTeamNames();
      // Filter out current team and already trusted teams
      const trustedIds = new Set(trustedTeams.map(t => t.trusted_team_id));
      setAllTeamNames(names.filter(t => t.id !== teamId && !trustedIds.has(t.id)));
    } catch (error) {
      console.error('Failed to load team names:', error);
      enqueueSnackbar('Failed to load team names', { variant: 'error' });
    }
  };

  const handleAddTrust = async () => {
    if (!teamId || !selectedTrustTeam) return;

    try {
      await teamsService.addTrust(teamId, selectedTrustTeam.id);
      setAddTrustOpen(false);
      setSelectedTrustTeam(null);
      await loadTeamData();
      enqueueSnackbar('Trust relationship added', { variant: 'success' });
    } catch (error) {
      console.error('Failed to add trust:', error);
      enqueueSnackbar('Failed to add trust relationship', { variant: 'error' });
    }
  };

  const handleRemoveTrust = async (trustedTeamId: string) => {
    if (!teamId || !window.confirm('Remove this trust relationship? Agents from this team will no longer be able to run your jobs.')) return;

    try {
      await teamsService.removeTrust(teamId, trustedTeamId);
      await loadTeamData();
      enqueueSnackbar('Trust relationship removed', { variant: 'success' });
    } catch (error) {
      console.error('Failed to remove trust:', error);
      enqueueSnackbar('Failed to remove trust relationship', { variant: 'error' });
    }
  };

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
        <Typography>Team not found</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Button
            startIcon={<ArrowBackIcon />}
            onClick={() => navigate('/teams')}
            sx={{ mb: 1 }}
          >
            Back to Teams
          </Button>
          <Typography variant="h4" component="h1" gutterBottom>
            {team.name}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {team.description || 'No description'}
          </Typography>
        </Box>
        {canEditTeam && (
          <Button variant="outlined" startIcon={<EditIcon />} onClick={handleEditOpen}>
            Edit Team
          </Button>
        )}
      </Box>

      <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
        <Tabs value={tabValue} onChange={(_, v) => setTabValue(v)}>
          <Tab label={`Members (${members.length})`} />
          <Tab label={`Clients (${clients.length})`} />
          <Tab label={`Trusted Teams (${trustedTeams.length})`} />
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
              Add Member
            </Button>
          </Box>
        )}

        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Username</TableCell>
                <TableCell>Email</TableCell>
                <TableCell>Role</TableCell>
                <TableCell>Joined</TableCell>
                {canManageMembers && <TableCell align="right">Actions</TableCell>}
              </TableRow>
            </TableHead>
            <TableBody>
              {members.map((member) => (
                <TableRow key={member.user_id}>
                  <TableCell>{member.username}</TableCell>
                  <TableCell>{member.email}</TableCell>
                  <TableCell>
                    {canManageMembers ? (
                      <Select
                        size="small"
                        value={member.role}
                        onChange={(e) => handleUpdateRole(member.user_id, e.target.value as TeamRole)}
                      >
                        <MenuItem value="member">Member</MenuItem>
                        <MenuItem value="admin">Admin</MenuItem>
                      </Select>
                    ) : (
                      <Chip
                        label={member.role === 'admin' ? 'Admin' : 'Member'}
                        color={member.role === 'admin' ? 'primary' : 'default'}
                        size="small"
                      />
                    )}
                  </TableCell>
                  <TableCell>{new Date(member.joined_at).toLocaleDateString()}</TableCell>
                  {canManageMembers && (
                    <TableCell align="right">
                      <IconButton
                        color="error"
                        onClick={() => handleRemoveMember(member.user_id)}
                        size="small"
                      >
                        <DeleteIcon />
                      </IconButton>
                    </TableCell>
                  )}
                </TableRow>
              ))}
              {members.length === 0 && (
                <TableRow>
                  <TableCell colSpan={canManageMembers ? 5 : 4} align="center">
                    No members in this team
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
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
              Assign Client
            </Button>
          </Box>
        )}

        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Client Name</TableCell>
                <TableCell>Description</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {clients.map((client) => (
                <TableRow key={client.id}>
                  <TableCell>{client.name}</TableCell>
                  <TableCell>{client.description || '-'}</TableCell>
                  <TableCell align="right">
                    <Button size="small" onClick={() => navigate(`/clients/${client.id}`)}>
                      View
                    </Button>
                    {canManageClients && (
                      <IconButton
                        color="error"
                        onClick={() => handleRemoveClient(client.id, client.name)}
                        size="small"
                        title="Remove client from team"
                      >
                        <DeleteIcon />
                      </IconButton>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {clients.length === 0 && (
                <TableRow>
                  <TableCell colSpan={3} align="center">
                    No clients assigned to this team
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      </TabPanel>

      {/* Trusted Teams Tab */}
      <TabPanel value={tabValue} index={2}>
        <Alert severity="info" sx={{ mb: 2 }}>
          Trusting another team allows their agents to run your team's jobs. This is a one-way relationship
          &mdash; trusting Team B does not give your agents access to Team B's jobs.
        </Alert>
        {canManageTrust && (
          <Box sx={{ mb: 2 }}>
            <Button
              variant="contained"
              startIcon={<SecurityIcon />}
              onClick={handleAddTrustOpen}
            >
              Add Trust
            </Button>
          </Box>
        )}

        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Trusted Team</TableCell>
                <TableCell>Trusted Since</TableCell>
                {canManageTrust && <TableCell align="right">Actions</TableCell>}
              </TableRow>
            </TableHead>
            <TableBody>
              {trustedTeams.map((trust) => (
                <TableRow key={trust.trusted_team_id}>
                  <TableCell>{trust.trusted_name || trust.trusted_team_id}</TableCell>
                  <TableCell>{new Date(trust.created_at).toLocaleDateString()}</TableCell>
                  {canManageTrust && (
                    <TableCell align="right">
                      <IconButton
                        color="error"
                        onClick={() => handleRemoveTrust(trust.trusted_team_id)}
                        size="small"
                        title="Remove trust"
                      >
                        <DeleteIcon />
                      </IconButton>
                    </TableCell>
                  )}
                </TableRow>
              ))}
              {trustedTeams.length === 0 && (
                <TableRow>
                  <TableCell colSpan={canManageTrust ? 3 : 2} align="center">
                    No trusted teams. Only system agents and this team's own agents can run jobs.
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      </TabPanel>

      {/* Edit Team Dialog */}
      <Dialog open={editDialogOpen} onClose={() => setEditDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Edit Team</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="Team Name"
            fullWidth
            required
            value={editName}
            onChange={(e) => setEditName(e.target.value)}
          />
          <TextField
            margin="dense"
            label="Description"
            fullWidth
            multiline
            rows={3}
            value={editDescription}
            onChange={(e) => setEditDescription(e.target.value)}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditDialogOpen(false)}>Cancel</Button>
          <Button onClick={handleEditSave} variant="contained" disabled={!editName.trim() || saving}>
            {saving ? 'Saving...' : 'Save'}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Add Member Dialog */}
      <Dialog open={addMemberOpen} onClose={() => setAddMemberOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Add Team Member</DialogTitle>
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
                label="Search users"
                margin="dense"
                placeholder="Type at least 2 characters..."
              />
            )}
            noOptionsText={searchQuery.length < 2 ? 'Type to search...' : 'No users found'}
          />
          <FormControl fullWidth margin="dense">
            <InputLabel>Role</InputLabel>
            <Select
              value={newMemberRole}
              label="Role"
              onChange={(e) => setNewMemberRole(e.target.value as TeamRole)}
            >
              <MenuItem value="member">Member</MenuItem>
              <MenuItem value="admin">Admin (Team Manager)</MenuItem>
            </Select>
          </FormControl>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAddMemberOpen(false)}>Cancel</Button>
          <Button onClick={handleAddMember} variant="contained" disabled={!selectedUser}>
            Add Member
          </Button>
        </DialogActions>
      </Dialog>

      {/* Add Trust Dialog */}
      <Dialog open={addTrustOpen} onClose={() => setAddTrustOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Add Trusted Team</DialogTitle>
        <DialogContent>
          <Alert severity="info" sx={{ mb: 2 }}>
            Agents owned by members of the trusted team will be allowed to run this team's jobs.
          </Alert>
          <Autocomplete
            options={allTeamNames}
            getOptionLabel={(option) => `${option.name} (${option.agent_count} agent${option.agent_count !== 1 ? 's' : ''})`}
            value={selectedTrustTeam}
            onChange={(_, value) => setSelectedTrustTeam(value)}
            renderInput={(params) => (
              <TextField
                {...params}
                label="Select team to trust"
                margin="dense"
                placeholder="Search teams..."
              />
            )}
            noOptionsText="No teams available"
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAddTrustOpen(false)}>Cancel</Button>
          <Button onClick={handleAddTrust} variant="contained" disabled={!selectedTrustTeam}>
            Add Trust
          </Button>
        </DialogActions>
      </Dialog>

      {/* Assign Client Dialog */}
      <Dialog open={assignClientOpen} onClose={() => setAssignClientOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Assign Client to Team</DialogTitle>
        <DialogContent>
          <Alert severity="info" sx={{ mb: 2 }}>
            Assigning a client grants all team members access to this client's hashlists, jobs, and cracked data.
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
                  label="Select client"
                  margin="dense"
                  placeholder="Search clients..."
                />
              )}
              noOptionsText="No unassigned clients available"
            />
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setAssignClientOpen(false)}>Cancel</Button>
          <Button onClick={handleAssignClient} variant="contained" disabled={!selectedClient}>
            Assign Client
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default TeamDetail;
