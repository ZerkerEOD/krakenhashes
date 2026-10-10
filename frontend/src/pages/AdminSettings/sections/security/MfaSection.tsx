import React from 'react';
import { Alert, Grid } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { Link as RouterLink } from 'react-router-dom';
import GroupSettingsProvider from '../../../../components/settings/GroupSettingsProvider';
import {
  CheckboxGroupSetting,
  ListTextSetting,
  NumberSetting,
  Panel,
  SwitchSetting,
  TextSetting,
} from '../../../../components/settings/fields';
import {
  getAdminMFASettings,
  getWebAuthnSettings,
  updateMFASettingsPartial,
  updateWebAuthnSettings,
} from '../../../../services/auth';
import { qk } from '../../../../services/queryKeys';
import { useSettingsStatus } from '../../../../hooks/useSettingsStatus';
import { isWebAuthnSupported } from '../../../../utils/webauthn';
import { ROUTES } from '../../../../constants/routes';
import type { MFASettings, WebAuthnSettings } from '../../../../types/auth';

type MfaGroup = Record<string, unknown>;
type WebAuthnGroup = Record<string, unknown>;

const loadMfa = async (): Promise<MfaGroup> => {
  const d = await getAdminMFASettings();
  return {
    requireMfa: Boolean(d.requireMfa),
    allowedMfaMethods: d.allowedMfaMethods ?? ['email'],
    emailCodeValidity: Number(d.emailCodeValidity || 5),
    backupCodesCount: Number(d.backupCodesCount || 8),
    mfaCodeCooldownMinutes: Number(d.mfaCodeCooldownMinutes || 1),
    mfaCodeExpiryMinutes: Number(d.mfaCodeExpiryMinutes || 5),
    mfaMaxAttempts: Number(d.mfaMaxAttempts || 3),
  };
};

const loadWebAuthn = async (): Promise<WebAuthnGroup> => {
  const d = await getWebAuthnSettings();
  return { rpId: d.rpId ?? '', rpDisplayName: d.rpDisplayName ?? '', rpOrigins: d.rpOrigins ?? [] };
};

/** Global MFA policy and WebAuthn (passkey) relying-party configuration. */
const MfaSection: React.FC = () => {
  const { t } = useTranslation('admin');
  const tr = (k: string, o?: any) => t(k, o) as string;
  const { status } = useSettingsStatus();
  const emailConfigured = status?.email.configured ?? false;
  const webAuthnSupported = isWebAuthnSupported();

  return (
    <Grid container spacing={3}>
      <GroupSettingsProvider<MfaGroup>
        queryKey={qk.admin.group('mfa')}
        load={loadMfa}
        saveField={(key, value) => updateMFASettingsPartial({ [key]: value } as Partial<MFASettings>)}
        render={(data) => (
          <Panel title={tr('authSettings.mfa.title')}>
            {!emailConfigured && (
              <Grid item xs={12}>
                <Alert severity="warning">
                  {tr('authSettings.mfa.requireMfaEmailRequired')}{' '}
                  <RouterLink to={ROUTES.admin.settingsSection('integrations', 'email')}>{tr('hub.cards.email')}</RouterLink>
                </Alert>
              </Grid>
            )}
            <Grid item xs={12}>
              <SwitchSetting
                settingKey="requireMfa"
                label={tr('authSettings.mfa.requireMfa')}
                helper={tr('authSettings.mfa.requireMfaHelper')}
                disabled={!emailConfigured && !data.requireMfa}
              />
            </Grid>
            <Grid item xs={12}>
              <CheckboxGroupSetting
                settingKey="allowedMfaMethods"
                label={tr('authSettings.mfa.allowedMethods')}
                row
                minSelected={data.requireMfa ? 1 : 0}
                options={[
                  { value: 'email', label: tr('authSettings.mfa.methodEmail') },
                  { value: 'authenticator', label: tr('authSettings.mfa.methodAuthenticator') },
                  {
                    value: 'passkey',
                    label: tr(webAuthnSupported ? 'authSettings.mfa.methodPasskey' : 'authSettings.mfa.methodPasskeyNotSupported'),
                    disabled: !webAuthnSupported,
                  },
                ]}
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <NumberSetting settingKey="emailCodeValidity" label={tr('authSettings.mfa.emailCodeValidity')} helper={tr('authSettings.mfa.emailCodeValidityHelper')} min={1} unit="min" />
            </Grid>
            <Grid item xs={12} md={6}>
              <NumberSetting settingKey="mfaCodeCooldownMinutes" label={tr('authSettings.mfa.codeCooldown')} helper={tr('authSettings.mfa.codeCooldownHelper')} min={1} unit="min" />
            </Grid>
            <Grid item xs={12} md={6}>
              <NumberSetting settingKey="mfaCodeExpiryMinutes" label={tr('authSettings.mfa.codeExpiry')} helper={tr('authSettings.mfa.codeExpiryHelper')} min={1} unit="min" />
            </Grid>
            <Grid item xs={12} md={6}>
              <NumberSetting settingKey="mfaMaxAttempts" label={tr('authSettings.mfa.maxCodeAttempts')} helper={tr('authSettings.mfa.maxCodeAttemptsHelper')} min={1} />
            </Grid>
            <Grid item xs={12} md={6}>
              <NumberSetting settingKey="backupCodesCount" label={tr('authSettings.mfa.backupCodesCount')} helper={tr('authSettings.mfa.backupCodesCountHelper')} min={1} />
            </Grid>
          </Panel>
        )}
      />

      <GroupSettingsProvider<WebAuthnGroup>
        queryKey={qk.admin.group('webauthn')}
        load={loadWebAuthn}
        saveField={(key, value, current) =>
          updateWebAuthnSettings({ ...(current as unknown as WebAuthnSettings), [key]: value } as Partial<WebAuthnSettings>)
        }
        render={(data) => (
          <Panel title={tr('authSettings.webauthn.title')}>
            {!webAuthnSupported && (
              <Grid item xs={12}>
                <Alert severity="warning">{tr('authSettings.webauthn.notSupported')}</Alert>
              </Grid>
            )}
            <Grid item xs={12}>
              <Alert severity="info">{tr('authSettings.webauthn.requiresDomain')}</Alert>
            </Grid>
            <Grid item xs={12} md={6}>
              <TextSetting settingKey="rpId" label={tr('authSettings.webauthn.rpId')} helper={tr('authSettings.webauthn.rpIdHelper')} placeholder="krakenhashes.example.com" />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextSetting settingKey="rpDisplayName" label={tr('authSettings.webauthn.rpDisplayName')} helper={tr('authSettings.webauthn.rpDisplayNameHelper')} placeholder="KrakenHashes" />
            </Grid>
            <Grid item xs={12}>
              <ListTextSetting settingKey="rpOrigins" label={tr('authSettings.webauthn.allowedOrigins')} helper={tr('authSettings.webauthn.allowedOriginsHelper')} placeholder="https://localhost:3000" />
            </Grid>
            {Boolean(data.rpId) && Array.isArray(data.rpOrigins) && (data.rpOrigins as string[]).length > 0 && (
              <Grid item xs={12}>
                <Alert severity="success">{tr('authSettings.webauthn.configured')}</Alert>
              </Grid>
            )}
          </Panel>
        )}
      />
    </Grid>
  );
};

export default MfaSection;
