import React from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { List, ListItemButton, ListItemIcon, ListItemText } from '@mui/material';
import {
  Settings as SettingsIcon,
  PlaylistAddCheck as PlaylistAddCheckIcon,
  AccountTree as AccountTreeIcon,
  SupervisorAccount as SupervisorAccountIcon,
  BugReport as BugReportIcon
} from '@mui/icons-material';

const AdminMenu: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();

  return (
    <List>
      <ListItemButton
        onClick={() => navigate('/admin/settings')}
        selected={location.pathname.startsWith('/admin/settings')}
        sx={{
          minHeight: 48,
          px: 2.5,
        }}
      >
        <ListItemIcon
          sx={{
            minWidth: 0,
            mr: 3,
            justifyContent: 'center',
          }}
        >
          <SettingsIcon />
        </ListItemIcon>
        <ListItemText primary="Admin Settings" />
      </ListItemButton>

      <ListItemButton
        onClick={() => navigate('/admin/users')}
        selected={location.pathname.startsWith('/admin/users')}
        sx={{
          minHeight: 48,
          px: 2.5,
        }}
      >
        <ListItemIcon
          sx={{
            minWidth: 0,
            mr: 3,
            justifyContent: 'center',
          }}
        >
          <SupervisorAccountIcon />
        </ListItemIcon>
        <ListItemText primary="User Management" />
      </ListItemButton>

      <ListItemButton
        onClick={() => navigate('/admin/preset-jobs')}
        selected={location.pathname.startsWith('/admin/preset-jobs')}
        sx={{
          minHeight: 48,
          px: 2.5,
        }}
      >
        <ListItemIcon
          sx={{
            minWidth: 0,
            mr: 3,
            justifyContent: 'center',
          }}
        >
          <PlaylistAddCheckIcon />
        </ListItemIcon>
        <ListItemText primary="Preset Jobs" />
      </ListItemButton>

      <ListItemButton
        onClick={() => navigate('/admin/job-workflows')}
        selected={location.pathname.startsWith('/admin/job-workflows')}
        sx={{
          minHeight: 48,
          px: 2.5,
        }}
      >
        <ListItemIcon
          sx={{
            minWidth: 0,
            mr: 3,
            justifyContent: 'center',
          }}
        >
          <AccountTreeIcon />
        </ListItemIcon>
        <ListItemText primary="Job Workflows" />
      </ListItemButton>

      <ListItemButton
        onClick={() => navigate('/admin/diagnostics')}
        selected={location.pathname.startsWith('/admin/diagnostics')}
        sx={{
          minHeight: 48,
          px: 2.5,
        }}
      >
        <ListItemIcon
          sx={{
            minWidth: 0,
            mr: 3,
            justifyContent: 'center',
          }}
        >
          <BugReportIcon />
        </ListItemIcon>
        <ListItemText primary="Diagnostics" />
      </ListItemButton>
    </List>
  );
};

export default AdminMenu; 