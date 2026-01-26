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

import { User } from '../../types/user';
import { listAdminUsers, enableAdminUser, disableAdminUser, createAdminUser, deleteAdminUser } from '../../services/api';
import { getPasswordPolicy } from '../../services/auth';
import { PasswordPolicy } from '../../types/auth';
import PasswordValidation from '../../components/common/PasswordValidation';
import { useAuth } from '../../contexts/AuthContext';

const UserList: React.FC = () => {
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
            setError('Failed to load users. Please try refreshing.');
            enqueueSnackbar('Failed to load users', { variant: 'error' });
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
            enqueueSnackbar('User enabled successfully', { variant: 'success' });
            fetchUsers(); // Refresh list
        } catch (err) {
            console.error('Failed to enable user:', err);
            enqueueSnackbar('Failed to enable user', { variant: 'error' });
        } finally {
            setActionLoading(null);
        }
    };

    const handleDisableUser = async () => {
        if (!disableUserId || !disableReason.trim()) {
            enqueueSnackbar('Please provide a reason for disabling this user', { variant: 'warning' });
            return;
        }

        setActionLoading(disableUserId);
        try {
            await disableAdminUser(disableUserId, { reason: disableReason });
            enqueueSnackbar('User disabled successfully', { variant: 'success' });
            fetchUsers(); // Refresh list
            setDisableDialogOpen(false);
            setDisableUserId(null);
            setDisableReason('');
        } catch (err) {
            console.error('Failed to disable user:', err);
            enqueueSnackbar('Failed to disable user', { variant: 'error' });
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
            enqueueSnackbar('User deleted successfully', { variant: 'success' });
            fetchUsers(); // Refresh list
            setDeleteDialogOpen(false);
            setDeleteUserId(null);
            setDeleteUsername('');
        } catch (err: any) {
            console.error('Failed to delete user:', err);
            const message = err.response?.data?.error || 'Failed to delete user';
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
            errors.username = 'Username is required';
        }
        
        if (!formData.email) {
            errors.email = 'Email is required';
        } else if (!formData.email.includes('@')) {
            errors.email = 'Invalid email format';
        }
        
        if (!formData.password) {
            errors.password = 'Password is required';
        } else if (policy) {
            // Validate against actual policy
            const passwordErrors: string[] = [];
            
            if (formData.password.length < policy.minPasswordLength) {
                passwordErrors.push(`at least ${policy.minPasswordLength} characters`);
            }
            
            if (policy.requireUppercase && !/[A-Z]/.test(formData.password)) {
                passwordErrors.push('one uppercase letter');
            }
            
            if (policy.requireLowercase && !/[a-z]/.test(formData.password)) {
                passwordErrors.push('one lowercase letter');
            }
            
            if (policy.requireNumbers && !/[0-9]/.test(formData.password)) {
                passwordErrors.push('one number');
            }
            
            if (policy.requireSpecialChars && !/[!@#$%^&*(),.?":{}|<>]/.test(formData.password)) {
                passwordErrors.push('one special character');
            }
            
            if (passwordErrors.length > 0) {
                errors.password = `Password must contain ${passwordErrors.join(', ')}`;
            }
        }
        
        if (!formData.confirmPassword) {
            errors.confirmPassword = 'Please confirm password';
        } else if (formData.password !== formData.confirmPassword) {
            errors.confirmPassword = 'Passwords do not match';
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
            
            enqueueSnackbar('User created successfully', { variant: 'success' });
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
            const message = err.response?.data?.error || 'Failed to create user';
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
            headerName: 'Username', 
            flex: 1, 
            minWidth: 150 
        },
        { 
            field: 'email', 
            headerName: 'Email', 
            flex: 1.5, 
            minWidth: 200 
        },
        {
            field: 'role',
            headerName: 'Role',
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
            headerName: 'Provider',
            width: 100,
            renderCell: (params: GridRenderCellParams) => (
                <Chip
                    size="small"
                    label={params.value || 'Local'}
                    variant="outlined"
                />
            )
        },
        {
            field: 'accountStatus',
            headerName: 'Status', 
            width: 120,
            renderCell: (params: GridRenderCellParams) => {
                const user = params.row as User;
                if (!user.accountEnabled) {
                    return <Chip label="Disabled" size="small" color="error" />;
                }
                if (user.accountLocked) {
                    return <Chip label="Locked" size="small" color="warning" />;
                }
                return <Chip label="Active" size="small" color="success" />;
            }
        },
        { 
            field: 'mfaEnabled', 
            headerName: 'MFA', 
            width: 80,
            renderCell: (params: GridRenderCellParams) => (
                params.value ? 
                    <CheckCircleIcon color="success" fontSize="small" /> : 
                    <CancelIcon color="disabled" fontSize="small" />
            )
        },
        { 
            field: 'lastLogin', 
            headerName: 'Last Login', 
            width: 180,
            renderCell: (params: GridRenderCellParams) => formatDate(params.value as string)
        },
        {
            field: 'createdAt',
            headerName: 'Created',
            width: 180,
            renderCell: (params: GridRenderCellParams) => formatDate(params.value as string)
        },
        {
            field: 'hasApiKey',
            headerName: 'API Key',
            width: 100,
            renderCell: (params: GridRenderCellParams) => {
                const user = params.row as User;
                if (user.hasApiKey) {
                    return <Chip label="Active" size="small" color="success" />;
                }
                return <Chip label="None" size="small" color="default" />;
            }
        },
        {
            field: 'apiKeyLastUsed',
            headerName: 'Last API Use',
            width: 180,
            renderCell: (params: GridRenderCellParams) => formatDate(params.value as string)
        },
        {
            field: 'actions',
            headerName: 'Actions',
            width: 180,
            sortable: false,
            renderCell: (params: GridRenderCellParams) => {
                const user = params.row as User;
                const isLoading = actionLoading === user.id;
                const isSystemUser = user.role === 'system';

                return (
                    <Box>
                        <Tooltip title="Edit User">
                            <IconButton
                                size="small"
                                onClick={() => navigate(`/admin/users/${user.id}`)}
                                disabled={isLoading}
                            >
                                <EditIcon fontSize="small" />
                            </IconButton>
                        </Tooltip>

                        {user.accountEnabled ? (
                            <Tooltip title="Disable User">
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
                            <Tooltip title="Enable User">
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

                        <Tooltip title={isSystemUser ? "Cannot delete system users" : "Delete User"}>
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
                        User Management
                    </Typography>
                    <Typography variant="body1" color="text.secondary">
                        Manage user accounts and permissions
                    </Typography>
                </Box>
                <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={() => setCreateDialogOpen(true)}
                >
                    Add User
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
                <DialogTitle>Create New User</DialogTitle>
                <DialogContent>
                    <Box sx={{ mt: 2 }}>
                        <TextField
                            fullWidth
                            label="Username"
                            value={formData.username}
                            onChange={(e) => setFormData({ ...formData, username: e.target.value })}
                            error={!!formErrors.username}
                            helperText={formErrors.username}
                            margin="normal"
                            required
                        />
                        <TextField
                            fullWidth
                            label="Email"
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
                            label="Password"
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
                            label="Confirm Password"
                            type="password"
                            value={formData.confirmPassword}
                            onChange={(e) => setFormData({ ...formData, confirmPassword: e.target.value })}
                            error={!!formErrors.confirmPassword}
                            helperText={formErrors.confirmPassword}
                            margin="normal"
                            required
                        />
                        <FormControl fullWidth margin="normal" required>
                            <InputLabel>Role</InputLabel>
                            <Select
                                value={formData.role}
                                onChange={(e) => setFormData({ ...formData, role: e.target.value })}
                                label="Role"
                            >
                                <MenuItem value="user">User</MenuItem>
                                <MenuItem value="admin">Admin</MenuItem>
                            </Select>
                            <FormHelperText>Select user's role in the system</FormHelperText>
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
                        Cancel
                    </Button>
                    <Button 
                        onClick={handleCreateUser}
                        variant="contained"
                        disabled={createLoading}
                    >
                        {createLoading ? <CircularProgress size={24} /> : 'Create'}
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
                <DialogTitle>Disable User Account</DialogTitle>
                <DialogContent>
                    <Box sx={{ mt: 2 }}>
                        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                            Please provide a reason for disabling this user account. This will be logged for audit purposes.
                        </Typography>
                        <TextField
                            fullWidth
                            label="Reason for Disabling"
                            value={disableReason}
                            onChange={(e) => setDisableReason(e.target.value)}
                            multiline
                            rows={3}
                            required
                            placeholder="e.g., Account compromise, Terms violation, User request, etc."
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
                        Cancel
                    </Button>
                    <Button
                        onClick={handleDisableUser}
                        variant="contained"
                        color="error"
                        disabled={!disableReason.trim()}
                    >
                        Disable User
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
                <DialogTitle>Delete User Account</DialogTitle>
                <DialogContent>
                    <Box sx={{ mt: 2 }}>
                        <Alert severity="warning" sx={{ mb: 2 }}>
                            This action cannot be undone. The user will be permanently removed from the system.
                        </Alert>
                        <Typography variant="body1">
                            Are you sure you want to delete the user <strong>{deleteUsername}</strong>?
                        </Typography>
                        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                            All associated data will be preserved, but the user will no longer be able to log in.
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
                        Cancel
                    </Button>
                    <Button
                        onClick={handleDeleteUser}
                        variant="contained"
                        color="error"
                        disabled={actionLoading === deleteUserId}
                    >
                        {actionLoading === deleteUserId ? <CircularProgress size={24} /> : 'Delete User'}
                    </Button>
                </DialogActions>
            </Dialog>
        </Box>
    );
};

export default UserList;