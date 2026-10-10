import React from 'react';
import { Alert, Grid } from '@mui/material';
import { useTranslation } from 'react-i18next';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import { NumberSetting, Panel, SliderSetting } from '../../../../components/settings/fields';

/** How agents fetch wordlists, rules and binaries from the server. */
const DownloadsSection: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = (k: string) => t(k) as string;
  return (
    <SystemSettingsProvider>
      <Grid container spacing={3}>
        <Panel title={tr('agentDownloads.title')} caption={tr('agentDownloads.description')}>
          <Grid item xs={12} md={6}>
            <SliderSetting
              settingKey="agent_max_concurrent_downloads"
              label={tr('agentDownloads.concurrentDownloads.title')}
              helper={tr('agentDownloads.concurrentDownloads.description')}
              min={1}
              max={10}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SliderSetting
              settingKey="agent_download_retry_attempts"
              label={tr('agentDownloads.retryAttempts.title')}
              helper={tr('agentDownloads.retryAttempts.description')}
              min={0}
              max={10}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="agent_download_timeout_minutes"
              label={tr('agentDownloads.downloadTimeout.title')}
              helper={tr('agentDownloads.downloadTimeout.helper')}
              min={1}
              max={1440}
              unit={tr('agentDownloads.downloadTimeout.unit')}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="agent_download_progress_interval_seconds"
              label={tr('agentDownloads.progressInterval.title')}
              helper={tr('agentDownloads.progressInterval.helper')}
              min={1}
              max={300}
              unit={tr('agentDownloads.progressInterval.unit')}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="agent_download_chunk_size_mb"
              label={tr('agentDownloads.chunkSize.title')}
              helper={tr('agentDownloads.chunkSize.helper')}
              min={1}
              max={100}
              unit={tr('agentDownloads.chunkSize.unit')}
            />
          </Grid>
          <Grid item xs={12}>
            <Alert severity="info">{tr('agentDownloads.applyNote')}</Alert>
          </Grid>
        </Panel>
      </Grid>
    </SystemSettingsProvider>
  );
};

export default DownloadsSection;
