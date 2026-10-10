import React, { useState } from 'react';
import { Box, Button, CircularProgress, Grid, TextField } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../../contexts/AuthContext';
import { updateUserProfile } from '../../services/user';
import { usePasswordConfirm } from '../../hooks/usePasswordConfirm';
import { useToast } from '../ui/toast';
import SectionCard from '../ui/SectionCard';

/** Username (read-only) and email, changed with a password confirmation. */
const AccountCard: React.FC = () => {
  const { t } = useTranslation('settings');
  const { user, setUser } = useAuth();
  const toast = useToast();
  const { showPasswordConfirm, PasswordConfirmDialog } = usePasswordConfirm();
  const [email, setEmail] = useState(user?.email || '');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    if (!email || email === user?.email) {
      setError(t('account.errors.noChanges') as string);
      return;
    }
    if (!email.includes('@')) {
      setError(t('account.errors.invalidEmail') as string);
      return;
    }
    setError(null);
    const password = await showPasswordConfirm(
      t('account.confirmEmailUpdate.title') as string,
      t('account.confirmEmailUpdate.message') as string
    );
    if (!password) return;
    setLoading(true);
    try {
      await updateUserProfile({ email, currentPassword: password });
      toast.success(t('account.success.emailUpdated') as string);
      if (setUser && user) setUser({ ...user, email });
    } catch (err: any) {
      const message: string = err?.response || err?.message || (t('account.errors.updateFailed') as string);
      setError(String(message).includes('password') ? (t('account.errors.incorrectPassword') as string) : String(message));
    } finally {
      setLoading(false);
    }
  };

  return (
    <SectionCard title={t('account.title') as string}>
      <PasswordConfirmDialog />
      <Grid container spacing={2}>
        <Grid item xs={12} sm={6}>
          <TextField fullWidth label={t('account.username') as string} value={user?.username || ''} disabled helperText={t('account.usernameCannotChange') as string} />
        </Grid>
        <Grid item xs={12} sm={6}>
          <TextField
            fullWidth
            type="email"
            label={t('account.email') as string}
            value={email}
            onChange={(e) => {
              setEmail(e.target.value);
              setError(null);
            }}
            error={Boolean(error)}
            helperText={error ?? undefined}
          />
        </Grid>
      </Grid>
      <Box sx={{ mt: 2, display: 'flex', justifyContent: 'flex-end' }}>
        <Button
          variant="contained"
          onClick={save}
          disabled={loading || email === user?.email}
          startIcon={loading ? <CircularProgress size={18} color="inherit" /> : undefined}
        >
          {loading ? (t('account.saving') as string) : (t('account.saveEmail') as string)}
        </Button>
      </Box>
    </SectionCard>
  );
};

export default AccountCard;
