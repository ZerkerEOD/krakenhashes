package routes

import (
	"context"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/email"
	adminhandlers "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/user"
	wshandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// NotificationServices holds references to all notification-related services
// that need to be accessible from other parts of the application
type NotificationServices struct {
	Dispatcher           *services.NotificationDispatcher
	Hub                  *services.UserNotificationHub
	WebhookService       *services.NotificationWebhookService
	AgentOfflineMonitor  *services.AgentOfflineMonitor
}

// notificationServicesInstance is the global instance of notification services
var notificationServicesInstance *NotificationServices

// GetNotificationServices returns the global notification services instance
// This allows other parts of the application to dispatch notifications
func GetNotificationServices() *NotificationServices {
	return notificationServicesInstance
}

// GetDispatcher returns the notification dispatcher for dispatching notifications
// Returns nil if notification services haven't been initialized yet
func GetDispatcher() *services.NotificationDispatcher {
	if notificationServicesInstance == nil {
		return nil
	}
	return notificationServicesInstance.Dispatcher
}

// SetupNotificationRoutes configures all notification-related routes and services
// Returns NotificationServices so other parts of the application can dispatch notifications
func SetupNotificationRoutes(
	router *mux.Router,
	database *db.DB,
	emailService *email.Service,
) *NotificationServices {
	debug.Info("Setting up notification routes")

	// Create services
	webhookService := services.NewNotificationWebhookService()
	hub := services.NewUserNotificationHub()

	// Create dispatcher (creates repositories internally)
	dispatcher := services.NewNotificationDispatcher(
		database.DB,
		emailService,
		webhookService,
		hub,
	)

	// Set global dispatcher for access from packages that can't import routes (e.g., handlers/auth)
	services.SetGlobalDispatcher(dispatcher)

	// Create agent offline monitor (creates repositories internally)
	agentOfflineMonitor := services.NewAgentOfflineMonitor(
		database.DB,
		dispatcher,
	)

	// Start the hub and monitor
	hub.Start()
	agentOfflineMonitor.Start(context.Background())
	debug.Info("Started UserNotificationHub and AgentOfflineMonitor")

	// Set the monitor getter for websocket handler to access
	wshandler.SetAgentOfflineMonitorGetter(func() *services.AgentOfflineMonitor {
		return agentOfflineMonitor
	})

	// Create handlers
	notificationHandler := user.NewNotificationHandler(dispatcher, hub)
	webhookHandler := user.NewWebhookHandler(database.DB, webhookService)
	adminNotificationHandler := adminhandlers.NewNotificationSettingsHandler(database.DB, webhookService)
	auditLogHandler := adminhandlers.NewAuditLogHandler(database.DB)
	userNotificationWSHandler := wshandler.NewUserNotificationHandler(hub, dispatcher)

	// =====================
	// User Notification Routes
	// =====================

	// Notification list and management
	router.HandleFunc("/user/notifications", notificationHandler.GetNotifications).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/user/notifications/recent", notificationHandler.GetRecentNotifications).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/user/notifications/unread-count", notificationHandler.GetUnreadCount).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/user/notifications/read-all", notificationHandler.MarkAllAsRead).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/user/notifications/{id}", notificationHandler.MarkAsRead).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/user/notifications", notificationHandler.DeleteNotifications).Methods(http.MethodDelete, http.MethodOptions)
	debug.Info("Configured user notification routes: /user/notifications/*")

	// User webhook management
	router.HandleFunc("/user/webhooks", webhookHandler.GetWebhooks).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/user/webhooks", webhookHandler.CreateWebhook).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/user/webhooks/test-url", webhookHandler.TestWebhookURL).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/user/webhooks/{id}", webhookHandler.GetWebhook).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/user/webhooks/{id}", webhookHandler.UpdateWebhook).Methods(http.MethodPut, http.MethodOptions)
	router.HandleFunc("/user/webhooks/{id}", webhookHandler.DeleteWebhook).Methods(http.MethodDelete, http.MethodOptions)
	router.HandleFunc("/user/webhooks/{id}/test", webhookHandler.TestWebhook).Methods(http.MethodPost, http.MethodOptions)
	debug.Info("Configured user webhook routes: /user/webhooks/*")

	// =====================
	// Admin Notification Routes
	// =====================

	// Admin notification settings (using admin subrouter if available, otherwise main router)
	adminRouter := router.PathPrefix("/admin").Subrouter()
	adminRouter.Use(middleware.AdminOnly)

	// Global webhook settings
	adminRouter.HandleFunc("/notification-settings", adminNotificationHandler.GetGlobalWebhookSettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/notification-settings", adminNotificationHandler.UpdateGlobalWebhookSettings).Methods(http.MethodPut, http.MethodOptions)
	adminRouter.HandleFunc("/notification-settings/test-webhook", adminNotificationHandler.TestGlobalWebhook).Methods(http.MethodPost, http.MethodOptions)

	// Admin view of all user webhooks
	adminRouter.HandleFunc("/users/webhooks", adminNotificationHandler.GetAllUserWebhooks).Methods(http.MethodGet, http.MethodOptions)

	// Agent offline buffer settings
	adminRouter.HandleFunc("/notification-settings/agent-offline", adminNotificationHandler.GetAgentOfflineSettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/notification-settings/agent-offline", adminNotificationHandler.UpdateAgentOfflineSettings).Methods(http.MethodPut, http.MethodOptions)
	debug.Info("Configured admin notification settings routes: /admin/notification-settings/*")

	// Audit log routes (admin-only visibility into security/critical events)
	adminRouter.HandleFunc("/audit-logs", auditLogHandler.GetAuditLogs).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/audit-logs/event-types", auditLogHandler.GetAuditableEventTypes).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/audit-logs/{id}", auditLogHandler.GetAuditLog).Methods(http.MethodGet, http.MethodOptions)
	debug.Info("Configured admin audit log routes: /admin/audit-logs/*")

	// =====================
	// WebSocket Route for User Notifications
	// =====================

	// The WebSocket route needs auth middleware but is under the main router
	router.HandleFunc("/user/notifications/ws", userNotificationWSHandler.ServeWS).Methods(http.MethodGet)
	debug.Info("Configured user notification WebSocket route: /user/notifications/ws")

	debug.Info("Notification routes setup completed")

	// Store the services instance for global access
	notificationServicesInstance = &NotificationServices{
		Dispatcher:          dispatcher,
		Hub:                 hub,
		WebhookService:      webhookService,
		AgentOfflineMonitor: agentOfflineMonitor,
	}

	return notificationServicesInstance
}
