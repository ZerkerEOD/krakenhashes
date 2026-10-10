import React from 'react';
import { Box, Paper, Tab, Tabs } from '@mui/material';
import { Navigate, Route, Routes, useLocation, useNavigate, useResolvedPath } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { ProviderConfig } from './ProviderConfig';
import { TemplateEditor } from './TemplateEditor';

/**
 * Email integration: provider configuration and templates. The two tabs are
 * URL-routed (`…/email` and `…/email/templates`) so a reload or a shared link
 * lands on the right one; nothing is kept in localStorage any more.
 */
export const EmailSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const navigate = useNavigate();
  const location = useLocation();
  const base = useResolvedPath('.').pathname.replace(/\/$/, '');
  const onTemplates = location.pathname.startsWith(`${base}/templates`);

  return (
    <Box>
      <Paper variant="outlined" sx={{ width: '100%' }}>
        <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
          <Tabs
            value={onTemplates ? 'templates' : 'provider'}
            onChange={(_e, v: string) => navigate(v === 'templates' ? `${base}/templates` : base)}
            aria-label="email settings tabs"
          >
            <Tab value="provider" label={t('emailSettings.tabs.providerConfiguration') as string} />
            <Tab value="templates" label={t('emailSettings.tabs.emailTemplates') as string} />
          </Tabs>
        </Box>
        <Box sx={{ p: 3 }}>
          <Routes>
            <Route index element={<ProviderConfig />} />
            <Route path="provider" element={<Navigate to={base} replace />} />
            <Route path="templates" element={<TemplateEditor />} />
            <Route path="*" element={<Navigate to={base} replace />} />
          </Routes>
        </Box>
      </Paper>
    </Box>
  );
};

export default EmailSettings;
