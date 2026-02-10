import React, { useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Button,
  Card,
  CardContent,
  CardActions,
  Grid,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  CircularProgress,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
  IconButton,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import GroupsIcon from '@mui/icons-material/Groups';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import ManageAccountsIcon from '@mui/icons-material/ManageAccounts';
import { useNavigate } from 'react-router-dom';
import { useSnackbar } from 'notistack';
import { Team, CreateTeamRequest } from '../../types/team';
import { teamsService, adminTeamsService } from '../../services/teams';
import { useTeamFilter } from '../../contexts/TeamFilterContext';
import { useAuth } from '../../contexts/AuthContext';

export const TeamList: React.FC = () => {
  const navigate = useNavigate();
  const { refreshTeams } = useTeamFilter();
  const { enqueueSnackbar } = useSnackbar();
  const { userRole } = useAuth();
  const isSystemAdmin = userRole === 'admin';

  const [teams, setTeams] = useState<Team[]>([]);
  const [loading, setLoading] = useState(true);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newTeam, setNewTeam] = useState<CreateTeamRequest>({ name: '', description: '' });
  const [creating, setCreating] = useState(false);

  // Edit team state (admin only)
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [editTeam, setEditTeam] = useState<Team | null>(null);
  const [editName, setEditName] = useState('');
  const [editDescription, setEditDescription] = useState('');
  const [saving, setSaving] = useState(false);

  const loadTeams = async () => {
    try {
      setLoading(true);
      const data = isSystemAdmin
        ? await adminTeamsService.listAllTeams()
        : await teamsService.listUserTeams();
      setTeams(data || []);
    } catch (error) {
      console.error('Failed to load teams:', error);
      enqueueSnackbar('Failed to load teams', { variant: 'error' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTeams();
  }, []);

  const handleCreateTeam = async () => {
    try {
      setCreating(true);
      if (isSystemAdmin) {
        await adminTeamsService.createTeam(newTeam);
      } else {
        await teamsService.createTeam(newTeam);
      }
      setCreateDialogOpen(false);
      setNewTeam({ name: '', description: '' });
      await loadTeams();
      await refreshTeams();
      enqueueSnackbar('Team created successfully', { variant: 'success' });
    } catch (error) {
      console.error('Failed to create team:', error);
      enqueueSnackbar('Failed to create team', { variant: 'error' });
    } finally {
      setCreating(false);
    }
  };

  const handleDeleteTeam = async (teamId: string, teamName: string) => {
    if (!window.confirm(`Are you sure you want to delete team "${teamName}"? This will remove all team memberships and client assignments.`)) {
      return;
    }

    try {
      await adminTeamsService.deleteTeam(teamId);
      await loadTeams();
      await refreshTeams();
      enqueueSnackbar('Team deleted successfully', { variant: 'success' });
    } catch (error) {
      console.error('Failed to delete team:', error);
      enqueueSnackbar('Failed to delete team', { variant: 'error' });
    }
  };

  const handleEditOpen = (team: Team) => {
    setEditTeam(team);
    setEditName(team.name);
    setEditDescription(team.description || '');
    setEditDialogOpen(true);
  };

  const handleEditSave = async () => {
    if (!editTeam || !editName.trim()) return;

    try {
      setSaving(true);
      await adminTeamsService.updateTeam(editTeam.id, { name: editName.trim(), description: editDescription.trim() });
      setEditDialogOpen(false);
      setEditTeam(null);
      await loadTeams();
      enqueueSnackbar('Team updated successfully', { variant: 'success' });
    } catch (error) {
      console.error('Failed to update team:', error);
      enqueueSnackbar('Failed to update team', { variant: 'error' });
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            {isSystemAdmin ? 'Team Management' : 'My Teams'}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {isSystemAdmin ? 'Manage all teams in the system' : 'Teams you are a member of'}
          </Typography>
        </Box>
        <Button
          variant="contained"
          startIcon={<AddIcon />}
          onClick={() => setCreateDialogOpen(true)}
        >
          Create Team
        </Button>
      </Box>

      {/* Admin view: table layout */}
      {isSystemAdmin ? (
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Team Name</TableCell>
                <TableCell>Description</TableCell>
                <TableCell>Members</TableCell>
                <TableCell>Clients</TableCell>
                <TableCell>Hashlists</TableCell>
                <TableCell>Agents</TableCell>
                <TableCell>Created</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {teams.map((team) => (
                <TableRow key={team.id}>
                  <TableCell>{team.name}</TableCell>
                  <TableCell>{team.description || '-'}</TableCell>
                  <TableCell>{team.member_count || 0}</TableCell>
                  <TableCell>{team.client_count || 0}</TableCell>
                  <TableCell>{team.hashlist_count || 0}</TableCell>
                  <TableCell>{team.agent_count || 0}</TableCell>
                  <TableCell>{new Date(team.created_at).toLocaleDateString()}</TableCell>
                  <TableCell align="right">
                    <IconButton
                      color="primary"
                      onClick={() => navigate(`/teams/${team.id}`)}
                      size="small"
                      title="Manage team"
                    >
                      <ManageAccountsIcon />
                    </IconButton>
                    <IconButton
                      color="default"
                      onClick={() => handleEditOpen(team)}
                      size="small"
                      title="Edit team"
                    >
                      <EditIcon />
                    </IconButton>
                    <IconButton
                      color="error"
                      onClick={() => handleDeleteTeam(team.id, team.name)}
                      size="small"
                      title="Delete team"
                    >
                      <DeleteIcon />
                    </IconButton>
                  </TableCell>
                </TableRow>
              ))}
              {teams.length === 0 && (
                <TableRow>
                  <TableCell colSpan={8} align="center">
                    No teams found
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </TableContainer>
      ) : (
        /* Regular user view: card layout */
        <Grid container spacing={3}>
          {teams.map((team) => (
            <Grid item xs={12} sm={6} md={4} key={team.id}>
              <Card>
                <CardContent>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                    <GroupsIcon color="primary" />
                    <Typography variant="h6">{team.name}</Typography>
                    {team.user_role === 'admin' && (
                      <Chip label="Admin" size="small" color="primary" />
                    )}
                  </Box>
                  <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                    {team.description || 'No description'}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {team.member_count || 0} member{(team.member_count || 0) !== 1 ? 's' : ''}
                    {' \u00B7 '}
                    {team.client_count || 0} client{(team.client_count || 0) !== 1 ? 's' : ''}
                    {' \u00B7 '}
                    {team.hashlist_count || 0} hashlist{(team.hashlist_count || 0) !== 1 ? 's' : ''}
                    {' \u00B7 '}
                    {team.agent_count || 0} agent{(team.agent_count || 0) !== 1 ? 's' : ''}
                  </Typography>
                </CardContent>
                <CardActions>
                  <Button size="small" onClick={() => navigate(`/teams/${team.id}`)}>
                    View Details
                  </Button>
                </CardActions>
              </Card>
            </Grid>
          ))}

          {teams.length === 0 && (
            <Grid item xs={12}>
              <Box sx={{ textAlign: 'center', py: 4 }}>
                <Typography color="text.secondary">
                  You are not a member of any teams yet.
                </Typography>
              </Box>
            </Grid>
          )}
        </Grid>
      )}

      {/* Create Team Dialog */}
      <Dialog open={createDialogOpen} onClose={() => setCreateDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Create New Team</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="Team Name"
            fullWidth
            required
            value={newTeam.name}
            onChange={(e) => setNewTeam({ ...newTeam, name: e.target.value })}
          />
          <TextField
            margin="dense"
            label="Description"
            fullWidth
            multiline
            rows={3}
            value={newTeam.description}
            onChange={(e) => setNewTeam({ ...newTeam, description: e.target.value })}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreateDialogOpen(false)}>Cancel</Button>
          <Button
            onClick={handleCreateTeam}
            variant="contained"
            disabled={!newTeam.name || creating}
          >
            {creating ? 'Creating...' : 'Create'}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Edit Team Dialog (admin only) */}
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
    </Box>
  );
};

export default TeamList;
