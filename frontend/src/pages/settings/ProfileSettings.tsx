import React, { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Button,
  Grid,
  Alert,
  CircularProgress,
  Divider,
} from '@mui/material';
import { useAuth } from '../../contexts/AuthContext';
import { updateUserProfile, ProfileUpdate } from '../../services/user';
import { getPasswordPolicy } from '../../services/auth';
import { PasswordPolicy } from '../../types/auth';
import PasswordValidation from '../../components/common/PasswordValidation';
import MFACard from '../../components/settings/MFACard';
import NotificationCard from '../../components/settings/NotificationCard';
import { usePasswordConfirm } from '../../hooks/usePasswordConfirm';

interface PasswordChangeForm {
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

const ProfileSettings: React.FC = () => {
  const { user, setUser } = useAuth();
  const { showPasswordConfirm, PasswordConfirmDialog } = usePasswordConfirm();

  // Email update state
  const [email, setEmail] = useState(user?.email || '');
  const [emailLoading, setEmailLoading] = useState(false);
  const [emailError, setEmailError] = useState<string | null>(null);
  const [emailSuccess, setEmailSuccess] = useState<string | null>(null);

  // Password change state
  const [passwordForm, setPasswordForm] = useState<PasswordChangeForm>({
    currentPassword: '',
    newPassword: '',
    confirmPassword: '',
  });
  const [passwordLoading, setPasswordLoading] = useState(false);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [passwordSuccess, setPasswordSuccess] = useState<string | null>(null);

  const [policy, setPolicy] = useState<PasswordPolicy | null>(null);

  useEffect(() => {
    const loadPolicy = async () => {
      try {
        const policyData = await getPasswordPolicy();
        setPolicy(policyData);
      } catch (error) {
        console.error('Failed to load password policy:', error);
      }
    };
    loadPolicy();
  }, []);

  const validatePassword = (password: string): boolean => {
    if (!policy) return false;

    const validation = {
      length: password.length >= (policy.minPasswordLength || 15),
      uppercase: !policy.requireUppercase || /[A-Z]/.test(password),
      lowercase: !policy.requireLowercase || /[a-z]/.test(password),
      numbers: !policy.requireNumbers || /[0-9]/.test(password),
      specialChars: !policy.requireSpecialChars || /[!@#$%^&*(),.?":{}|<>]/.test(password),
    };

    return Object.values(validation).every(Boolean);
  };

  const handlePasswordChange = (field: keyof PasswordChangeForm) => (event: React.ChangeEvent<HTMLInputElement>) => {
    setPasswordForm(prev => ({ ...prev, [field]: event.target.value }));
  };

  const handleEmailUpdate = async () => {
    if (!email || email === user?.email) {
      setEmailError('No changes to save');
      return;
    }

    // Basic email validation
    if (!email.includes('@')) {
      setEmailError('Invalid email address');
      return;
    }

    setEmailError(null);
    setEmailSuccess(null);

    // Show password confirmation dialog
    const password = await showPasswordConfirm(
      'Confirm Email Update',
      'Please enter your current password to update your email address.'
    );

    if (!password) {
      return; // User cancelled
    }

    setEmailLoading(true);

    try {
      const updates: ProfileUpdate = {
        email: email,
        currentPassword: password,
      };

      await updateUserProfile(updates);
      setEmailSuccess('Email updated successfully');

      if (setUser && user) {
        setUser({ ...user, email: email });
      }
    } catch (error: any) {
      const errorMessage = error?.response || error?.message || 'Failed to update email';
      if (errorMessage.includes('password')) {
        setEmailError('Incorrect password');
      } else {
        setEmailError(errorMessage);
      }
    } finally {
      setEmailLoading(false);
    }
  };

  const handlePasswordUpdate = async (event: React.FormEvent) => {
    event.preventDefault();
    setPasswordLoading(true);
    setPasswordError(null);
    setPasswordSuccess(null);

    try {
      if (!passwordForm.currentPassword) {
        throw new Error('Current password is required');
      }

      if (!passwordForm.newPassword) {
        throw new Error('New password is required');
      }

      if (passwordForm.newPassword !== passwordForm.confirmPassword) {
        throw new Error('New passwords do not match');
      }

      if (!validatePassword(passwordForm.newPassword)) {
        throw new Error('New password does not meet requirements');
      }

      const updates: ProfileUpdate = {
        currentPassword: passwordForm.currentPassword,
        newPassword: passwordForm.newPassword,
      };

      await updateUserProfile(updates);
      setPasswordSuccess('Password changed successfully');

      // Clear password fields after successful update
      setPasswordForm({
        currentPassword: '',
        newPassword: '',
        confirmPassword: '',
      });
    } catch (error: any) {
      const errorMessage = error?.response || error?.message || 'Failed to update password';
      setPasswordError(errorMessage);
    } finally {
      setPasswordLoading(false);
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h4" gutterBottom>
        Profile Settings
      </Typography>

      <PasswordConfirmDialog />

      {/* Account Information Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Account Information
          </Typography>

          {emailError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {emailError}
            </Alert>
          )}

          {emailSuccess && (
            <Alert severity="success" sx={{ mb: 2 }}>
              {emailSuccess}
            </Alert>
          )}

          <Grid container spacing={2}>
            <Grid item xs={12} sm={6}>
              <TextField
                fullWidth
                label="Username"
                value={user?.username || ''}
                disabled
                margin="normal"
                helperText="Username cannot be changed"
              />
            </Grid>
            <Grid item xs={12} sm={6}>
              <TextField
                fullWidth
                label="Email"
                value={email}
                onChange={(e) => {
                  setEmail(e.target.value);
                  setEmailError(null);
                  setEmailSuccess(null);
                }}
                type="email"
                margin="normal"
              />
            </Grid>
          </Grid>

          <Box sx={{ mt: 2, display: 'flex', justifyContent: 'flex-end' }}>
            <Button
              variant="contained"
              color="primary"
              onClick={handleEmailUpdate}
              disabled={emailLoading || email === user?.email}
              startIcon={emailLoading && <CircularProgress size={20} color="inherit" />}
            >
              {emailLoading ? 'Saving...' : 'Save Email'}
            </Button>
          </Box>
        </CardContent>
      </Card>

      {/* Change Password Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Change Password
          </Typography>

          {passwordError && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {passwordError}
            </Alert>
          )}

          {passwordSuccess && (
            <Alert severity="success" sx={{ mb: 2 }}>
              {passwordSuccess}
            </Alert>
          )}

          <form onSubmit={handlePasswordUpdate}>
            <Grid container spacing={2}>
              <Grid item xs={12}>
                <TextField
                  fullWidth
                  label="Current Password"
                  value={passwordForm.currentPassword}
                  onChange={handlePasswordChange('currentPassword')}
                  type="password"
                  margin="normal"
                />
              </Grid>
              <Grid item xs={12} sm={6}>
                <TextField
                  fullWidth
                  label="New Password"
                  value={passwordForm.newPassword}
                  onChange={handlePasswordChange('newPassword')}
                  type="password"
                  margin="normal"
                  disabled={!passwordForm.currentPassword}
                  helperText={!passwordForm.currentPassword ? "Enter current password first" : ""}
                />
                {passwordForm.newPassword && (
                  <PasswordValidation password={passwordForm.newPassword} />
                )}
              </Grid>
              <Grid item xs={12} sm={6}>
                <TextField
                  fullWidth
                  label="Confirm New Password"
                  value={passwordForm.confirmPassword}
                  onChange={handlePasswordChange('confirmPassword')}
                  type="password"
                  margin="normal"
                  disabled={!passwordForm.currentPassword}
                  error={passwordForm.newPassword !== passwordForm.confirmPassword && passwordForm.confirmPassword !== ''}
                  helperText={
                    !passwordForm.currentPassword ? "Enter current password first" :
                    passwordForm.confirmPassword !== '' && (
                      passwordForm.newPassword !== passwordForm.confirmPassword
                        ? 'Passwords do not match'
                        : passwordForm.newPassword === passwordForm.confirmPassword
                          ? 'Passwords match'
                          : ''
                    )
                  }
                  FormHelperTextProps={{
                    sx: {
                      color: passwordForm.confirmPassword !== '' && passwordForm.newPassword === passwordForm.confirmPassword
                        ? 'success.main'
                        : 'error.main'
                    }
                  }}
                />
              </Grid>
            </Grid>

            <Box sx={{ mt: 3, display: 'flex', justifyContent: 'flex-end' }}>
              <Button
                variant="contained"
                color="primary"
                type="submit"
                disabled={passwordLoading}
                startIcon={passwordLoading && <CircularProgress size={20} color="inherit" />}
              >
                {passwordLoading ? 'Changing Password...' : 'Change Password'}
              </Button>
            </Box>
          </form>
        </CardContent>
      </Card>

      <MFACard onMFAChange={() => {
        // Refresh user data when MFA settings change
        if (setUser && user) {
          setUser({ ...user });
        }
      }} />

      <NotificationCard onNotificationChange={() => {
        // You can add any refresh logic here if needed
        console.log('Notification preferences updated');
      }} />
    </Box>
  );
}

export default ProfileSettings; 