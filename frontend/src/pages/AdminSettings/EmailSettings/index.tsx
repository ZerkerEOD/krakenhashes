import React, { useState } from 'react';
import { Box, Tabs, Tab, Paper } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { ProviderConfig } from './ProviderConfig';
import { TemplateEditor } from './TemplateEditor';
import { Notification } from '../../../components/Notification';
import { AlertColor } from '@mui/material';

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
      id={`email-settings-tabpanel-${index}`}
      aria-labelledby={`email-settings-tab-${index}`}
      {...other}
    >
      {value === index && <Box sx={{ p: 3 }}>{children}</Box>}
    </div>
  );
};

export const EmailSettings = () => {
  const { t } = useTranslation('admin');
  const [currentTab, setCurrentTab] = useState(() => {
    // Restore tab state from localStorage
    const savedTab = localStorage.getItem('emailSettingsTab');
    return savedTab ? parseInt(savedTab, 10) : 0;
  });
  const [notification, setNotification] = useState<{
    open: boolean;
    message: string;
    severity: AlertColor;
  }>({
    open: false,
    message: '',
    severity: 'success',
  });

  const handleTabChange = (_: React.SyntheticEvent, newValue: number) => {
    setCurrentTab(newValue);
    // Save tab state to localStorage
    localStorage.setItem('emailSettingsTab', newValue.toString());
  };

  const handleNotification = (message: string, severity: 'success' | 'error') => {
    setNotification({
      open: true,
      message,
      severity,
    });
  };

  const handleCloseNotification = () => {
    setNotification(prev => ({ ...prev, open: false }));
  };

  return (
    <Box>
      <Paper sx={{ width: '100%' }}>
        <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
          <Tabs
            value={currentTab}
            onChange={handleTabChange}
            aria-label="email settings tabs"
          >
            <Tab label={t('emailSettings.tabs.providerConfiguration') as string} />
            <Tab label={t('emailSettings.tabs.emailTemplates') as string} />
          </Tabs>
        </Box>

        <TabPanel value={currentTab} index={0}>
          <ProviderConfig onNotification={handleNotification} />
        </TabPanel>
        <TabPanel value={currentTab} index={1}>
          <TemplateEditor onNotification={handleNotification} />
        </TabPanel>
      </Paper>

      <Notification
        open={notification.open}
        message={notification.message}
        severity={notification.severity}
        onClose={handleCloseNotification}
      />
    </Box>
  );
}; 