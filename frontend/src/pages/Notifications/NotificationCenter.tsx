import React, { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Typography,
  Paper,
  List,
  ListItem,
  ListItemText,
  ListItemIcon,
  ListItemSecondaryAction,
  IconButton,
  Checkbox,
  Button,
  Chip,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Pagination,
  CircularProgress,
  Alert,
  Tooltip,
  Divider,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  DoneAll as DoneAllIcon,
  CheckCircle as CheckCircleIcon,
  Error as ErrorIcon,
  Warning as WarningIcon,
  Info as InfoIcon,
  Refresh as RefreshIcon,
  FilterList as FilterListIcon,
} from '@mui/icons-material';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { formatDistanceToNow, format } from 'date-fns';
import { useNotifications } from '../../contexts/NotificationContext';
import {
  getNotifications,
  markAsRead,
  markAllAsRead,
  deleteNotifications,
} from '../../services/notifications';
import type {
  Notification,
  NotificationCategory,
  NotificationType,
  NotificationListParams,
} from '../../types/notifications';
import { NOTIFICATION_CATEGORIES, getNotificationTypeMetadata } from '../../types/notifications';

const PAGE_SIZE = 20;

// Get icon for notification type
function getNotificationIcon(type: NotificationType) {
  switch (type) {
    case 'job_completed':
    case 'first_crack':
    case 'task_completed_with_cracks':
      return <CheckCircleIcon color="success" />;
    case 'job_failed':
    case 'agent_error':
    case 'webhook_failure':
      return <ErrorIcon color="error" />;
    case 'agent_offline':
    case 'security_suspicious_login':
      return <WarningIcon color="warning" />;
    case 'security_mfa_disabled':
    case 'security_password_changed':
      return <InfoIcon color="info" />;
    default:
      return <InfoIcon color="action" />;
  }
}

// Get chip color for category
function getCategoryColor(
  category: NotificationCategory
): 'primary' | 'secondary' | 'error' | 'warning' | 'info' | 'success' {
  switch (category) {
    case 'job':
      return 'primary';
    case 'agent':
      return 'secondary';
    case 'security':
      return 'error';
    case 'system':
      return 'warning';
    default:
      return 'info';
  }
}

export const NotificationCenter: React.FC = () => {
  const { t } = useTranslation('notifications');
  const navigate = useNavigate();
  const { refreshUnreadCount } = useNotifications();

  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Filters
  const [categoryFilter, setCategoryFilter] = useState<NotificationCategory | ''>('');
  const [readFilter, setReadFilter] = useState<'all' | 'read' | 'unread'>('all');

  // Selection
  const [selected, setSelected] = useState<Set<string>>(new Set());

  // Fetch notifications
  const fetchNotifications = useCallback(async () => {
    setIsLoading(true);
    setError(null);

    try {
      const params: NotificationListParams = {
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
      };

      if (categoryFilter) {
        params.category = categoryFilter;
      }

      if (readFilter !== 'all') {
        params.read = readFilter === 'read';
      }

      const response = await getNotifications(params);
      setNotifications(response.notifications || []);
      setTotal(response.total);
    } catch (err) {
      console.error('[NotificationCenter] Failed to fetch notifications:', err);
      setError(t('errors.fetchFailed', 'Failed to load notifications'));
    } finally {
      setIsLoading(false);
    }
  }, [page, categoryFilter, readFilter, t]);

  useEffect(() => {
    fetchNotifications();
  }, [fetchNotifications]);

  // Handle mark as read
  const handleMarkAsRead = async (notificationId: string) => {
    try {
      await markAsRead(notificationId);
      setNotifications((prev) =>
        prev.map((n) =>
          n.id === notificationId ? { ...n, in_app_read: true } : n
        )
      );
      refreshUnreadCount();
    } catch (err) {
      console.error('[NotificationCenter] Failed to mark as read:', err);
    }
  };

  // Handle mark all as read
  const handleMarkAllAsRead = async () => {
    try {
      await markAllAsRead();
      setNotifications((prev) =>
        prev.map((n) => ({ ...n, in_app_read: true }))
      );
      refreshUnreadCount();
    } catch (err) {
      console.error('[NotificationCenter] Failed to mark all as read:', err);
    }
  };

  // Handle delete selected
  const handleDeleteSelected = async () => {
    if (selected.size === 0) return;

    try {
      await deleteNotifications(Array.from(selected));
      setNotifications((prev) =>
        prev.filter((n) => !selected.has(n.id))
      );
      setSelected(new Set());
      setTotal((prev) => prev - selected.size);
      refreshUnreadCount();
    } catch (err) {
      console.error('[NotificationCenter] Failed to delete notifications:', err);
    }
  };

  // Handle selection
  const handleSelect = (id: string) => {
    const newSelected = new Set(selected);
    if (newSelected.has(id)) {
      newSelected.delete(id);
    } else {
      newSelected.add(id);
    }
    setSelected(newSelected);
  };

  const handleSelectAll = () => {
    if (selected.size === notifications.length) {
      setSelected(new Set());
    } else {
      setSelected(new Set(notifications.map((n) => n.id)));
    }
  };

  // Handle notification click
  const handleNotificationClick = async (notification: Notification) => {
    if (!notification.in_app_read) {
      await handleMarkAsRead(notification.id);
    }

    if (notification.source_type && notification.source_id) {
      switch (notification.source_type) {
        case 'job':
          navigate(`/jobs/${notification.source_id}`);
          break;
        case 'agent':
          navigate(`/agents/${notification.source_id}`);
          break;
        case 'hashlist':
          navigate(`/hashlists/${notification.source_id}`);
          break;
      }
    }
  };

  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <Box sx={{ p: 3 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', mb: 3 }}>
        <Box>
          <Typography variant="h4" component="h1" gutterBottom>
            {t('center.title', 'Notification Center')}
          </Typography>
          <Typography variant="body1" color="text.secondary">
            {t('center.description', 'View and manage your notifications')}
          </Typography>
        </Box>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button
            variant="outlined"
            startIcon={<DoneAllIcon />}
            onClick={handleMarkAllAsRead}
          >
            {t('markAllRead', 'Mark all read')}
          </Button>
          <IconButton onClick={fetchNotifications} disabled={isLoading}>
            <RefreshIcon />
          </IconButton>
        </Box>
      </Box>

      {/* Filters */}
      <Paper sx={{ p: 2, mb: 3 }}>
        <Box sx={{ display: 'flex', gap: 2, alignItems: 'center', flexWrap: 'wrap' }}>
          <FilterListIcon color="action" />
          <FormControl size="small" sx={{ minWidth: 150 }}>
            <InputLabel>{t('filters.category', 'Category')}</InputLabel>
            <Select
              value={categoryFilter}
              label={t('filters.category', 'Category')}
              onChange={(e) => {
                setCategoryFilter(e.target.value as NotificationCategory | '');
                setPage(1);
              }}
            >
              <MenuItem value="">{t('filters.allCategories', 'All categories')}</MenuItem>
              {NOTIFICATION_CATEGORIES.map((cat) => (
                <MenuItem key={cat.category} value={cat.category}>
                  {t(`categories.${cat.category}`, cat.category)}
                </MenuItem>
              ))}
            </Select>
          </FormControl>

          <FormControl size="small" sx={{ minWidth: 120 }}>
            <InputLabel>{t('filters.status', 'Status')}</InputLabel>
            <Select
              value={readFilter}
              label={t('filters.status', 'Status')}
              onChange={(e) => {
                setReadFilter(e.target.value as 'all' | 'read' | 'unread');
                setPage(1);
              }}
            >
              <MenuItem value="all">{t('filters.all', 'All')}</MenuItem>
              <MenuItem value="unread">{t('filters.unread', 'Unread')}</MenuItem>
              <MenuItem value="read">{t('filters.read', 'Read')}</MenuItem>
            </Select>
          </FormControl>

          {selected.size > 0 && (
            <Button
              color="error"
              startIcon={<DeleteIcon />}
              onClick={handleDeleteSelected}
            >
              {t('deleteSelected', 'Delete {{count}} selected', { count: selected.size })}
            </Button>
          )}
        </Box>
      </Paper>

      {/* Error */}
      {error && (
        <Alert severity="error" sx={{ mb: 3 }}>
          {error}
        </Alert>
      )}

      {/* Notifications List */}
      <Paper>
        {isLoading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 4 }}>
            <CircularProgress />
          </Box>
        ) : notifications.length === 0 ? (
          <Box sx={{ p: 4, textAlign: 'center' }}>
            <Typography color="text.secondary">
              {t('empty', 'No notifications')}
            </Typography>
          </Box>
        ) : (
          <>
            <Box sx={{ p: 1, borderBottom: 1, borderColor: 'divider' }}>
              <Checkbox
                checked={selected.size === notifications.length && notifications.length > 0}
                indeterminate={selected.size > 0 && selected.size < notifications.length}
                onChange={handleSelectAll}
              />
              <Typography variant="body2" component="span" color="text.secondary">
                {t('selectAll', 'Select all')}
              </Typography>
            </Box>

            <List sx={{ py: 0 }}>
              {notifications.map((notification, index) => {
                const metadata = getNotificationTypeMetadata(notification.notification_type);

                return (
                  <React.Fragment key={notification.id}>
                    {index > 0 && <Divider component="li" />}
                    <ListItem
                      sx={{
                        bgcolor: notification.in_app_read ? 'transparent' : 'action.hover',
                      }}
                    >
                      <Checkbox
                        checked={selected.has(notification.id)}
                        onChange={() => handleSelect(notification.id)}
                        onClick={(e) => e.stopPropagation()}
                      />
                      <ListItemIcon sx={{ minWidth: 40 }}>
                        {getNotificationIcon(notification.notification_type)}
                      </ListItemIcon>
                      <ListItemText
                        primary={
                          <Box
                            sx={{ display: 'flex', alignItems: 'center', gap: 1, cursor: 'pointer' }}
                            onClick={() => handleNotificationClick(notification)}
                          >
                            <Typography
                              variant="body1"
                              sx={{
                                fontWeight: notification.in_app_read ? 'normal' : 'bold',
                              }}
                            >
                              {notification.title}
                            </Typography>
                            {metadata && (
                              <Chip
                                label={t(`categories.${metadata.category}`, metadata.category)}
                                size="small"
                                color={getCategoryColor(metadata.category)}
                                variant="outlined"
                              />
                            )}
                          </Box>
                        }
                        secondary={
                          <Box sx={{ mt: 0.5 }}>
                            <Typography variant="body2" color="text.secondary">
                              {notification.message}
                            </Typography>
                            <Typography variant="caption" color="text.secondary">
                              {format(new Date(notification.created_at), 'PPpp')} (
                              {formatDistanceToNow(new Date(notification.created_at), {
                                addSuffix: true,
                              })}
                              )
                            </Typography>
                          </Box>
                        }
                      />
                      <ListItemSecondaryAction>
                        {!notification.in_app_read && (
                          <Tooltip title={t('markRead', 'Mark as read')}>
                            <IconButton
                              edge="end"
                              onClick={() => handleMarkAsRead(notification.id)}
                            >
                              <CheckCircleIcon />
                            </IconButton>
                          </Tooltip>
                        )}
                      </ListItemSecondaryAction>
                    </ListItem>
                  </React.Fragment>
                );
              })}
            </List>

            {/* Pagination */}
            {totalPages > 1 && (
              <Box sx={{ display: 'flex', justifyContent: 'center', p: 2 }}>
                <Pagination
                  count={totalPages}
                  page={page}
                  onChange={(_, newPage) => setPage(newPage)}
                  color="primary"
                />
              </Box>
            )}
          </>
        )}
      </Paper>
    </Box>
  );
};

export default NotificationCenter;
