import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  TextField,
  Button,
  Grid,
  Typography,
  /* TODO: These components are preserved for planned dialog-based template editing features
   * and will be used in future UI improvements
   * List,
   * ListItem,
   * ListItemText,
   * ListItemSecondaryAction,
   * Dialog,
   * DialogTitle,
   * DialogContent,
   * DialogActions,
   */
  Paper,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  TableContainer,
  Table,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
  CircularProgress,
  IconButton,
} from '@mui/material';
import LoadingButton from '@mui/lab/LoadingButton';
import {
  Edit as EditIcon,
  Delete as DeleteIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import {
  getEmailTemplates,
  createEmailTemplate,
  updateEmailTemplate,
  deleteEmailTemplate
} from '../../../services/api';

interface TemplateEditorProps {
  onNotification: (message: string, severity: 'success' | 'error') => void;
}

interface Template {
  id?: number;
  templateType:
    | 'security_event'
    | 'job_completion'
    | 'admin_error'
    | 'mfa_code'
    | 'security_password_changed'
    | 'security_mfa_disabled'
    | 'security_suspicious_login'
    | 'job_started'
    | 'job_failed'
    | 'first_crack'
    | 'task_completed'
    | 'agent_offline'
    | 'agent_error'
    | 'webhook_failure';
  name: string;
  subject: string;
  htmlContent: string;
  textContent: string;
}

const STORAGE_KEY = 'templateEditorState';

// Keep sampleData for both testing and live preview
const sampleData: Record<string, Record<string, string>> = {
  security_event: {
    EventType: 'Login Attempt',
    Timestamp: new Date().toISOString(),
    Details: 'Failed login attempt from unknown IP',
    IPAddress: '192.168.1.1',
  },
  job_completion: {
    JobName: 'Sample Hash Cracking Job',
    Duration: '2h 15m',
    HashesProcessed: '1,000,000',
    CrackedCount: '750,000',
    SuccessRate: '75',
  },
  admin_error: {
    ErrorType: 'Database Connection',
    Component: 'User Service',
    Timestamp: new Date().toISOString(),
    ErrorMessage: 'Failed to connect to database',
    StackTrace: 'Error: Connection timeout\n  at Database.connect (/app/db.js:25)',
  },
  mfa_code: {
    Code: '123456',
    ExpiryMinutes: '5',
  },
  // Notification email template sample data
  security_password_changed: {
    Username: 'john.doe',
    Email: 'john.doe@example.com',
    Timestamp: new Date().toISOString(),
    IPAddress: '192.168.1.100',
    UserAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0.0.0',
  },
  security_mfa_disabled: {
    Username: 'john.doe',
    DisabledMethod: 'Authenticator App',
    Timestamp: new Date().toISOString(),
    IPAddress: '192.168.1.100',
  },
  security_suspicious_login: {
    Username: 'john.doe',
    Message: 'Multiple failed login attempts detected from your account.',
    Timestamp: new Date().toISOString(),
    IPAddress: '192.168.1.100',
    UserAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0.0.0',
    FailedAttempts: '5',
  },
  job_started: {
    JobName: 'Corporate Password Audit',
    HashlistName: 'Q4 2024 Hashes',
    Priority: '5',
    TotalHashes: '50,000',
  },
  job_failed: {
    JobName: 'Corporate Password Audit',
    ErrorMessage: 'Agent disconnected during processing',
    FailedAt: new Date().toISOString(),
  },
  first_crack: {
    HashlistName: 'Q4 2024 Hashes',
    JobName: 'Corporate Password Audit',
    CrackedHash: '5d41402abc4b2a76b9719d911017c592',
  },
  task_completed: {
    JobName: 'Corporate Password Audit',
    AgentName: 'GPU-Server-01',
    CrackCount: '1,234',
  },
  agent_offline: {
    AgentName: 'GPU-Server-01',
    DisconnectedAt: new Date().toISOString(),
    OfflineDuration: '15 minutes',
  },
  agent_error: {
    AgentName: 'GPU-Server-01',
    Error: 'GPU memory allocation failed',
    Context: 'During hashcat execution',
    ReportedAt: new Date().toISOString(),
  },
  webhook_failure: {
    WebhookName: 'Slack Notifications',
    WebhookURL: 'https://hooks.slack.com/services/xxx',
    Error: 'Connection refused',
  },
};

export const TemplateEditor: React.FC<TemplateEditorProps> = ({ onNotification }) => {
  const { t } = useTranslation('admin');
  const [loading, setLoading] = useState(false);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);
  const [isEditing, setIsEditing] = useState(false);

  const loadData = useCallback(async () => {
    try {
      console.debug('[TemplateEditor] Loading templates...');
      setLoading(true);
      const response = await getEmailTemplates();
      console.debug('[TemplateEditor] Templates loaded:', response.data);

      const transformedTemplates = response.data.map((template: any) => ({
        id: template.id,
        templateType: template.template_type,
        name: template.name,
        subject: template.subject,
        htmlContent: template.html_content,
        textContent: template.text_content,
      }));

      console.debug('[TemplateEditor] Transformed templates:', transformedTemplates);
      setTemplates(transformedTemplates);
    } catch (error) {
      console.error('[TemplateEditor] Failed to load templates:', error);
      onNotification(t('emailSettings.templates.messages.loadFailed') as string, 'error');
    } finally {
      setLoading(false);
    }
  }, [onNotification]);

  const restoreState = useCallback(() => {
    const savedState = localStorage.getItem(STORAGE_KEY);
    if (savedState) {
      const { template, editing } = JSON.parse(savedState);
      if (template && editing) {
        console.debug('[TemplateEditor] Restoring edit state:', template);
        setSelectedTemplate(template);
        setIsEditing(true);
      }
    }
  }, []);

  useEffect(() => {
    loadData();
    restoreState();
  }, [loadData, restoreState]);

  // Save edit state to localStorage whenever it changes
  useEffect(() => {
    if (selectedTemplate && isEditing) {
      console.debug('[TemplateEditor] Saving edit state:', selectedTemplate);
      localStorage.setItem(STORAGE_KEY, JSON.stringify({
        template: selectedTemplate,
        editing: isEditing,
      }));
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
  }, [selectedTemplate, isEditing]);

  const handleEditTemplate = (template: Template) => {
    console.debug('[TemplateEditor] Editing template:', template);
    setSelectedTemplate(template);
    setIsEditing(true);
  };

  const handleDeleteTemplate = async (id: number) => {
    try {
      setLoading(true);
      await deleteEmailTemplate(id);
      onNotification(t('emailSettings.templates.messages.deleteSuccess') as string, 'success');
      await loadData();
    } catch (error) {
      console.error('[TemplateEditor] Failed to delete template:', error);
      onNotification(t('emailSettings.templates.messages.deleteFailed') as string, 'error');
    } finally {
      setLoading(false);
    }
  };

  const handleSave = async () => {
    if (!selectedTemplate) return;

    try {
      setLoading(true);
      const payload = {
        template_type: selectedTemplate.templateType,
        name: selectedTemplate.name,
        subject: selectedTemplate.subject,
        html_content: selectedTemplate.htmlContent,
        text_content: selectedTemplate.textContent,
      };

      if (selectedTemplate.id) {
        await updateEmailTemplate(selectedTemplate.id, payload);
        onNotification(t('emailSettings.templates.messages.updateSuccess') as string, 'success');
      } else {
        await createEmailTemplate(payload);
        onNotification(t('emailSettings.templates.messages.createSuccess') as string, 'success');
      }

      await loadData();
      setIsEditing(false);
      setSelectedTemplate(null);
    } catch (error) {
      console.error('[TemplateEditor] Failed to save template:', error);
      onNotification(t('emailSettings.templates.messages.saveFailed') as string, 'error');
    } finally {
      setLoading(false);
    }
  };

  const handleTest = async () => {
    if (!selectedTemplate) return;

    try {
      setLoading(true);
      // TODO: Implement test email sending
      await new Promise(resolve => setTimeout(resolve, 1000));
      onNotification(t('emailSettings.templates.messages.testSuccess') as string, 'success');
    } catch (error) {
      console.error('[TemplateEditor] Failed to send test email:', error);
      onNotification(t('emailSettings.templates.messages.testFailed') as string, 'error');
    } finally {
      setLoading(false);
    }
  };

  const handleCancel = () => {
    setIsEditing(false);
    setSelectedTemplate(null);
    localStorage.removeItem(STORAGE_KEY);
  };

  const getPreviewContent = () => {
    if (!selectedTemplate?.templateType || !selectedTemplate?.htmlContent) {
      return '';
    }

    let content = selectedTemplate.htmlContent;
    const data = sampleData[selectedTemplate.templateType] || {};

    // Replace template variables with sample data
    Object.entries(data).forEach(([key, value]) => {
      const regex = new RegExp(`{{\\s*\\.${key}\\s*}}`, 'g');
      content = content.replace(regex, String(value));
    });

    return content;
  };

  if (loading) {
    return (
      <Box display="flex" justifyContent="center" p={3}>
        <CircularProgress />
      </Box>
    );
  }

  if (isEditing && selectedTemplate) {
    return (
      <Box>
        <Box mb={3} display="flex" justifyContent="space-between" alignItems="center">
          <Typography variant="h6">
            {selectedTemplate.id ? t('emailSettings.templates.editTemplate') as string : t('emailSettings.templates.createTemplate') as string}
          </Typography>
          <Box>
            <Button
              variant="outlined"
              onClick={handleCancel}
              sx={{ mr: 1 }}
            >
              {t('emailSettings.templates.buttons.cancel') as string}
            </Button>
            <Button
              variant="outlined"
              onClick={handleTest}
              sx={{ mr: 1 }}
            >
              {t('emailSettings.templates.buttons.test') as string}
            </Button>
            <LoadingButton
              variant="contained"
              onClick={handleSave}
              loading={loading}
            >
              {t('emailSettings.templates.buttons.save') as string}
            </LoadingButton>
          </Box>
        </Box>

        <Grid container spacing={3}>
          <Grid item xs={12} md={6}>
            <FormControl fullWidth>
              <InputLabel>{t('emailSettings.templates.labels.templateType')}</InputLabel>
              <Select
                value={selectedTemplate.templateType}
                label={t('emailSettings.templates.labels.templateType')}
                onChange={(e) => setSelectedTemplate(prev => prev ? {
                  ...prev,
                  templateType: e.target.value as Template['templateType']
                } : null)}
              >
                {/* Original template types */}
                <MenuItem value="security_event">{t('emailSettings.templates.menuItems.securityEvent')}</MenuItem>
                <MenuItem value="job_completion">{t('emailSettings.templates.menuItems.jobCompletion')}</MenuItem>
                <MenuItem value="admin_error">{t('emailSettings.templates.menuItems.adminError')}</MenuItem>
                <MenuItem value="mfa_code">{t('emailSettings.templates.menuItems.mfaCode')}</MenuItem>
                {/* Notification template types */}
                <MenuItem value="security_password_changed">{t('emailSettings.templates.menuItems.securityPasswordChanged', 'Password Changed')}</MenuItem>
                <MenuItem value="security_mfa_disabled">{t('emailSettings.templates.menuItems.securityMfaDisabled', 'MFA Disabled')}</MenuItem>
                <MenuItem value="security_suspicious_login">{t('emailSettings.templates.menuItems.securitySuspiciousLogin', 'Suspicious Login')}</MenuItem>
                <MenuItem value="job_started">{t('emailSettings.templates.menuItems.jobStarted', 'Job Started')}</MenuItem>
                <MenuItem value="job_failed">{t('emailSettings.templates.menuItems.jobFailed', 'Job Failed')}</MenuItem>
                <MenuItem value="first_crack">{t('emailSettings.templates.menuItems.firstCrack', 'First Crack')}</MenuItem>
                <MenuItem value="task_completed">{t('emailSettings.templates.menuItems.taskCompleted', 'Task Completed')}</MenuItem>
                <MenuItem value="agent_offline">{t('emailSettings.templates.menuItems.agentOffline', 'Agent Offline')}</MenuItem>
                <MenuItem value="agent_error">{t('emailSettings.templates.menuItems.agentError', 'Agent Error')}</MenuItem>
                <MenuItem value="webhook_failure">{t('emailSettings.templates.menuItems.webhookFailure', 'Webhook Failure')}</MenuItem>
              </Select>
            </FormControl>
          </Grid>

          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label={t('emailSettings.templates.labels.name')}
              value={selectedTemplate.name}
              onChange={(e) => setSelectedTemplate(prev => prev ? {
                ...prev,
                name: e.target.value
              } : null)}
            />
          </Grid>

          <Grid item xs={12}>
            <TextField
              fullWidth
              label={t('emailSettings.templates.labels.subject')}
              value={selectedTemplate.subject}
              onChange={(e) => setSelectedTemplate(prev => prev ? {
                ...prev,
                subject: e.target.value
              } : null)}
            />
          </Grid>

          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label={t('emailSettings.templates.labels.htmlContent')}
              value={selectedTemplate.htmlContent}
              onChange={(e) => setSelectedTemplate(prev => prev ? {
                ...prev,
                htmlContent: e.target.value
              } : null)}
              multiline
              rows={15}
            />
          </Grid>

          <Grid item xs={12} md={6}>
            <Paper 
              sx={{ 
                p: 2, 
                height: '100%', 
                maxHeight: '500px', 
                overflow: 'auto',
                backgroundColor: '#1a1a1a',
                color: '#ffffff',
                '& a': { color: '#4fc3f7' },
                '& *': { maxWidth: '100%' }
              }}
            >
              <Typography variant="subtitle2" gutterBottom sx={{ color: '#ffffff' }}>
                {t('emailSettings.templates.preview')}
              </Typography>
              <Box 
                sx={{ 
                  mt: 2,
                }}
                dangerouslySetInnerHTML={{ __html: getPreviewContent() }} 
              />
            </Paper>
          </Grid>

          <Grid item xs={12}>
            <TextField
              fullWidth
              label={t('emailSettings.templates.labels.textContent')}
              value={selectedTemplate.textContent}
              onChange={(e) => setSelectedTemplate(prev => prev ? {
                ...prev,
                textContent: e.target.value
              } : null)}
              multiline
              rows={8}
            />
          </Grid>
        </Grid>
      </Box>
    );
  }

  return (
    <Box>
      <Box mb={3} display="flex" justifyContent="space-between" alignItems="center">
        <Typography variant="h6">{t('emailSettings.templates.title')}</Typography>
        <Button
          variant="contained"
          onClick={() => handleEditTemplate({
            templateType: 'security_event',
            name: '',
            subject: '',
            htmlContent: '',
            textContent: '',
          })}
        >
          {t('emailSettings.templates.buttons.create')}
        </Button>
      </Box>

      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>{t('emailSettings.templates.table.name')}</TableCell>
              <TableCell>{t('emailSettings.templates.table.type')}</TableCell>
              <TableCell>{t('emailSettings.templates.table.subject')}</TableCell>
              <TableCell align="right">{t('emailSettings.templates.table.actions')}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {templates.map((template) => (
              <TableRow key={template.id}>
                <TableCell>{template.name}</TableCell>
                <TableCell>{template.templateType}</TableCell>
                <TableCell>{template.subject}</TableCell>
                <TableCell align="right">
                  <IconButton
                    onClick={() => handleEditTemplate(template)}
                    size="small"
                  >
                    <EditIcon />
                  </IconButton>
                  <IconButton
                    onClick={() => handleDeleteTemplate(template.id!)}
                    size="small"
                  >
                    <DeleteIcon />
                  </IconButton>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  );
}; 