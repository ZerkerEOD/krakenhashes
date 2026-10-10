import React from 'react';
import { Grid, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import GroupSettingsProvider from '../../../../components/settings/GroupSettingsProvider';
import { Panel, SwitchSetting } from '../../../../components/settings/fields';
import { adminTeamsService } from '../../../../services/teams';
import { qk } from '../../../../services/queryKeys';

type AccessGroup = Record<string, unknown>;

/** Multi-team mode: the system-wide access model switch. */
const AccessSection: React.FC = () => {
  const { t } = useTranslation('admin');
  return (
    <GroupSettingsProvider<AccessGroup>
      queryKey={qk.admin.group('access')}
      load={async () => ({ teamsEnabled: await adminTeamsService.getTeamsEnabled() })}
      saveField={(_key, value) => adminTeamsService.setTeamsEnabled(Boolean(value))}
    >
      <Grid container spacing={3}>
        <Panel title={t('authSettings.teams.title') as string}>
          <Grid item xs={12}>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              {t('authSettings.teams.description') as string}
            </Typography>
            <SwitchSetting settingKey="teamsEnabled" label={t('authSettings.teams.enable') as string} helper={t('authSettings.teams.enableHelper') as string} />
          </Grid>
        </Panel>
      </Grid>
    </GroupSettingsProvider>
  );
};

export default AccessSection;
