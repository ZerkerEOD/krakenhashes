import React from 'react';
import { Alert } from '@mui/material';
import { SettingsCtx, SettingsLoading, useSystemSettingsForm } from './fields';

/**
 * Loads the system settings once and provides the autosave context to every
 * field rendered inside. Sections that only use system-setting keys wrap
 * their panels in this.
 */
const SystemSettingsProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { error, clearError, reload: _reload, ...ctx } = useSystemSettingsForm();
  if (ctx.loading) return <SettingsLoading />;
  return (
    <SettingsCtx.Provider value={ctx}>
      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={clearError}>
          {error}
        </Alert>
      )}
      {children}
    </SettingsCtx.Provider>
  );
};

export default SystemSettingsProvider;
