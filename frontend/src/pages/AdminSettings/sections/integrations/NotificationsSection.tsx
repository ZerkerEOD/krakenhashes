import React from 'react';
import { Grid } from '@mui/material';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import { JobNotificationsPanel } from '../../../../components/admin/jobExecutionPanels';

/** Which job events produce notifications. */
const NotificationsSection: React.FC = () => (
  <SystemSettingsProvider>
    <Grid container spacing={3}>
      <JobNotificationsPanel />
    </Grid>
  </SystemSettingsProvider>
);

export default NotificationsSection;
