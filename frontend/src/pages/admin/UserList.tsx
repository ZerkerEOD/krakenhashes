import React, { useState, useEffect, useCallback } from 'react';
import {
    Box, Typography, Paper, CircularProgress, Alert, Chip, IconButton, Tooltip,
    Button, Dialog, DialogTitle, DialogContent, DialogActions, TextField,
    FormControl, InputLabel, Select, MenuItem, FormHelperText
} from '@mui/material';
import { DataGrid, GridColDef, GridRenderCellParams } from '@mui/x-data-grid';
import EditIcon from '@mui/icons-material/Edit';
import LockIcon from '@mui/icons-material/Lock';
import LockOpenIcon from '@mui/icons-material/LockOpen';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import CancelIcon from '@mui/icons-material/Cancel';
import AddIcon from '@mui/icons-material/Add';
import DeleteIcon from '@mui/icons-material/Delete';
import { useSnackbar } from 'notistack';
import { useNavigate } from 'react-router-dom';
import { format } from 'date-fns';
import { useTranslation } from 'react-i18next';

import { User } from '../../types/user';
import { listAdminUsers, enableAdminUser, disableAdminUser, createAdminUser, deleteAdminUser } from '../../services/api';
import { getPasswordPolicy } from '../../services/auth';
import { PasswordPolicy } from '../../types/auth';
import PasswordValidation from '../../components/common/PasswordValidation';
import { useAuth } from '../../contexts/AuthContext';

const UserList: React.FC = () => {
    const { t } = useTranslation('admin');
    const [users, setUsers] = useState<User[]>([]);
    const [loading, setLoading] = useState<boolean>(true);
    const [error, setError] = useState<string | null>(null);
    const [actionLoading, setActionLoading] = useState<string | null>(null);
    const [createDialogOpen, setCreateDialogOpen] = useState(false);
    const [createLoading, setCreateLoading] = useState(false);
    const [formData, setFormData] = useState({
        username: '',
        email: '',
        password: '',
        confirmPassword: '',
        role: 'user'
    });
    const [formErrors, setFormErrors] = useState<Record<string, string>>({});
    const [policy, setPolicy] = useState<PasswordPolicy | null>(null);
    const [disableDialogOpen, setDisableDialogOpen] = useState(false);
    const [disableUserId, setDisableUserId] = useState<string | null>(null);
    const [disableReason, setDisableReason] = useState('');
    const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
    const [deleteUserId, setDeleteUserId] = useState<string | null>(null);
    const [deleteUsername, setDeleteUsername] = useState<string>('');

    const { enqueueSnackbar } = useSnackbar();
    const { userRole } = useAuth();
    const navigate = useNavigate();

    const fetchUsers = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const response = await listAdminUsers();
            setUsers(response.data.data || []); 
        } catch (err) {
            console.error("Failed to fetch users:", err);
            setError(t('users.errors.loadFailed') as string);
            enqueueSnackbar(t('users.errors.loadFailed') as string, { variant: 'error' });
        } finally {
            setLoading(false);
        }
    }, [enqueueSnackbar]);

    useEffect(() => {
        if (userRole === 'admin') { 
            fetchUsers();
            // Load password policy
            const loadPolicy = async () => {
                try {
                    const policyData = await getPasswordPolicy();
                    setPolicy(policyData);
                } catch (error) {
                    console.error('Failed to load password policy:', error);
                }
            };
            loadPolicy();
        }
    }, [userRole, fetchUsers]);

    const handleEnableUser = async (userId: string) => {
        setActionLoading(userId);
        try {
            await enableAdminUser(userId);
            enqueueSnackbar(t('users.messages.enableSuccess') as string, { variant: 'success' });
            fetchUsers(); // Refresh list
        } catch (err) {
            console.error('Failed to enable user:', err);
            enqueueSnackbar(t('users.errors.enableFailed') as string, { variant: 'error' });
        } finally {
            setActionLoading(null);
        }
    };

    const handleDisableUser = async () => {
        if (!disableUserId || !disableReason.trim()) {
            enqueueSnackbar(t('users.errors.reasonRequired') as string, { variant: 'warning' });
            return;
        }

        setActionLoading(disableUserId);
        try {
            await disableAdminUser(disableUserId, { reason: disableReason });
            enqueueSnackbar(t('users.messages.disableSuccess') as string, { variant: 'success' });
            fetchUsers(); // Refresh list
            setDisableDialogOpen(false);
            setDisableUserId(null);
            setDisableReason('');
        } catch (err) {
            console.error('Failed to disable user:', err);
            enqueueSnackbar(t('users.errors.disableFailed') as string, { variant: 'error' });
        } finally {
            setActionLoading(null);
        }
    };

    const openDisableDialog = (userId: string) => {
        setDisableUserId(userId);
        setDisableDialogOpen(true);
    };

    const openDeleteDialog = (userId: string, username: string) => {
        setDeleteUserId(userId);
        setDeleteUsername(username);
        setDeleteDialogOpen(true);
    };

    const handleDeleteUser = async () => {
        if (!deleteUserId) return;

        setActionLoading(deleteUserId);
        try {
            await deleteAdminUser(deleteUserId);
            enqueueSnackbar(t('users.messages.deleteSuccess') as string, { variant: 'success' });
            fetchUsers(); // Refresh list
            setDeleteDialogOpen(false);
            setDeleteUserId(null);
            setDeleteUsername('');
        } catch (err: any) {
            console.error('Failed to delete user:', err);
            const message = err.response?.data?.error || t('users.errors.deleteFailed') as string;
            enqueueSnackbar(message, { variant: 'error' });
        } finally {
            setActionLoading(null);
        }
    };

    const handleCreateUser = async () => {
        // Reset errors
        setFormErrors({});
        
        // Validate form
        const errors: Record<string, string> = {};

        if (!formData.username) {
            errors.username = t('users.validation.usernameRequired') as string;
        }

        if (!formData.email) {
            errors.email = t('users.validation.emailRequired') as string;
        } else if (!formData.email.includes('@')) {
            errors.email = t('users.validation.emailInvalid') as string;
        }

        if (!formData.password) {
            errors.password = t('users.validation.passwordRequired') as string;
        } else if (policy) {
            // Validate against actual policy
            const passwordErrors: string[] = [];

            if (formData.password.length < policy.minPasswordLength) {
                passwordErrors.push(t('users.validation.passwordMinLength', { value: policy.minPasswordLength }) as string);
            }

            if (policy.requireUppercase && !/[A-Z]/.test(formData.password)) {
                passwordErrors.push(t('users.validation.passwordUppercase') as string);
            }

            if (policy.requireLowercase && !/[a-z]/.test(formData.password)) {
                passwordErrors.push(t('users.validation.passwordLowercase') as string);
            }

            if (policy.requireNumbers && !/[0-9]/.test(formData.password)) {
                passwordErrors.push(t('users.validation.passwordNumber') as string);
            }

            if (policy.requireSpecialChars && !/[!@#$%^&*(),.?":{}|<>]/.test(formData.password)) {
                passwordErrors.push(t('users.validation.passwordSpecial') as string);
            }

            if (passwordErrors.length > 0) {
                errors.password = t('users.validation.passwordMustContain', { requirements: passwordErrors.join(', ') }) as string;
            }
        }

        if (!formData.confirmPassword) {
            errors.confirmPassword = t('users.validation.confirmPasswordRequired') as string;
        } else if (formData.password !== formData.confirmPassword) {
            errors.confirmPassword = t('users.validation.passwordsMismatch') as string;
        }
        
        if (Object.keys(errors).length > 0) {
            setFormErrors(errors);
            return;
        }
        
        setCreateLoading(true);
        try {
            await createAdminUser({
                username: formData.username,
                email: formData.email,
                password: formData.password,
                role: formData.role
            });
            
            enqueueSnackbar(t('users.messages.createSuccess') as string, { variant: 'success' });
            setCreateDialogOpen(false);
            setFormData({
                username: '',
                email: '',
                password: '',
                confirmPassword: '',
                role: 'user'
            });
            fetchUsers(); // Refresh list
        } catch (err: any) {
            console.error('Failed to create user:', err);
            const message = err.response?.data?.error || t('users.errors.createFailed') as string;
            enqueueSnackbar(message, { variant: 'error' });
        } finally {
            setCreateLoading(false);
        }
    };

    const formatDate = (dateString?: string) => {
        if (!dateString) return '-';
        try {
            return format(new Date(dateString), 'MMM dd, yyyy HH:mm');
        } catch {
            return '-';
        }
    };

    const columns: GridColDef[] = [
        {
            field: 'username',
            headerName: t('users.columns.username') as string,
            flex: 1,
            minWidth: 150
        },
        {
            field: 'email',
            headerName: t('users.columns.email') as string,
            flex: 1.5,
            minWidth: 200
        },
        {
            field: 'role',
            headerName: t('users.columns.role') as string,
            width: 100,
            renderCell: (params: GridRenderCellParams) => (
                <Chip
                    label={params.value}
                    size="small"
                    color={
                        params.value === 'system' ? 'success' :
                        params.value === 'admin' ? 'error' :
                        'default'
                    }
                />
            )
        },
        {
            field: 'lastAuthProvider',
            headerName: t('users.columns.provider') as string,
            width: 100,
            renderCell: (params: GridRenderCellParams) => (
                <Chip
                    size="small"
                    label={params.value || t('users.columns.providerLocal') as string}
                    variant="outlined"
                />
            )
        },
        {
            field: 'accountStatus',
            headerName: t('users.columns.status') as string,
            width: 120,
            renderCell: (params: GridRenderCellParams) => {
                const user = params.row as User;
                if (!user.accountEnabled) {
                    return <Chip label={t('users.status.disabled') as string} size="small" color="error" />;
                }
                if (user.accountLocked) {
                    return <Chip label={t('users.status.locked') as string} size="small" color="warning" />;
                }
                return <Chip label={t('users.status.active') as string} size="small" color="success" />;
            }
        },
        {
            field: 'mfaEnabled',
            headerName: t('users.columns.mfa') as string,
            width: 80,
            renderCell: (params: GridRenderCellParams) => (
                params.value ?
                    <CheckCircleIcon color="success" fontSize="small" /> :
                    <CancelIcon color="disabled" fontSize="small" />
            )
        },
        {
            field: 'lastLogin',
            headerName: t('users.columns.lastLogin') as string,
            width: 180,
            renderCell: (params: GridRenderCellParams) => formatDate(params.value as string)
        },
        {
            field: 'createdAt',
            headerName: t('users.columns.createdAt') as string,
            width: 180,
            renderCell: (params: GridRenderCellParams) => formatDate(params.value as string)
        },
        {
            field: 'hasApiKey',
            headerName: t('users.columns.apiKey') as string,
            width: 100,
            renderCell: (params: GridRenderCellParams) => {
                const user = params.row as User;
                if (user.hasApiKey) {
                    return <Chip label={t('users.status.active') as string} size="small" color="success" />;
                }
                return <Chip label={t('users.apiKey.none') as string} size="small" color="default" />;
            }
        },
        {
            field: 'apiKeyLastUsed',
            headerName: t('users.columns.lastApiUse') as string,
            width: 180,
            renderCell: (params: GridRenderCellParams) => formatDate(params.value as string)
        },
        {
            field: 'actions',
            headerName: t('users.columns.actions') as string,
            width: 180,
            sortable: false,
            renderCell: (params: GridRenderCellParams) => {
                const user = params.row as User;
                const isLoading = actionLoading === user.id;
                const isSystemUser = user.role === 'system';

                return (
                    <Box>
                        <Tooltip title={t('users.actions.edit') as string}>
                            <IconButton
                                size="small"
                                onClick={() => navigate(`/admin/users/${user.id}`)}
                                disabled={isLoading}
                            >
                                <EditIcon fontSize="small" />
                            </IconButton>
                        </Tooltip>

                        {user.accountEnabled ? (
                            <Tooltip title={t('users.actions.disable') as string}>
                                <IconButton
                                    size="small"
                                    onClick={() => openDisableDialog(user.id)}
                                    disabled={isLoading || isSystemUser}
                                    color="error"
                                >
                                    {isLoading ? <CircularProgress size={16} /> : <LockIcon fontSize="small" />}
                                </IconButton>
                            </Tooltip>
                        ) : (
                            <Tooltip title={t('users.actions.enable') as string}>
                                <IconButton
                                    size="small"
                                    onClick={() => handleEnableUser(user.id)}
                                    disabled={isLoading}
                                    color="success"
                                >
                                    {isLoading ? <CircularProgress size={16} /> : <LockOpenIcon fontSize="small" />}
                                </IconButton>
                            </Tooltip>
                        )}

                        <Tooltip title={isSystemUser ? t('users.actions.cannotDeleteSystem') as string : t('users.actions.delete') as string}>
                            <span>
                                <IconButton
                                    size="small"
                                    onClick={() => openDeleteDialog(user.id, user.username)}
                                    disabled={isLoading || isSystemUser}
                                    color="error"
                                >
                                    <DeleteIcon fontSize="small" />
                                </IconButton>
                            </span>
                        </Tooltip>
                    </Box>
                );
            }
        }
    ];

    if (loading) {
        return (
            <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
                <CircularProgress />
            </Box>
        );
    }

    return (
        <Box sx={{ p: 3 }}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
                <Box>
                    <Typography variant="h4" component="h1" gutterBottom>
                        {t('users.title')}
                    </Typography>
                    <Typography variant="body1" color="text.secondary">
                        {t('users.description')}
                    </Typography>
                </Box>
                <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={() => setCreateDialogOpen(true)}
                >
                    {t('users.addUser')}
                </Button>
            </Box>
            
            <Paper sx={{ p: 2, mt: 3 }}>
                {error && (
                    <Alert severity="error" sx={{ mb: 2 }}>
                        {error}
                    </Alert>
                )}
                
                <DataGrid
                    rows={users}
                    columns={columns}
                    initialState={{
                        pagination: {
                            paginationModel: { pageSize: 10 }
                        }
                    }}
                    pageSizeOptions={[10, 25, 50]}
                    autoHeight
                    disableRowSelectionOnClick
                    getRowId={(row) => row.id}
                    sx={{
                        '& .MuiDataGrid-row': {
                            cursor: 'pointer'
                        }
                    }}
                />
            </Paper>

            {/* Create User Dialog */}
            <Dialog
                open={createDialogOpen}
                onClose={() => {
                    setCreateDialogOpen(false);
                    setFormData({
                        username: '',
                        email: '',
                        password: '',
                        confirmPassword: '',
                        role: 'user'
                    });
                    setFormErrors({});
                }}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle>{t('users.dialogs.createUser.title')}</DialogTitle>
                <DialogContent>
                    <Box sx={{ mt: 2 }}>
                        <TextField
                            fullWidth
                            label={t('users.columns.username')}
                            value={formData.username}
                            onChange={(e) => setFormData({ ...formData, username: e.target.value })}
                            error={!!formErrors.username}
                            helperText={formErrors.username}
                            margin="normal"
                            required
                        />
                        <TextField
                            fullWidth
                            label={t('users.columns.email')}
                            type="email"
                            value={formData.email}
                            onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                            error={!!formErrors.email}
                            helperText={formErrors.email}
                            margin="normal"
                            required
                        />
                        <TextField
                            fullWidth
                            label={t('users.dialogs.createUser.password')}
                            type="password"
                            value={formData.password}
                            onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                            error={!!formErrors.password}
                            helperText={formErrors.password}
                            margin="normal"
                            required
                        />
                        {formData.password && (
                            <PasswordValidation password={formData.password} />
                        )}
                        <TextField
                            fullWidth
                            label={t('users.dialogs.createUser.confirmPassword')}
                            type="password"
                            value={formData.confirmPassword}
                            onChange={(e) => setFormData({ ...formData, confirmPassword: e.target.value })}
                            error={!!formErrors.confirmPassword}
                            helperText={formErrors.confirmPassword}
                            margin="normal"
                            required
                        />
                        <FormControl fullWidth margin="normal" required>
                            <InputLabel>{t('users.columns.role')}</InputLabel>
                            <Select
                                value={formData.role}
                                onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                                label={t('users.columns.role')}
                            >
                                <MenuItem value="user">{t('users.roles.user')}</MenuItem>
                                <MenuItem value="admin">{t('users.roles.admin')}</MenuItem>
                            </Select>
                            <FormHelperText>{t('users.dialogs.createUser.roleHelperText')}</FormHelperText>
                        </FormControl>
                    </Box>
                </DialogContent>
                <DialogActions>
                    <Button
                        onClick={() => {
                            setCreateDialogOpen(false);
                            setFormData({
                                username: '',
                                email: '',
                                password: '',
                                confirmPassword: '',
                                role: 'user'
                            });
                            setFormErrors({});
                        }}
                        disabled={createLoading}
                    >
                        {t('common.cancel')}
                    </Button>
                    <Button
                        onClick={handleCreateUser}
                        variant="contained"
                        disabled={createLoading}
                    >
                        {createLoading ? <CircularProgress size={24} /> : t('common.create')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Disable User Dialog */}
            <Dialog
                open={disableDialogOpen}
                onClose={() => {
                    setDisableDialogOpen(false);
                    setDisableUserId(null);
                    setDisableReason('');
                }}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle>{t('users.dialogs.disableUser.title')}</DialogTitle>
                <DialogContent>
                    <Box sx={{ mt: 2 }}>
                        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                            {t('users.dialogs.disableUser.description')}
                        </Typography>
                        <TextField
                            fullWidth
                            label={t('users.dialogs.disableUser.reasonLabel')}
                            value={disableReason}
                            onChange={(e) => setDisableReason(e.target.value)}
                            multiline
                            rows={3}
                            required
                            placeholder={t('users.dialogs.disableUser.reasonPlaceholder') as string}
                        />
                    </Box>
                </DialogContent>
                <DialogActions>
                    <Button
                        onClick={() => {
                            setDisableDialogOpen(false);
                            setDisableUserId(null);
                            setDisableReason('');
                        }}
                    >
                        {t('common.cancel')}
                    </Button>
                    <Button
                        onClick={handleDisableUser}
                        variant="contained"
                        color="error"
                        disabled={!disableReason.trim()}
                    >
                        {t('users.actions.disable')}
                    </Button>
                </DialogActions>
            </Dialog>

            {/* Delete User Dialog */}
            <Dialog
                open={deleteDialogOpen}
                onClose={() => {
                    setDeleteDialogOpen(false);
                    setDeleteUserId(null);
                    setDeleteUsername('');
                }}
                maxWidth="sm"
                fullWidth
            >
                <DialogTitle>{t('users.dialogs.deleteUser.title')}</DialogTitle>
                <DialogContent>
                    <Box sx={{ mt: 2 }}>
                        <Alert severity="warning" sx={{ mb: 2 }}>
                            {t('users.dialogs.deleteUser.warning')}
                        </Alert>
                        <Typography variant="body1">
                            {t('users.dialogs.deleteUser.confirmation', { username: deleteUsername })}
                        </Typography>
                        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                            {t('users.dialogs.deleteUser.dataNote')}
                        </Typography>
                    </Box>
                </DialogContent>
                <DialogActions>
                    <Button
                        onClick={() => {
                            setDeleteDialogOpen(false);
                            setDeleteUserId(null);
                            setDeleteUsername('');
                        }}
                    >
                        {t('common.cancel')}
                    </Button>
                    <Button
                        onClick={handleDeleteUser}
                        variant="contained"
                        color="error"
                        disabled={actionLoading === deleteUserId}
                    >
                        {actionLoading === deleteUserId ? <CircularProgress size={24} /> : t('users.actions.delete')}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default UserList;