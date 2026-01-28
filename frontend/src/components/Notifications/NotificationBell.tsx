import React, { useState } from 'react';
import {
  IconButton,
  Badge,
  Popover,
  Box,
  Typography,
  List,
  ListItem,
  ListItemText,
  ListItemIcon,
  Button,
  Divider,
  CircularProgress,
  Tooltip,
} from '@mui/material';
import {
  Notifications as NotificationsIcon,
  NotificationsOff as NotificationsOffIcon,
  CheckCircle as CheckCircleIcon,
  Error as ErrorIcon,
  Warning as WarningIcon,
  Info as InfoIcon,
  DoneAll as DoneAllIcon,
} from '@mui/icons-material';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useNotifications } from '../../contexts/NotificationContext';
import { formatDistanceToNow } from 'date-fns';
import type { Notification, NotificationType } from '../../types/notifications';

// Get icon for notification type
function getNotificationIcon(type: NotificationType) {
  switch (type) {
    case 'job_completed':
    case 'first_crack':
    case 'task_completed_with_cracks':
      return <CheckCircleIcon color="success" fontSize="small" />;
    case 'job_failed':
    case 'agent_error':
    case 'webhook_failure':
      return <ErrorIcon color="error" fontSize="small" />;
    case 'agent_offline':
    case 'security_suspicious_login':
      return <WarningIcon color="warning" fontSize="small" />;
    case 'security_mfa_disabled':
    case 'security_password_changed':
      return <InfoIcon color="info" fontSize="small" />;
    default:
      return <InfoIcon color="action" fontSize="small" />;
  }
}

export const NotificationBell: React.FC = () => {
  const { t } = useTranslation('notifications');
  const navigate = useNavigate();
  const {
    unreadCount,
    recentNotifications,
    isConnected,
    isLoading,
    markAsRead,
    markAllAsRead,
    refreshRecentNotifications,
  } = useNotifications();

  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);
  const open = Boolean(anchorEl);

  const handleClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    setAnchorEl(event.currentTarget);
    refreshRecentNotifications();
  };

  const handleClose = () => {
    setAnchorEl(null);
  };

  const handleNotificationClick = async (notification: Notification) => {
    // Mark as read
    if (!notification.in_app_read) {
      await markAsRead(notification.id);
    }

    // Navigate to source if available
    if (notification.source_type && notification.source_id) {
      handleClose();
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
        default:
          navigate('/notifications');
      }
    }
  };

  const handleViewAll = () => {
    handleClose();
    navigate('/notifications');
  };

  const handleMarkAllAsRead = async () => {
    await markAllAsRead();
  };

  return (
    <>
      <Tooltip title={isConnected ? t('connected', 'Notifications') : t('disconnected', 'Reconnecting...')}>
        <IconButton
          color="inherit"
          onClick={handleClick}
          aria-label={t('aria.bellButton', 'Show notifications')}
        >
          <Badge badgeContent={unreadCount} color="error" max={99}>
            {isConnected ? (
              <NotificationsIcon />
            ) : (
              <NotificationsOffIcon color="disabled" />
            )}
          </Badge>
        </IconButton>
      </Tooltip>

      <Popover
        open={open}
        anchorEl={anchorEl}
        onClose={handleClose}
        anchorOrigin={{
          vertical: 'bottom',
          horizontal: 'right',
        }}
        transformOrigin={{
          vertical: 'top',
          horizontal: 'right',
        }}
        PaperProps={{
          sx: {
            width: 360,
            maxHeight: 480,
          },
        }}
      >
        <Box sx={{ p: 2, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <Typography variant="h6">{t('title', 'Notifications')}</Typography>
          {unreadCount > 0 && (
            <Button
              size="small"
              startIcon={<DoneAllIcon />}
              onClick={handleMarkAllAsRead}
            >
              {t('markAllRead', 'Mark all read')}
            </Button>
          )}
        </Box>

        <Divider />

        {isLoading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 3 }}>
            <CircularProgress size={24} />
          </Box>
        ) : recentNotifications.length === 0 ? (
          <Box sx={{ p: 3, textAlign: 'center' }}>
            <NotificationsIcon sx={{ fontSize: 48, color: 'text.secondary', mb: 1 }} />
            <Typography color="text.secondary">
              {t('empty', 'No notifications')}
            </Typography>
          </Box>
        ) : (
          <List sx={{ py: 0 }}>
            {recentNotifications.map((notification) => (
              <ListItem
                key={notification.id}
                button
                onClick={() => handleNotificationClick(notification)}
                sx={{
                  bgcolor: notification.in_app_read ? 'transparent' : 'action.hover',
                  '&:hover': {
                    bgcolor: 'action.selected',
                  },
                }}
              >
                <ListItemIcon sx={{ minWidth: 36 }}>
                  {getNotificationIcon(notification.notification_type)}
                </ListItemIcon>
                <ListItemText
                  primary={
                    <Typography
                      variant="body2"
                      sx={{
                        fontWeight: notification.in_app_read ? 'normal' : 'bold',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {notification.title}
                    </Typography>
                  }
                  secondary={
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      sx={{
                        display: 'block',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {formatDistanceToNow(new Date(notification.created_at), {
                        addSuffix: true,
                      })}
                    </Typography>
                  }
                />
              </ListItem>
            ))}
          </List>
        )}

        <Divider />

        <Box sx={{ p: 1 }}>
          <Button fullWidth onClick={handleViewAll}>
            {t('viewAll', 'View all notifications')}
          </Button>
        </Box>
      </Popover>
    </>
  );
};

export default NotificationBell;
