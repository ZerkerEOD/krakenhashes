import React, { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
    Box, Typography, TextField, Button, CircularProgress,
    Alert, Grid, Card, CardContent, Divider, Chip, IconButton,
    Dialog, DialogTitle, DialogContent, DialogActions, FormControlLabel,
    Checkbox, List, ListItem, ListItemText, ListItemIcon, Select,
    MenuItem, FormControl, InputLabel, Badge, Tooltip
} from '@mui/material';
import type { GridColDef } from '@mui/x-data-grid';
import { useQuery } from '@tanstack/react-query';
import GroupsIcon from '@mui/icons-material/Groups';
import { DataTable, EntityLink, SimpleTable, StatusChip } from '../../components/ui';
import { api } from '../../services/api';
import type { Agent } from '../../types/agent';
import { formatAgentVersion } from '../../utils/agentVersion';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import SaveIcon from '@mui/icons-material/Save';
import LockResetIcon from '@mui/icons-material/LockReset';
import SecurityIcon from '@mui/icons-material/Security';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import CancelIcon from '@mui/icons-material/Cancel';
import PersonIcon from '@mui/icons-material/Person';
import EmailIcon from '@mui/icons-material/Email';
import CalendarTodayIcon from '@mui/icons-material/CalendarToday';
import CloseIcon from '@mui/icons-material/Close';
import DevicesIcon from '@mui/icons-material/Devices';
import HistoryIcon from '@mui/icons-material/History';
import DeleteIcon from '@mui/icons-material/Delete';
import DeleteSweepIcon from '@mui/icons-material/DeleteSweep';
import VpnKeyIcon from '@mui/icons-material/VpnKey';
import { useSnackbar, closeSnackbar } from 'notistack';
import { format, formatDistanceToNow } from 'date-fns';
import i18n from '../../i18n';
import { dateFnsLocaleFor } from '../../i18n/locales';

/** date-fns locale for the current UI language. */
const dateLocale = () => dateFnsLocaleFor(i18n.language);
import { useTranslation } from 'react-i18next';

import { User, LoginAttempt, ActiveSession } from '../../types/user';
import {
    getAdminUser,
    updateAdminUser,
    resetAdminUserPassword,
    disableAdminUserMFA,
    enableAdminUser,
    disableAdminUser,
    unlockAdminUser,
    getUserLoginAttempts,
    getUserSessions,
    terminateSession,
    terminateAllUserSessions,
    getAdminUserApiKeyInfo,
    revokeAdminUserApiKey
} from '../../services/api';
import { ApiKeyInfo, Team as UserTeam } from '../../types/user';

const UserDetail: React.FC = () => {
    const { t } = useTranslation('admin');
    const { id } = useParams<{ id: string }>();
    const navigate = useNavigate();
    const { enqueueSnackbar } = useSnackbar();

    const [user, setUser] = useState<User | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    // Form state
    const [username, setUsername] = useState('');
    const [email, setEmail] = useState('');
    const [role, setRole] = useState('');
    const [hasChanges, setHasChanges] = useState(false);

    // Dialog states
    const [resetPasswordOpen, setResetPasswordOpen] = useState(false);
    const [disableMFAOpen, setDisableMFAOpen] = useState(false);
    const [disableAccountOpen, setDisableAccountOpen] = useState(false);

    // Password reset state
    const [newPassword, setNewPassword] = useState('');
    const [temporaryPassword, setTemporaryPassword] = useState(true);

    // Sessions and login attempts state
    const [sessions, setSessions] = useState<ActiveSession[]>([]);
    const [loginAttempts, setLoginAttempts] = useState<LoginAttempt[]>([]);
    const [sessionsLoading, setSessionsLoading] = useState(false);
    const [attemptsLoading, setAttemptsLoading] = useState(false);
    const [terminateSessionId, setTerminateSessionId] = useState<string | null>(null);
    const [terminateAllDialogOpen, setTerminateAllDialogOpen] = useState(false);
    const [attemptFilter, setAttemptFilter] = useState<'all' | 'success' | 'failed'>('all');

    // API Key state
    const [apiKeyInfo, setApiKeyInfo] = useState<ApiKeyInfo | null>(null);
    const [apiKeyLoading, setApiKeyLoading] = useState(false);
    const [showRevokeApiKeyDialog, setShowRevokeApiKeyDialog] = useState(false);

    const fetchUser = useCallback(async () => {
        if (!id) return;
        
        setLoading(true);
        setError(null);
        try {
            const response = await getAdminUser(id);
            setUser(response.data.data);
            setUsername(response.data.data.username);
            setEmail(response.data.data.email);
            setRole(response.data.data.role);
        } catch (err) {
            console.error("Failed to fetch user:", err);
            setError(t('users.errors.loadDetailsFailed') as string);
            enqueueSnackbar(t('users.errors.loadDetailsFailed') as string, { variant: 'error' });
        } finally {
            setLoading(false);
        }
    }, [id, enqueueSnackbar]);

    const fetchSessions = useCallback(async () => {
        if (!id) return;

        setSessionsLoading(true);
        try {
            const response = await getUserSessions(id);
            setSessions(response.data.data || []);
        } catch (err) {
            console.error("Failed to fetch sessions:", err);
            enqueueSnackbar(t('users.errors.loadSessionsFailed') as string, { variant: 'error' });
        } finally {
            setSessionsLoading(false);
        }
    }, [id, enqueueSnackbar]);

    const fetchLoginAttempts = useCallback(async () => {
        if (!id) return;

        setAttemptsLoading(true);
        try {
            const response = await getUserLoginAttempts(id, 50);
            setLoginAttempts(response.data.data || []);
        } catch (err) {
            console.error("Failed to fetch login attempts:", err);
            enqueueSnackbar(t('users.errors.loadLoginAttemptsFailed') as string, { variant: 'error' });
        } finally {
            setAttemptsLoading(false);
        }
    }, [id, enqueueSnackbar]);

    const fetchApiKeyInfo = useCallback(async () => {
        if (!id) return;

        setApiKeyLoading(true);
        try {
            const response = await getAdminUserApiKeyInfo(id);
            setApiKeyInfo(response.data.data);
        } catch (err) {
            console.error("Failed to fetch API key info:", err);
            enqueueSnackbar(t('users.errors.loadApiKeyFailed') as string, { variant: 'error' });
        } finally {
            setApiKeyLoading(false);
        }
    }, [id, enqueueSnackbar]);

    useEffect(() => {
        fetchUser();
        fetchSessions();
        fetchLoginAttempts();
        fetchApiKeyInfo();
    }, [fetchUser, fetchSessions, fetchLoginAttempts, fetchApiKeyInfo]);

    useEffect(() => {
        if (user) {
            setHasChanges(
                username !== user.username ||
                email !== user.email ||
                role !== user.role
            );
        }
    }, [username, email, role, user]);

    const handleSave = async () => {
        if (!user || !hasChanges) return;

        setSaving(true);
        try {
            const updateData: any = { username, email };
            // Only include role if it's different and not trying to set to system
            if (role !== user.role && role !== 'system') {
                updateData.role = role;
            }
            await updateAdminUser(user.id, updateData);
            enqueueSnackbar(t('users.messages.updateSuccess') as string, { variant: 'success' });
            fetchUser(); // Refresh data
        } catch (err: any) {
            console.error('Failed to update user:', err);
            const message = err.response?.data?.error || t('users.errors.updateFailed') as string;
            enqueueSnackbar(message, { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleResetPassword = async () => {
        if (!user) return;

        setSaving(true);
        try {
            const response = await resetAdminUserPassword(user.id, {
                password: newPassword || undefined,
                temporary: temporaryPassword
            });

            if (response.data.data.temporary_password) {
                // Show temporary password in a dialog
                const tempPassword = response.data.data.temporary_password;

                // Create a custom notification with copy functionality
                const message = (
                    <Box>
                        <Typography variant="body2" gutterBottom>
                            {t('users.messages.passwordResetSuccess')}
                        </Typography>
                        <Typography variant="body2" sx={{ fontWeight: 'bold', my: 1 }}>
                            {t('users.detail.temporaryPassword')}: {tempPassword}
                        </Typography>
                        <Button
                            size="small"
                            variant="outlined"
                            onClick={() => {
                                navigator.clipboard.writeText(tempPassword);
                                enqueueSnackbar(t('users.messages.passwordCopied') as string, { variant: 'info' });
                            }}
                            sx={{ mt: 1 }}
                        >
                            {t('common.copyToClipboard')}
                        </Button>
                        <Typography variant="caption" display="block" sx={{ mt: 2 }}>
                            {t('users.detail.sharePasswordSecurely')}
                        </Typography>
                    </Box>
                );

                enqueueSnackbar(message, {
                    variant: 'success',
                    persist: true,
                    action: (key) => (
                        <IconButton
                            size="small"
                            color="inherit"
                            onClick={() => closeSnackbar(key)}
                        >
                            <CloseIcon />
                        </IconButton>
                    )
                });
            } else {
                enqueueSnackbar(t('users.messages.passwordResetSuccess') as string, { variant: 'success' });
            }

            setResetPasswordOpen(false);
            setNewPassword('');
            setTemporaryPassword(true);
        } catch (err) {
            console.error('Failed to reset password:', err);
            enqueueSnackbar(t('users.errors.passwordResetFailed') as string, { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleDisableMFA = async () => {
        if (!user) return;

        setSaving(true);
        try {
            await disableAdminUserMFA(user.id);
            enqueueSnackbar(t('users.messages.mfaDisableSuccess') as string, { variant: 'success' });
            setDisableMFAOpen(false);
            fetchUser(); // Refresh data
        } catch (err) {
            console.error('Failed to disable MFA:', err);
            enqueueSnackbar(t('users.errors.mfaDisableFailed') as string, { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleToggleAccount = async (reason?: string) => {
        if (!user) return;

        setSaving(true);
        try {
            if (user.accountEnabled) {
                await disableAdminUser(user.id, { reason: reason! });
                enqueueSnackbar(t('users.messages.accountDisabled') as string, { variant: 'success' });
            } else {
                await enableAdminUser(user.id);
                enqueueSnackbar(t('users.messages.accountEnabled') as string, { variant: 'success' });
            }
            setDisableAccountOpen(false);
            fetchUser(); // Refresh data
        } catch (err) {
            console.error('Failed to toggle account:', err);
            enqueueSnackbar(t('users.errors.accountStatusFailed') as string, { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleUnlockAccount = async () => {
        if (!user) return;

        setSaving(true);
        try {
            await unlockAdminUser(user.id);
            enqueueSnackbar(t('users.messages.accountUnlocked') as string, { variant: 'success' });
            fetchUser(); // Refresh data
        } catch (err) {
            console.error('Failed to unlock account:', err);
            enqueueSnackbar(t('users.errors.unlockFailed') as string, { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleTerminateSession = async (sessionId: string) => {
        if (!user) return;

        setSaving(true);
        try {
            await terminateSession(user.id, sessionId);
            enqueueSnackbar(t('users.messages.sessionTerminated') as string, { variant: 'success' });
            setTerminateSessionId(null);
            fetchSessions(); // Refresh sessions
        } catch (err) {
            console.error('Failed to terminate session:', err);
            enqueueSnackbar(t('users.errors.terminateSessionFailed') as string, { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleTerminateAllSessions = async () => {
        if (!user) return;

        setSaving(true);
        try {
            const response = await terminateAllUserSessions(user.id);
            enqueueSnackbar(t('users.messages.allSessionsTerminated', { count: response.data.data.count }) as string, { variant: 'success' });
            setTerminateAllDialogOpen(false);
            fetchSessions(); // Refresh sessions
        } catch (err) {
            console.error('Failed to terminate all sessions:', err);
            enqueueSnackbar(t('users.errors.terminateAllSessionsFailed') as string, { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleRevokeApiKey = async () => {
        if (!user) return;

        setSaving(true);
        try {
            await revokeAdminUserApiKey(user.id);
            enqueueSnackbar(t('users.messages.apiKeyRevoked') as string, { variant: 'success' });
            setShowRevokeApiKeyDialog(false);
            fetchApiKeyInfo(); // Refresh API key info
        } catch (err) {
            console.error('Failed to revoke API key:', err);
            enqueueSnackbar(t('users.errors.revokeApiKeyFailed') as string, { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    // Agents owned by this user (admin sees every agent; filter client-side).
    const agentsQuery = useQuery({
        queryKey: ['agents'],
        queryFn: async () => (await api.get<Agent[]>('/api/agents')).data || [],
        enabled: !!id,
    });
    const ownedAgents = (agentsQuery.data || []).filter((a) => a.ownerId === id);

    const formatDate = (dateString?: string) => {
        if (!dateString) return t('common.never') as string;
        try {
            return format(new Date(dateString), 'MMM dd, yyyy HH:mm:ss', { locale: dateLocale() });
        } catch {
            return t('common.invalidDate') as string;
        }
    };

    const formatRelativeTime = (dateString?: string) => {
        if (!dateString) return t('common.never') as string;
        try {
            return formatDistanceToNow(new Date(dateString), { locale: dateLocale(), addSuffix: true });
        } catch {
            return t('common.invalidDate') as string;
        }
    };

    const filteredAttempts = loginAttempts.filter(attempt => {
        if (attemptFilter === 'all') return true;
        if (attemptFilter === 'success') return attempt.success;
        if (attemptFilter === 'failed') return !attempt.success;
        return true;
    });

    if (loading) {
        return (
            <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
                <CircularProgress />
            </Box>
        );
    }

    const sessionColumns: GridColDef<ActiveSession>[] = [
        { field: 'ipAddress', headerName: t('users.detail.sessions.ipAddress') as string, width: 150 },
        {
            field: 'userAgent',
            headerName: t('users.detail.sessions.deviceBrowser') as string,
            flex: 1,
            minWidth: 200,
            renderCell: (p) => (
                <Tooltip title={p.row.userAgent}>
                    <Typography variant="body2" noWrap>
                        {p.row.userAgent}
                    </Typography>
                </Tooltip>
            ),
        },
        {
            field: 'lastActiveAt',
            headerName: t('users.detail.sessions.lastActive') as string,
            width: 160,
            renderCell: (p) => formatRelativeTime(p.row.lastActiveAt),
        },
        {
            field: 'createdAt',
            headerName: t('users.detail.created') as string,
            width: 190,
            renderCell: (p) => formatDate(p.row.createdAt),
        },
    ];

    const attemptColumns: GridColDef<LoginAttempt>[] = [
        {
            field: 'attempted_at',
            headerName: t('users.detail.loginAttempts.timestamp') as string,
            width: 190,
            renderCell: (p) => formatDate(p.row.attempted_at),
        },
        {
            field: 'provider_type',
            headerName: t('users.columns.provider') as string,
            width: 130,
            renderCell: (p) => (
                <Chip size="small" label={p.row.provider_type || t('users.columns.providerLocal')} variant="outlined" />
            ),
        },
        { field: 'ip_address', headerName: t('users.detail.sessions.ipAddress') as string, width: 150 },
        {
            field: 'success',
            headerName: t('users.columns.status') as string,
            width: 130,
            renderCell: (p) => (
                <Chip
                    size="small"
                    icon={p.row.success ? <CheckCircleIcon /> : <CancelIcon />}
                    label={p.row.success ? t('users.detail.loginAttempts.success') : t('users.detail.loginAttempts.failed')}
                    color={p.row.success ? 'success' : 'error'}
                />
            ),
        },
        {
            field: 'failure_reason',
            headerName: t('users.detail.loginAttempts.failureReason') as string,
            flex: 1,
            minWidth: 160,
            renderCell: (p) =>
                p.row.failure_reason ? (
                    <Typography variant="body2" color="error" sx={{ fontWeight: 'bold' }}>
                        {p.row.failure_reason.replace(/_/g, ' ')}
                    </Typography>
                ) : (
                    '-'
                ),
        },
    ];

    const agentColumns: GridColDef<Agent>[] = [
        {
            field: 'name',
            headerName: t('users.detail.agentName', 'Agent') as string,
            flex: 1,
            minWidth: 150,
            renderCell: (p) => <EntityLink type="agent" id={p.row.id} label={p.row.name || `#${p.row.id}`} />,
        },
        {
            field: 'status',
            headerName: t('users.columns.status') as string,
            width: 120,
            renderCell: (p) => <StatusChip entity="agent" status={p.row.status} />,
        },
        {
            field: 'version',
            headerName: t('users.detail.agentVersion', 'Version') as string,
            width: 130,
            valueFormatter: (v) => (v ? formatAgentVersion(v as string) : '-'),
        },
        {
            field: 'lastHeartbeat',
            headerName: t('users.detail.agentLastSeen', 'Last Seen') as string,
            width: 150,
            renderCell: (p) => formatRelativeTime(p.row.lastHeartbeat),
        },
    ];

    if (error || !user) {
        return (
            <Box sx={{ p: 3 }}>
                <Alert severity="error">{error || t('users.errors.userNotFound')}</Alert>
                <Button startIcon={<ArrowBackIcon />} onClick={() => navigate('/admin/users')} sx={{ mt: 2 }}>
                    {t('users.detail.backToUsers')}
                </Button>
            </Box>
        );
    }

    return (
        <Box sx={{ p: 3 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', mb: 3 }}>
                <IconButton onClick={() => navigate('/admin/users')} sx={{ mr: 2 }}>
                    <ArrowBackIcon />
                </IconButton>
                <Typography variant="h4">{t('users.detail.title')}</Typography>
            </Box>

            <Grid container spacing={3}>
                {/* User Information Card */}
                <Grid item xs={12} md={8}>
                    <Card>
                        <CardContent>
                            <Typography variant="h6" gutterBottom>{t('users.detail.userInformation')}</Typography>
                            <Divider sx={{ mb: 2 }} />

                            <Grid container spacing={2}>
                                <Grid item xs={12} sm={6}>
                                    <TextField
                                        fullWidth
                                        label={t('users.columns.username')}
                                        value={username}
                                        onChange={(e) => setUsername(e.target.value)}
                                        disabled={user.role === 'system'}
                                        InputProps={{
                                            startAdornment: <PersonIcon sx={{ mr: 1, color: 'action.active' }} />
                                        }}
                                    />
                                </Grid>
                                <Grid item xs={12} sm={6}>
                                    <TextField
                                        fullWidth
                                        label={t('users.columns.email')}
                                        value={email}
                                        onChange={(e) => setEmail(e.target.value)}
                                        disabled={user.role === 'system'}
                                        InputProps={{
                                            startAdornment: <EmailIcon sx={{ mr: 1, color: 'action.active' }} />
                                        }}
                                    />
                                </Grid>
                                <Grid item xs={12} sm={6}>
                                    {user.role === 'system' ? (
                                        <TextField
                                            fullWidth
                                            label={t('users.columns.role')}
                                            value={user.role}
                                            disabled
                                            InputProps={{
                                                endAdornment: (
                                                    <Chip
                                                        label={user.role}
                                                        size="small"
                                                        color="success"
                                                    />
                                                )
                                            }}
                                        />
                                    ) : (
                                        <FormControl fullWidth>
                                            <InputLabel>{t('users.columns.role')}</InputLabel>
                                            <Select
                                                value={role}
                                                label={t('users.columns.role')}
                                                onChange={(e) => setRole(e.target.value)}
                                            >
                                                <MenuItem value="user">{t('users.roles.user')}</MenuItem>
                                                <MenuItem value="admin">{t('users.roles.admin')}</MenuItem>
                                            </Select>
                                        </FormControl>
                                    )}
                                </Grid>
                                <Grid item xs={12} sm={6}>
                                    <TextField
                                        fullWidth
                                        label={t('users.detail.userId')}
                                        value={user.id}
                                        disabled
                                    />
                                </Grid>
                            </Grid>

                            <Box sx={{ mt: 3, display: 'flex', gap: 2 }}>
                                <Button
                                    variant="contained"
                                    startIcon={<SaveIcon />}
                                    onClick={handleSave}
                                    disabled={!hasChanges || saving || user.role === 'system'}
                                >
                                    {t('common.saveChanges')}
                                </Button>
                                {user.role === 'system' && (
                                    <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center' }}>
                                        {t('users.detail.systemUserReadOnly')}
                                    </Typography>
                                )}
                            </Box>
                        </CardContent>
                    </Card>

                    {/* Account Status Card */}
                    <Card sx={{ mt: 3 }}>
                        <CardContent>
                            <Typography variant="h6" gutterBottom>{t('users.detail.accountStatus')}</Typography>
                            <Divider sx={{ mb: 2 }} />

                            <List>
                                <ListItem>
                                    <ListItemIcon>
                                        {user.accountEnabled ? <CheckCircleIcon color="success" /> : <CancelIcon color="error" />}
                                    </ListItemIcon>
                                    <ListItemText
                                        primary={t('users.detail.accountStatusLabel')}
                                        secondary={user.accountEnabled ? t('users.detail.enabled') : `${t('users.detail.disabled')} - ${user.disabledReason || t('users.detail.noReasonProvided')}`}
                                    />
                                    <Button
                                        variant="outlined"
                                        color={user.accountEnabled ? 'error' : 'success'}
                                        onClick={() => user.accountEnabled ? setDisableAccountOpen(true) : handleToggleAccount()}
                                        disabled={user.role === 'system'}
                                    >
                                        {user.accountEnabled ? t('users.actions.disable') : t('users.actions.enable')}
                                    </Button>
                                </ListItem>

                                <ListItem>
                                    <ListItemIcon>
                                        {user.accountLocked ? <CancelIcon color="warning" /> : <CheckCircleIcon color="success" />}
                                    </ListItemIcon>
                                    <ListItemText
                                        primary={t('users.detail.lockStatus')}
                                        secondary={user.accountLocked ?
                                            t('users.detail.lockedUntil', { date: formatDate(user.accountLockedUntil) }) :
                                            t('users.detail.notLocked')}
                                    />
                                    {user.accountLocked && (
                                        <Button
                                            variant="outlined"
                                            color="warning"
                                            onClick={handleUnlockAccount}
                                        >
                                            {t('users.actions.unlock')}
                                        </Button>
                                    )}
                                </ListItem>

                                <ListItem>
                                    <ListItemIcon>
                                        <SecurityIcon color={user.mfaEnabled ? 'success' : 'disabled'} />
                                    </ListItemIcon>
                                    <ListItemText
                                        primary={t('users.detail.mfaStatus')}
                                        secondary={user.mfaEnabled ?
                                            t('users.detail.mfaEnabled', { types: user.mfaType.join(', ') }) :
                                            t('users.detail.mfaDisabled')}
                                    />
                                    {user.mfaEnabled && (
                                        <Button
                                            variant="outlined"
                                            color="warning"
                                            onClick={() => setDisableMFAOpen(true)}
                                        >
                                            {t('users.actions.disableMfa')}
                                        </Button>
                                    )}
                                </ListItem>
                            </List>

                            <Box sx={{ mt: 3 }}>
                                <Button
                                    variant="outlined"
                                    startIcon={<LockResetIcon />}
                                    onClick={() => setResetPasswordOpen(true)}
                                    disabled={user.role === 'system'}
                                >
                                    {t('users.actions.resetPassword')}
                                </Button>
                            </Box>
                        </CardContent>
                    </Card>

                    {/* API Key Management Card */}
                    <Card sx={{ mt: 3 }}>
                        <CardContent>
                            <Typography variant="h6" gutterBottom>{t('users.detail.apiKeyManagement')}</Typography>
                            <Divider sx={{ mb: 2 }} />

                            {apiKeyLoading ? (
                                <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                                    <CircularProgress size={24} />
                                </Box>
                            ) : (
                                <List>
                                    <ListItem>
                                        <ListItemIcon>
                                            <VpnKeyIcon color={apiKeyInfo?.hasKey ? 'success' : 'disabled'} />
                                        </ListItemIcon>
                                        <ListItemText
                                            primary={t('users.detail.apiKeyStatus')}
                                            secondary={apiKeyInfo?.hasKey ? t('users.status.active') : t('users.detail.noApiKeyGenerated')}
                                        />
                                    </ListItem>

                                    {apiKeyInfo?.hasKey && (
                                        <>
                                            <ListItem>
                                                <ListItemText
                                                    primary={t('users.detail.created')}
                                                    secondary={formatDate(apiKeyInfo.createdAt)}
                                                    sx={{ pl: 7 }}
                                                />
                                            </ListItem>
                                            <ListItem>
                                                <ListItemText
                                                    primary={t('users.detail.lastUsed')}
                                                    secondary={formatDate(apiKeyInfo.lastUsed)}
                                                    sx={{ pl: 7 }}
                                                />
                                            </ListItem>
                                        </>
                                    )}
                                </List>
                            )}

                            {apiKeyInfo?.hasKey && (
                                <Box sx={{ mt: 2 }}>
                                    <Button
                                        variant="outlined"
                                        color="error"
                                        onClick={() => setShowRevokeApiKeyDialog(true)}
                                        disabled={apiKeyLoading}
                                    >
                                        {t('users.actions.revokeApiKey')}
                                    </Button>
                                </Box>
                            )}

                            <Alert severity="info" sx={{ mt: 2 }}>
                                <Typography variant="caption">
                                    {t('users.detail.apiKeySecurityNote')}
                                </Typography>
                            </Alert>
                        </CardContent>
                    </Card>
                </Grid>

                {/* Activity Card */}
                <Grid item xs={12} md={4}>
                    <Card>
                        <CardContent>
                            <Typography variant="h6" gutterBottom>{t('users.detail.activityInformation')}</Typography>
                            <Divider sx={{ mb: 2 }} />

                            <List dense>
                                <ListItem>
                                    <ListItemIcon>
                                        <CalendarTodayIcon fontSize="small" />
                                    </ListItemIcon>
                                    <ListItemText
                                        primary={t('users.detail.created')}
                                        secondary={formatDate(user.createdAt)}
                                    />
                                </ListItem>
                                <ListItem>
                                    <ListItemIcon>
                                        <CalendarTodayIcon fontSize="small" />
                                    </ListItemIcon>
                                    <ListItemText
                                        primary={t('users.columns.lastLogin')}
                                        secondary={formatDate(user.lastLogin)}
                                    />
                                </ListItem>
                                <ListItem>
                                    <ListItemIcon>
                                        <CalendarTodayIcon fontSize="small" />
                                    </ListItemIcon>
                                    <ListItemText
                                        primary={t('users.detail.passwordChanged')}
                                        secondary={formatDate(user.lastPasswordChange)}
                                    />
                                </ListItem>
                                {user.failedLoginAttempts > 0 && (
                                    <ListItem>
                                        <ListItemText
                                            primary={t('users.detail.failedLoginAttempts')}
                                            secondary={t('users.detail.attemptsCount', { count: user.failedLoginAttempts })}
                                        />
                                    </ListItem>
                                )}
                                {user.disabledAt && (
                                    <ListItem>
                                        <ListItemText
                                            primary={t('users.detail.disabledAt')}
                                            secondary={formatDate(user.disabledAt)}
                                        />
                                    </ListItem>
                                )}
                            </List>
                        </CardContent>
                    </Card>
                </Grid>
            </Grid>

            {/* Teams & Owned Agents */}
            <Grid container spacing={3} sx={{ mt: 0 }}>
                <Grid item xs={12} md={5}>
                    <Card sx={{ height: '100%' }}>
                        <CardContent>
                            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                                <GroupsIcon />
                                <Typography variant="h6">{t('users.detail.teams', 'Teams')}</Typography>
                            </Box>
                            <Divider sx={{ mb: 2 }} />
                            <SimpleTable<UserTeam>
                                rows={user.teams || []}
                                getRowKey={(r) => r.id}
                                columns={[
                                    {
                                        field: 'name',
                                        headerName: t('users.detail.teamName', 'Team'),
                                        render: (r) => <EntityLink type="team" id={r.id} label={r.name} />,
                                    },
                                    {
                                        field: 'description',
                                        headerName: t('users.detail.teamDescription', 'Description'),
                                        render: (r) => r.description || '-',
                                    },
                                ]}
                                emptyState={{ title: t('users.detail.noTeams', 'Not a member of any team') as string }}
                            />
                        </CardContent>
                    </Card>
                </Grid>
                <Grid item xs={12} md={7}>
                    <Card sx={{ height: '100%' }}>
                        <CardContent>
                            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                                <DevicesIcon />
                                <Typography variant="h6">{t('users.detail.ownedAgents', 'Owned Agents')}</Typography>
                            </Box>
                            <Divider sx={{ mb: 2 }} />
                            <DataTable<Agent>
                                flat
                                rows={ownedAgents}
                                columns={agentColumns}
                                getRowId={(r) => r.id}
                                loading={agentsQuery.isLoading}
                                error={agentsQuery.error}
                                onRetry={() => agentsQuery.refetch()}
                                pagination={false}
                                sorting={{ mode: 'client', initial: [{ field: 'name', sort: 'asc' }] }}
                                emptyState={{ title: t('users.detail.noOwnedAgents', 'This user owns no agents') as string }}
                            />
                        </CardContent>
                    </Card>
                </Grid>
            </Grid>

            {/* Active Sessions Section */}
            <Card sx={{ mt: 3 }}>
                <CardContent>
                    <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <DevicesIcon />
                            <Typography variant="h6">
                                {t('users.detail.activeSessions')}
                                {sessions.length > 0 && (
                                    <Badge badgeContent={sessions.length} color="primary" sx={{ ml: 2 }} />
                                )}
                            </Typography>
                        </Box>
                        {sessions.length > 0 && (
                            <Button
                                variant="outlined"
                                color="error"
                                size="small"
                                startIcon={<DeleteSweepIcon />}
                                onClick={() => setTerminateAllDialogOpen(true)}
                            >
                                {t('users.actions.terminateAllSessions')}
                            </Button>
                        )}
                    </Box>
                    <Divider sx={{ mb: 2 }} />

                    <DataTable<ActiveSession>
                        flat
                        rows={sessions}
                        columns={sessionColumns}
                        getRowId={(r) => r.id}
                        loading={sessionsLoading}
                        pagination={false}
                        sorting={{ mode: 'client', initial: [{ field: 'lastActiveAt', sort: 'desc' }] }}
                        rowActions={() => [
                            {
                                key: 'terminate',
                                label: t('users.actions.terminateSession') as string,
                                icon: <DeleteIcon fontSize="small" />,
                                danger: true,
                                onClick: (r) => setTerminateSessionId(r.id),
                            },
                        ]}
                        emptyState={{ title: t('users.detail.noActiveSessions') as string }}
                    />
                </CardContent>
            </Card>

            {/* Login History Section */}
            <Card sx={{ mt: 3 }}>
                <CardContent>
                    <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <HistoryIcon />
                            <Typography variant="h6">{t('users.detail.loginHistory')}</Typography>
                        </Box>
                        <Box sx={{ display: 'flex', gap: 1 }}>
                            <Button
                                size="small"
                                variant={attemptFilter === 'all' ? 'contained' : 'outlined'}
                                onClick={() => setAttemptFilter('all')}
                            >
                                {t('common.all')}
                            </Button>
                            <Button
                                size="small"
                                variant={attemptFilter === 'success' ? 'contained' : 'outlined'}
                                color="success"
                                onClick={() => setAttemptFilter('success')}
                            >
                                {t('users.detail.loginAttempts.success')}
                            </Button>
                            <Button
                                size="small"
                                variant={attemptFilter === 'failed' ? 'contained' : 'outlined'}
                                color="error"
                                onClick={() => setAttemptFilter('failed')}
                            >
                                {t('users.detail.loginAttempts.failed')}
                            </Button>
                        </Box>
                    </Box>
                    <Divider sx={{ mb: 2 }} />

                    <DataTable<LoginAttempt>
                        flat
                        rows={filteredAttempts}
                        columns={attemptColumns}
                        getRowId={(r) => r.id}
                        loading={attemptsLoading}
                        pagination={{ mode: 'client', initialPageSize: 25 }}
                        sorting={{ mode: 'client', initial: [{ field: 'attempted_at', sort: 'desc' }] }}
                        emptyState={{ title: t('users.detail.noLoginAttempts') as string }}
                        tableKey="admin-user-login-attempts"
                    />
                </CardContent>
            </Card>

            {/* Reset Password Dialog */}
            <Dialog open={resetPasswordOpen} onClose={() => setResetPasswordOpen(false)} maxWidth="sm" fullWidth>
                <DialogTitle>{t('users.dialogs.resetPassword.title')}</DialogTitle>
                <DialogContent>
                    <FormControlLabel
                        control={
                            <Checkbox
                                checked={temporaryPassword}
                                onChange={(e) => setTemporaryPassword(e.target.checked)}
                            />
                        }
                        label={t('users.dialogs.resetPassword.generateTemporary')}
                        sx={{ mb: 2 }}
                    />
                    {!temporaryPassword && (
                        <TextField
                            fullWidth
                            type="password"
                            label={t('users.dialogs.resetPassword.newPassword')}
                            value={newPassword}
                            onChange={(e) => setNewPassword(e.target.value)}
                            helperText={t('users.dialogs.resetPassword.passwordHelperText')}
                        />
                    )}
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setResetPasswordOpen(false)}>{t('common.cancel')}</Button>
                    <Button
                        onClick={handleResetPassword}
                        variant="contained"
                        disabled={saving || (!temporaryPassword && newPassword.length < 8)}
                    >
                        {t('users.actions.resetPassword')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Disable MFA Dialog */}
            <Dialog open={disableMFAOpen} onClose={() => setDisableMFAOpen(false)}>
                <DialogTitle>{t('users.dialogs.disableMfa.title')}</DialogTitle>
                <DialogContent>
                    <Typography>
                        {t('users.dialogs.disableMfa.confirmation')}
                    </Typography>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setDisableMFAOpen(false)}>{t('common.cancel')}</Button>
                    <Button onClick={handleDisableMFA} variant="contained" color="warning" disabled={saving}>
                        {t('users.actions.disableMfa')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Disable Account Dialog */}
            <Dialog open={disableAccountOpen} onClose={() => setDisableAccountOpen(false)} maxWidth="sm" fullWidth>
                <DialogTitle>{t('users.dialogs.disableUser.title')}</DialogTitle>
                <DialogContent>
                    <TextField
                        fullWidth
                        multiline
                        rows={3}
                        label={t('users.dialogs.disableUser.reasonLabel')}
                        placeholder={t('users.dialogs.disableUser.reasonPlaceholder') as string}
                        sx={{ mt: 2 }}
                        onChange={(e) => setUser({ ...user, disabledReason: e.target.value })}
                    />
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setDisableAccountOpen(false)}>{t('common.cancel')}</Button>
                    <Button
                        onClick={() => handleToggleAccount(user.disabledReason)}
                        variant="contained"
                        color="error"
                        disabled={saving || !user.disabledReason}
                    >
                        {t('users.dialogs.disableUser.disableAccount')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Terminate Session Dialog */}
            <Dialog
                open={terminateSessionId !== null}
                onClose={() => setTerminateSessionId(null)}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle>{t('users.dialogs.terminateSession.title')}</DialogTitle>
                <DialogContent>
                    <Typography>
                        {t('users.dialogs.terminateSession.description')}
                    </Typography>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setTerminateSessionId(null)}>{t('common.cancel')}</Button>
                    <Button
                        onClick={() => terminateSessionId && handleTerminateSession(terminateSessionId)}
                        variant="contained"
                        color="error"
                        disabled={saving}
                    >
                        {t('users.dialogs.terminateSession.terminate')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Terminate All Sessions Dialog */}
            <Dialog
                open={terminateAllDialogOpen}
                onClose={() => setTerminateAllDialogOpen(false)}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle>{t('users.dialogs.terminateAllSessions.title')}</DialogTitle>
                <DialogContent>
                    <Typography gutterBottom>
                        {t('users.dialogs.terminateAllSessions.description')}
                    </Typography>
                    <Alert severity="warning" sx={{ mt: 2 }}>
                        {t('users.dialogs.terminateAllSessions.warning')}
                    </Alert>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setTerminateAllDialogOpen(false)}>{t('common.cancel')}</Button>
                    <Button
                        onClick={handleTerminateAllSessions}
                        variant="contained"
                        color="error"
                        disabled={saving}
                    >
                        {t('users.dialogs.terminateAllSessions.terminateAll')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Revoke API Key Dialog */}
            <Dialog
                open={showRevokeApiKeyDialog}
                onClose={() => setShowRevokeApiKeyDialog(false)}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle>{t('users.dialogs.revokeApiKey.title')}</DialogTitle>
                <DialogContent>
                    <Alert severity="error" sx={{ mb: 2 }}>
                        {t('users.dialogs.revokeApiKey.warning')}
                    </Alert>
                    <Typography>
                        {t('users.dialogs.revokeApiKey.confirmation')}
                    </Typography>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setShowRevokeApiKeyDialog(false)}>{t('common.cancel')}</Button>
                    <Button
                        onClick={handleRevokeApiKey}
                        variant="contained"
                        color="error"
                        disabled={saving}
                    >
                        {t('users.actions.revokeApiKey')}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default UserDetail;