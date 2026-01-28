import React, {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  useRef,
} from 'react';
import { useSnackbar } from 'notistack';
import { useAuth } from './AuthContext';
import type {
  Notification,
  WSMessage,
  WSNotificationPayload,
  WSUnreadCountPayload,
  WSMarkReadPayload,
  SystemAlert,
  ExtendedWSMessage,
} from '../types/notifications';
import {
  getUnreadCount,
  getRecentNotifications,
  markAsRead as apiMarkAsRead,
  markAllAsRead as apiMarkAllAsRead,
  getNotificationWebSocketUrl,
} from '../services/notifications';

interface NotificationContextType {
  // State
  unreadCount: number;
  recentNotifications: Notification[];
  isConnected: boolean;
  isLoading: boolean;

  // System Alerts (for admins)
  systemAlerts: SystemAlert[];

  // Actions
  refreshUnreadCount: () => Promise<void>;
  refreshRecentNotifications: () => Promise<void>;
  markAsRead: (notificationId: string) => Promise<void>;
  markAllAsRead: () => Promise<void>;
  addNotification: (notification: Notification) => void;
  dismissSystemAlert: (alertId: string) => void;
  clearSystemAlerts: () => void;
}

const NotificationContext = createContext<NotificationContextType | undefined>(
  undefined
);

// WebSocket reconnection settings
const WS_RECONNECT_DELAY = 3000;
const WS_MAX_RECONNECT_ATTEMPTS = 10;

export const NotificationProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const { isAuth, user } = useAuth();
  const { enqueueSnackbar } = useSnackbar();

  const [unreadCount, setUnreadCount] = useState(0);
  const [recentNotifications, setRecentNotifications] = useState<Notification[]>([]);
  const [isConnected, setIsConnected] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [systemAlerts, setSystemAlerts] = useState<SystemAlert[]>([]);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectAttemptsRef = useRef(0);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);

  // Refresh unread count from API
  const refreshUnreadCount = useCallback(async () => {
    if (!isAuth) return;
    try {
      const { count } = await getUnreadCount();
      setUnreadCount(count);
    } catch (error) {
      console.error('[Notifications] Failed to refresh unread count:', error);
    }
  }, [isAuth]);

  // Refresh recent notifications from API
  const refreshRecentNotifications = useCallback(async () => {
    if (!isAuth) return;
    setIsLoading(true);
    try {
      const { notifications } = await getRecentNotifications(5);
      setRecentNotifications(notifications || []);
    } catch (error) {
      console.error('[Notifications] Failed to refresh recent notifications:', error);
    } finally {
      setIsLoading(false);
    }
  }, [isAuth]);

  // Mark notification as read
  const markAsRead = useCallback(
    async (notificationId: string) => {
      try {
        await apiMarkAsRead(notificationId);

        // Update local state
        setRecentNotifications((prev) =>
          prev.map((n) =>
            n.id === notificationId ? { ...n, in_app_read: true } : n
          )
        );
        setUnreadCount((prev) => Math.max(0, prev - 1));
      } catch (error) {
        console.error('[Notifications] Failed to mark as read:', error);
        throw error;
      }
    },
    []
  );

  // Mark all notifications as read
  const markAllAsRead = useCallback(async () => {
    try {
      await apiMarkAllAsRead();

      // Update local state
      setRecentNotifications((prev) =>
        prev.map((n) => ({ ...n, in_app_read: true }))
      );
      setUnreadCount(0);
    } catch (error) {
      console.error('[Notifications] Failed to mark all as read:', error);
      throw error;
    }
  }, []);

  // Add a new notification (from WebSocket)
  const addNotification = useCallback(
    (notification: Notification) => {
      setRecentNotifications((prev) => {
        // Add to front, keep only 5 most recent
        const updated = [notification, ...prev.filter((n) => n.id !== notification.id)];
        return updated.slice(0, 5);
      });

      if (!notification.in_app_read) {
        setUnreadCount((prev) => prev + 1);
      }

      // Show toast notification
      enqueueSnackbar(notification.title, {
        variant: getNotificationVariant(notification.notification_type),
        autoHideDuration: 5000,
      });
    },
    [enqueueSnackbar]
  );

  // Dismiss a system alert
  const dismissSystemAlert = useCallback((alertId: string) => {
    setSystemAlerts((prev) => prev.filter((a) => a.id !== alertId));
  }, []);

  // Clear all system alerts
  const clearSystemAlerts = useCallback(() => {
    setSystemAlerts([]);
  }, []);

  // Add a system alert (from WebSocket - for admins)
  const addSystemAlert = useCallback(
    (alert: SystemAlert) => {
      // Add to front, keep only last 10 alerts
      setSystemAlerts((prev) => {
        const updated = [alert, ...prev.filter((a) => a.id !== alert.id)];
        return updated.slice(0, 10);
      });

      // Show distinctive toast notification for system alerts
      const variant = alert.severity === 'critical' ? 'error' : 'warning';
      enqueueSnackbar(`[SYSTEM] ${alert.title} - ${alert.affected_user}`, {
        variant,
        autoHideDuration: 10000,
        anchorOrigin: { vertical: 'top', horizontal: 'right' },
      });
    },
    [enqueueSnackbar]
  );

  // Handle WebSocket message
  const handleWSMessage = useCallback(
    (event: MessageEvent) => {
      try {
        const message: ExtendedWSMessage = JSON.parse(event.data);

        switch (message.type) {
          case 'notification':
            const notificationPayload = message.payload as WSNotificationPayload;
            addNotification(notificationPayload);
            break;

          case 'unread_count':
            const countPayload = message.payload as WSUnreadCountPayload;
            setUnreadCount(countPayload.count);
            break;

          case 'mark_read':
            const markReadPayload = message.payload as WSMarkReadPayload;
            setRecentNotifications((prev) =>
              prev.map((n) =>
                markReadPayload.notification_ids.includes(n.id)
                  ? { ...n, in_app_read: true }
                  : n
              )
            );
            break;

          case 'system_alert':
            // System alerts are for admin users - real-time security/critical event notifications
            const systemAlertPayload = message.payload as SystemAlert;
            addSystemAlert(systemAlertPayload);
            break;

          case 'pong':
            // Heartbeat response, ignore
            break;

          default:
            console.debug('[Notifications] Unknown WS message type:', message.type);
        }
      } catch (error) {
        console.error('[Notifications] Failed to parse WS message:', error);
      }
    },
    [addNotification, addSystemAlert]
  );

  // Connect to WebSocket
  const connectWebSocket = useCallback(() => {
    if (!isAuth || wsRef.current?.readyState === WebSocket.OPEN) {
      return;
    }

    const wsUrl = getNotificationWebSocketUrl();
    console.debug('[Notifications] Connecting to WebSocket:', wsUrl);

    try {
      const ws = new WebSocket(wsUrl);

      ws.onopen = () => {
        console.debug('[Notifications] WebSocket connected');
        setIsConnected(true);
        reconnectAttemptsRef.current = 0;
      };

      ws.onmessage = handleWSMessage;

      ws.onclose = (event) => {
        console.debug('[Notifications] WebSocket closed:', event.code, event.reason);
        setIsConnected(false);
        wsRef.current = null;

        // Attempt reconnect if still authenticated
        if (
          isAuth &&
          reconnectAttemptsRef.current < WS_MAX_RECONNECT_ATTEMPTS
        ) {
          reconnectAttemptsRef.current += 1;
          console.debug(
            `[Notifications] Scheduling reconnect attempt ${reconnectAttemptsRef.current}/${WS_MAX_RECONNECT_ATTEMPTS}`
          );
          reconnectTimeoutRef.current = setTimeout(() => {
            connectWebSocket();
          }, WS_RECONNECT_DELAY);
        }
      };

      ws.onerror = (error) => {
        console.error('[Notifications] WebSocket error:', error);
      };

      wsRef.current = ws;
    } catch (error) {
      console.error('[Notifications] Failed to create WebSocket:', error);
    }
  }, [isAuth, handleWSMessage]);

  // Disconnect WebSocket
  const disconnectWebSocket = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }

    setIsConnected(false);
    reconnectAttemptsRef.current = 0;
  }, []);

  // WebSocket heartbeat
  useEffect(() => {
    if (!isConnected || !wsRef.current) return;

    const pingInterval = setInterval(() => {
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        wsRef.current.send(JSON.stringify({ type: 'ping' }));
      }
    }, 30000);

    return () => clearInterval(pingInterval);
  }, [isConnected]);

  // Connect/disconnect based on auth state
  useEffect(() => {
    if (isAuth && user) {
      connectWebSocket();
      refreshUnreadCount();
      refreshRecentNotifications();
    } else {
      disconnectWebSocket();
      setUnreadCount(0);
      setRecentNotifications([]);
    }

    return () => {
      disconnectWebSocket();
    };
  }, [isAuth, user, connectWebSocket, disconnectWebSocket, refreshUnreadCount, refreshRecentNotifications]);

  const value: NotificationContextType = {
    unreadCount,
    recentNotifications,
    isConnected,
    isLoading,
    systemAlerts,
    refreshUnreadCount,
    refreshRecentNotifications,
    markAsRead,
    markAllAsRead,
    addNotification,
    dismissSystemAlert,
    clearSystemAlerts,
  };

  return (
    <NotificationContext.Provider value={value}>
      {children}
    </NotificationContext.Provider>
  );
};

// Hook to use notification context
export function useNotifications(): NotificationContextType {
  const context = useContext(NotificationContext);
  if (context === undefined) {
    throw new Error(
      'useNotifications must be used within a NotificationProvider'
    );
  }
  return context;
}

// Helper to get snackbar variant based on notification type
function getNotificationVariant(
  type: string
): 'default' | 'error' | 'success' | 'warning' | 'info' {
  switch (type) {
    case 'job_completed':
    case 'first_crack':
    case 'task_completed_with_cracks':
      return 'success';
    case 'job_failed':
    case 'agent_error':
    case 'webhook_failure':
      return 'error';
    case 'agent_offline':
    case 'security_suspicious_login':
      return 'warning';
    case 'security_mfa_disabled':
    case 'security_password_changed':
      return 'info';
    default:
      return 'default';
  }
}
