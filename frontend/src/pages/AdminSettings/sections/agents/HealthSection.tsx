import React from 'react';
import { Grid } from '@mui/material';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import { TaskHealthPanel } from '../../../../components/admin/jobExecutionPanels';

/**
 * Heartbeats, grace periods, retries and the agent-offline buffer. This is the
 * single home of `agent_offline_buffer_minutes` (it used to be editable from
 * two tabs).
 */
const HealthSection: React.FC = () => (
  <SystemSettingsProvider>
    <Grid container spacing={3}>
      <TaskHealthPanel />
    </Grid>
  </SystemSettingsProvider>
);

export default HealthSection;
