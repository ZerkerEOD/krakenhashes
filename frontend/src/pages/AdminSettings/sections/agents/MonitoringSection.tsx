import React from 'react';
import { Alert, Grid, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import { DependentFields, NumberSetting, Panel, SelectSetting, SwitchSetting, boolValueOf, useSettingsCtx } from '../../../../components/settings/fields';

const Fields: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = (k: string) => t(k) as string;
  const { values } = useSettingsCtx();
  const aggregation = boolValueOf(values, 'enable_aggregation');
  return (
    <Grid container spacing={3}>
      <Panel title={tr('monitoring.metricsRetention.title')} caption={tr('monitoring.metricsRetention.description')}>
        <Grid item xs={12} md={4}>
          <NumberSetting settingKey="metrics_retention_realtime_days" label={tr('monitoring.metricsRetention.realtimeData')} helper={tr('monitoring.metricsRetention.realtimeDataHelper')} min={0} max={3650} unit="d" />
        </Grid>
        <Grid item xs={12} md={4}>
          <NumberSetting settingKey="metrics_retention_daily_days" label={tr('monitoring.metricsRetention.dailyAggregates')} helper={tr('monitoring.metricsRetention.dailyAggregatesHelper')} min={0} max={3650} unit="d" />
        </Grid>
        <Grid item xs={12} md={4}>
          <NumberSetting settingKey="metrics_retention_weekly_days" label={tr('monitoring.metricsRetention.weeklyAggregates')} helper={tr('monitoring.metricsRetention.weeklyAggregatesHelper')} min={0} max={3650} unit="d" />
        </Grid>
      </Panel>
      <Panel title={tr('monitoring.aggregation.title')}>
        <Grid item xs={12} md={6}>
          <SwitchSetting settingKey="enable_aggregation" label={tr('monitoring.aggregation.enableAggregation')} helper={tr('monitoring.aggregation.enableAggregationDescription')} />
        </Grid>
        <Grid item xs={12} md={6}>
          <DependentFields enabled={aggregation}>
            <SelectSetting
              settingKey="aggregation_interval"
              label={tr('monitoring.aggregation.interval')}
              helper={tr('monitoring.aggregation.intervalHelper')}
              options={[
                { value: 'hourly', label: tr('monitoring.aggregation.hourly') },
                { value: 'daily', label: tr('monitoring.aggregation.daily') },
                { value: 'weekly', label: tr('monitoring.aggregation.weekly') },
              ]}
            />
          </DependentFields>
        </Grid>
        <Grid item xs={12}>
          <Alert severity="info">
            <Typography variant="body2">
              <strong>{tr('monitoring.cascading.title')}</strong>
              <br />• {tr('monitoring.cascading.realtime')}
              <br />• {tr('monitoring.cascading.daily')}
              <br />• {tr('monitoring.cascading.weekly')}
              <br />• {tr('monitoring.cascading.benefit')}
            </Typography>
          </Alert>
        </Grid>
      </Panel>
    </Grid>
  );
};

/** Metrics retention and aggregation. */
const MonitoringSection: React.FC = () => (
  <SystemSettingsProvider>
    <Fields />
  </SystemSettingsProvider>
);

export default MonitoringSection;
