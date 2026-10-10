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
  Alert,
  Link,
} from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import AddIcon from '@mui/icons-material/Add';
import GroupsIcon from '@mui/icons-material/Groups';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import ManageAccountsIcon from '@mui/icons-material/ManageAccounts';
import { useNavigate, Link as RouterLink } from 'react-router-dom';
import { useTranslation, Trans } from 'react-i18next';
import { DataTable, EntityLink, PageHeader, useToast, useConfirm } from '../../components/ui';
import { Team, CreateTeamRequest } from '../../types/team';
import { teamsService, adminTeamsService } from '../../services/teams';
import { useTeamFilter } from '../../contexts/TeamFilterContext';
import { useAuth } from '../../contexts/AuthContext';

export const TeamList: React.FC = () => {
  const navigate = useNavigate();
  const { t } = useTranslation('admin');
  const tr = (k: string, o?: any) => t(k, o) as string;
  const { refreshTeams } = useTeamFilter();
  const toast = useToast();
  const confirm = useConfirm();
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
      toast.error(tr('teams.list.errors.loadFailed'));
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
      toast.success(tr('teams.list.messages.createSuccess'));
    } catch (error) {
      console.error('Failed to create team:', error);
      toast.error(tr('teams.list.errors.createFailed'));
    } finally {
      setCreating(false);
    }
  };

  const handleDeleteTeam = async (teamId: string, teamName: string) => {
    const ok = await confirm({
      title: tr('teams.list.dialogs.delete.title'),
      message: tr('teams.list.dialogs.delete.confirmation', { name: teamName }),
      severity: 'danger',
      confirmLabel: tr('common.delete'),
    });
    if (!ok) return;

    try {
      await adminTeamsService.deleteTeam(teamId);
      await loadTeams();
      await refreshTeams();
      toast.success(tr('teams.list.messages.deleteSuccess'));
    } catch (error) {
      console.error('Failed to delete team:', error);
      toast.error(tr('teams.list.errors.deleteFailed'));
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
      toast.success(tr('teams.list.messages.updateSuccess'));
    } catch (error) {
      console.error('Failed to update team:', error);
      toast.error(tr('teams.list.errors.updateFailed'));
    } finally {
      setSaving(false);
    }
  };

  const columns: GridColDef<Team>[] = [
    {
      field: 'name',
      headerName: tr('teams.list.columns.name'),
      flex: 1,
      minWidth: 160,
      renderCell: (p) => <EntityLink type="team" id={p.row.id} label={p.row.name} />,
    },
    {
      field: 'description',
      headerName: tr('teams.list.columns.description'),
      flex: 1.5,
      minWidth: 180,
      valueFormatter: (v) => (v as string) || '-',
    },
    { field: 'member_count', headerName: tr('teams.list.columns.members'), width: 100, type: 'number', valueGetter: (_v, row) => row.member_count || 0 },
    { field: 'client_count', headerName: tr('teams.list.columns.clients'), width: 100, type: 'number', valueGetter: (_v, row) => row.client_count || 0 },
    { field: 'hashlist_count', headerName: tr('teams.list.columns.hashlists'), width: 100, type: 'number', valueGetter: (_v, row) => row.hashlist_count || 0 },
    { field: 'agent_count', headerName: tr('teams.list.columns.agents'), width: 100, type: 'number', valueGetter: (_v, row) => row.agent_count || 0 },
    {
      field: 'created_at',
      headerName: tr('teams.list.columns.created'),
      width: 130,
      valueFormatter: (v) => (v ? new Date(v as string).toLocaleDateString() : ''),
    },
  ];

  return (
    <Box sx={{ p: 3 }}>
      <PageHeader
        title={isSystemAdmin ? tr('teams.list.title') : tr('teams.list.titleMine')}
        description={isSystemAdmin ? tr('teams.list.description') : tr('teams.list.descriptionMine')}
        actions={
          <Button variant="contained" startIcon={<AddIcon />} onClick={() => setCreateDialogOpen(true)}>
            {tr('teams.list.createTeam')}
          </Button>
        }
      />

      {isSystemAdmin && (
        <Alert severity="info" sx={{ mb: 2 }}>
          <Trans
            t={t}
            i18nKey="teams.list.bulkAssignHint"
            components={{ link: <Link component={RouterLink} to="/clients" /> }}
          />
        </Alert>
      )}

      {/* Admin view: table layout */}
      {isSystemAdmin ? (
        <DataTable<Team>
          rows={teams}
          columns={columns}
          getRowId={(r) => r.id}
          loading={loading}
          onRetry={loadTeams}
          pagination={{ mode: 'client', initialPageSize: 25 }}
          sorting={{ mode: 'client', initial: [{ field: 'name', sort: 'asc' }] }}
          rowLinkTo={(r) => `/teams/${r.id}`}
          rowActions={() => [
            {
              key: 'manage',
              label: tr('teams.list.rowActions.manage'),
              icon: <ManageAccountsIcon fontSize="small" />,
              onClick: (r) => navigate(`/teams/${r.id}`),
            },
            {
              key: 'edit',
              label: tr('teams.list.rowActions.edit'),
              icon: <EditIcon fontSize="small" />,
              onClick: (r) => handleEditOpen(r),
            },
            {
              key: 'delete',
              label: tr('teams.list.rowActions.delete'),
              icon: <DeleteIcon fontSize="small" />,
              danger: true,
              onClick: (r) => handleDeleteTeam(r.id, r.name),
            },
          ]}
          rowActionsInlineLimit={3}
          emptyState={{ title: tr('teams.list.empty') }}
          tableKey="admin-teams"
        />
      ) : (
        /* Regular user view: card layout */
        loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
            <CircularProgress />
          </Box>
        ) : (
        <Grid container spacing={3}>
          {teams.map((team) => (
            <Grid item xs={12} sm={6} md={4} key={team.id}>
              <Card>
                <CardContent>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                    <GroupsIcon color="primary" />
                    <Typography variant="h6">{team.name}</Typography>
                    {team.user_role === 'admin' && (
                      <Chip label={tr('teams.detail.roles.admin')} size="small" color="primary" />
                    )}
                  </Box>
                  <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                    {team.description || tr('teams.detail.noDescription')}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {tr('teams.list.memberCount', { count: team.member_count || 0 })}
                    {' \u00B7 '}
                    {tr('teams.list.clientCount', { count: team.client_count || 0 })}
                    {' \u00B7 '}
                    {tr('teams.list.hashlistCount', { count: team.hashlist_count || 0 })}
                    {' \u00B7 '}
                    {tr('teams.list.agentCount', { count: team.agent_count || 0 })}
                  </Typography>
                </CardContent>
                <CardActions>
                  <Button size="small" onClick={() => navigate(`/teams/${team.id}`)}>
                    {tr('teams.list.viewDetails')}
                  </Button>
                </CardActions>
              </Card>
            </Grid>
          ))}

          {teams.length === 0 && (
            <Grid item xs={12}>
              <Box sx={{ textAlign: 'center', py: 4 }}>
                <Typography color="text.secondary">
                  {tr('teams.list.emptyMine')}
                </Typography>
              </Box>
            </Grid>
          )}
        </Grid>
        )
      )}

      {/* Create Team Dialog */}
      <Dialog open={createDialogOpen} onClose={() => setCreateDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{tr('teams.list.dialogs.create.title')}</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label={tr('teams.list.dialogs.create.nameLabel')}
            fullWidth
            required
            value={newTeam.name}
            onChange={(e) => setNewTeam({ ...newTeam, name: e.target.value })}
          />
          <TextField
            margin="dense"
            label={tr('teams.list.dialogs.create.descriptionLabel')}
            fullWidth
            multiline
            rows={3}
            value={newTeam.description}
            onChange={(e) => setNewTeam({ ...newTeam, description: e.target.value })}
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreateDialogOpen(false)}>{tr('common.cancel')}</Button>
          <Button
            onClick={handleCreateTeam}
            variant="contained"
            disabled={!newTeam.name || creating}
          >
            {creating ? tr('teams.list.dialogs.create.creatingButton') : tr('common.create')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Edit Team Dialog (admin only) */}
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
          <Button onClick={() => setEditDialogOpen(false)}>{tr('common.cancel')}</Button>
          <Button onClick={handleEditSave} variant="contained" disabled={!editName.trim() || saving}>
            {saving ? tr('teams.detail.dialogs.editTeam.savingButton') : tr('teams.detail.dialogs.editTeam.saveButton')}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
};

export default TeamList;
