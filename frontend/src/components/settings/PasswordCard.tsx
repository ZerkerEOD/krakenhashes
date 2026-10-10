import React, { useEffect, useState } from 'react';
import { Alert, Box, Button, CircularProgress, Grid, TextField } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { updateUserProfile } from '../../services/user';
import { getPasswordPolicy } from '../../services/auth';
import { PasswordPolicy } from '../../types/auth';
import PasswordValidation from '../common/PasswordValidation';
import { useToast } from '../ui/toast';
import SectionCard from '../ui/SectionCard';

interface PasswordChangeForm {
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

const EMPTY: PasswordChangeForm = { currentPassword: '', newPassword: '', confirmPassword: '' };

/** Change password against the server's password policy. */
const PasswordCard: React.FC = () => {
  const { t } = useTranslation('settings');
  const toast = useToast();
  const [form, setForm] = useState<PasswordChangeForm>(EMPTY);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [policy, setPolicy] = useState<PasswordPolicy | null>(null);

  useEffect(() => {
    getPasswordPolicy()
      .then(setPolicy)
      .catch((err) => console.error('Failed to load password policy:', err));
  }, []);

  const meetsPolicy = (password: string): boolean => {
    if (!policy) return false;
    return (
      password.length >= (policy.minPasswordLength || 15) &&
      (!policy.requireUppercase || /[A-Z]/.test(password)) &&
      (!policy.requireLowercase || /[a-z]/.test(password)) &&
      (!policy.requireNumbers || /[0-9]/.test(password)) &&
      (!policy.requireSpecialChars || /[!@#$%^&*(),.?":{}|<>]/.test(password))
    );
  };

  const set = (field: keyof PasswordChangeForm) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((prev) => ({ ...prev, [field]: e.target.value }));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      if (!form.currentPassword) throw new Error(t('password.errors.currentRequired') as string);
      if (!form.newPassword) throw new Error(t('password.errors.newRequired') as string);
      if (form.newPassword !== form.confirmPassword) throw new Error(t('password.errors.mismatch') as string);
      if (!meetsPolicy(form.newPassword)) throw new Error(t('password.errors.requirements') as string);
      setLoading(true);
      await updateUserProfile({ currentPassword: form.currentPassword, newPassword: form.newPassword });
      toast.success(t('password.success.changed') as string);
      setForm(EMPTY);
    } catch (err: any) {
      setError(err?.response || err?.message || (t('password.errors.updateFailed') as string));
    } finally {
      setLoading(false);
    }
  };

  const mismatch = Boolean(form.confirmPassword) && form.newPassword !== form.confirmPassword;

  return (
    <SectionCard title={t('password.title') as string}>
      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
          {String(error)}
        </Alert>
      )}
      <form onSubmit={submit}>
        <Grid container spacing={2}>
          <Grid item xs={12}>
            <TextField fullWidth type="password" autoComplete="current-password" label={t('password.currentPassword') as string} value={form.currentPassword} onChange={set('currentPassword')} />
          </Grid>
          <Grid item xs={12} sm={6}>
            <TextField
              fullWidth
              type="password"
              autoComplete="new-password"
              label={t('password.newPassword') as string}
              value={form.newPassword}
              onChange={set('newPassword')}
              disabled={!form.currentPassword}
              helperText={!form.currentPassword ? (t('password.enterCurrentFirst') as string) : ''}
            />
            {form.newPassword && <PasswordValidation password={form.newPassword} />}
          </Grid>
          <Grid item xs={12} sm={6}>
            <TextField
              fullWidth
              type="password"
              autoComplete="new-password"
              label={t('password.confirmNewPassword') as string}
              value={form.confirmPassword}
              onChange={set('confirmPassword')}
              disabled={!form.currentPassword}
              error={mismatch}
              helperText={mismatch ? (t('password.errors.mismatch') as string) : form.confirmPassword && !mismatch ? (t('password.passwordsMatch') as string) : ''}
            />
          </Grid>
        </Grid>
        <Box sx={{ mt: 2, display: 'flex', justifyContent: 'flex-end' }}>
          <Button
            type="submit"
            variant="contained"
            disabled={loading || !form.currentPassword || !form.newPassword || mismatch}
            startIcon={loading ? <CircularProgress size={18} color="inherit" /> : undefined}
          >
            {loading ? (t('password.changing') as string) : (t('password.changePassword') as string)}
          </Button>
        </Box>
      </form>
    </SectionCard>
  );
};

export default PasswordCard;
