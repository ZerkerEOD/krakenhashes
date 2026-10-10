import React from 'react';
import { Alert, Grid } from '@mui/material';
import { useTranslation } from 'react-i18next';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import { DependentFields, NumberSetting, Panel, SliderSetting, SwitchSetting, boolValueOf, useSettingsCtx } from '../../../../components/settings/fields';

const Fields: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = (k: string) => t(k) as string;
  const { values } = useSettingsCtx();
  const enabled = boolValueOf(values, 'agent_auto_update_enabled');
  return (
    <Grid container spacing={3}>
      <Panel title={tr('agentAutoUpdate.title')} caption={tr('agentAutoUpdate.description')}>
        <Grid item xs={12}>
          <SwitchSetting settingKey="agent_auto_update_enabled" label={tr('agentAutoUpdate.enable')} />
        </Grid>
        <DependentFields enabled={enabled}>
          <Grid item xs={12} md={6}>
            <SliderSetting
              settingKey="agent_update_max_concurrent"
              label={tr('agentAutoUpdate.maxConcurrent')}
              helper={tr('agentAutoUpdate.maxConcurrentHelper')}
              min={1}
              max={10}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <SliderSetting
              settingKey="agent_update_max_attempts"
              label={tr('agentAutoUpdate.maxAttempts')}
              helper={tr('agentAutoUpdate.maxAttemptsHelper')}
              min={1}
              max={10}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <NumberSetting
              settingKey="agent_update_health_timeout_seconds"
              label={tr('agentAutoUpdate.healthTimeout')}
              helper={tr('agentAutoUpdate.healthTimeoutHelper')}
              min={60}
              max={3600}
              unit="s"
            />
          </Grid>
        </DependentFields>
        <Grid item xs={12}>
          <Alert severity="info">{tr('agentAutoUpdate.applyNote')}</Alert>
        </Grid>
      </Panel>
    </Grid>
  );
};

/** Automatic agent binary updates. */
const UpdatesSection: React.FC = () => (
  <SystemSettingsProvider>
    <Fields />
  </SystemSettingsProvider>
);

export default UpdatesSection;
