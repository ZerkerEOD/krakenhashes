import React from 'react';
import { Grid } from '@mui/material';
import { useTranslation } from 'react-i18next';
import GroupSettingsProvider from '../../../../components/settings/GroupSettingsProvider';
import { NumberSetting, Panel, SwitchSetting } from '../../../../components/settings/fields';
import { getAccountSecurity, getPasswordPolicy, updateAuthSettingsPartial } from '../../../../services/auth';
import { qk } from '../../../../services/queryKeys';

type AuthGroup = Record<string, unknown>;

const loadAuth = async (): Promise<AuthGroup> => {
  const [policy, security] = await Promise.all([getPasswordPolicy(), getAccountSecurity()]);
  return { ...policy, ...security } as AuthGroup;
};

/** Password policy, account security and session management (auth_settings row). */
const AuthenticationSection: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = (k: string) => t(k) as string;
  return (
    <GroupSettingsProvider<AuthGroup>
      queryKey={qk.admin.group('auth')}
      load={loadAuth}
      saveField={(key, value) => updateAuthSettingsPartial({ [key]: value })}
    >
      <Grid container spacing={3}>
        <Panel title={tr('authSettings.passwordPolicy.title')}>
          <Grid item xs={12} md={4}>
            <NumberSetting settingKey="minPasswordLength" label={tr('authSettings.passwordPolicy.minLength')} min={1} max={128} />
          </Grid>
          <Grid item xs={12} md={8}>
            <Grid container spacing={1}>
              <Grid item xs={12} sm={6}>
                <SwitchSetting settingKey="requireUppercase" label={tr('authSettings.passwordPolicy.requireUppercase')} />
              </Grid>
              <Grid item xs={12} sm={6}>
                <SwitchSetting settingKey="requireLowercase" label={tr('authSettings.passwordPolicy.requireLowercase')} />
              </Grid>
              <Grid item xs={12} sm={6}>
                <SwitchSetting settingKey="requireNumbers" label={tr('authSettings.passwordPolicy.requireNumbers')} />
              </Grid>
              <Grid item xs={12} sm={6}>
                <SwitchSetting settingKey="requireSpecialChars" label={tr('authSettings.passwordPolicy.requireSpecialChars')} />
              </Grid>
            </Grid>
          </Grid>
        </Panel>

        <Panel title={tr('authSettings.accountSecurity.title')}>
          <Grid item xs={12} md={6}>
            <NumberSetting settingKey="maxFailedAttempts" label={tr('authSettings.accountSecurity.maxFailedAttempts')} min={0} />
          </Grid>
          <Grid item xs={12} md={6}>
            <NumberSetting settingKey="lockoutDuration" label={tr('authSettings.accountSecurity.lockoutDuration')} min={0} unit="min" />
          </Grid>
          <Grid item xs={12} md={6}>
            <NumberSetting settingKey="jwtExpiryMinutes" label={tr('authSettings.accountSecurity.jwtExpiry')} min={1} unit="min" />
          </Grid>
          <Grid item xs={12} md={6}>
            <NumberSetting
              settingKey="notificationAggregationMinutes"
              label={tr('authSettings.accountSecurity.notificationAggregation')}
              helper={tr('authSettings.accountSecurity.notificationAggregationHelper')}
              min={0}
              unit="min"
            />
          </Grid>
        </Panel>

        <Panel title={tr('authSettings.session.title')}>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="tokenCleanupIntervalSeconds"
              label={tr('authSettings.session.tokenCleanupInterval')}
              helper={tr('authSettings.session.tokenCleanupIntervalHelper')}
              min={10}
              unit="s"
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="maxConcurrentSessions"
              label={tr('authSettings.session.maxConcurrentSessions')}
              helper={tr('authSettings.session.maxConcurrentSessionsHelper')}
              min={0}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <NumberSetting
              settingKey="sessionAbsoluteTimeoutHours"
              label={tr('authSettings.session.absoluteTimeout')}
              helper={tr('authSettings.session.absoluteTimeoutHelper')}
              min={0}
              unit="h"
            />
          </Grid>
        </Panel>
      </Grid>
    </GroupSettingsProvider>
  );
};

export default AuthenticationSection;
