import React from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { List, ListItemButton, ListItemIcon, ListItemText } from '@mui/material';
import {
    Settings as SettingsIcon,
    PlaylistAddCheck as PlaylistAddCheckIcon,
    AccountTree as AccountTreeIcon,
    SupervisorAccount as SupervisorAccountIcon,
    BugReport as BugReportIcon,
    History as HistoryIcon,
} from '@mui/icons-material';

const AdminMenu: React.FC = () => {
    const navigate = useNavigate();
    const location = useLocation();
    const { t } = useTranslation('navigation');

    return (
        <List aria-label={t('aria.adminNavigation') as string}>
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
                <ListItemText primary={t('admin.settings') as string} />
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
                <ListItemText primary={t('admin.userManagement') as string} />
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
                <ListItemText primary={t('admin.presetJobs') as string} />
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
                <ListItemText primary={t('admin.jobWorkflows') as string} />
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
                <ListItemText primary={t('admin.diagnostics') as string} />
            </ListItemButton>

            <ListItemButton
                onClick={() => navigate('/admin/audit-log')}
                selected={location.pathname.startsWith('/admin/audit-log')}
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
                    <HistoryIcon />
                </ListItemIcon>
                <ListItemText primary={t('admin.auditLog') as string} />
            </ListItemButton>
        </List>
    );
};

export default AdminMenu;
