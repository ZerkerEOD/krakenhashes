import React, { useEffect, useState } from 'react';
import { Alert, Grid } from '@mui/material';
import { useTranslation } from 'react-i18next';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import {
  MoneySetting,
  NumberSetting,
  Panel,
  SelectSetting,
  SwitchSetting,
  TextSetting,
  numberValueOf,
  useSettingsCtx,
} from '../../../../components/settings/fields';
import { listClients } from '../../../../services/api';

/**
 * System-wide cloud ceilings and timings. Each key saves itself; the one
 * that matters most is the monthly cap, where 0 means provisioning is OFF.
 */
const Fields: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = (k: string) => t(k) as string;
  const { values } = useSettingsCtx();
  const [clients, setClients] = useState<Array<{ value: string; label: string }>>([]);

  useEffect(() => {
    let cancelled = false;
    listClients()
      .then((res) => {
        if (cancelled) return;
        const rows = res.data?.data ?? [];
        setClients(rows.map((c: any) => ({ value: String(c.id), label: c.name })));
      })
      .catch(() => {
        /* picker stays empty; the rest of the section still works */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const capCents = numberValueOf(values, 'cloud_global_monthly_cap_cents', 0);
  const seconds = tr('cloud.system.units.seconds');
  const minutes = tr('cloud.system.units.minutes');

  return (
    <Grid container spacing={3}>
      {capCents <= 0 && (
        <Grid item xs={12}>
          <Alert severity="warning">{tr('cloud.system.capDisabledWarning')}</Alert>
        </Grid>
      )}
      <Panel title={tr('cloud.system.ceilings')} caption={tr('cloud.system.ceilingsHelp')}>
        <Grid item xs={12} md={4}>
          <MoneySetting settingKey="cloud_global_monthly_cap_cents" label={tr('cloud.system.fields.monthlyCap')} helper={tr('cloud.system.fields.monthlyCapHelp')} />
        </Grid>
        <Grid item xs={12} md={4}>
          <NumberSetting settingKey="cloud_global_concurrent_instance_cap" label={tr('cloud.system.fields.concurrentCap')} helper={tr('cloud.system.fields.concurrentCapHelp')} min={0} />
        </Grid>
        <Grid item xs={12} md={4}>
          <NumberSetting settingKey="cloud_default_max_instances_per_job" label={tr('cloud.system.fields.defaultMaxInstancesPerJob')} helper={tr('cloud.system.fields.defaultMaxInstancesPerJobHelp')} min={0} />
        </Grid>
      </Panel>
      <Panel title={tr('cloud.system.eligibility')} caption={tr('cloud.system.eligibilityHelp')}>
        <Grid item xs={12} md={6}>
          <SwitchSetting settingKey="cloud_default_burst_enabled" label={tr('cloud.system.fields.defaultBurstEnabled')} helper={tr('cloud.system.fields.defaultBurstEnabledHelp')} />
        </Grid>
        <Grid item xs={12} md={6}>
          <SelectSetting
            settingKey="cloud_default_client_id"
            label={tr('cloud.system.fields.defaultClient')}
            helper={tr('cloud.system.fields.defaultClientHelp')}
            noneLabel={tr('cloud.system.fields.defaultClientNone')}
            options={clients}
          />
        </Grid>
      </Panel>
      <Panel title={tr('cloud.system.agent')} caption={tr('cloud.system.agentHelp')}>
        <Grid item xs={12}>
          <TextSetting settingKey="cloud_agent_image" label={tr('cloud.system.fields.agentImage')} helper={tr('cloud.system.fields.agentImageHelp')} placeholder="zerkereod/krakenhashes-agent-cloud:latest" />
        </Grid>
      </Panel>
      <Panel title={tr('cloud.system.work')} caption={tr('cloud.system.workHelp')}>
        <Grid item xs={12} md={6}>
          <NumberSetting settingKey="cloud_chunk_duration_seconds" label={tr('cloud.system.fields.chunkDuration')} helper={tr('cloud.system.fields.chunkDurationHelp')} min={0} unit={seconds} />
        </Grid>
        <Grid item xs={12} md={6}>
          <NumberSetting settingKey="cloud_teardown_slack_seconds" label={tr('cloud.system.fields.teardownSlack')} helper={tr('cloud.system.fields.teardownSlackHelp')} min={0} unit={seconds} />
        </Grid>
        <Grid item xs={12} md={6}>
          <NumberSetting settingKey="cloud_idle_drain_minutes" label={tr('cloud.system.fields.idleDrain')} helper={tr('cloud.system.fields.idleDrainHelp')} min={0} unit={minutes} />
        </Grid>
        <Grid item xs={12} md={6}>
          <NumberSetting settingKey="cloud_commissioning_grace_minutes" label={tr('cloud.system.fields.commissioningGrace')} helper={tr('cloud.system.fields.commissioningGraceHelp')} min={0} unit={minutes} />
        </Grid>
        <Grid item xs={12} md={6}>
          <NumberSetting settingKey="cloud_crack_drain_grace_minutes" label={tr('cloud.system.fields.crackDrainGrace')} helper={tr('cloud.system.fields.crackDrainGraceHelp')} min={0} unit={minutes} />
        </Grid>
      </Panel>
      <Panel title={tr('cloud.system.reconciliation')} caption={tr('cloud.system.reconciliationHelp')}>
        <Grid item xs={12} md={6}>
          <NumberSetting settingKey="cloud_reaper_interval_seconds" label={tr('cloud.system.fields.reaperInterval')} helper={tr('cloud.system.fields.reaperIntervalHelp')} min={1} unit={seconds} />
        </Grid>
        <Grid item xs={12} md={6}>
          <NumberSetting settingKey="cloud_orphan_grace_minutes" label={tr('cloud.system.fields.orphanGrace')} helper={tr('cloud.system.fields.orphanGraceHelp')} min={0} unit={minutes} />
        </Grid>
      </Panel>
    </Grid>
  );
};

const LimitsSection: React.FC = () => (
  <SystemSettingsProvider>
    <Fields />
  </SystemSettingsProvider>
);

export default LimitsSection;
