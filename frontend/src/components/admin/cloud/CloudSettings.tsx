import React, { useState } from 'react';
import { Alert, AlertTitle, Box, Divider, Tab, Tabs } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { Link as RouterLink } from 'react-router-dom';
import CloudProviderSettings from './CloudProviderSettings';
import CloudClientBudgets from './CloudClientBudgets';
import CloudBudgetPolicySettings from './CloudBudgetPolicySettings';
import CloudProvisioningRulesSettings from './CloudProvisioningRulesSettings';
import CloudSystemSettings from './CloudSystemSettings';

/**
 * Cloud GPU provisioning admin tab.
 *
 * Five concerns, in the order an operator sets them up: where instances come
 * from, what each client may spend, what happens as a cap is approached, when
 * the system is allowed to spend at all, and the system-wide ceilings and
 * timings underneath all of it.
 *
 * The last two are separate tabs because they answer different questions. The
 * budget ladder is HOW MUCH; the rules are WHEN — priority floors, starvation
 * delays, per-job ceilings and time windows. Merging them would put an
 * autoscaler tuning knob next to a hard spending cap and invite the two to be
 * read as the same kind of setting.
 *
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
        <Tab label={t('cloud.sections.rules') as string} />
        <Tab label={t('cloud.sections.system') as string} />
      </Tabs>
      <Divider sx={{ mb: 3 }} />

      {section === 0 && <CloudProviderSettings />}
      {section === 1 && <CloudClientBudgets />}
      {section === 2 && <CloudBudgetPolicySettings />}
      {section === 3 && <CloudProvisioningRulesSettings />}
      {section === 4 && <CloudSystemSettings />}
    </Box>
  );
};

export default CloudSettings;
