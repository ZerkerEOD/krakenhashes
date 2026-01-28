package services

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// UserNotificationHub manages WebSocket connections for user notifications
type UserNotificationHub struct {
	// Map of user ID to their connected clients
	clients map[uuid.UUID]map[*UserNotificationClient]bool

	// Map of admin user IDs for system alert broadcasting
	adminClients map[uuid.UUID]bool

	// Channels for hub operations
	register   chan *UserNotificationClient
	unregister chan *UserNotificationClient
	broadcast  chan *UserNotificationBroadcast

	// Mutex for thread-safe access to clients map
	mu sync.RWMutex

	// Running state
	running bool
	stopCh  chan struct{}
}

// UserNotificationClient represents a connected browser client
type UserNotificationClient struct {
	hub     *UserNotificationHub
	userID  uuid.UUID
	conn    *websocket.Conn
	send    chan []byte
	closeCh chan struct{}
}

// UserNotificationBroadcast represents a message to broadcast to a user
type UserNotificationBroadcast struct {
	UserID  uuid.UUID
	Message []byte
}

// WebSocket message types
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// NewUserNotificationHub creates a new notification hub
func NewUserNotificationHub() *UserNotificationHub {
	return &UserNotificationHub{
		clients:      make(map[uuid.UUID]map[*UserNotificationClient]bool),
		adminClients: make(map[uuid.UUID]bool),
		register:     make(chan *UserNotificationClient, 256),
		unregister:   make(chan *UserNotificationClient, 256),
		broadcast:    make(chan *UserNotificationBroadcast, 256),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the hub's main loop
func (h *UserNotificationHub) Start() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.mu.Unlock()

	debug.Info("Starting user notification hub")
	go h.run()
}

// Stop stops the hub
func (h *UserNotificationHub) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return
	}

	close(h.stopCh)
	h.running = false
	debug.Info("User notification hub stopped")
}

// run is the main hub loop
func (h *UserNotificationHub) run() {
	for {
		select {
		case <-h.stopCh:
			// Close all client connections
			h.mu.Lock()
			for _, clients := range h.clients {
				for client := range clients {
					close(client.closeCh)
				}
			}
			h.clients = make(map[uuid.UUID]map[*UserNotificationClient]bool)
			h.adminClients = make(map[uuid.UUID]bool)
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			if h.clients[client.userID] == nil {
				h.clients[client.userID] = make(map[*UserNotificationClient]bool)
			}
			h.clients[client.userID][client] = true
			h.mu.Unlock()

			debug.Log("User notification client registered", map[string]interface{}{
				"user_id":      client.userID,
				"total_clients": h.GetConnectionCount(client.userID),
			})

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.clients[client.userID]; ok {
				if _, exists := clients[client]; exists {
					delete(clients, client)
					close(client.send)
					if len(clients) == 0 {
						delete(h.clients, client.userID)
						// Also remove from admin tracking if no more connections
						delete(h.adminClients, client.userID)
					}
				}
			}
			h.mu.Unlock()

			debug.Log("User notification client unregistered", map[string]interface{}{
				"user_id": client.userID,
			})

		case broadcast := <-h.broadcast:
			h.mu.RLock()
			if clients, ok := h.clients[broadcast.UserID]; ok {
				for client := range clients {
					select {
					case client.send <- broadcast.Message:
					default:
						// Buffer full, client is slow
						debug.Warning("Client send buffer full, dropping message", map[string]interface{}{
							"user_id": broadcast.UserID,
						})
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// SendToUser sends a notification to all of a user's connected clients
func (h *UserNotificationHub) SendToUser(userID uuid.UUID, notification *models.Notification) {
	msg := WSMessage{
		Type:    "notification",
		Payload: notification,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		debug.Error("Failed to marshal notification: %v", err)
		return
	}

	h.broadcast <- &UserNotificationBroadcast{
		UserID:  userID,
		Message: data,
	}
}

// SendUnreadCount sends the current unread count to a user
func (h *UserNotificationHub) SendUnreadCount(userID uuid.UUID, count int) {
	msg := WSMessage{
		Type: "unread_count",
		Payload: map[string]int{
			"count": count,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		debug.Error("Failed to marshal unread count: %v", err)
		return
	}

	h.broadcast <- &UserNotificationBroadcast{
		UserID:  userID,
		Message: data,
	}
}

// SendMarkRead sends a mark read confirmation to a user
func (h *UserNotificationHub) SendMarkRead(userID uuid.UUID, notificationIDs []uuid.UUID) {
	msg := WSMessage{
		Type: "mark_read",
		Payload: map[string]interface{}{
			"notification_ids": notificationIDs,
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		debug.Error("Failed to marshal mark read: %v", err)
		return
	}

	h.broadcast <- &UserNotificationBroadcast{
		UserID:  userID,
		Message: data,
	}
}

// IsUserConnected checks if a user has any active connections
func (h *UserNotificationHub) IsUserConnected(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

// GetConnectionCount returns the number of active connections for a user
func (h *UserNotificationHub) GetConnectionCount(userID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID])
}

// GetTotalConnections returns the total number of active connections
func (h *UserNotificationHub) GetTotalConnections() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	total := 0
	for _, clients := range h.clients {
		total += len(clients)
	}
	return total
}

// Register registers a new client with the hub
func (h *UserNotificationHub) Register(client *UserNotificationClient) {
	h.register <- client
}

// Unregister unregisters a client from the hub
func (h *UserNotificationHub) Unregister(client *UserNotificationClient) {
	h.unregister <- client
}

// NewUserNotificationClient creates a new client
func NewUserNotificationClient(hub *UserNotificationHub, userID uuid.UUID, conn *websocket.Conn) *UserNotificationClient {
	return &UserNotificationClient{
		hub:     hub,
		userID:  userID,
		conn:    conn,
		send:    make(chan []byte, 256),
		closeCh: make(chan struct{}),
	}
}

// WritePump pumps messages from the hub to the websocket connection
func (c *UserNotificationClient) WritePump() {
	ticker := time.NewTicker(54 * time.Second) // Ping interval
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case <-c.closeCh:
			return
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Channel closed
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Batch any queued messages
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump pumps messages from the websocket connection to the hub
func (c *UserNotificationClient) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512 * 1024) // 512KB max message size
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				debug.Warning("WebSocket read error: %v", err)
			}
			break
		}

		// Handle incoming messages from client
		c.handleMessage(message)
	}
}

// handleMessage handles incoming messages from the client
func (c *UserNotificationClient) handleMessage(message []byte) {
	var msg WSMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		debug.Warning("Failed to unmarshal client message: %v", err)
		return
	}

	switch msg.Type {
	case "ping":
		// Respond with pong
		response := WSMessage{Type: "pong", Payload: nil}
		data, _ := json.Marshal(response)
		select {
		case c.send <- data:
		default:
		}

	case "mark_read":
		// Client is marking notifications as read
		// This would typically be handled via REST API, but we can acknowledge it
		debug.Log("Client marked notifications as read", map[string]interface{}{
			"user_id": c.userID,
		})

	default:
		debug.Log("Unknown message type from client", map[string]interface{}{
			"type":    msg.Type,
			"user_id": c.userID,
		})
	}
}

// RegisterAdmin marks a user as an admin for system alert broadcasting
func (h *UserNotificationHub) RegisterAdmin(userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.adminClients[userID] = true
	debug.Log("Admin registered for system alerts", map[string]interface{}{
		"user_id": userID,
	})
}

// UnregisterAdmin removes a user from admin tracking
func (h *UserNotificationHub) UnregisterAdmin(userID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.adminClients, userID)
	debug.Log("Admin unregistered from system alerts", map[string]interface{}{
		"user_id": userID,
	})
}

// IsAdmin checks if a user is registered as an admin
func (h *UserNotificationHub) IsAdmin(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.adminClients[userID]
}

// BroadcastSystemAlert sends a system alert to all connected admin users
// This is used for security events and critical system alerts
func (h *UserNotificationHub) BroadcastSystemAlert(alert *models.SystemAlert) {
	if alert == nil {
		return
	}

	msg := WSMessage{
		Type:    "system_alert",
		Payload: alert,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		debug.Error("Failed to marshal system alert: %v", err)
		return
	}

	h.mu.RLock()
	adminCount := 0
	for adminID := range h.adminClients {
		// Check if admin has active connections
		if clients, ok := h.clients[adminID]; ok && len(clients) > 0 {
			adminCount++
			for client := range clients {
				select {
				case client.send <- data:
				default:
					debug.Warning("Admin client send buffer full, dropping system alert", map[string]interface{}{
						"user_id": adminID,
					})
				}
			}
		}
	}
	h.mu.RUnlock()

	debug.Log("System alert broadcasted to admins", map[string]interface{}{
		"event_type":  alert.EventType,
		"severity":    alert.Severity,
		"admin_count": adminCount,
	})
}
