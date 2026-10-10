import React from 'react';
import { Grid } from '@mui/material';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import { LoopbackPanel, PotfilePanel } from '../../../../components/admin/jobExecutionPanels';

/** Potfile behaviour and loopback rounds. */
const PotfileSection: React.FC = () => (
  <SystemSettingsProvider>
    <Grid container spacing={3}>
      <PotfilePanel />
      <LoopbackPanel />
    </Grid>
  </SystemSettingsProvider>
);

export default PotfileSection;
