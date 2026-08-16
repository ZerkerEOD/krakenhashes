import React, { useState } from 'react';
import { Alert, AlertTitle, Box, Divider, Tab, Tabs } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { Link as RouterLink } from 'react-router-dom';
import CloudProviderSettings from './CloudProviderSettings';
import CloudClientBudgets from './CloudClientBudgets';
import CloudBudgetPolicySettings from './CloudBudgetPolicySettings';

/**
 * Cloud GPU provisioning admin tab.
 *
 * Three concerns, in the order an operator sets them up: where instances come
 * from, what each client may spend, and what happens as a cap is approached.
 * The live fleet lives on its own page since it needs the full width and polls.
 */
const CloudSettings: React.FC = () => {
  const { t } = useTranslation('admin');
  const [section, setSection] = useState(0);

  return (
    <Box>
      <Alert severity="info" sx={{ mb: 3 }}>
        <AlertTitle>{t('cloud.intro.title') as string}</AlertTitle>
        {t('cloud.intro.body') as string}
        <Box sx={{ mt: 1 }}>
          <RouterLink to="/admin/cloud/fleet">{t('cloud.intro.fleetLink') as string}</RouterLink>
        </Box>
      </Alert>

      <Tabs value={section} onChange={(_e, v) => setSection(v)} sx={{ mb: 2 }}>
        <Tab label={t('cloud.sections.providers') as string} />
        <Tab label={t('cloud.sections.clients') as string} />
        <Tab label={t('cloud.sections.policy') as string} />
      </Tabs>
      <Divider sx={{ mb: 3 }} />

      {section === 0 && <CloudProviderSettings />}
      {section === 1 && <CloudClientBudgets />}
      {section === 2 && <CloudBudgetPolicySettings />}
    </Box>
  );
};

export default CloudSettings;
