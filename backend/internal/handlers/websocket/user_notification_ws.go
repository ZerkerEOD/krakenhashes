package websocket

import (
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var userNotificationUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Implement proper origin checking based on configuration
		return true
	},
}

// UserNotificationHandler handles WebSocket connections for user notifications
type UserNotificationHandler struct {
	hub        *services.UserNotificationHub
	dispatcher *services.NotificationDispatcher
}

// NewUserNotificationHandler creates a new user notification WebSocket handler
func NewUserNotificationHandler(
	hub *services.UserNotificationHub,
	dispatcher *services.NotificationDispatcher,
) *UserNotificationHandler {
	return &UserNotificationHandler{
		hub:        hub,
		dispatcher: dispatcher,
	}
}

// ServeWS handles WebSocket connections from browser clients
func (h *UserNotificationHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context (set by auth middleware)
	userIDValue := r.Context().Value("user_id")
	if userIDValue == nil {
		debug.Warning("WebSocket connection attempt without user ID in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID, ok := userIDValue.(uuid.UUID)
	if !ok {
		// Try string conversion
		if userIDStr, ok := userIDValue.(string); ok {
			var err error
			userID, err = uuid.Parse(userIDStr)
			if err != nil {
				debug.Warning("Invalid user ID format in context: %v", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		} else {
			debug.Warning("User ID in context is not a UUID")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	debug.Log("User notification WebSocket connection attempt", map[string]interface{}{
		"user_id":     userID,
		"remote_addr": r.RemoteAddr,
	})

	// Upgrade the HTTP connection to a WebSocket connection
	conn, err := userNotificationUpgrader.Upgrade(w, r, nil)
	if err != nil {
		debug.Error("Failed to upgrade WebSocket connection: %v", err)
		return
	}

	// Create a new client
	client := services.NewUserNotificationClient(h.hub, userID, conn)

	// Register the client with the hub
	h.hub.Register(client)

	// Check if user is an admin and register them for system alerts
	if roleValue := r.Context().Value("user_role"); roleValue != nil {
		if role, ok := roleValue.(string); ok && role == "admin" {
			h.hub.RegisterAdmin(userID)
			debug.Log("Admin user registered for system alerts", map[string]interface{}{
				"user_id": userID,
			})
		}
	}

	// Send current unread count immediately after connection
	go func() {
		count, err := h.dispatcher.GetUnreadCount(r.Context(), userID)
		if err != nil {
			debug.Warning("Failed to get unread count for new connection: %v", err)
			return
		}
		h.hub.SendUnreadCount(userID, count)
	}()

	// Start the read and write pumps in goroutines
	go client.WritePump()
	go client.ReadPump()

	debug.Log("User notification WebSocket connection established", map[string]interface{}{
		"user_id":           userID,
		"total_connections": h.hub.GetConnectionCount(userID),
	})
}
