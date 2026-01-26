import React, { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
    Box, Typography, Paper, TextField, Button, CircularProgress,
    Alert, Grid, Card, CardContent, Divider, Chip, IconButton,
    Dialog, DialogTitle, DialogContent, DialogActions, FormControlLabel,
    Checkbox, List, ListItem, ListItemText, ListItemIcon, Select,
    MenuItem, FormControl, InputLabel, Table, TableBody, TableCell,
    TableContainer, TableHead, TableRow, Badge, Tooltip
} from '@mui/material';
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
import { ApiKeyInfo } from '../../types/user';

const UserDetail: React.FC = () => {
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
            setError('Failed to load user details');
            enqueueSnackbar('Failed to load user details', { variant: 'error' });
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
            enqueueSnackbar('Failed to load sessions', { variant: 'error' });
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
            enqueueSnackbar('Failed to load login attempts', { variant: 'error' });
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
            enqueueSnackbar('Failed to load API key info', { variant: 'error' });
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
            enqueueSnackbar('User details updated successfully', { variant: 'success' });
            fetchUser(); // Refresh data
        } catch (err: any) {
            console.error('Failed to update user:', err);
            const message = err.response?.data?.error || 'Failed to update user';
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
                            Password reset successfully!
                        </Typography>
                        <Typography variant="body2" sx={{ fontWeight: 'bold', my: 1 }}>
                            Temporary password: {tempPassword}
                        </Typography>
                        <Button
                            size="small"
                            variant="outlined"
                            onClick={() => {
                                navigator.clipboard.writeText(tempPassword);
                                enqueueSnackbar('Password copied to clipboard', { variant: 'info' });
                            }}
                            sx={{ mt: 1 }}
                        >
                            Copy to Clipboard
                        </Button>
                        <Typography variant="caption" display="block" sx={{ mt: 2 }}>
                            Please share this password securely with the user.
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
                enqueueSnackbar('Password reset successfully', { variant: 'success' });
            }
            
            setResetPasswordOpen(false);
            setNewPassword('');
            setTemporaryPassword(true);
        } catch (err) {
            console.error('Failed to reset password:', err);
            enqueueSnackbar('Failed to reset password', { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleDisableMFA = async () => {
        if (!user) return;

        setSaving(true);
        try {
            await disableAdminUserMFA(user.id);
            enqueueSnackbar('MFA disabled successfully', { variant: 'success' });
            setDisableMFAOpen(false);
            fetchUser(); // Refresh data
        } catch (err) {
            console.error('Failed to disable MFA:', err);
            enqueueSnackbar('Failed to disable MFA', { variant: 'error' });
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
                enqueueSnackbar('User account disabled', { variant: 'success' });
            } else {
                await enableAdminUser(user.id);
                enqueueSnackbar('User account enabled', { variant: 'success' });
            }
            setDisableAccountOpen(false);
            fetchUser(); // Refresh data
        } catch (err) {
            console.error('Failed to toggle account:', err);
            enqueueSnackbar('Failed to update account status', { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleUnlockAccount = async () => {
        if (!user) return;

        setSaving(true);
        try {
            await unlockAdminUser(user.id);
            enqueueSnackbar('User account unlocked', { variant: 'success' });
            fetchUser(); // Refresh data
        } catch (err) {
            console.error('Failed to unlock account:', err);
            enqueueSnackbar('Failed to unlock account', { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleTerminateSession = async (sessionId: string) => {
        if (!user) return;

        setSaving(true);
        try {
            await terminateSession(user.id, sessionId);
            enqueueSnackbar('Session terminated successfully', { variant: 'success' });
            setTerminateSessionId(null);
            fetchSessions(); // Refresh sessions
        } catch (err) {
            console.error('Failed to terminate session:', err);
            enqueueSnackbar('Failed to terminate session', { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleTerminateAllSessions = async () => {
        if (!user) return;

        setSaving(true);
        try {
            const response = await terminateAllUserSessions(user.id);
            enqueueSnackbar(`Terminated ${response.data.data.count} session(s) successfully`, { variant: 'success' });
            setTerminateAllDialogOpen(false);
            fetchSessions(); // Refresh sessions
        } catch (err) {
            console.error('Failed to terminate all sessions:', err);
            enqueueSnackbar('Failed to terminate all sessions', { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const handleRevokeApiKey = async () => {
        if (!user) return;

        setSaving(true);
        try {
            await revokeAdminUserApiKey(user.id);
            enqueueSnackbar('API key revoked successfully', { variant: 'success' });
            setShowRevokeApiKeyDialog(false);
            fetchApiKeyInfo(); // Refresh API key info
        } catch (err) {
            console.error('Failed to revoke API key:', err);
            enqueueSnackbar('Failed to revoke API key', { variant: 'error' });
        } finally {
            setSaving(false);
        }
    };

    const formatDate = (dateString?: string) => {
        if (!dateString) return 'Never';
        try {
            return format(new Date(dateString), 'MMM dd, yyyy HH:mm:ss');
        } catch {
            return 'Invalid date';
        }
    };

    const formatRelativeTime = (dateString?: string) => {
        if (!dateString) return 'Never';
        try {
            return formatDistanceToNow(new Date(dateString), { addSuffix: true });
        } catch {
            return 'Invalid date';
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

    if (error || !user) {
        return (
            <Box sx={{ p: 3 }}>
                <Alert severity="error">{error || 'User not found'}</Alert>
                <Button startIcon={<ArrowBackIcon />} onClick={() => navigate('/admin/users')} sx={{ mt: 2 }}>
                    Back to Users
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
                <Typography variant="h4">User Details</Typography>
            </Box>

            <Grid container spacing={3}>
                {/* User Information Card */}
                <Grid item xs={12} md={8}>
                    <Card>
                        <CardContent>
                            <Typography variant="h6" gutterBottom>User Information</Typography>
                            <Divider sx={{ mb: 2 }} />
                            
                            <Grid container spacing={2}>
                                <Grid item xs={12} sm={6}>
                                    <TextField
                                        fullWidth
                                        label="Username"
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
                                        label="Email"
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
                                            label="Role"
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
                                            <InputLabel>Role</InputLabel>
                                            <Select
                                                value={role}
                                                label="Role"
                                                onChange={(e) => setRole(e.target.value)}
                                            >
                                                <MenuItem value="user">User</MenuItem>
                                                <MenuItem value="admin">Admin</MenuItem>
                                            </Select>
                                        </FormControl>
                                    )}
                                </Grid>
                                <Grid item xs={12} sm={6}>
                                    <TextField
                                        fullWidth
                                        label="User ID"
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
                                    Save Changes
                                </Button>
                                {user.role === 'system' && (
                                    <Typography variant="caption" color="text.secondary" sx={{ alignSelf: 'center' }}>
                                        System users cannot be modified
                                    </Typography>
                                )}
                            </Box>
                        </CardContent>
                    </Card>

                    {/* Account Status Card */}
                    <Card sx={{ mt: 3 }}>
                        <CardContent>
                            <Typography variant="h6" gutterBottom>Account Status</Typography>
                            <Divider sx={{ mb: 2 }} />
                            
                            <List>
                                <ListItem>
                                    <ListItemIcon>
                                        {user.accountEnabled ? <CheckCircleIcon color="success" /> : <CancelIcon color="error" />}
                                    </ListItemIcon>
                                    <ListItemText
                                        primary="Account Status"
                                        secondary={user.accountEnabled ? 'Enabled' : `Disabled - ${user.disabledReason || 'No reason provided'}`}
                                    />
                                    <Button
                                        variant="outlined"
                                        color={user.accountEnabled ? 'error' : 'success'}
                                        onClick={() => user.accountEnabled ? setDisableAccountOpen(true) : handleToggleAccount()}
                                        disabled={user.role === 'system'}
                                    >
                                        {user.accountEnabled ? 'Disable' : 'Enable'}
                                    </Button>
                                </ListItem>

                                <ListItem>
                                    <ListItemIcon>
                                        {user.accountLocked ? <CancelIcon color="warning" /> : <CheckCircleIcon color="success" />}
                                    </ListItemIcon>
                                    <ListItemText
                                        primary="Lock Status"
                                        secondary={user.accountLocked ? 
                                            `Locked until ${formatDate(user.accountLockedUntil)}` : 
                                            'Not locked'}
                                    />
                                    {user.accountLocked && (
                                        <Button
                                            variant="outlined"
                                            color="warning"
                                            onClick={handleUnlockAccount}
                                        >
                                            Unlock
                                        </Button>
                                    )}
                                </ListItem>

                                <ListItem>
                                    <ListItemIcon>
                                        <SecurityIcon color={user.mfaEnabled ? 'success' : 'disabled'} />
                                    </ListItemIcon>
                                    <ListItemText
                                        primary="MFA Status"
                                        secondary={user.mfaEnabled ? 
                                            `Enabled (${user.mfaType.join(', ')})` : 
                                            'Disabled'}
                                    />
                                    {user.mfaEnabled && (
                                        <Button
                                            variant="outlined"
                                            color="warning"
                                            onClick={() => setDisableMFAOpen(true)}
                                        >
                                            Disable MFA
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
                                    Reset Password
                                </Button>
                            </Box>
                        </CardContent>
                    </Card>

                    {/* API Key Management Card */}
                    <Card sx={{ mt: 3 }}>
                        <CardContent>
                            <Typography variant="h6" gutterBottom>API Key Management</Typography>
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
                                            primary="API Key Status"
                                            secondary={apiKeyInfo?.hasKey ? 'Active' : 'No API Key Generated'}
                                        />
                                    </ListItem>

                                    {apiKeyInfo?.hasKey && (
                                        <>
                                            <ListItem>
                                                <ListItemText
                                                    primary="Created"
                                                    secondary={formatDate(apiKeyInfo.createdAt)}
                                                    sx={{ pl: 7 }}
                                                />
                                            </ListItem>
                                            <ListItem>
                                                <ListItemText
                                                    primary="Last Used"
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
                                        Revoke API Key
                                    </Button>
                                </Box>
                            )}

                            <Alert severity="info" sx={{ mt: 2 }}>
                                <Typography variant="caption">
                                    Admins can revoke API keys but cannot view the plaintext key for security reasons.
                                </Typography>
                            </Alert>
                        </CardContent>
                    </Card>
                </Grid>

                {/* Activity Card */}
                <Grid item xs={12} md={4}>
                    <Card>
                        <CardContent>
                            <Typography variant="h6" gutterBottom>Activity Information</Typography>
                            <Divider sx={{ mb: 2 }} />
                            
                            <List dense>
                                <ListItem>
                                    <ListItemIcon>
                                        <CalendarTodayIcon fontSize="small" />
                                    </ListItemIcon>
                                    <ListItemText
                                        primary="Created"
                                        secondary={formatDate(user.createdAt)}
                                    />
                                </ListItem>
                                <ListItem>
                                    <ListItemIcon>
                                        <CalendarTodayIcon fontSize="small" />
                                    </ListItemIcon>
                                    <ListItemText
                                        primary="Last Login"
                                        secondary={formatDate(user.lastLogin)}
                                    />
                                </ListItem>
                                <ListItem>
                                    <ListItemIcon>
                                        <CalendarTodayIcon fontSize="small" />
                                    </ListItemIcon>
                                    <ListItemText
                                        primary="Password Changed"
                                        secondary={formatDate(user.lastPasswordChange)}
                                    />
                                </ListItem>
                                {user.failedLoginAttempts > 0 && (
                                    <ListItem>
                                        <ListItemText
                                            primary="Failed Login Attempts"
                                            secondary={`${user.failedLoginAttempts} attempts`}
                                        />
                                    </ListItem>
                                )}
                                {user.disabledAt && (
                                    <ListItem>
                                        <ListItemText
                                            primary="Disabled At"
                                            secondary={formatDate(user.disabledAt)}
                                        />
                                    </ListItem>
                                )}
                            </List>
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
                                Active Sessions
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
                                Terminate All Sessions
                            </Button>
                        )}
                    </Box>
                    <Divider sx={{ mb: 2 }} />

                    {sessionsLoading ? (
                        <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : sessions.length === 0 ? (
                        <Typography color="text.secondary" align="center" sx={{ py: 3 }}>
                            No active sessions
                        </Typography>
                    ) : (
                        <TableContainer>
                            <Table size="small">
                                <TableHead>
                                    <TableRow>
                                        <TableCell>IP Address</TableCell>
                                        <TableCell>Device / Browser</TableCell>
                                        <TableCell>Last Active</TableCell>
                                        <TableCell>Created</TableCell>
                                        <TableCell align="right">Actions</TableCell>
                                    </TableRow>
                                </TableHead>
                                <TableBody>
                                    {sessions.map((session) => (
                                        <TableRow key={session.id}>
                                            <TableCell>{session.ipAddress}</TableCell>
                                            <TableCell>
                                                <Tooltip title={session.userAgent}>
                                                    <Typography variant="body2" noWrap sx={{ maxWidth: 200 }}>
                                                        {session.userAgent}
                                                    </Typography>
                                                </Tooltip>
                                            </TableCell>
                                            <TableCell>{formatRelativeTime(session.lastActiveAt)}</TableCell>
                                            <TableCell>{formatDate(session.createdAt)}</TableCell>
                                            <TableCell align="right">
                                                <IconButton
                                                    size="small"
                                                    color="error"
                                                    onClick={() => setTerminateSessionId(session.id)}
                                                    title="Terminate session"
                                                >
                                                    <DeleteIcon fontSize="small" />
                                                </IconButton>
                                            </TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </TableContainer>
                    )}
                </CardContent>
            </Card>

            {/* Login History Section */}
            <Card sx={{ mt: 3 }}>
                <CardContent>
                    <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <HistoryIcon />
                            <Typography variant="h6">Login History</Typography>
                        </Box>
                        <Box sx={{ display: 'flex', gap: 1 }}>
                            <Button
                                size="small"
                                variant={attemptFilter === 'all' ? 'contained' : 'outlined'}
                                onClick={() => setAttemptFilter('all')}
                            >
                                All
                            </Button>
                            <Button
                                size="small"
                                variant={attemptFilter === 'success' ? 'contained' : 'outlined'}
                                color="success"
                                onClick={() => setAttemptFilter('success')}
                            >
                                Success
                            </Button>
                            <Button
                                size="small"
                                variant={attemptFilter === 'failed' ? 'contained' : 'outlined'}
                                color="error"
                                onClick={() => setAttemptFilter('failed')}
                            >
                                Failed
                            </Button>
                        </Box>
                    </Box>
                    <Divider sx={{ mb: 2 }} />

                    {attemptsLoading ? (
                        <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                            <CircularProgress size={24} />
                        </Box>
                    ) : filteredAttempts.length === 0 ? (
                        <Typography color="text.secondary" align="center" sx={{ py: 3 }}>
                            No login attempts found
                        </Typography>
                    ) : (
                        <TableContainer>
                            <Table size="small">
                                <TableHead>
                                    <TableRow>
                                        <TableCell>Timestamp</TableCell>
                                        <TableCell>Provider</TableCell>
                                        <TableCell>IP Address</TableCell>
                                        <TableCell>Status</TableCell>
                                        <TableCell>Failure Reason</TableCell>
                                    </TableRow>
                                </TableHead>
                                <TableBody>
                                    {filteredAttempts.map((attempt) => (
                                        <TableRow key={attempt.id}>
                                            <TableCell>{formatDate(attempt.attempted_at)}</TableCell>
                                            <TableCell>
                                                <Chip
                                                    size="small"
                                                    label={attempt.provider_type || 'Local'}
                                                    variant="outlined"
                                                />
                                            </TableCell>
                                            <TableCell>{attempt.ip_address}</TableCell>
                                            <TableCell>
                                                <Chip
                                                    size="small"
                                                    icon={attempt.success ? <CheckCircleIcon /> : <CancelIcon />}
                                                    label={attempt.success ? 'Success' : 'Failed'}
                                                    color={attempt.success ? 'success' : 'error'}
                                                />
                                            </TableCell>
                                            <TableCell>
                                                {attempt.failure_reason ? (
                                                    <Typography
                                                        variant="body2"
                                                        color="error"
                                                        sx={{ fontWeight: 'bold' }}
                                                    >
                                                        {attempt.failure_reason.replace(/_/g, ' ')}
                                                    </Typography>
                                                ) : (
                                                    '-'
                                                )}
                                            </TableCell>
                                        </TableRow>
                                    ))}
                                </TableBody>
                            </Table>
                        </TableContainer>
                    )}
                </CardContent>
            </Card>

            {/* Reset Password Dialog */}
            <Dialog open={resetPasswordOpen} onClose={() => setResetPasswordOpen(false)} maxWidth="sm" fullWidth>
                <DialogTitle>Reset User Password</DialogTitle>
                <DialogContent>
                    <FormControlLabel
                        control={
                            <Checkbox
                                checked={temporaryPassword}
                                onChange={(e) => setTemporaryPassword(e.target.checked)}
                            />
                        }
                        label="Generate temporary password"
                        sx={{ mb: 2 }}
                    />
                    {!temporaryPassword && (
                        <TextField
                            fullWidth
                            type="password"
                            label="New Password"
                            value={newPassword}
                            onChange={(e) => setNewPassword(e.target.value)}
                            helperText="Must be at least 8 characters"
                        />
                    )}
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setResetPasswordOpen(false)}>Cancel</Button>
                    <Button 
                        onClick={handleResetPassword} 
                        variant="contained" 
                        disabled={saving || (!temporaryPassword && newPassword.length < 8)}
                    >
                        Reset Password
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Disable MFA Dialog */}
            <Dialog open={disableMFAOpen} onClose={() => setDisableMFAOpen(false)}>
                <DialogTitle>Disable MFA</DialogTitle>
                <DialogContent>
                    <Typography>
                        Are you sure you want to disable MFA for this user? 
                        They will need to set it up again if required.
                    </Typography>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setDisableMFAOpen(false)}>Cancel</Button>
                    <Button onClick={handleDisableMFA} variant="contained" color="warning" disabled={saving}>
                        Disable MFA
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Disable Account Dialog */}
            <Dialog open={disableAccountOpen} onClose={() => setDisableAccountOpen(false)} maxWidth="sm" fullWidth>
                <DialogTitle>Disable User Account</DialogTitle>
                <DialogContent>
                    <TextField
                        fullWidth
                        multiline
                        rows={3}
                        label="Reason for disabling"
                        placeholder="Please provide a reason..."
                        sx={{ mt: 2 }}
                        onChange={(e) => setUser({ ...user, disabledReason: e.target.value })}
                    />
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setDisableAccountOpen(false)}>Cancel</Button>
                    <Button 
                        onClick={() => handleToggleAccount(user.disabledReason)} 
                        variant="contained" 
                        color="error"
                        disabled={saving || !user.disabledReason}
                    >
                        Disable Account
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
                <DialogTitle>Terminate Session?</DialogTitle>
                <DialogContent>
                    <Typography>
                        This will log the user out from this device. The user will need to log in again to continue using the application.
                    </Typography>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setTerminateSessionId(null)}>Cancel</Button>
                    <Button
                        onClick={() => terminateSessionId && handleTerminateSession(terminateSessionId)}
                        variant="contained"
                        color="error"
                        disabled={saving}
                    >
                        Terminate
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
                <DialogTitle>Terminate All Sessions?</DialogTitle>
                <DialogContent>
                    <Typography gutterBottom>
                        This will log the user out from <strong>ALL</strong> devices, including their current session.
                        The user will need to log in again. This action cannot be undone.
                    </Typography>
                    <Alert severity="warning" sx={{ mt: 2 }}>
                        ⚠️ This includes any active work sessions!
                    </Alert>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setTerminateAllDialogOpen(false)}>Cancel</Button>
                    <Button
                        onClick={handleTerminateAllSessions}
                        variant="contained"
                        color="error"
                        disabled={saving}
                    >
                        Terminate All
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
                <DialogTitle>Revoke User API Key?</DialogTitle>
                <DialogContent>
                    <Alert severity="error" sx={{ mb: 2 }}>
                        This will immediately disable API access for this user. Any applications using this key will stop working.
                    </Alert>
                    <Typography>
                        Are you sure you want to revoke this user's API key?
                    </Typography>
                </DialogContent>
                <DialogActions>
                    <Button onClick={() => setShowRevokeApiKeyDialog(false)}>Cancel</Button>
                    <Button
                        onClick={handleRevokeApiKey}
                        variant="contained"
                        color="error"
                        disabled={saving}
                    >
                        Revoke API Key
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default UserDetail;