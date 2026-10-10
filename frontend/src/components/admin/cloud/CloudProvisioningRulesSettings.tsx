import React, { useMemo } from 'react';
import { Alert, AlertTitle, Grid, Typography } from '@mui/material';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import GroupSettingsProvider from '../../settings/GroupSettingsProvider';
import {
  DependentFields,
  MoneySetting,
  NumberSetting,
  Panel,
  SliderSetting,
  SwitchSetting,
  TextSetting,
} from '../../settings/fields';
import { getDefaultProvisioningRules, updateDefaultProvisioningRules } from '../../../services/cloud';
import { getMaxPriority } from '../../../services/systemSettings';
import { qk } from '../../../services/queryKeys';
import { CloudProvisioningRules } from '../../../types/cloud';

/**
 * The system-default provisioning rules: WHEN the autoscaler may spend, as
 * opposed to the budget ladder's HOW MUCH.
 *
 * Edits the UNMERGED default deliberately. Per-client overrides are edited on
 * the client screen, where the merged view is the useful one.
 *
 * Every field autosaves by merging into the last loaded rules (the endpoint
 * takes the whole object). The provisioning window is a pair: enabling it
 * writes both ends, disabling it clears both, so the DB "pair or nothing"
 * constraint can never be tripped by a single-field save.
 */

/** "HH:MM" for <input type="time"> from the backend's "HH:MM:SS", and back. */
const toTimeInput = (value?: string | null): string => (value ? value.slice(0, 5) : '');
const fromTimeInput = (value: string): string => `${value || '00:00'}:00`;

type RulesGroup = {
  min_job_priority: number;
  min_starvation_seconds: number;
  skip_if_finishing_within_seconds: number;
  max_spend_per_job_cents: number;
  window_enabled: boolean;
  provisioning_window_start: string;
  provisioning_window_end: string;
  provisioning_window_tz: string;
  /** The last loaded rules, used as the merge base. No field binds to it. */
  _raw: CloudProvisioningRules;
};

const loadRules = async (): Promise<RulesGroup> => {
  const rules = await getDefaultProvisioningRules();
  return {
    min_job_priority: rules.min_job_priority ?? 0,
    min_starvation_seconds: rules.min_starvation_seconds ?? 0,
    skip_if_finishing_within_seconds: rules.skip_if_finishing_within_seconds ?? 0,
    max_spend_per_job_cents: rules.max_spend_per_job_cents ?? 0,
    window_enabled: Boolean(rules.provisioning_window_start && rules.provisioning_window_end),
    provisioning_window_start: toTimeInput(rules.provisioning_window_start),
    provisioning_window_end: toTimeInput(rules.provisioning_window_end),
    provisioning_window_tz: rules.provisioning_window_tz ?? '',
    _raw: rules,
  };
};

const saveRuleField = async (key: keyof RulesGroup & string, value: RulesGroup[keyof RulesGroup], current: RulesGroup) => {
  const raw = current._raw;
  const next: Partial<CloudProvisioningRules> = { ...raw };
  switch (key) {
    case 'window_enabled':
      if (value) {
        next.provisioning_window_start = raw.provisioning_window_start || '00:00:00';
        next.provisioning_window_end = raw.provisioning_window_end || '00:00:00';
      } else {
        next.provisioning_window_start = null;
        next.provisioning_window_end = null;
      }
      break;
    case 'provisioning_window_start':
    case 'provisioning_window_end':
      next[key] = fromTimeInput(String(value));
      // Keep the pair complete even if the other end was somehow empty.
      next.provisioning_window_start = next.provisioning_window_start || '00:00:00';
      next.provisioning_window_end = next.provisioning_window_end || '00:00:00';
      break;
    case 'provisioning_window_tz':
      next.provisioning_window_tz = String(value) || null;
      break;
    case '_raw':
      return;
    default:
      (next as Record<string, unknown>)[key] = value;
  }
  return updateDefaultProvisioningRules(next);
};

const RulesFields: React.FC<{ data: RulesGroup; ceiling: number }> = ({ data, ceiling }) => {
  const { t } = useTranslation('admin');
  const tr = (k: string, o?: Record<string, unknown>) => t(k, o) as string;

  /**
   * Band marks scaled to the live ceiling. At 1000 "High" lands on 700; at 100
   * it lands on 70. Rendering fixed 0-100 marks against a 1000 ceiling is
   * exactly how an admin ends up setting a floor that disables everything.
   */
  const marks = useMemo(
    () =>
      [
        { fraction: 0, key: 'minimal' },
        { fraction: 0.1, key: 'low' },
        { fraction: 0.4, key: 'normal' },
        { fraction: 0.7, key: 'high' },
        { fraction: 0.9, key: 'critical' },
      ].map(({ fraction, key }) => ({ value: Math.round(ceiling * fraction), label: tr(`cloud.rules.bands.${key}`) })),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [ceiling, t]
  );

  return (
    <Grid container spacing={3}>
      <Grid item xs={12}>
        <Alert severity="info">
          <AlertTitle>{tr('cloud.rules.inheritTitle')}</AlertTitle>
          {tr('cloud.rules.inheritBody')}
        </Alert>
      </Grid>

      <Panel title={tr('cloud.rules.fields.minJobPriority')} caption={tr('cloud.rules.helperText.minJobPriority', { ceiling })}>
        <Grid item xs={12}>
          <SliderSetting
            settingKey="min_job_priority"
            label={tr('cloud.rules.fields.minJobPriority')}
            helper={data.min_job_priority === 0 ? tr('cloud.rules.helperText.floorOff') : undefined}
            min={0}
            max={ceiling}
            step={Math.max(1, Math.round(ceiling / 100))}
            marks={marks}
          />
        </Grid>
      </Panel>

      <Panel title={tr('cloud.rules.title')}>
        <Grid item xs={12} sm={6}>
          <NumberSetting
            settingKey="min_starvation_seconds"
            label={tr('cloud.rules.fields.minStarvation')}
            helper={tr('cloud.rules.helperText.minStarvation')}
            min={0}
            toDisplay={(seconds) => Math.round(seconds / 60)}
            toStored={(minutes) => Math.round(minutes * 60)}
            unit="min"
          />
        </Grid>
        <Grid item xs={12} sm={6}>
          <NumberSetting
            settingKey="skip_if_finishing_within_seconds"
            label={tr('cloud.rules.fields.skipIfFinishing')}
            helper={tr('cloud.rules.helperText.skipIfFinishing')}
            min={0}
            toDisplay={(seconds) => Math.round(seconds / 60)}
            toStored={(minutes) => Math.round(minutes * 60)}
            unit="min"
          />
        </Grid>
        <Grid item xs={12} sm={6}>
          <MoneySetting
            settingKey="max_spend_per_job_cents"
            label={tr('cloud.rules.fields.maxSpendPerJob')}
            helper={tr('cloud.rules.helperText.maxSpendPerJob')}
          />
        </Grid>
      </Panel>

      <Panel title={tr('cloud.rules.fields.windowEnabled')} caption={tr('cloud.rules.helperText.windowEnabled')}>
        <Grid item xs={12}>
          <SwitchSetting settingKey="window_enabled" label={tr('cloud.rules.fields.windowEnabled')} />
        </Grid>
        <DependentFields enabled={data.window_enabled}>
          <Grid item xs={12} sm={4}>
            <TextSetting settingKey="provisioning_window_start" label={tr('cloud.rules.fields.windowStart')} type="time" shrinkLabel />
          </Grid>
          <Grid item xs={12} sm={4}>
            <TextSetting settingKey="provisioning_window_end" label={tr('cloud.rules.fields.windowEnd')} type="time" shrinkLabel />
          </Grid>
          <Grid item xs={12} sm={4}>
            <TextSetting
              settingKey="provisioning_window_tz"
              label={tr('cloud.rules.fields.windowTz')}
              helper={tr('cloud.rules.helperText.windowTz')}
              placeholder="UTC"
            />
          </Grid>
          <Grid item xs={12}>
            <Typography variant="body2" color="text.secondary">
              {tr('cloud.rules.helperText.windowWrap')}
            </Typography>
          </Grid>
        </DependentFields>
      </Panel>
    </Grid>
  );
};

const CloudProvisioningRulesSettings: React.FC = () => {
  /*
   * The live priority ceiling. The floor is stored as an ABSOLUTE integer, so
   * a value typed against the wrong ceiling silently matches nothing — the
   * slider range and marks are scaled from this rather than hardcoded to 0-100.
   */
  const { data: maxPriority } = useQuery({ queryKey: qk.admin.group('max-priority'), queryFn: getMaxPriority });
  const ceiling = maxPriority?.max_priority ?? 1000;

  return (
    <GroupSettingsProvider<RulesGroup>
      queryKey={qk.admin.group('cloud-rules')}
      load={loadRules}
      saveField={saveRuleField}
      render={(data) => <RulesFields data={data} ceiling={ceiling} />}
    />
  );
};

export default CloudProvisioningRulesSettings;
