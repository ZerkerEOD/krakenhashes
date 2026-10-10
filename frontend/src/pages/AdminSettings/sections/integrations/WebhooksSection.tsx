import React, { useState } from 'react';
import { Box, Button, Grid } from '@mui/material';
import SendIcon from '@mui/icons-material/Send';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { GridColDef } from '@mui/x-data-grid';
import GroupSettingsProvider from '../../../../components/settings/GroupSettingsProvider';
import { Panel, SwitchSetting, TextSetting } from '../../../../components/settings/fields';
import { DataTable, SectionCard, StatusChip } from '../../../../components/ui';
import { useToast } from '../../../../components/ui/toast';
import { getErrorMessage } from '../../../../utils/errors';
import {
  getAllUserWebhooks,
  getGlobalWebhookSettings,
  testGlobalWebhook,
  updateGlobalWebhookSettings,
} from '../../../../services/notifications';
import { qk } from '../../../../services/queryKeys';
import type { AdminWebhookView } from '../../../../types/notifications';

type GlobalWebhookGroup = {
  enabled: boolean;
  url: string;
  /** Write-only: never returned by the API, cleared after every save. */
  secret: string;
  custom_headers: string;
  has_secret: boolean;
};

const loadGlobal = async (): Promise<GlobalWebhookGroup> => {
  const s = await getGlobalWebhookSettings();
  return { enabled: s.enabled, url: s.url, secret: '', custom_headers: s.custom_headers || '{}', has_secret: s.has_secret };
};

const saveGlobal = async (key: keyof GlobalWebhookGroup & string, value: GlobalWebhookGroup[keyof GlobalWebhookGroup]) => {
  if (key === 'has_secret') return;
  if (key === 'secret' && !value) return;
  return updateGlobalWebhookSettings({ [key]: value });
};

const isValidJson = (text: string): boolean => {
  try {
    JSON.parse(text);
    return true;
  } catch {
    return false;
  }
};

const TestButton: React.FC = () => {
  const { t } = useTranslation('notifications');
  const toast = useToast();
  const [testing, setTesting] = useState(false);
  const run = async () => {
    setTesting(true);
    try {
      const result = await testGlobalWebhook();
      if (result.success) toast.success(t('webhooks.testSuccess') as string);
      else toast.error(result.error || (t('webhooks.testFailed') as string));
    } catch (err) {
      toast.error(getErrorMessage(err) || (t('webhooks.testFailed') as string));
    } finally {
      setTesting(false);
    }
  };
  return (
    <Button size="small" variant="outlined" startIcon={<SendIcon />} disabled={testing} onClick={run}>
      {t('admin.globalWebhook.test') as string}
    </Button>
  );
};

/** Global webhook (autosave) and a read-only overview of every user's webhooks. */
const WebhooksSection: React.FC = () => {
  const { t } = useTranslation('notifications');
  const tr = (k: string) => t(k) as string;

  const userWebhooks = useQuery({
    queryKey: qk.admin.group('user-webhooks'),
    queryFn: async () => (await getAllUserWebhooks()).webhooks ?? [],
  });

  const columns: GridColDef<AdminWebhookView>[] = [
    { field: 'username', headerName: tr('admin.userWebhooks.username'), flex: 1, minWidth: 120 },
    { field: 'email', headerName: tr('admin.userWebhooks.email'), flex: 1.2, minWidth: 160 },
    { field: 'name', headerName: tr('webhooks.name'), flex: 1, minWidth: 120 },
    { field: 'url', headerName: tr('webhooks.url'), flex: 2, minWidth: 220 },
    {
      field: 'is_active',
      headerName: tr('webhooks.isActive'),
      width: 110,
      renderCell: (p) => (
        <StatusChip entity="generic" status={p.value ? 'enabled' : 'disabled'} label={(p.value ? t('common:active') : t('common:inactive')) as string} />
      ),
    },
    { field: 'total_sent', headerName: tr('webhooks.totalSent'), width: 110, type: 'number' },
    { field: 'total_failed', headerName: tr('webhooks.totalFailed'), width: 110, type: 'number' },
  ];

  return (
    <Box>
      <GroupSettingsProvider<GlobalWebhookGroup>
        queryKey={qk.admin.group('global-webhook')}
        load={loadGlobal}
        saveField={saveGlobal}
        render={(data) => (
          <Grid container spacing={3} sx={{ mb: 3 }}>
            <Panel title={tr('admin.globalWebhook.title')} caption={tr('admin.globalWebhook.description')} actions={<TestButton />}>
              <Grid item xs={12}>
                <SwitchSetting settingKey="enabled" label={tr('admin.globalWebhook.enabled')} />
              </Grid>
              <Grid item xs={12} md={7}>
                <TextSetting settingKey="url" label={tr('admin.globalWebhook.url')} placeholder="https://" type="url" />
              </Grid>
              <Grid item xs={12} md={5}>
                <TextSetting
                  settingKey="secret"
                  label={tr('admin.globalWebhook.secret')}
                  helper={data.has_secret ? tr('webhooks.secretExists') : tr('webhooks.secretPlaceholder')}
                  secret
                />
              </Grid>
              <Grid item xs={12}>
                <TextSetting
                  settingKey="custom_headers"
                  label={tr('admin.globalWebhook.customHeaders')}
                  multiline
                  rows={3}
                  validate={(next) => (isValidJson(next || '{}') ? null : tr('webhooks.invalidJson'))}
                />
              </Grid>
            </Panel>
          </Grid>
        )}
      />

      <SectionCard title={tr('admin.userWebhooks.title')} subtitle={tr('admin.userWebhooks.description')} flush>
        <DataTable<AdminWebhookView>
          rows={userWebhooks.data ?? []}
          columns={columns}
          loading={userWebhooks.isLoading}
          error={userWebhooks.error}
          onRetry={() => void userWebhooks.refetch()}
          pagination={{ mode: 'client', initialPageSize: 10 }}
          sorting={{ mode: 'client' }}
          emptyState={{ title: tr('admin.userWebhooks.empty') }}
          tableKey="admin-user-webhooks"
          height="auto"
        />
      </SectionCard>
    </Box>
  );
};

export default WebhooksSection;
