import React from 'react';
import { Grid, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import GroupSettingsProvider from '../../../../components/settings/GroupSettingsProvider';
import { NumberSetting, Panel, SwitchSetting } from '../../../../components/settings/fields';
import { getDefaultClientRetentionSetting, updateDefaultClientRetentionSetting } from '../../../../services/api';
import { qk } from '../../../../services/queryKeys';

type RetentionGroup = Record<string, unknown>;

/** Data retention defaults and hashlist ingestion. */
const DataSection: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = (k: string, o?: any) => t(k, o) as string;
  return (
    <Grid container spacing={3}>
      <GroupSettingsProvider<RetentionGroup>
        queryKey={qk.admin.group('retention')}
        load={async () => {
          const res = await getDefaultClientRetentionSetting();
          return { months: Number(res.data?.data?.value ?? 0) };
        }}
        saveField={(_key, value) => updateDefaultClientRetentionSetting({ value: String(Number(value)) })}
      >
        <Panel title={tr('clientSettings.title')}>
          <Grid item xs={12} md={5}>
            <NumberSetting settingKey="months" label={tr('clientSettings.retentionPeriod')} helper={tr('clientSettings.retentionHelperText')} min={0} />
          </Grid>
        </Panel>
      </GroupSettingsProvider>

      <SystemSettingsProvider>
        <Panel title={tr('systemSettings.hashlist.title')} caption={tr('systemSettings.hashlist.tooltip')}>
          <Grid item xs={12} md={6}>
            <SwitchSetting settingKey="require_client_for_hashlist" label={tr('systemSettings.hashlist.requireClient')} helper={tr('systemSettings.hashlist.requireClientDescription')} />
          </Grid>
          <Grid item xs={12} md={6}>
            <NumberSetting
              settingKey="hashlist_bulk_batch_size"
              label={tr('systemSettings.hashlist.batchSize')}
              helper={tr('systemSettings.hashlist.batchSizeHelper')}
              min={10000}
              max={2000000}
              step={50000}
            />
            <Typography variant="caption" color="text.secondary" component="div" sx={{ mt: 1 }}>
              <strong>{tr('systemSettings.hashlist.performanceGuide')}</strong>
              <br />• {tr('systemSettings.hashlist.performance100k')}
              <br />• {tr('systemSettings.hashlist.performance500k')}
              <br />• {tr('systemSettings.hashlist.performance1m')}
            </Typography>
          </Grid>
        </Panel>
      </SystemSettingsProvider>
    </Grid>
  );
};

export default DataSection;
