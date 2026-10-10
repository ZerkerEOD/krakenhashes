import React from 'react';
import { Grid } from '@mui/material';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import { BenchmarkReliabilityPanel, ChunkingPanel, KeyspaceBenchmarkPanel } from '../../../../components/admin/jobExecutionPanels';

/** How a job is cut into chunks and how keyspace/benchmarks are measured. */
const ExecutionSection: React.FC = () => (
  <SystemSettingsProvider>
    <Grid container spacing={3}>
      <ChunkingPanel />
      <KeyspaceBenchmarkPanel />
      <BenchmarkReliabilityPanel />
    </Grid>
  </SystemSettingsProvider>
);

export default ExecutionSection;
