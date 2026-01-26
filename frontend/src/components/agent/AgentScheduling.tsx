import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Box,
  Paper,
  Typography,
  Switch,
  FormControlLabel,
  Grid,
  TextField,
  Button,
  IconButton,
  Chip,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Alert,
  Tooltip,
  Stack,
} from '@mui/material';
import {
  Edit as EditIcon,
  Add as AddIcon,
  Delete as DeleteIcon,
  ContentCopy as CopyIcon,
} from '@mui/icons-material';
import {
  convertLocalTimeToUTC,
  convertUTCTimeToLocal,
  getUserTimezone,
  getTimezoneAbbreviation,
  getUTCOffset,
  getDaysOfWeek,
  isOvernightSchedule,
  getDefaultWorkingHours,
} from '../../utils/timezone';
import { AgentSchedule, AgentScheduleDTO } from '../../types/scheduling';
import { getSystemSetting } from '../../services/systemSettings';

interface AgentSchedulingProps {
  agentId: number;
  schedulingEnabled: boolean;
  scheduleTimezone: string;
  schedules: AgentSchedule[];
  onToggleScheduling: (enabled: boolean, timezone: string) => Promise<void>;
  onUpdateSchedules: (schedules: AgentScheduleDTO[]) => Promise<void>;
  onDeleteSchedule: (dayOfWeek: number) => Promise<void>;
}

const AgentScheduling: React.FC<AgentSchedulingProps> = ({
  agentId,
  schedulingEnabled,
  scheduleTimezone,
  schedules,
  onToggleScheduling,
  onUpdateSchedules,
  onDeleteSchedule,
}) => {
  const { t } = useTranslation('agents');
  const [isEditDialogOpen, setIsEditDialogOpen] = useState(false);
  const [editingSchedules, setEditingSchedules] = useState<Map<number, AgentSchedule>>(new Map());
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [globalSchedulingEnabled, setGlobalSchedulingEnabled] = useState(true);
  const [loadingGlobalSetting, setLoadingGlobalSetting] = useState(true);
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false);
  const [unscheduledDays, setUnscheduledDays] = useState<string[]>([]);
  const daysOfWeek = getDaysOfWeek();
  const userTimezone = getUserTimezone();
  const timezoneDisplay = `${getTimezoneAbbreviation()} (${getUTCOffset()})`;

  // Fetch global scheduling setting
  useEffect(() => {
    const fetchGlobalSetting = async () => {
      try {
        const setting = await getSystemSetting('agent_scheduling_enabled');
        setGlobalSchedulingEnabled(setting.value === 'true');
      } catch (err) {
        console.error('Failed to fetch global scheduling setting:', err);
        // Default to true if we can't fetch the setting
        setGlobalSchedulingEnabled(true);
      } finally {
        setLoadingGlobalSetting(false);
      }
    };
    fetchGlobalSetting();
  }, []);

  // Initialize editing schedules when dialog opens
  useEffect(() => {
    if (isEditDialogOpen && schedules) {
      const scheduleMap = new Map<number, AgentSchedule>();
      schedules.forEach(schedule => {
        // Convert UTC times to local times for display
        const localSchedule: AgentSchedule = {
          ...schedule,
          startTime: convertUTCTimeToLocal(schedule.startTime, schedule.dayOfWeek),
          endTime: convertUTCTimeToLocal(schedule.endTime, schedule.dayOfWeek),
        };
        scheduleMap.set(schedule.dayOfWeek, localSchedule);
      });
      setEditingSchedules(scheduleMap);
    }
  }, [isEditDialogOpen, schedules]);

  const handleToggleScheduling = async () => {
    if (!globalSchedulingEnabled) {
      setError(t('scheduling.globalDisabledError') as string);
      return;
    }

    try {
      await onToggleScheduling(!schedulingEnabled, userTimezone);
    } catch (err) {
      console.error('Failed to toggle scheduling:', err);
    }
  };

  const handleSaveSchedules = async () => {
    // Check for unscheduled days
    const missingDays: string[] = [];
    daysOfWeek.forEach(day => {
      const schedule = editingSchedules.get(day.value);
      if (!schedule || !schedule.startTime || !schedule.endTime) {
        missingDays.push(day.label);
      }
    });

    // If there are unscheduled days, show confirmation
    if (missingDays.length > 0) {
      setUnscheduledDays(missingDays);
      setConfirmDialogOpen(true);
      return;
    }

    // Proceed with save
    await performSave();
  };

  const performSave = async () => {
    setSaving(true);
    setError('');
    setConfirmDialogOpen(false);

    try {
      // Convert local times to UTC before sending
      const scheduleDTOs: AgentScheduleDTO[] = [];

      editingSchedules.forEach((schedule, dayOfWeek) => {
        if (schedule.startTime && schedule.endTime) {
          scheduleDTOs.push({
            dayOfWeek,
            startTimeUTC: convertLocalTimeToUTC(schedule.startTime, dayOfWeek),
            endTimeUTC: convertLocalTimeToUTC(schedule.endTime, dayOfWeek),
            timezone: userTimezone,
            isActive: schedule.isActive,
          });
        }
      });

      await onUpdateSchedules(scheduleDTOs);
      setIsEditDialogOpen(false);
    } catch (err) {
      setError(t('errors.saveSchedulesFailed') as string);
      console.error('Failed to save schedules:', err);
    } finally {
      setSaving(false);
    }
  };

  const handleTimeChange = (dayOfWeek: number, field: 'startTime' | 'endTime', value: string) => {
    const current = editingSchedules.get(dayOfWeek) || {
      agentId,
      dayOfWeek,
      startTime: '',
      endTime: '',
      timezone: userTimezone,
      isActive: true,
    };

    const updated = { ...current, [field]: value };
    setEditingSchedules(new Map(editingSchedules.set(dayOfWeek, updated)));
  };

  const handleDeleteSchedule = async (dayOfWeek: number) => {
    try {
      await onDeleteSchedule(dayOfWeek);
      // Remove from editing schedules if dialog is open
      if (isEditDialogOpen) {
        const newSchedules = new Map(editingSchedules);
        newSchedules.delete(dayOfWeek);
        setEditingSchedules(newSchedules);
      }
    } catch (err) {
      console.error('Failed to delete schedule:', err);
    }
  };

  const handleCopySchedule = (fromDay: number) => {
    const sourceSchedule = editingSchedules.get(fromDay);
    if (!sourceSchedule) return;

    // Copy to all other days
    const newSchedules = new Map(editingSchedules);
    for (let day = 0; day < 7; day++) {
      if (day !== fromDay) {
        newSchedules.set(day, {
          ...sourceSchedule,
          dayOfWeek: day,
        });
      }
    }
    setEditingSchedules(newSchedules);
  };

  const applyPreset = (preset: string) => {
    const newSchedules = new Map<number, AgentSchedule>();

    switch (preset) {
      case 'business':
        // Monday-Friday 08:00-17:00
        for (let day = 1; day <= 5; day++) {
          newSchedules.set(day, {
            agentId,
            dayOfWeek: day,
            startTime: '08:00',
            endTime: '17:00',
            timezone: userTimezone,
            isActive: true,
          });
        }
        break;

      case 'overnight':
        // All days 20:00-08:00
        for (let day = 0; day < 7; day++) {
          newSchedules.set(day, {
            agentId,
            dayOfWeek: day,
            startTime: '20:00',
            endTime: '08:00',
            timezone: userTimezone,
            isActive: true,
          });
        }
        break;

      case 'afterhours':
        // Monday-Friday 17:01-07:59, Saturday-Sunday 00:00-23:59
        for (let day = 1; day <= 5; day++) {
          newSchedules.set(day, {
            agentId,
            dayOfWeek: day,
            startTime: '17:01',
            endTime: '07:59',
            timezone: userTimezone,
            isActive: true,
          });
        }
        // Weekend full days
        newSchedules.set(0, { // Sunday
          agentId,
          dayOfWeek: 0,
          startTime: '00:00',
          endTime: '23:59',
          timezone: userTimezone,
          isActive: true,
        });
        newSchedules.set(6, { // Saturday
          agentId,
          dayOfWeek: 6,
          startTime: '00:00',
          endTime: '23:59',
          timezone: userTimezone,
          isActive: true,
        });
        break;

      case '24hours':
        // All days 00:00-23:59
        for (let day = 0; day < 7; day++) {
          newSchedules.set(day, {
            agentId,
            dayOfWeek: day,
            startTime: '00:00',
            endTime: '23:59',
            timezone: userTimezone,
            isActive: true,
          });
        }
        break;
    }

    setEditingSchedules(newSchedules);
  };

  const renderScheduleSummary = (dayOfWeek: number) => {
    const schedule = schedules?.find(s => s.dayOfWeek === dayOfWeek);
    if (!schedule) {
      return (
        <Box display="flex" alignItems="center" gap={1}>
          <Chip
            size="small"
            label={t('scheduling.unavailable') as string}
            color="error"
            variant="filled"
            sx={{ fontWeight: 'medium' }}
          />
          <Typography variant="body2" color="error">
            {t('scheduling.noScheduleSet') as string}
          </Typography>
        </Box>
      );
    }

    // Convert UTC to local for display
    const localStart = convertUTCTimeToLocal(schedule.startTime, dayOfWeek);
    const localEnd = convertUTCTimeToLocal(schedule.endTime, dayOfWeek);
    const overnight = isOvernightSchedule(localStart, localEnd);

    if (!schedule.isActive) {
      return (
        <Box display="flex" alignItems="center" gap={1}>
          <Chip
            size="small"
            label={t('scheduling.disabled') as string}
            color="default"
            variant="filled"
          />
          <Typography variant="body2" color="text.disabled">
            {localStart} - {localEnd}
          </Typography>
        </Box>
      );
    }

    return (
      <Box display="flex" alignItems="center" gap={1}>
        <Chip
          size="small"
          label={t('scheduling.active') as string}
          color="success"
          variant="outlined"
        />
        <Typography variant="body2" color="text.primary">
          {localStart} - {localEnd}
        </Typography>
        {overnight && <Chip size="small" label={t('scheduling.overnight') as string} color="info" variant="outlined" />}
      </Box>
    );
  };

  return (
    <Paper sx={{ p: 3 }}>
      <Box display="flex" justifyContent="space-between" alignItems="center" mb={2}>
        <Typography variant="h6">{t('scheduling.title') as string}</Typography>
        <FormControlLabel
          control={
            <Switch
              checked={schedulingEnabled}
              onChange={handleToggleScheduling}
              color="primary"
              disabled={!globalSchedulingEnabled || loadingGlobalSetting}
            />
          }
          label={t('scheduling.enableScheduling') as string}
        />
      </Box>

      {!globalSchedulingEnabled && !loadingGlobalSetting && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          {t('scheduling.globalDisabledWarning') as string}
        </Alert>
      )}

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

      {schedulingEnabled ? (
        <>
          <Alert severity="warning" sx={{ mb: 2 }}>
            <strong>{t('common.important') as string}:</strong> {t('scheduling.schedulingImportantNote') as string}
          </Alert>

          {!globalSchedulingEnabled && (
            <Alert severity="info" sx={{ mb: 2 }}>
              {t('scheduling.schedulesNotActiveInfo') as string}
            </Alert>
          )}

          <Typography variant="body2" color="text.secondary" mb={2}>
            {t('scheduling.timesShownIn', { timezone: timezoneDisplay }) as string}
          </Typography>

          <Grid container spacing={2}>
            {daysOfWeek.map(day => (
              <Grid item xs={12} key={day.value}>
                <Box display="flex" justifyContent="space-between" alignItems="center">
                  <Box flex={1}>
                    <Typography variant="subtitle2">{day.label}</Typography>
                    {renderScheduleSummary(day.value)}
                  </Box>
                  <IconButton
                    size="small"
                    onClick={() => setIsEditDialogOpen(true)}
                    color="primary"
                  >
                    <EditIcon />
                  </IconButton>
                </Box>
              </Grid>
            ))}
          </Grid>

          <Box mt={3}>
            <Button
              variant="outlined"
              fullWidth
              startIcon={<EditIcon />}
              onClick={() => setIsEditDialogOpen(true)}
            >
              {t('scheduling.editAllSchedules') as string}
            </Button>
          </Box>
        </>
      ) : (
        <Alert severity="info">
          {t('scheduling.alwaysAvailable') as string}
        </Alert>
      )}

      {/* Edit Dialog */}
      <Dialog
        open={isEditDialogOpen}
        onClose={() => setIsEditDialogOpen(false)}
        maxWidth="md"
        fullWidth
      >
        <DialogTitle>{t('scheduling.editDialogTitle') as string}</DialogTitle>
        <DialogContent>
          {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}

          <Typography variant="body2" color="text.secondary" mb={1}>
            {t('scheduling.setDailySchedules', { timezone: timezoneDisplay }) as string}
          </Typography>

          <Alert severity="info" sx={{ mb: 2 }}>
            <strong>{t('common.tip') as string}:</strong> {t('scheduling.overnightTip') as string}
          </Alert>

          <Box sx={{ mb: 3 }}>
            <Typography variant="subtitle2" gutterBottom>
              {t('scheduling.quickPresets') as string}:
            </Typography>
            <Stack direction="row" spacing={1}>
              <Tooltip
                title={
                  <Box>
                    <Typography variant="body2" fontWeight="bold">{t('scheduling.presets.businessHours') as string}</Typography>
                    <Typography variant="caption" display="block">{t('scheduling.presets.businessHoursDesc1') as string}</Typography>
                    <Typography variant="caption">{t('scheduling.presets.businessHoursDesc2') as string}</Typography>
                  </Box>
                }
                placement="top"
              >
                <Button
                  size="small"
                  variant="outlined"
                  onClick={() => applyPreset('business')}
                >
                  {t('scheduling.presets.businessHours') as string}
                </Button>
              </Tooltip>
              <Tooltip
                title={
                  <Box>
                    <Typography variant="body2" fontWeight="bold">{t('scheduling.presets.overnight') as string}</Typography>
                    <Typography variant="caption" display="block">{t('scheduling.presets.overnightDesc1') as string}</Typography>
                    <Typography variant="caption">{t('scheduling.presets.overnightDesc2') as string}</Typography>
                  </Box>
                }
                placement="top"
              >
                <Button
                  size="small"
                  variant="outlined"
                  onClick={() => applyPreset('overnight')}
                >
                  {t('scheduling.presets.overnight') as string}
                </Button>
              </Tooltip>
              <Tooltip
                title={
                  <Box>
                    <Typography variant="body2" fontWeight="bold">{t('scheduling.presets.afterHours') as string}</Typography>
                    <Typography variant="caption" display="block">{t('scheduling.presets.afterHoursDesc1') as string}</Typography>
                    <Typography variant="caption">{t('scheduling.presets.afterHoursDesc2') as string}</Typography>
                  </Box>
                }
                placement="top"
              >
                <Button
                  size="small"
                  variant="outlined"
                  onClick={() => applyPreset('afterhours')}
                >
                  {t('scheduling.presets.afterHours') as string}
                </Button>
              </Tooltip>
              <Tooltip
                title={
                  <Box>
                    <Typography variant="body2" fontWeight="bold">{t('scheduling.presets.24Hours') as string}</Typography>
                    <Typography variant="caption" display="block">{t('scheduling.presets.24HoursDesc1') as string}</Typography>
                    <Typography variant="caption">{t('scheduling.presets.24HoursDesc2') as string}</Typography>
                  </Box>
                }
                placement="top"
              >
                <Button
                  size="small"
                  variant="outlined"
                  onClick={() => applyPreset('24hours')}
                >
                  {t('scheduling.presets.24Hours') as string}
                </Button>
              </Tooltip>
            </Stack>
          </Box>

          <Grid container spacing={2}>
            {daysOfWeek.map(day => {
              const schedule = editingSchedules.get(day.value);
              const hasSchedule = schedule?.startTime && schedule?.endTime;
              
              return (
                <Grid item xs={12} key={day.value}>
                  <Paper variant="outlined" sx={{ p: 2 }}>
                    <Box display="flex" alignItems="center" gap={2}>
                      <Typography variant="subtitle2" sx={{ minWidth: 100 }}>
                        {day.label}
                      </Typography>
                      
                      {hasSchedule ? (
                        <>
                          <TextField
                            label={t('scheduling.startTime') as string}
                            value={schedule.startTime}
                            onChange={(e) => handleTimeChange(day.value, 'startTime', e.target.value)}
                            placeholder="HH:MM"
                            size="small"
                            sx={{ width: 120 }}
                          />
                          <Typography>-</Typography>
                          <TextField
                            label={t('scheduling.endTime') as string}
                            value={schedule.endTime}
                            onChange={(e) => handleTimeChange(day.value, 'endTime', e.target.value)}
                            placeholder="HH:MM"
                            size="small"
                            sx={{ width: 120 }}
                          />
                          <Tooltip
                            title={t('scheduling.activeTooltip') as string}
                            placement="top"
                          >
                            <FormControlLabel
                              control={
                                <Switch
                                  checked={schedule.isActive}
                                  onChange={(e) => {
                                    const updated = { ...schedule, isActive: e.target.checked };
                                    setEditingSchedules(new Map(editingSchedules.set(day.value, updated)));
                                  }}
                                  size="small"
                                />
                              }
                              label={t('scheduling.active') as string}
                            />
                          </Tooltip>
                          <IconButton
                            size="small"
                            onClick={() => handleCopySchedule(day.value)}
                            title={t('scheduling.copyToOtherDays') as string}
                          >
                            <CopyIcon />
                          </IconButton>
                          <IconButton
                            size="small"
                            onClick={() => {
                              const newSchedules = new Map(editingSchedules);
                              newSchedules.delete(day.value);
                              setEditingSchedules(newSchedules);
                            }}
                            color="error"
                          >
                            <DeleteIcon />
                          </IconButton>
                        </>
                      ) : (
                        <Button
                          variant="outlined"
                          size="small"
                          startIcon={<AddIcon />}
                          onClick={() => {
                            const defaultHours = getDefaultWorkingHours();
                            handleTimeChange(day.value, 'startTime', defaultHours.start);
                            handleTimeChange(day.value, 'endTime', defaultHours.end);
                          }}
                        >
                          {t('scheduling.addSchedule') as string}
                        </Button>
                      )}
                    </Box>
                    
                    {hasSchedule && isOvernightSchedule(schedule.startTime, schedule.endTime) && (
                      <Alert severity="info" sx={{ mt: 1 }}>
                        {t('scheduling.spansMidnight') as string}
                      </Alert>
                    )}
                  </Paper>
                </Grid>
              );
            })}
          </Grid>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setIsEditDialogOpen(false)}>{t('buttons.cancel') as string}</Button>
          <Button
            onClick={handleSaveSchedules}
            variant="contained"
            disabled={saving}
          >
            {saving ? (t('buttons.saving') as string) : (t('buttons.saveChanges') as string)}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Confirmation Dialog for Unscheduled Days */}
      <Dialog
        open={confirmDialogOpen}
        onClose={() => setConfirmDialogOpen(false)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>{t('scheduling.warningUnscheduledDays') as string}</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>
            {t('scheduling.unscheduledDaysWarning') as string}
          </Alert>
          <Box sx={{ pl: 2 }}>
            {unscheduledDays.map((day, index) => (
              <Typography key={index} variant="body2" color="error" sx={{ mb: 0.5 }}>
                * {day}
              </Typography>
            ))}
          </Box>
          <Typography variant="body2" sx={{ mt: 2 }}>
            {t('scheduling.continueQuestion') as string}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setConfirmDialogOpen(false)}>
            {t('buttons.goBack') as string}
          </Button>
          <Button
            onClick={performSave}
            variant="contained"
            color="warning"
          >
            {t('buttons.continueAnyway') as string}
          </Button>
        </DialogActions>
      </Dialog>
    </Paper>
  );
};

export default AgentScheduling;