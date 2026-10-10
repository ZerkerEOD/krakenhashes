import React from 'react';
import { Grid, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import SystemSettingsProvider from '../../../../components/settings/SystemSettingsProvider';
import GroupSettingsProvider from '../../../../components/settings/GroupSettingsProvider';
import { NumberSetting, Panel } from '../../../../components/settings/fields';
import { SchedulingPanel } from '../../../../components/admin/jobExecutionPanels';
import { getMaxPriority, updateMaxPriority } from '../../../../services/systemSettings';
import { qk } from '../../../../services/queryKeys';

type PriorityGroup = Record<string, unknown>;

/** Agent allocation, overflow mode and the job priority ceiling. */
const SchedulingSection: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = (k: string) => t(k) as string;
  return (
    <Grid container spacing={3}>
      <SystemSettingsProvider>
        <SchedulingPanel />
      </SystemSettingsProvider>
      <GroupSettingsProvider<PriorityGroup>
        queryKey={qk.admin.group('max-priority')}
        load={async () => ({ max_priority: (await getMaxPriority()).max_priority })}
        saveField={(_key, value) => updateMaxPriority(Number(value))}
        render={(data) => (
          <Panel title={tr('systemSettings.priority.title')} caption={tr('systemSettings.priority.tooltip')}>
            <Grid item xs={12} md={5}>
              <NumberSetting
                settingKey="max_priority"
                label={tr('systemSettings.priority.maxPriority')}
                helper={tr('systemSettings.priority.helperText')}
                min={1}
                max={1000000}
              />
            </Grid>
            <Grid item xs={12} md={7}>
              <Typography variant="body2" color="text.secondary" paragraph>
                {tr('systemSettings.priorityInfo.description')}
              </Typography>
              <Typography variant="body2" color="text.secondary" paragraph>
                <strong>{tr('systemSettings.priorityInfo.currentMax')}:</strong> {Number(data.max_priority).toLocaleString()}
              </Typography>
              <Typography variant="body2" color="text.secondary">
                <strong>{tr('systemSettings.priorityInfo.recommended')}</strong>
                <br />• {tr('systemSettings.priorityInfo.small')}
                <br />• {tr('systemSettings.priorityInfo.medium')}
                <br />• {tr('systemSettings.priorityInfo.large')}
              </Typography>
            </Grid>
          </Panel>
        )}
      />
    </Grid>
  );
};

export default SchedulingSection;
