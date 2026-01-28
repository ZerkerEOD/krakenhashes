import React, { useState, useEffect, lazy, Suspense } from 'react';
import { Box, Tabs, Tab, Typography, Paper, TextField, Button, Alert, CircularProgress, IconButton } from '@mui/material';
import ChevronLeftIcon from '@mui/icons-material/ChevronLeft';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import { useTranslation } from 'react-i18next';
import { EmailSettings } from './EmailSettings';
import { useAuth } from '../../contexts/AuthContext';
import { Navigate } from 'react-router-dom';
import AuthSettings from '../../components/admin/AuthSettings';
import BinaryManagement from '../../components/admin/BinaryManagement';
import SystemSettings from '../../components/admin/SystemSettings';
import { HashTypeManager } from '../../components/admin/HashTypeManager';
import JobExecutionSettings from '../../components/admin/JobExecutionSettings';
import MonitoringSettings from '../../components/admin/MonitoringSettings';
import AgentDownloadSettings from '../../components/admin/AgentDownloadSettings';
import NotificationSettings from '../../components/admin/NotificationSettings';
import { useSnackbar } from 'notistack';
import { updateAuthSettings } from '../../services/auth';
import { getDefaultClientRetentionSetting, updateDefaultClientRetentionSetting } from '../../services/api';

// Lazy load SSO Settings to avoid circular dependency
const SSOSettingsPage = lazy(() => import('../admin/SSOSettings'));

interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

const TabPanel = (props: TabPanelProps) => {
  const { children, value, index, ...other } = props;

  return (
    <div
      role="tabpanel"
      hidden={value !== index}
      id={`admin-settings-tabpanel-${index}`}
      aria-labelledby={`admin-settings-tab-${index}`}
      {...other}
    >
      {value === index && (
        <Box sx={{ p: 3 }}>
          {children}
        </Box>
      )}
    </div>
  );
};

// --- Client Settings Component ---
const ClientSettingsTab: React.FC = () => {
  // Add a log to confirm component rendering
  console.log("[ClientSettingsTab] Rendering...");

  const { t } = useTranslation('admin');
  const [retentionMonths, setRetentionMonths] = useState<string>('');
  const [initialLoading, setInitialLoading] = useState<boolean>(true);
  const [saveLoading, setSaveLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);
  const { enqueueSnackbar } = useSnackbar();

  useEffect(() => {
    const fetchSettings = async () => {
      setInitialLoading(true);
      setError(null);
      try {
        const response = await getDefaultClientRetentionSetting();
        console.log('[ClientSettingsTab] useEffect - API response received:', response);
        
        // Add guards and detailed logging
        if (response && response.data && response.data.data) { // Check nested data object
          // Access value correctly via response.data.data.value
          const apiValue = response.data.data.value; 
          console.log(`[ClientSettingsTab] useEffect - Raw API value: ${apiValue} (Type: ${typeof apiValue})`);

          const valueOrDefault = apiValue ?? '0';
          console.log(`[ClientSettingsTab] useEffect - Value after ?? '0': ${valueOrDefault} (Type: ${typeof valueOrDefault})`);

          const valueToSet = String(valueOrDefault);
          console.log(`[ClientSettingsTab] useEffect - Final valueToSet (string): ${valueToSet}`);

          setRetentionMonths(valueToSet); 
        } else {
          console.error('[ClientSettingsTab] useEffect - Invalid response structure:', response);
          setError('Failed to process settings from server.');
          setRetentionMonths('0'); // Default to 0 on error
        }
      } catch (err) {
        console.error("Failed to fetch client retention settings:", err);
        setError('Failed to load settings. Please try again.'); // Ensure error is set
        setRetentionMonths('0'); // Default to 0 on fetch error
      } finally {
        console.log('[ClientSettingsTab] useEffect - Setting initialLoading to false.');
        setInitialLoading(false);
      }
    };
    fetchSettings();
  }, []); // Empty dependency array

  const handleSave = async () => {
    setError(null);
    setSaveLoading(true);
    const valueToSave = retentionMonths.trim();
    const numericValue = parseInt(valueToSave, 10);

    if (isNaN(numericValue) || numericValue < 0) {
      setError(t('clientSettings.errors.invalidRetention') as string);
      setSaveLoading(false);
      return;
    }

    try {
      await updateDefaultClientRetentionSetting({ value: numericValue.toString() });
      enqueueSnackbar(t('clientSettings.messages.updateSuccess') as string, { variant: 'success' });
    } catch (err: any) {
      console.error("Failed to update client retention settings:", err);
      const message = err.response?.data?.error || t('clientSettings.errors.saveFailed') as string;
      setError(message);
      enqueueSnackbar(message, { variant: 'error' });
    } finally {
      setSaveLoading(false);
    }
  };

  return (
    <Box>
      <Typography variant="h6" gutterBottom>
        {t('clientSettings.title') as string}
      </Typography>
      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {initialLoading ? (
        <CircularProgress />
      ) : (
        <Box component="form" noValidate autoComplete="off">
          <TextField
            fullWidth
            type="number"
            label={t('clientSettings.retentionPeriod') as string}
            value={retentionMonths}
            onChange={(e) => setRetentionMonths(e.target.value)}
            helperText={t('clientSettings.retentionHelperText') as string}
            margin="normal"
            InputProps={{
              inputProps: {
                  min: 0
              }
          }}
          />
          <Button
            variant="contained"
            color="primary"
            onClick={handleSave}
            disabled={saveLoading || initialLoading}
            sx={{ mt: 2 }}
          >
            {saveLoading ? <CircularProgress size={24} /> : t('clientSettings.saveButton') as string}
          </Button>
        </Box>
      )}
    </Box>
  );
};
// --- End Client Settings Component ---

export const AdminSettings = () => {
  const { t } = useTranslation('admin');
  const [currentTab, setCurrentTab] = useState(() => {
    const savedTab = localStorage.getItem('adminSettingsTab');
    const initialTab = savedTab ? parseInt(savedTab, 10) : 0;
    return initialTab >= 0 && initialTab < 11 ? initialTab : 0;
  });

  const [loading, setLoading] = useState(false);
  const { userRole } = useAuth();
  const { enqueueSnackbar } = useSnackbar();

  // Redirect if not admin
  if (userRole !== 'admin') {
    return <Navigate to="/" replace />;
  }

  const handleTabChange = (event: React.SyntheticEvent, newValue: number) => {
    setCurrentTab(newValue);
    localStorage.setItem('adminSettingsTab', newValue.toString());
  };

  return (
    <Box sx={{ width: '100%', p: 3 }}>
      <Typography variant="h4" gutterBottom>
        {t('title') as string}
      </Typography>

      <Paper sx={{ width: '100%', mt: 3 }}>
        <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
          <Tabs
            value={currentTab}
            onChange={handleTabChange}
            aria-label="admin settings tabs"
            variant="scrollable"
            scrollButtons="auto"
            allowScrollButtonsMobile
            sx={{
              '& .MuiTabs-scrollButtons': {
                '&.Mui-disabled': {
                  opacity: 0.3,
                },
              },
            }}
          >
            <Tab label={t('tabs.emailSettings') as string} />
            <Tab label={t('tabs.authenticationSettings') as string} />
            <Tab label={t('tabs.ssoSettings') as string} />
            <Tab label={t('tabs.binaryManagement') as string} />
            <Tab label={t('tabs.systemSettings') as string} />
            <Tab label={t('tabs.clientSettings') as string} />
            <Tab label={t('tabs.hashTypes') as string} />
            <Tab label={t('tabs.jobExecution') as string} />
            <Tab label={t('tabs.monitoring') as string} />
            <Tab label={t('tabs.agentDownloads') as string} />
            <Tab label={t('tabs.notifications') as string} />
          </Tabs>
        </Box>

        <TabPanel value={currentTab} index={0}>
          <EmailSettings />
        </TabPanel>
        <TabPanel value={currentTab} index={1}>
          <AuthSettings
            onSave={async (settings) => {
              setLoading(true);
              try {
                await updateAuthSettings(settings);
                enqueueSnackbar(t('settings.saved') as string, { variant: 'success' });
              } catch (error) {
                console.error('Failed to update settings:', error);
                enqueueSnackbar(error instanceof Error ? error.message : t('settings.saveFailed') as string, { variant: 'error' });
                throw error; // Propagate error to trigger form error state
              } finally {
                setLoading(false);
              }
            }}
            loading={loading}
          />
        </TabPanel>
        <TabPanel value={currentTab} index={2}>
          <Suspense fallback={<Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}><CircularProgress /></Box>}>
            <SSOSettingsPage />
          </Suspense>
        </TabPanel>
        <TabPanel value={currentTab} index={3}>
          <BinaryManagement />
        </TabPanel>
        <TabPanel value={currentTab} index={4}>
          <SystemSettings />
        </TabPanel>
        <TabPanel value={currentTab} index={5}>
          <ClientSettingsTab />
        </TabPanel>
        <TabPanel value={currentTab} index={6}>
          <HashTypeManager />
        </TabPanel>
        <TabPanel value={currentTab} index={7}>
          <JobExecutionSettings />
        </TabPanel>
        <TabPanel value={currentTab} index={8}>
          <MonitoringSettings />
        </TabPanel>
        <TabPanel value={currentTab} index={9}>
          <AgentDownloadSettings />
        </TabPanel>
        <TabPanel value={currentTab} index={10}>
          <NotificationSettings />
        </TabPanel>
      </Paper>
    </Box>
  );
}; 