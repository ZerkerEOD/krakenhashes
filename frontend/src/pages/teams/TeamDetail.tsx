import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Button,
  Card,
  CardContent,
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
} from '@mui/material';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import PersonAddIcon from '@mui/icons-material/PersonAdd';
import { useParams, useNavigate } from 'react-router-dom';
import { Team, TeamMember, TeamRole, UserSearchResult } from '../../types/team';
import { Client } from '../../types/client';
import { teamsService } from '../../services/teams';

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
  const [team, setTeam] = useState<Team | null>(null);
  const [members, setMembers] = useState<TeamMember[]>([]);
  const [clients, setClients] = useState<Client[]>([]);
  const [loading, setLoading] = useState(true);
  const [tabValue, setTabValue] = useState(0);
  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<UserSearchResult[]>([]);
  const [selectedUser, setSelectedUser] = useState<UserSearchResult | null>(null);
  const [newMemberRole, setNewMemberRole] = useState<TeamRole>('member');

  const isTeamAdmin = team?.user_role === 'admin';

  const loadTeamData = async () => {
    if (!teamId) return;

    try {
      setLoading(true);
      const [teamData, membersData, clientsData] = await Promise.all([
        teamsService.getTeam(teamId),
        teamsService.getTeamMembers(teamId),
        teamsService.getTeamClients(teamId),
      ]);
      setTeam(teamData);
      setMembers(membersData || []);
      setClients(clientsData || []);
    } catch (error) {
      console.error('Failed to load team data:', error);
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
    } catch (error) {
      console.error('Failed to add member:', error);
    }
  };

  const handleRemoveMember = async (userId: string) => {
    if (!teamId || !window.confirm('Are you sure you want to remove this member?')) return;

    try {
      await teamsService.removeMember(teamId, userId);
      await loadTeamData();
    } catch (error) {
      console.error('Failed to remove member:', error);
    }
  };

  const handleUpdateRole = async (userId: string, newRole: TeamRole) => {
    if (!teamId) return;

    try {
      await teamsService.updateMemberRole(teamId, userId, { role: newRole });
      await loadTeamData();
    } catch (error) {
      console.error('Failed to update role:', error);
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
          <Typography variant="h4" component="h1" gutterBottom>
            {team.name}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {team.description || 'No description'}
          </Typography>
        </Box>
        {isTeamAdmin && (
          <Button variant="outlined" startIcon={<EditIcon />}>
            Edit Team
          </Button>
        )}
      </Box>

      <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
        <Tabs value={tabValue} onChange={(_, v) => setTabValue(v)}>
          <Tab label={`Members (${members.length})`} />
          <Tab label={`Clients (${clients.length})`} />
        </Tabs>
      </Box>

      {/* Members Tab */}
      <TabPanel value={tabValue} index={0}>
        {isTeamAdmin && (
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
                {isTeamAdmin && <TableCell align="right">Actions</TableCell>}
              </TableRow>
            </TableHead>
            <TableBody>
              {members.map((member) => (
                <TableRow key={member.user_id}>
                  <TableCell>{member.username}</TableCell>
                  <TableCell>{member.email}</TableCell>
                  <TableCell>
                    {isTeamAdmin ? (
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
                  {isTeamAdmin && (
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
            </TableBody>
          </Table>
        </TableContainer>
      </TabPanel>

      {/* Clients Tab */}
      <TabPanel value={tabValue} index={1}>
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Client Name</TableCell>
                <TableCell>Description</TableCell>
                <TableCell>Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {clients.map((client) => (
                <TableRow key={client.id}>
                  <TableCell>{client.name}</TableCell>
                  <TableCell>{client.description || '-'}</TableCell>
                  <TableCell>
                    <Button size="small" onClick={() => navigate(`/clients/${client.id}`)}>
                      View
                    </Button>
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
    </Box>
  );
};

export default TeamDetail;
