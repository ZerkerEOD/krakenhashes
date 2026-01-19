package websocket

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/cache/filehash"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Default connection timing values
const (
	defaultWriteWait  = 10 * time.Second
	defaultPongWait   = 60 * time.Second
	defaultPingPeriod = 54 * time.Second
	maxMessageSize    = 50 * 1024 * 1024 // 50MB (for large crack batches)
)

// Connection timing configuration
var (
	writeWait  time.Duration
	pongWait   time.Duration
	pingPeriod time.Duration
)

// getEnvDuration gets a duration from an environment variable with a default value
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	debug.Info("Attempting to load environment variable: %s", key)
	value := os.Getenv(key)
	debug.Info("Environment variable %s value: %q", key, value)

	if value != "" {
		duration, err := time.ParseDuration(value)
		if err == nil {
			debug.Info("Successfully parsed %s: %v", key, duration)
			return duration
		}
		debug.Warning("Invalid %s value: %s, using default: %v", key, value, defaultValue)
	}
	debug.Info("No %s environment variable found, using default: %v", key, defaultValue)
	return defaultValue
}

// initTimingConfig initializes the timing configuration from environment variables
func initTimingConfig() {
	debug.Info("Initializing WebSocket timing configuration")
	writeWait = getEnvDuration("KH_WRITE_WAIT", defaultWriteWait)
	pongWait = getEnvDuration("KH_PONG_WAIT", defaultPongWait)
	pingPeriod = getEnvDuration("KH_PING_PERIOD", defaultPingPeriod)
	debug.Info("WebSocket timing configuration initialized:")
	debug.Info("- Write Wait: %v", writeWait)
	debug.Info("- Pong Wait: %v", pongWait)
	debug.Info("- Ping Period: %v", pingPeriod)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  maxMessageSize,
	WriteBufferSize: maxMessageSize,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Implement proper origin checking
		return true
	},
	HandshakeTimeout: writeWait,
	// TLS configuration is handled by the server
}

// Handler manages WebSocket connections for agents
type Handler struct {
	wsService           *wsservice.Service
	agentService        *services.AgentService
	jobExecutionService *services.JobExecutionService
	systemSettingsRepo  *repository.SystemSettingsRepository
	jobTaskRepo         *repository.JobTaskRepository
	jobExecRepo         *repository.JobExecutionRepository
	potfileHistory      *filehash.PotfileHistory
	tlsConfig           *tls.Config
	clients             map[int]*Client
	mu                  sync.RWMutex

	// Inventory callback system for pre-benchmark file checks
	// Key is agentID - only one pending file sync callback per agent at a time
	inventoryCallbacks   map[int]chan *wsservice.FileSyncResponsePayload
	inventoryCallbacksMu sync.RWMutex
}

// Client represents a connected agent
type Client struct {
	handler *Handler
	conn    *websocket.Conn
	agent   *models.Agent
	send    chan *wsservice.Message
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewHandler creates a new WebSocket handler
func NewHandler(wsService *wsservice.Service, agentService *services.AgentService, jobExecutionService *services.JobExecutionService, systemSettingsRepo *repository.SystemSettingsRepository, jobTaskRepo *repository.JobTaskRepository, jobExecRepo *repository.JobExecutionRepository, tlsConfig *tls.Config, potfileHistory *filehash.PotfileHistory) *Handler {
	// Initialize timing configuration
	initTimingConfig()

	return &Handler{
		wsService:           wsService,
		agentService:        agentService,
		jobExecutionService: jobExecutionService,
		systemSettingsRepo:  systemSettingsRepo,
		jobTaskRepo:         jobTaskRepo,
		jobExecRepo:         jobExecRepo,
		potfileHistory:      potfileHistory,
		tlsConfig:           tlsConfig,
		clients:             make(map[int]*Client),
		inventoryCallbacks:  make(map[int]chan *wsservice.FileSyncResponsePayload),
	}
}

// ServeWS handles WebSocket connections from agents
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	debug.Info("Starting WebSocket connection with timing configuration:")
	debug.Info("- Write Wait: %v", writeWait)
	debug.Info("- Pong Wait: %v", pongWait)
	debug.Info("- Ping Period: %v", pingPeriod)

	debug.Info("New WebSocket connection attempt received from %s", r.RemoteAddr)
	debug.Debug("Request headers: %s", debug.SanitizeHeaders(r.Header))

	if h.tlsConfig != nil {
		if r.TLS == nil {
			debug.Error("TLS connection required but not provided from %s", r.RemoteAddr)
			http.Error(w, "TLS required", http.StatusBadRequest)
			return
		}
		debug.Info("TLS connection verified for %s", r.RemoteAddr)
	}

	// Get API key from header
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		debug.Error("No API key provided from %s", r.RemoteAddr)
		http.Error(w, "API key required", http.StatusUnauthorized)
		return
	}

	// Get agent ID from header
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		debug.Error("No agent ID provided from %s", r.RemoteAddr)
		http.Error(w, "Agent ID required", http.StatusUnauthorized)
		return
	}

	// Validate API key and get agent
	agent, err := h.agentService.GetByAPIKey(r.Context(), apiKey)
	if err != nil {
		debug.Error("Invalid API key from %s: %v", r.RemoteAddr, err)
		http.Error(w, "Invalid API key", http.StatusUnauthorized)
		return
	}

	// Verify agent ID matches
	if fmt.Sprintf("%d", agent.ID) != agentID {
		debug.Error("Agent ID mismatch from %s: provided=%s, actual=%d", r.RemoteAddr, agentID, agent.ID)
		http.Error(w, "Invalid agent ID", http.StatusUnauthorized)
		return
	}

	debug.Info("API key validated for agent %d from %s", agent.ID, r.RemoteAddr)

	// Configure WebSocket upgrader
	upgrader.EnableCompression = true

	// Upgrade connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		debug.Error("Failed to upgrade connection from %s: %v", r.RemoteAddr, err)
		return
	}
	debug.Info("Successfully upgraded to WebSocket connection for agent %d", agent.ID)

	// Create client context
	ctx, cancel := context.WithCancel(context.Background())

	client := &Client{
		handler: h,
		conn:    conn,
		agent:   agent,
		send:    make(chan *wsservice.Message, 256),
		ctx:     ctx,
		cancel:  cancel,
	}

	// If this is the agent's first connection and it has a claim code in metadata, mark it as used
	if agent.Status == models.AgentStatusPending {
		debug.Info("Processing first-time connection for agent %d", agent.ID)
		if claimCode, ok := agent.Metadata["claim_code"]; ok {
			debug.Info("Found claim code for agent %d", agent.ID)
			debug.Debug("Claim code details: %s", claimCode)
			if err := h.agentService.MarkClaimCodeUsed(ctx, claimCode, agent.ID); err != nil {
				debug.Error("Failed to mark claim code as used for agent %d: %v", agent.ID, err)
			} else {
				debug.Info("Successfully marked claim code as used for agent %d", agent.ID)
			}
			// Remove claim code from metadata as it's no longer needed
			delete(agent.Metadata, "claim_code")
			if err := h.agentService.Update(ctx, agent); err != nil {
				debug.Error("Failed to update agent metadata for agent %d: %v", agent.ID, err)
			} else {
				debug.Info("Successfully updated agent %d status", agent.ID)
			}
		}
	}

	debug.Info("Agent %d fully registered and ready", agent.ID)

	// Reset sync status when agent connects (only if not currently executing a task)
	// This ensures the agent will sync files on each connection
	// We check for active tasks to preserve task reconnection behavior
	activeTasks, err := h.jobTaskRepo.GetActiveTasksByAgent(ctx, agent.ID)
	if err != nil {
		debug.Error("Failed to check active tasks for agent %d: %v", agent.ID, err)
	} else if len(activeTasks) == 0 {
		// No active tasks, check if we need to reset sync status
		// Only reset if sync is not already completed (to avoid breaking job scheduling)
		if agent.SyncStatus != models.AgentSyncStatusCompleted {
			debug.Info("Agent %d has no active tasks and sync status is %s, resetting to pending", agent.ID, agent.SyncStatus)
			agent.SyncStatus = models.AgentSyncStatusPending
			agent.SyncStartedAt = sql.NullTime{Valid: false}
			agent.SyncCompletedAt = sql.NullTime{Valid: false}
			agent.SyncError = sql.NullString{Valid: false}
			agent.FilesToSync = 0
			agent.FilesSynced = 0

			if err := h.agentService.Update(ctx, agent); err != nil {
				debug.Error("Failed to reset sync status for agent %d: %v", agent.ID, err)
			} else {
				debug.Info("Successfully reset sync status to pending for agent %d", agent.ID)
			}
		} else {
			debug.Info("Agent %d has completed sync status, preserving it for job scheduling", agent.ID)
		}
	} else {
		debug.Info("Agent %d has %d active task(s), preserving sync status", agent.ID, len(activeTasks))
	}

	// NOTE: We no longer immediately mark agent as active here
	// The agent will be marked active after we receive its current task status
	debug.Info("Agent %d connected - waiting for task status report before marking as active", agent.ID)
	
	// Log job handler status for debugging
	if h.wsService.GetJobHandler() != nil {
		debug.Info("Agent %d connected - job handler is available", agent.ID)
	} else {
		debug.Warning("Agent %d connected - job handler is NOT available", agent.ID)
	}

	// Update heartbeat timestamp
	if err := h.agentService.UpdateHeartbeat(ctx, agent.ID); err != nil {
		debug.Error("Failed to update agent heartbeat: %v", err)
	} else {
		debug.Info("Successfully updated heartbeat for agent %d", agent.ID)
	}

	// Register client
	h.mu.Lock()
	h.clients[agent.ID] = client
	h.mu.Unlock()
	debug.Info("Added agent %d to active clients", agent.ID)

	// Start client routines
	go client.writePump()
	go client.readPump()

	// Send initial configuration including download settings
	go h.sendInitialConfiguration(client)

	// Initiate file sync with agent
	go h.initiateFileSync(client)

	// Start state sync loop for periodic reconciliation (GH Issue #12)
	go client.startStateSyncLoop()
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		debug.Info("Agent %d: ReadPump closing", c.agent.ID)

		// Check if sync was in progress and mark as failed
		if c.agent.SyncStatus == models.AgentSyncStatusInProgress {
			debug.Warning("Agent %d disconnected during file sync, marking sync as failed", c.agent.ID)
			c.agent.SyncStatus = models.AgentSyncStatusFailed
			c.agent.SyncError = sql.NullString{String: "Agent disconnected during sync", Valid: true}
			if err := c.handler.agentService.UpdateAgentSyncStatus(c.ctx, c.agent.ID, c.agent.SyncStatus, c.agent.SyncError.String); err != nil {
				debug.Error("Failed to update sync status to failed: %v", err)
			}
		}

		// Update agent status to inactive when connection is closed
		if err := c.handler.agentService.UpdateAgentStatus(c.ctx, c.agent.ID, models.AgentStatusInactive, nil); err != nil {
			debug.Error("Failed to update agent status to inactive: %v", err)
		} else {
			debug.Info("Successfully updated agent %d status to inactive", c.agent.ID)
		}
		c.handler.unregisterClient(c)
		c.conn.Close()
		c.cancel()
	}()

	debug.Info("Agent %d: Starting readPump with timing configuration:", c.agent.ID)
	debug.Info("Agent %d: - Write Wait: %v", c.agent.ID, writeWait)
	debug.Info("Agent %d: - Pong Wait: %v", c.agent.ID, pongWait)
	debug.Info("Agent %d: - Ping Period: %v", c.agent.ID, pingPeriod)

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))

	// Set up ping handler
	c.conn.SetPingHandler(func(appData string) error {
		debug.Info("Agent %d: Received ping from client, sending pong", c.agent.ID)
		err := c.conn.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			debug.Error("Agent %d: Failed to set read deadline: %v", c.agent.ID, err)
			return err
		}
		// Send pong response immediately
		err = c.conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(writeWait))
		if err != nil {
			debug.Error("Agent %d: Failed to send pong: %v", c.agent.ID, err)
			return err
		}
		debug.Info("Agent %d: Successfully sent pong response", c.agent.ID)
		return nil
	})

	// Set up pong handler
	c.conn.SetPongHandler(func(string) error {
		debug.Info("Agent %d: Received pong", c.agent.ID)
		err := c.conn.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			debug.Error("Agent %d: Failed to set read deadline: %v", c.agent.ID, err)
			return err
		}

		// Update heartbeat when we receive a pong (proof of active connection)
		if err := c.handler.agentService.UpdateHeartbeat(c.ctx, c.agent.ID); err != nil {
			debug.Error("Agent %d: Failed to update heartbeat on pong: %v", c.agent.ID, err)
			// Don't return error - pong handling should continue
		} else {
			debug.Info("Agent %d: Updated heartbeat on pong", c.agent.ID)
		}

		debug.Info("Agent %d: Successfully updated read deadline after pong", c.agent.ID)
		return nil
	})

	debug.Info("Agent %d: Ping/Pong handlers configured", c.agent.ID)

	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				debug.Error("Agent %d: Unexpected WebSocket close error: %v", c.agent.ID, err)
			} else {
				debug.Info("Agent %d: Connection closed: %v", c.agent.ID, err)
			}
			break
		}
		debug.Debug("Agent %d: Received message type %d: %s", c.agent.ID, messageType, string(data))

		var msg wsservice.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			debug.Error("Agent %d: Failed to unmarshal message: %v", c.agent.ID, err)
			continue
		}

		debug.Info("Agent %d: Processing message type: %s", c.agent.ID, msg.Type)

		// Handle message based on type
		if err := c.handler.wsService.HandleMessage(c.ctx, c.agent, &msg); err != nil {
			debug.Error("Agent %d: Failed to handle message: %v", c.agent.ID, err)
		} else {
			debug.Info("Agent %d: Successfully processed message type: %s", c.agent.ID, msg.Type)
		}

		// Handle different message types
		switch msg.Type {
		case wsservice.TypeSyncResponse:
			c.handler.handleSyncResponse(c, &msg)

		case wsservice.TypeSyncStatus:
			c.handler.handleSyncStatus(c, &msg)

		case wsservice.TypeDeviceDetection:
			c.handler.handleDeviceDetection(c, &msg)

		case wsservice.TypePhysicalDeviceDetection:
			c.handler.handlePhysicalDeviceDetection(c, &msg)

		case wsservice.TypeDeviceUpdate:
			c.handler.handleDeviceUpdate(c, &msg)
			
		case wsservice.TypeBufferedMessages:
			c.handler.handleBufferedMessages(c, &msg)
		
		case wsservice.TypeCurrentTaskStatus:
			c.handler.handleCurrentTaskStatus(c, &msg)
		
		case wsservice.TypeAgentShutdown:
			c.handler.handleAgentShutdown(c, &msg)

		case wsservice.TypeDownloadProgress:
			c.handler.handleDownloadProgress(c, &msg)

		case wsservice.TypeDownloadComplete:
			c.handler.handleDownloadComplete(c, &msg)

		case wsservice.TypeDownloadFailed:
			c.handler.handleDownloadFailed(c, &msg)

		case wsservice.TypePendingOutfiles:
			c.handler.handlePendingOutfiles(c, &msg)

		case wsservice.TypeOutfileDeleteRejected:
			c.handler.handleOutfileDeleteRejected(c, &msg)

		case wsservice.TypeTaskStopAck:
			c.handler.handleTaskStopAck(c, &msg)

		case wsservice.TypeStateSyncResponse:
			c.handler.handleStateSyncResponse(c, &msg)

		// Diagnostics message handlers (GH Issue #23)
		case wsservice.TypeDebugStatusReport:
			c.handler.handleDebugStatusReport(c, &msg)

		case wsservice.TypeDebugToggleAck:
			c.handler.handleDebugToggleAck(c, &msg)

		case wsservice.TypeLogData:
			c.handler.handleLogData(c, &msg)

		case wsservice.TypeLogStatusResponse:
			c.handler.handleLogStatusResponse(c, &msg)

		case wsservice.TypeLogPurgeAck:
			c.handler.handleLogPurgeAck(c, &msg)

		default:
			// Handle other message types
		}
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		debug.Info("Agent %d: WritePump closing", c.agent.ID)
		ticker.Stop()
		c.conn.Close()
	}()

	debug.Info("Agent %d: Starting writePump with timing configuration:", c.agent.ID)
	debug.Info("Agent %d: - Write Wait: %v", c.agent.ID, writeWait)
	debug.Info("Agent %d: - Pong Wait: %v", c.agent.ID, pongWait)
	debug.Info("Agent %d: - Ping Period: %v", c.agent.ID, pingPeriod)

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				debug.Info("Agent %d: Send channel closed", c.agent.ID)
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				debug.Error("Agent %d: Failed to get next writer: %v", c.agent.ID, err)
				return
			}

			data, err := json.Marshal(message)
			if err != nil {
				debug.Error("Agent %d: Failed to marshal message: %v", c.agent.ID, err)
				continue
			}

			debug.Info("Agent %d: Sending message type: %s", c.agent.ID, message.Type)
			debug.Debug("Message details - Length: %d bytes", len(data))

			if _, err := w.Write(data); err != nil {
				debug.Error("Agent %d: Failed to write message: %v", c.agent.ID, err)
				return
			}

			if err := w.Close(); err != nil {
				debug.Error("Agent %d: Failed to close writer: %v", c.agent.ID, err)
				return
			}

			debug.Info("Agent %d: Successfully sent message type: %s", c.agent.ID, message.Type)

		case <-ticker.C:
			debug.Info("Agent %d: Sending ping", c.agent.ID)
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				debug.Error("Agent %d: Failed to send ping: %v", c.agent.ID, err)
				return
			}
			debug.Info("Agent %d: Successfully sent ping", c.agent.ID)

		case <-c.ctx.Done():
			debug.Info("Agent %d: Context cancelled", c.agent.ID)
			return
		}
	}
}

// SendMessage sends a message to a specific agent
func (h *Handler) SendMessage(agentID int, msg *wsservice.Message) error {
	h.mu.RLock()
	client, ok := h.clients[agentID]
	h.mu.RUnlock()

	if !ok {
		return fmt.Errorf("agent %d not connected", agentID)
	}

	select {
	case client.send <- msg:
		return nil
	default:
		return fmt.Errorf("agent %d send buffer full", agentID)
	}
}

// Broadcast sends a message to all connected agents
func (h *Handler) Broadcast(msg *wsservice.Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		select {
		case client.send <- msg:
		default:
			debug.Error("failed to broadcast to agent %d: send buffer full", client.agent.ID)
		}
	}
}

// unregisterClient removes a client from the handler
func (h *Handler) unregisterClient(c *Client) {
	h.mu.Lock()
	if client, ok := h.clients[c.agent.ID]; ok {
		if client == c {
			delete(h.clients, c.agent.ID)
		}
	}
	h.mu.Unlock()
	
	// Mark agent's tasks as reconnect_pending
	if h.wsService != nil && h.wsService.GetJobHandler() != nil {
		debug.Info("Agent %d: Marking tasks as reconnect_pending due to disconnection", c.agent.ID)
		if err := h.wsService.HandleAgentDisconnection(c.ctx, c.agent.ID); err != nil {
			debug.Error("Agent %d: Failed to handle disconnection: %v", c.agent.ID, err)
		}
	}
}

// GetConnectedAgents returns a list of connected agent IDs
func (h *Handler) GetConnectedAgents() []int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	agents := make([]int, 0, len(h.clients))
	for agentID := range h.clients {
		agents = append(agents, agentID)
	}
	return agents
}

// sendInitialConfiguration sends initial configuration to the agent including download settings
func (h *Handler) sendInitialConfiguration(client *Client) {
	debug.Info("Sending initial configuration to agent %d", client.agent.ID)

	// Get agent download settings from repository
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	settings, err := h.systemSettingsRepo.GetAgentDownloadSettings(ctx)
	if err != nil {
		debug.Error("Failed to get agent download settings: %v", err)
		// Use defaults if we can't fetch settings
		settings = &models.AgentDownloadSettings{
			MaxConcurrentDownloads:      3,
			DownloadTimeoutMinutes:      60,
			DownloadRetryAttempts:       3,
			ProgressIntervalSeconds:     10,
			ChunkSizeMB:                 10,
		}
	}

	// Get preferred binary version for this agent (if job execution service is available)
	var preferredBinaryID int64
	if h.jobExecutionService != nil {
		var err error
		preferredBinaryID, err = h.jobExecutionService.GetAgentPreferredBinary(ctx, client.agent.ID)
		if err != nil {
			debug.Error("Failed to get preferred binary for agent %d: %v", client.agent.ID, err)
			// Don't fail completely, just log the error
			preferredBinaryID = 0
		}
	} else {
		debug.Debug("JobExecutionService not available, skipping preferred binary version")
		preferredBinaryID = 0
	}

	// Create configuration payload
	configPayload := map[string]interface{}{
		"download_settings":        settings,
		"preferred_binary_version": preferredBinaryID,
	}

	payloadBytes, err := json.Marshal(configPayload)
	if err != nil {
		debug.Error("Failed to marshal configuration payload: %v", err)
		return
	}

	// Create configuration message
	msg := &wsservice.Message{
		Type:    wsservice.TypeConfigUpdate,
		Payload: payloadBytes,
	}

	// Send configuration to agent
	select {
	case client.send <- msg:
		debug.Info("Sent initial configuration to agent %d with download settings and preferred binary version %d", client.agent.ID, preferredBinaryID)
	case <-client.ctx.Done():
		debug.Warning("Failed to send configuration: agent %d disconnected", client.agent.ID)
	}
}

// initiateFileSync starts the file synchronization process with an agent
func (h *Handler) initiateFileSync(client *Client) {
	debug.Info("Initiating file sync with agent %d", client.agent.ID)

	// Create a unique request ID
	requestID := fmt.Sprintf("sync-%d-%d", client.agent.ID, time.Now().UnixNano())

	// Create sync request payload
	payload := wsservice.FileSyncRequestPayload{
		RequestID: requestID,
		FileTypes: []string{"wordlist", "rule", "binary"},
	}

	// Marshal payload
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		debug.Error("Failed to marshal file sync request payload: %v", err)
		return
	}

	// Create message
	msg := &wsservice.Message{
		Type:    wsservice.TypeSyncRequest,
		Payload: payloadBytes,
	}

	// Send message to agent
	select {
	case client.send <- msg:
		debug.Info("Sent file sync request to agent %d", client.agent.ID)
	case <-client.ctx.Done():
		debug.Warning("Failed to send file sync request: agent %d disconnected", client.agent.ID)
	}
}

// RegisterInventoryCallback registers a callback channel for a specific agent.
// When a file sync response from this agent arrives, the response will be sent
// to the channel instead of being processed by the normal sync flow.
// Only one callback per agent is supported - subsequent registrations overwrite previous ones.
// Returns a receive-only channel that will receive the inventory response.
func (h *Handler) RegisterInventoryCallback(agentID int) <-chan *wsservice.FileSyncResponsePayload {
	h.inventoryCallbacksMu.Lock()
	defer h.inventoryCallbacksMu.Unlock()

	ch := make(chan *wsservice.FileSyncResponsePayload, 1)
	h.inventoryCallbacks[agentID] = ch
	debug.Info("Registered inventory callback for agent %d", agentID)
	return ch
}

// UnregisterInventoryCallback removes a previously registered callback for an agent.
// This should be called after receiving the response or on timeout.
func (h *Handler) UnregisterInventoryCallback(agentID int) {
	h.inventoryCallbacksMu.Lock()
	defer h.inventoryCallbacksMu.Unlock()

	if ch, exists := h.inventoryCallbacks[agentID]; exists {
		close(ch)
		delete(h.inventoryCallbacks, agentID)
		debug.Info("Unregistered inventory callback for agent %d", agentID)
	}
}

// handleSyncResponse processes a file sync response from an agent
func (h *Handler) handleSyncResponse(client *Client, msg *wsservice.Message) {
	// First check if this is a progress message
	var progressCheck map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &progressCheck); err == nil {
		if status, ok := progressCheck["status"].(string); ok && status == "scanning" {
			// This is a progress message
			message := ""
			if m, ok := progressCheck["message"].(string); ok {
				message = m
			}
			progress := 0.0
			if p, ok := progressCheck["progress"].(float64); ok {
				progress = p
			}
			debug.Info("Agent %d file sync progress: %s (%.1f%%)", client.agent.ID, message, progress)
			return
		}
	}

	// Otherwise, parse as a full file sync response
	var payload wsservice.FileSyncResponsePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Failed to unmarshal file sync response: %v", err)
		return
	}

	debug.Info("Received file sync response from agent %d: %d files",
		client.agent.ID, len(payload.Files))

	// Check if there's a registered callback for this agent (pre-benchmark file check)
	h.inventoryCallbacksMu.RLock()
	ch, hasCallback := h.inventoryCallbacks[client.agent.ID]
	h.inventoryCallbacksMu.RUnlock()

	if hasCallback {
		debug.Info("Routing file sync response to registered callback for agent %d", client.agent.ID)
		// Send to callback channel (non-blocking - channel is buffered)
		select {
		case ch <- &payload:
			// Successfully sent to callback
		default:
			debug.Warning("Callback channel full for agent %d, dropping response", client.agent.ID)
		}
		// Don't process through normal sync flow - caller will handle comparison
		return
	}

	// Determine which files need to be synced
	filesToSync, err := h.determineFilesToSync(client.agent.ID, payload.Files)
	if err != nil {
		debug.Error("Failed to determine files to sync: %v", err)
		return
	}

	if len(filesToSync) == 0 {
		debug.Info("Agent %d is up to date, no files to sync", client.agent.ID)

		// Mark sync as completed since agent already has all files
		if h.agentService != nil {
			err := h.agentService.UpdateAgentSyncStatus(context.Background(), client.agent.ID,
				models.AgentSyncStatusCompleted, "")
			if err != nil {
				debug.Error("Failed to update sync status for agent %d: %v", client.agent.ID, err)
			} else {
				debug.Info("Agent %d sync status updated to completed", client.agent.ID)

				// Now that sync is complete (no files needed), mark agent as active and available for work
				if err := h.agentService.UpdateAgentStatus(client.ctx, client.agent.ID, models.AgentStatusActive, nil); err != nil {
					debug.Error("Failed to update agent status to active: %v", err)
				} else {
					debug.Info("Agent %d marked as active and available for work (no sync needed)", client.agent.ID)
				}
			}
		}

		return
	}

	// Create sync command payload
	commandPayload := wsservice.FileSyncCommandPayload{
		RequestID: fmt.Sprintf("sync-cmd-%d-%d", client.agent.ID, time.Now().UnixNano()),
		Action:    "download",
		Files:     filesToSync,
	}

	// Marshal payload
	commandBytes, err := json.Marshal(commandPayload)
	if err != nil {
		debug.Error("Failed to marshal file sync command payload: %v", err)
		return
	}

	// Create message
	command := &wsservice.Message{
		Type:    wsservice.TypeSyncCommand,
		Payload: commandBytes,
	}

	// Send message to agent
	select {
	case client.send <- command:
		debug.Info("Sent file sync command to agent %d to download %d files", client.agent.ID, len(filesToSync))
	case <-client.ctx.Done():
		debug.Warning("Failed to send file sync command: agent %d disconnected", client.agent.ID)
	}
}

// handleSyncStatus processes a file sync status update from an agent
func (h *Handler) handleSyncStatus(client *Client, msg *wsservice.Message) {
	var payload wsservice.FileSyncStatusPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Failed to unmarshal file sync status: %v", err)
		return
	}

	debug.Info("File sync status update from agent %d: %s (%d%%)",
		client.agent.ID, payload.Status, payload.Progress)

	// If sync is complete, update agent status
	if payload.Status == "completed" {
		debug.Info("File sync completed for agent %d", client.agent.ID)

		// Update agent sync status in database
		if h.agentService != nil {
			err := h.agentService.UpdateAgentSyncStatus(context.Background(), client.agent.ID,
				models.AgentSyncStatusCompleted, "")
			if err != nil {
				debug.Error("Failed to update sync status for agent %d: %v", client.agent.ID, err)
			} else {
				debug.Info("Agent %d sync status updated to completed after file downloads", client.agent.ID)

				// Now that sync is complete, mark agent as active and available for work
				if err := h.agentService.UpdateAgentStatus(client.ctx, client.agent.ID, models.AgentStatusActive, nil); err != nil {
					debug.Error("Failed to update agent status to active after sync: %v", err)
				} else {
					debug.Info("Agent %d marked as active and available for work (sync complete)", client.agent.ID)
				}
			}
		}
	}
}

// determineFilesToSync compares agent files with the backend and returns files that need syncing
func (h *Handler) determineFilesToSync(agentID int, agentFiles []wsservice.FileInfo) ([]wsservice.FileInfo, error) {
	// Get files from backend
	backendFiles, err := h.getBackendFiles(context.Background(), []string{"wordlist", "rule", "binary"}, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get backend files: %w", err)
	}

	// Create a map of agent files for quick lookup
	agentFileMap := make(map[string]wsservice.FileInfo)
	for _, file := range agentFiles {
		var key string
		if file.FileType == "binary" && file.ID > 0 {
			// Binaries are stored by ID, so include ID in the key
			key = fmt.Sprintf("%s:%d:%s", file.FileType, file.ID, file.Name)
		} else {
			// Wordlists and rules use path-based keys
			key = fmt.Sprintf("%s:%s", file.FileType, file.Name)
		}
		agentFileMap[key] = file
	}

	// Determine which files need to be synced
	var filesToSync []wsservice.FileInfo

	for _, file := range backendFiles {
		var key string
		if file.FileType == "binary" && file.ID > 0 {
			// Binaries are stored by ID, so include ID in the key
			key = fmt.Sprintf("%s:%d:%s", file.FileType, file.ID, file.Name)
		} else {
			// Wordlists and rules use path-based keys
			key = fmt.Sprintf("%s:%s", file.FileType, file.Name)
		}
		agentFile, exists := agentFileMap[key]

		// Special handling for potfile during heavy crack ingestion
		// If agent has a recent valid potfile hash (within 5-min window), skip re-download
		// This prevents infinite re-download loops when potfile MD5 changes frequently
		if file.FileType == "wordlist" && strings.HasSuffix(file.Name, "potfile.txt") {
			if exists && h.potfileHistory != nil && h.potfileHistory.IsValid(agentFile.MD5Hash) {
				debug.Debug("Agent has valid recent potfile hash %s, skipping sync", agentFile.MD5Hash)
				continue
			}
		}

		// If the file doesn't exist on agent or MD5 hash doesn't match, add to sync list
		if !exists || agentFile.MD5Hash != file.MD5Hash {
			filesToSync = append(filesToSync, file)
		}
	}

	return filesToSync, nil
}

// getBackendFiles retrieves files from the backend database based on file types
func (h *Handler) getBackendFiles(ctx context.Context, fileTypes []string, category string) ([]wsservice.FileInfo, error) {
	debug.Info("Retrieving backend files for types: %v, category: %s", fileTypes, category)

	// Retrieve files using the agent service
	repoFiles, err := h.agentService.GetFiles(ctx, fileTypes, category)
	if err != nil {
		return nil, fmt.Errorf("failed to get files from repository: %w", err)
	}

	// Convert repository.FileInfo to wsservice.FileInfo
	var files []wsservice.FileInfo
	for _, file := range repoFiles {
		files = append(files, wsservice.FileInfo{
			Name:      file.Name,
			MD5Hash:   file.MD5Hash,
			Size:      file.Size,
			FileType:  file.FileType,
			Category:  file.Category,
			ID:        file.ID,
			Timestamp: file.Timestamp,
		})
	}

	debug.Info("Retrieved %d files from repository", len(files))
	return files, nil
}

// handleDeviceDetection handles device detection results from agents
func (h *Handler) handleDeviceDetection(client *Client, msg *wsservice.Message) {
	debug.Info("Agent %d: Received device detection result", client.agent.ID)

	// Parse the device detection result
	var result models.DeviceDetectionResult
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		debug.Error("Agent %d: Failed to parse device detection result: %v", client.agent.ID, err)
		return
	}

	// Check if there was an error in detection
	if result.Error != "" {
		debug.Error("Agent %d: Device detection failed: %s", client.agent.ID, result.Error)
		// Update agent status to error
		if err := h.agentService.UpdateDeviceDetectionStatus(client.agent.ID, "error", &result.Error); err != nil {
			debug.Error("Failed to update device detection status: %v", err)
		}
		return
	}

	// Store devices in database
	if err := h.agentService.UpdateAgentDevices(client.agent.ID, result.Devices); err != nil {
		debug.Error("Agent %d: Failed to update devices: %v", client.agent.ID, err)
		errorMsg := err.Error()
		if updateErr := h.agentService.UpdateDeviceDetectionStatus(client.agent.ID, "error", &errorMsg); updateErr != nil {
			debug.Error("Failed to update device detection status: %v", updateErr)
		}
		return
	}

	// Update agent device detection status to success
	if err := h.agentService.UpdateDeviceDetectionStatus(client.agent.ID, "success", nil); err != nil {
		debug.Error("Failed to update device detection status: %v", err)
	}

	// Check if agent has enabled devices, disable agent if not
	hasEnabledDevices := false
	for _, device := range result.Devices {
		if device.Enabled {
			hasEnabledDevices = true
			break
		}
	}

	if !hasEnabledDevices {
		debug.Warning("Agent %d: No enabled devices found, disabling agent", client.agent.ID)
		if err := h.agentService.UpdateAgentStatus(context.Background(), client.agent.ID, "disabled", nil); err != nil {
			debug.Error("Failed to disable agent: %v", err)
		}
	}

	debug.Info("Agent %d: Successfully updated %d devices", client.agent.ID, len(result.Devices))
}

// handlePhysicalDeviceDetection handles physical device detection results from agents
func (h *Handler) handlePhysicalDeviceDetection(client *Client, msg *wsservice.Message) {
	debug.Info("Agent %d: Received physical device detection result", client.agent.ID)

	// Parse the physical device detection result
	var result models.PhysicalDeviceDetectionResult
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		debug.Error("Agent %d: Failed to parse physical device detection result: %v", client.agent.ID, err)
		return
	}

	// Check if there was an error in detection
	if result.Error != "" {
		debug.Error("Agent %d: Physical device detection failed: %s", client.agent.ID, result.Error)
		// Update agent status to error
		if err := h.agentService.UpdateDeviceDetectionStatus(client.agent.ID, "error", &result.Error); err != nil {
			debug.Error("Failed to update device detection status: %v", err)
		}
		return
	}

	// Store physical devices in database
	if err := h.agentService.UpdateAgentPhysicalDevices(client.agent.ID, result.Devices); err != nil {
		debug.Error("Agent %d: Failed to update physical devices: %v", client.agent.ID, err)
		errorMsg := err.Error()
		if updateErr := h.agentService.UpdateDeviceDetectionStatus(client.agent.ID, "error", &errorMsg); updateErr != nil {
			debug.Error("Failed to update device detection status: %v", updateErr)
		}
		return
	}

	// Update agent device detection status to success
	if err := h.agentService.UpdateDeviceDetectionStatus(client.agent.ID, "success", nil); err != nil {
		debug.Error("Failed to update device detection status: %v", err)
	}

	// Check if agent has enabled devices, disable agent if not
	hasEnabledDevices := false
	for _, device := range result.Devices {
		if device.Enabled {
			hasEnabledDevices = true
			break
		}
	}

	if !hasEnabledDevices {
		debug.Warning("Agent %d: No enabled physical devices found, disabling agent", client.agent.ID)
		if err := h.agentService.UpdateAgentStatus(context.Background(), client.agent.ID, "disabled", nil); err != nil {
			debug.Error("Failed to disable agent: %v", err)
		}
	}

	debug.Info("Agent %d: Successfully updated %d physical devices", client.agent.ID, len(result.Devices))
}

// handleDeviceUpdate handles device update responses from agents
func (h *Handler) handleDeviceUpdate(client *Client, msg *wsservice.Message) {
	debug.Info("Agent %d: Received device update response", client.agent.ID)

	// Parse the response
	var response map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &response); err != nil {
		debug.Error("Agent %d: Failed to parse device update response: %v", client.agent.ID, err)
		return
	}

	success, ok := response["success"].(bool)
	if !ok {
		debug.Error("Agent %d: Invalid device update response format", client.agent.ID)
		return
	}

	if !success {
		if errorMsg, ok := response["error"].(string); ok {
			debug.Error("Agent %d: Device update failed: %s", client.agent.ID, errorMsg)
		}
		return
	}

	// Device update was successful
	if deviceID, ok := response["device_id"].(float64); ok {
		if enabled, ok := response["enabled"].(bool); ok {
			debug.Info("Agent %d: Successfully updated device %d to enabled=%v",
				client.agent.ID, int(deviceID), enabled)

			// Check if all devices are disabled after update
			devices, err := h.agentService.GetAgentDevices(client.agent.ID)
			if err != nil {
				debug.Error("Failed to get agent devices: %v", err)
				return
			}

			hasEnabledDevice := false
			for _, device := range devices {
				if device.Enabled {
					hasEnabledDevice = true
					break
				}
			}

			// Update agent status based on device availability
			if !hasEnabledDevice {
				debug.Warning("Agent %d: All devices disabled, disabling agent", client.agent.ID)
				if err := h.agentService.UpdateAgentStatus(context.Background(), client.agent.ID, "disabled", nil); err != nil {
					debug.Error("Failed to disable agent: %v", err)
				}
			} else if client.agent.Status == "disabled" {
				// Re-enable agent if it was disabled and now has enabled devices
				debug.Info("Agent %d: Has enabled devices, enabling agent", client.agent.ID)
				if err := h.agentService.UpdateAgentStatus(context.Background(), client.agent.ID, "active", nil); err != nil {
					debug.Error("Failed to enable agent: %v", err)
				}
			}
		}
	}
}

// handleBufferedMessages processes buffered messages from agents after reconnection
func (h *Handler) handleBufferedMessages(client *Client, msg *wsservice.Message) {
	debug.Info("Agent %d: Received buffered messages", client.agent.ID)
	
	// Parse the buffered messages payload
	var payload struct {
		Messages []struct {
			ID        string          `json:"id"`
			Type      string          `json:"type"`
			Payload   json.RawMessage `json:"payload"`
			Timestamp time.Time       `json:"timestamp"`
			AgentID   int             `json:"agent_id,omitempty"`
		} `json:"messages"`
		AgentID int `json:"agent_id"`
	}
	
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to parse buffered messages: %v", client.agent.ID, err)
		return
	}
	
	debug.Info("Agent %d: Processing %d buffered messages", client.agent.ID, len(payload.Messages))
	
	// Track processed message IDs for acknowledgment
	processedIDs := make([]string, 0, len(payload.Messages))
	
	// Process each buffered message
	for _, bufferedMsg := range payload.Messages {
		debug.Info("Agent %d: Processing buffered message %s of type %s from %v",
			client.agent.ID, bufferedMsg.ID, bufferedMsg.Type, bufferedMsg.Timestamp)
		
		// Create a Message struct for the buffered message
		reconstructedMsg := wsservice.Message{
			Type:    wsservice.MessageType(bufferedMsg.Type),
			Payload: bufferedMsg.Payload,
		}
		
		// Process the message based on its type
		switch reconstructedMsg.Type {
		case wsservice.TypeJobProgress:
			// Check if message contains crack information
			if containsCracks(bufferedMsg.Payload) {
				debug.Info("Agent %d: Buffered message contains crack information", client.agent.ID)
			}
			
			// Forward to WebSocket service for processing
			if h.wsService != nil {
				// The WebSocket service will handle forwarding to the appropriate integration
				debug.Info("Agent %d: Processing buffered job progress through WebSocket service", client.agent.ID)
				// Note: The actual job progress processing happens through the integration layer
				// which is registered with the WebSocket service
			}
			
		case wsservice.TypeHashcatOutput:
			// Log hashcat output which may contain cracks
			debug.Info("Agent %d: Processing buffered hashcat output", client.agent.ID)
			// The hashcat output is typically logged for debugging
			// Actual crack processing happens through job progress messages
			
		case wsservice.TypeBenchmarkResult:
			// Process benchmark result
			debug.Info("Agent %d: Processing buffered benchmark result", client.agent.ID)
			// Similar to job progress, benchmark results are processed through integration
			
		default:
			debug.Warning("Agent %d: Unsupported buffered message type: %s", client.agent.ID, bufferedMsg.Type)
			continue
		}
		
		// Mark message as processed
		processedIDs = append(processedIDs, bufferedMsg.ID)
	}
	
	debug.Info("Agent %d: Successfully processed %d/%d buffered messages",
		client.agent.ID, len(processedIDs), len(payload.Messages))
	
	// Send acknowledgment back to agent
	ackPayload := map[string]interface{}{
		"message_ids": processedIDs,
	}
	
	ackData, err := json.Marshal(ackPayload)
	if err != nil {
		debug.Error("Agent %d: Failed to marshal ACK payload: %v", client.agent.ID, err)
		return
	}
	
	ackMsg := wsservice.Message{
		Type:    wsservice.TypeBufferAck,
		Payload: ackData,
	}
	
	client.send <- &ackMsg
	debug.Info("Agent %d: Sent buffer acknowledgment for %d messages", client.agent.ID, len(processedIDs))
}

// containsCracks checks if a job progress message contains crack information
func containsCracks(payload json.RawMessage) bool {
	var progress struct {
		CrackedCount  int      `json:"cracked_count"`
		CrackedHashes []string `json:"cracked_hashes"`
	}
	
	if err := json.Unmarshal(payload, &progress); err != nil {
		return false
	}
	
	return progress.CrackedCount > 0 || len(progress.CrackedHashes) > 0
}

// handleCurrentTaskStatus processes the current task status from an agent
func (h *Handler) handleCurrentTaskStatus(client *Client, msg *wsservice.Message) {
	debug.Info("Agent %d: Received current task status", client.agent.ID)
	
	// Parse the status payload - includes all progress fields for offline completion handling
	var status struct {
		AgentID                int     `json:"agent_id"`
		HasRunningTask         bool    `json:"has_running_task"`
		TaskID                 string  `json:"task_id,omitempty"`
		JobID                  string  `json:"job_id,omitempty"`
		KeyspaceProcessed      int64   `json:"keyspace_processed,omitempty"`
		EffectiveProgress      int64   `json:"effective_progress,omitempty"`
		ProgressPercent        float64 `json:"progress_percent,omitempty"`
		TotalEffectiveKeyspace *int64  `json:"total_effective_keyspace,omitempty"`
		HashRate               int64   `json:"hash_rate,omitempty"`
		CrackedCount           int     `json:"cracked_count,omitempty"`
		AllHashesCracked       bool    `json:"all_hashes_cracked,omitempty"`
		Status                 string  `json:"status,omitempty"`
		ErrorMessage           string  `json:"error_message,omitempty"`
	}
	
	if err := json.Unmarshal(msg.Payload, &status); err != nil {
		debug.Error("Agent %d: Failed to parse task status: %v", client.agent.ID, err)
		return
	}
	
	debug.Info("Agent %d: Task status - HasTask: %v, TaskID: %s, JobID: %s, Status: %s",
		client.agent.ID, status.HasRunningTask, status.TaskID, status.JobID, status.Status)
	
	// If agent has a running task, try to recover it
	if status.HasRunningTask && status.TaskID != "" {
		// Validate the task before setting busy status
		jobHandler := h.wsService.GetJobHandler()
		if jobHandler != nil {
			// First, validate that the task exists and is assigned to this agent
			task, err := jobHandler.GetTask(client.ctx, status.TaskID)
			if err != nil || task == nil {
				debug.Warning("Agent %d reported status for non-existent task %s", client.agent.ID, status.TaskID)
				// Tell agent to stop the non-existent task
				stopMsg := wsservice.Message{
					Type: wsservice.TypeJobStop,
					Payload: json.RawMessage(`{"task_id":"` + status.TaskID + `"}`),
				}
				select {
				case client.send <- &stopMsg:
					debug.Info("Agent %d: Sent job stop for non-existent task %s", client.agent.ID, status.TaskID)
				case <-client.ctx.Done():
				}
				// Don't set busy status for non-existent task
			} else if task.AgentID == nil || *task.AgentID != client.agent.ID {
				// Task exists but is not assigned to this agent
				debug.Warning("Agent %d reported status for task %s not assigned to it (assigned to agent %v)",
					client.agent.ID, status.TaskID, task.AgentID)
				// Tell agent to stop the task that's not assigned to it
				stopMsg := wsservice.Message{
					Type: wsservice.TypeJobStop,
					Payload: json.RawMessage(`{"task_id":"` + status.TaskID + `"}`),
				}
				select {
				case client.send <- &stopMsg:
					debug.Info("Agent %d: Sent job stop for unassigned task %s", client.agent.ID, status.TaskID)
				case <-client.ctx.Done():
				}
				// Don't set busy status for unassigned task
			} else {
				// Task is valid and assigned to this agent

				// Check if agent reports completed/failed status - process as final completion
				// This handles the case where agent completed task while disconnected
				if status.Status == "completed" || status.Status == "failed" {
					debug.Info("Agent %d: Processing offline task completion for task %s with status %s",
						client.agent.ID, status.TaskID, status.Status)
					h.handleOfflineTaskCompletion(client, &status, task)
					return
				}

				// Task is still running, set busy status
				if client.agent.Metadata == nil {
					client.agent.Metadata = make(map[string]string)
				}
				client.agent.Metadata["busy_status"] = "true"
				client.agent.Metadata["current_task_id"] = status.TaskID
				client.agent.Metadata["current_job_id"] = status.JobID
				if err := h.agentService.UpdateAgentMetadata(client.ctx, client.agent.ID, client.agent.Metadata); err != nil {
					debug.Error("Failed to update agent busy status metadata: %v", err)
				}

				// Check if the task is in reconnect_pending state and try to recover
				err := jobHandler.RecoverTask(client.ctx, status.TaskID, client.agent.ID, status.KeyspaceProcessed)
				if err != nil {
					debug.Error("Agent %d: Failed to recover task %s: %v", client.agent.ID, status.TaskID, err)
					// Clear busy status since recovery failed
					client.agent.Metadata["busy_status"] = "false"
					delete(client.agent.Metadata, "current_task_id")
					delete(client.agent.Metadata, "current_job_id")
					h.agentService.UpdateAgentMetadata(client.ctx, client.agent.ID, client.agent.Metadata)

					// Tell agent to stop the task if recovery failed
					stopMsg := wsservice.Message{
						Type: wsservice.TypeJobStop,
						Payload: json.RawMessage(`{"task_id":"` + status.TaskID + `"}`),
					}
					select {
					case client.send <- &stopMsg:
						debug.Info("Agent %d: Sent job stop for unrecoverable task %s", client.agent.ID, status.TaskID)
					case <-client.ctx.Done():
						debug.Warning("Agent %d: Failed to send job stop (disconnected)", client.agent.ID)
					}
				} else {
					debug.Info("Agent %d: Successfully recovered task %s", client.agent.ID, status.TaskID)
					// Don't mark agent as available since it's running a task
					return
				}
			}
		}
	}
	
	// Only mark agent as active/available if it has no running tasks
	if !status.HasRunningTask {
		// Check if there are any reconnect_pending tasks for this agent
		// If there are, reset them for retry immediately instead of waiting for grace period
		jobHandler := h.wsService.GetJobHandler()
		if jobHandler != nil {
			debug.Info("Agent %d: Calling HandleAgentReconnectionWithNoTask", client.agent.ID)
			resetCount, err := jobHandler.HandleAgentReconnectionWithNoTask(client.ctx, client.agent.ID)
			if err != nil {
				debug.Error("Agent %d: Failed to handle reconnection with no task: %v", client.agent.ID, err)
			} else if resetCount > 0 {
				debug.Info("Agent %d: Reset %d reconnect_pending tasks for immediate retry", client.agent.ID, resetCount)
			} else {
				debug.Info("Agent %d: No reconnect_pending tasks to reset", client.agent.ID)
			}
		} else {
			debug.Warning("Agent %d: JobHandler is nil, cannot handle reconnection", client.agent.ID)
		}
		
		// Clear busy status in metadata
		if client.agent.Metadata == nil {
			client.agent.Metadata = make(map[string]string)
		}
		client.agent.Metadata["busy_status"] = "false"
		delete(client.agent.Metadata, "current_task_id")
		delete(client.agent.Metadata, "current_job_id")
		
		// Update agent in database
		if err := h.agentService.UpdateAgentMetadata(client.ctx, client.agent.ID, client.agent.Metadata); err != nil {
			debug.Error("Failed to update agent metadata: %v", err)
		}
		
		// Check sync status before marking agent as active
		// This prevents a race condition where the agent is marked active before file sync completes
		agent, err := h.agentService.GetByID(client.ctx, client.agent.ID)
		if err != nil {
			debug.Error("Failed to get agent %d for sync status check: %v", client.agent.ID, err)
		} else if agent.SyncStatus == models.AgentSyncStatusCompleted {
			if err := h.agentService.UpdateAgentStatus(client.ctx, client.agent.ID, models.AgentStatusActive, nil); err != nil {
				debug.Error("Failed to update agent status to active: %v", err)
			} else {
				debug.Info("Agent %d marked as active and available for work", client.agent.ID)
			}
		} else {
			debug.Info("Agent %d not yet available (sync_status=%s), waiting for sync to complete",
				client.agent.ID, agent.SyncStatus)
		}
	}
}

// handleOfflineTaskCompletion processes task completion that happened while agent was offline
// This is called when agent reconnects and reports a completed/failed task status
func (h *Handler) handleOfflineTaskCompletion(client *Client, status *struct {
	AgentID                int     `json:"agent_id"`
	HasRunningTask         bool    `json:"has_running_task"`
	TaskID                 string  `json:"task_id,omitempty"`
	JobID                  string  `json:"job_id,omitempty"`
	KeyspaceProcessed      int64   `json:"keyspace_processed,omitempty"`
	EffectiveProgress      int64   `json:"effective_progress,omitempty"`
	ProgressPercent        float64 `json:"progress_percent,omitempty"`
	TotalEffectiveKeyspace *int64  `json:"total_effective_keyspace,omitempty"`
	HashRate               int64   `json:"hash_rate,omitempty"`
	CrackedCount           int     `json:"cracked_count,omitempty"`
	AllHashesCracked       bool    `json:"all_hashes_cracked,omitempty"`
	Status                 string  `json:"status,omitempty"`
	ErrorMessage           string  `json:"error_message,omitempty"`
}, task *models.JobTask) {
	debug.Info("Agent %d: Processing offline task completion for task %s with status %s, progress %.2f%%",
		client.agent.ID, status.TaskID, status.Status, status.ProgressPercent)

	// Construct a job_status message with the cached progress data
	// This mimics what the agent would have sent if it was connected
	jobStatusPayload := map[string]interface{}{
		"task_id":            status.TaskID,
		"keyspace_processed": status.KeyspaceProcessed,
		"effective_progress": status.EffectiveProgress,
		"progress_percent":   status.ProgressPercent,
		"hash_rate":          status.HashRate,
		"cracked_count":      status.CrackedCount,
		"all_hashes_cracked": status.AllHashesCracked,
		"status":             status.Status,
		"error_message":      status.ErrorMessage,
	}
	if status.TotalEffectiveKeyspace != nil {
		jobStatusPayload["total_effective_keyspace"] = *status.TotalEffectiveKeyspace
	}

	payloadBytes, err := json.Marshal(jobStatusPayload)
	if err != nil {
		debug.Error("Agent %d: Failed to marshal job status for offline completion: %v", client.agent.ID, err)
		return
	}

	// Process as a normal job_status message (same path as live progress)
	jobHandler := h.wsService.GetJobHandler()
	if jobHandler != nil {
		if err := jobHandler.ProcessJobProgress(client.ctx, client.agent.ID, payloadBytes); err != nil {
			debug.Error("Agent %d: Failed to process offline completion: %v", client.agent.ID, err)
			return
		}
	} else {
		debug.Error("Agent %d: No job handler available to process offline completion", client.agent.ID)
		return
	}

	// Clear agent busy status metadata since task is now complete
	if client.agent.Metadata == nil {
		client.agent.Metadata = make(map[string]string)
	}
	client.agent.Metadata["busy_status"] = "false"
	delete(client.agent.Metadata, "current_task_id")
	delete(client.agent.Metadata, "current_job_id")
	if err := h.agentService.UpdateAgentMetadata(client.ctx, client.agent.ID, client.agent.Metadata); err != nil {
		debug.Error("Agent %d: Failed to update agent metadata after offline completion: %v", client.agent.ID, err)
	}

	// Mark agent as active/available
	if err := h.agentService.UpdateAgentStatus(client.ctx, client.agent.ID, models.AgentStatusActive, nil); err != nil {
		debug.Error("Agent %d: Failed to update agent status to active: %v", client.agent.ID, err)
	}

	debug.Info("Agent %d: Successfully processed offline completion for task %s with status %s",
		client.agent.ID, status.TaskID, status.Status)
}

// handleAgentShutdown processes graceful shutdown notification from an agent
func (h *Handler) handleAgentShutdown(client *Client, msg *wsservice.Message) {
	debug.Info("Agent %d: Received graceful shutdown notification", client.agent.ID)
	
	// Parse the shutdown payload
	var shutdownPayload struct {
		AgentID        int    `json:"agent_id"`
		Reason         string `json:"reason"`
		HasRunningTask bool   `json:"has_running_task"`
		TaskID         string `json:"task_id,omitempty"`
		JobID          string `json:"job_id,omitempty"`
	}
	
	if err := json.Unmarshal(msg.Payload, &shutdownPayload); err != nil {
		debug.Error("Agent %d: Failed to parse shutdown payload: %v", client.agent.ID, err)
		return
	}
	
	debug.Info("Agent %d: Shutdown reason: %s, HasTask: %v, TaskID: %s", 
		client.agent.ID, shutdownPayload.Reason, shutdownPayload.HasRunningTask, shutdownPayload.TaskID)
	
	// If agent had running tasks, reset them immediately to pending
	if shutdownPayload.HasRunningTask && shutdownPayload.TaskID != "" {
		debug.Info("Agent %d: Agent was running task %s, will reset to pending immediately",
			client.agent.ID, shutdownPayload.TaskID)

		// Parse task ID and reset it directly to pending
		taskID, err := uuid.Parse(shutdownPayload.TaskID)
		if err != nil {
			debug.Error("Agent %d: Failed to parse task ID %s: %v",
				client.agent.ID, shutdownPayload.TaskID, err)
		} else {
			// Directly set the task to pending for immediate reassignment
			err = h.jobTaskRepo.SetTaskPending(client.ctx, taskID)
			if err != nil {
				debug.Error("Agent %d: Failed to reset task %s to pending: %v",
					client.agent.ID, taskID, err)
			} else {
				debug.Info("Agent %d: Successfully reset task %s to pending for immediate reassignment",
					client.agent.ID, taskID)

				// Check if we need to update the job status
				// If this was the only running task, the job should also be set to pending
				if shutdownPayload.JobID != "" {
					jobID, err := uuid.Parse(shutdownPayload.JobID)
					if err == nil {
						// Get active task count for this job
						activeCount, err := h.jobTaskRepo.GetActiveTasksCount(client.ctx, jobID)
						if err == nil && activeCount == 0 {
							debug.Info("Agent %d: No other active tasks for job %s, updating job status to pending",
								client.agent.ID, jobID)

							// No active tasks remaining, set job to pending
							err = h.jobExecRepo.UpdateStatus(client.ctx, jobID, models.JobExecutionStatusPending)
							if err != nil {
								debug.Error("Agent %d: Failed to update job %s status to pending: %v",
									client.agent.ID, jobID, err)
							} else {
								debug.Info("Agent %d: Successfully updated job %s status to pending",
									client.agent.ID, jobID)
							}
						}
					}
				}
			}
		}
	}
	
	// Mark agent as inactive
	if err := h.agentService.UpdateAgentStatus(client.ctx, client.agent.ID, models.AgentStatusInactive, nil); err != nil {
		debug.Error("Agent %d: Failed to update status to inactive: %v", client.agent.ID, err)
	} else {
		debug.Info("Agent %d: Marked as inactive due to graceful shutdown", client.agent.ID)
	}
	
	// Clear agent metadata
	if client.agent.Metadata == nil {
		client.agent.Metadata = make(map[string]string)
	}
	client.agent.Metadata["busy_status"] = "false"
	client.agent.Metadata["current_task_id"] = ""
	client.agent.Metadata["current_job_id"] = ""

	if err := h.agentService.UpdateAgentMetadata(client.ctx, client.agent.ID, client.agent.Metadata); err != nil {
		debug.Error("Agent %d: Failed to clear metadata on shutdown: %v", client.agent.ID, err)
	}
}

// handleDownloadProgress processes download progress updates from agents
func (h *Handler) handleDownloadProgress(client *Client, msg *wsservice.Message) {
	var payload models.DownloadProgressPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal download progress: %v", client.agent.ID, err)
		return
	}

	debug.Info("Agent %d: Download progress for %s: %.1f%% (%d/%d bytes)",
		client.agent.ID, payload.FileName, payload.PercentComplete,
		payload.BytesDownloaded, payload.TotalBytes)

	// TODO: Forward progress to UI via WebSocket or store in database for UI polling
}

// handleDownloadComplete processes download completion notifications from agents
func (h *Handler) handleDownloadComplete(client *Client, msg *wsservice.Message) {
	var payload models.DownloadCompletePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal download complete: %v", client.agent.ID, err)
		return
	}

	debug.Info("Agent %d: Successfully downloaded %s (%d bytes, MD5: %s) in %d seconds",
		client.agent.ID, payload.FileName, payload.TotalBytes,
		payload.MD5Hash, payload.DownloadTime)

	// TODO: Update file sync status in database if needed
}

// handleDownloadFailed processes download failure notifications from agents
func (h *Handler) handleDownloadFailed(client *Client, msg *wsservice.Message) {
	var payload models.DownloadFailedPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal download failed: %v", client.agent.ID, err)
		return
	}

	if payload.WillRetry {
		debug.Warning("Agent %d: Download failed for %s (attempt %d): %s - will retry",
			client.agent.ID, payload.FileName, payload.RetryAttempt, payload.Error)
	} else {
		debug.Error("Agent %d: Download permanently failed for %s: %s",
			client.agent.ID, payload.FileName, payload.Error)
		// TODO: Notify administrators or take corrective action
	}
}

// handlePendingOutfiles processes pending outfiles notification from agents on reconnect
func (h *Handler) handlePendingOutfiles(client *Client, msg *wsservice.Message) {
	debug.Info("Agent %d: Received pending_outfiles notification", client.agent.ID)

	// Forward to job handler for processing
	jobHandler := h.wsService.GetJobHandler()
	if jobHandler == nil {
		debug.Error("Agent %d: Job handler not available for pending_outfiles processing", client.agent.ID)
		return
	}

	if err := jobHandler.ProcessPendingOutfiles(client.ctx, client.agent.ID, msg.Payload); err != nil {
		debug.Error("Agent %d: Failed to process pending_outfiles: %v", client.agent.ID, err)
	}
}

// handleOutfileDeleteRejected processes outfile deletion rejection from agents (line count mismatch)
func (h *Handler) handleOutfileDeleteRejected(client *Client, msg *wsservice.Message) {
	debug.Info("Agent %d: Received outfile_delete_rejected notification", client.agent.ID)

	// Forward to job handler for processing
	jobHandler := h.wsService.GetJobHandler()
	if jobHandler == nil {
		debug.Error("Agent %d: Job handler not available for outfile_delete_rejected processing", client.agent.ID)
		return
	}

	if err := jobHandler.ProcessOutfileDeleteRejected(client.ctx, client.agent.ID, msg.Payload); err != nil {
		debug.Error("Agent %d: Failed to process outfile_delete_rejected: %v", client.agent.ID, err)
	}
}

// handleTaskStopAck handles task stop acknowledgment from agent (GH Issue #12)
func (h *Handler) handleTaskStopAck(client *Client, msg *wsservice.Message) {
	var ackPayload wsservice.TaskStopAckPayload
	if err := json.Unmarshal(msg.Payload, &ackPayload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal task_stop_ack payload: %v", client.agent.ID, err)
		return
	}

	if ackPayload.Stopped {
		debug.Info("Agent %d: Task stop acknowledged - task %s stopped successfully (stop_id: %s)",
			client.agent.ID, ackPayload.TaskID, ackPayload.StopID)
	} else {
		debug.Warning("Agent %d: Task stop acknowledged but task %s was not running (stop_id: %s, message: %s)",
			client.agent.ID, ackPayload.TaskID, ackPayload.StopID, ackPayload.Message)
	}

	// Note: Could track pending stops here and clear them when ACK received
	// For now, logging is sufficient for debugging purposes
}

// startStateSyncLoop periodically sends state sync requests to reconcile agent state (GH Issue #12)
func (c *Client) startStateSyncLoop() {
	// Wait 30 seconds after connection before first sync to let agent settle
	time.Sleep(30 * time.Second)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			debug.Debug("Agent %d: State sync loop stopping (context cancelled)", c.agent.ID)
			return
		case <-ticker.C:
			c.requestStateSync()
		}
	}
}

// requestStateSync sends a state sync request to the agent
func (c *Client) requestStateSync() {
	requestID := uuid.New().String()
	debug.Debug("Agent %d: Sending state sync request (request_id: %s)", c.agent.ID, requestID)

	payload := wsservice.StateSyncRequestPayload{
		RequestID: requestID,
		AgentID:   c.agent.ID,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		debug.Error("Agent %d: Failed to marshal state sync request: %v", c.agent.ID, err)
		return
	}

	msg := &wsservice.Message{
		Type:    wsservice.TypeStateSyncRequest,
		Payload: payloadBytes,
	}

	select {
	case c.send <- msg:
		debug.Debug("Agent %d: State sync request sent", c.agent.ID)
	default:
		debug.Warning("Agent %d: Failed to send state sync request (send buffer full)", c.agent.ID)
	}
}

// handleStateSyncResponse handles state sync response from agent and reconciles state (GH Issue #12)
func (h *Handler) handleStateSyncResponse(client *Client, msg *wsservice.Message) {
	var response wsservice.StateSyncResponsePayload
	if err := json.Unmarshal(msg.Payload, &response); err != nil {
		debug.Error("Agent %d: Failed to unmarshal state_sync_response: %v", client.agent.ID, err)
		return
	}

	debug.Info("Agent %d: State sync response received (request_id: %s, status: %s, has_task: %v)",
		client.agent.ID, response.RequestID, response.Status, response.HasRunningTask)

	// Reconcile state with backend records
	h.reconcileAgentState(client, &response)
}

// ============================================================================
// Diagnostics Handlers (GH Issue #23)
// ============================================================================

// AgentDebugStatus tracks the debug status of an agent (GH Issue #23)
type AgentDebugStatus struct {
	AgentID            int       `json:"agent_id"`
	Enabled            bool      `json:"enabled"`
	Level              string    `json:"level"`
	FileLoggingEnabled bool      `json:"file_logging_enabled"`
	LogFilePath        string    `json:"log_file_path,omitempty"`
	LogFileExists      bool      `json:"log_file_exists"`
	LogFileSize        int64     `json:"log_file_size"`
	LogFileModified    int64     `json:"log_file_modified"`
	BufferCount        int       `json:"buffer_count"`
	BufferCapacity     int       `json:"buffer_capacity"`
	LastUpdated        time.Time `json:"last_updated"`
}

// agentDebugStatuses stores debug status for each agent (in-memory cache)
var (
	agentDebugStatuses   = make(map[int]*AgentDebugStatus)
	agentDebugStatusesMu sync.RWMutex
)

// GetAgentDebugStatus returns the debug status for a specific agent
func GetAgentDebugStatus(agentID int) *AgentDebugStatus {
	agentDebugStatusesMu.RLock()
	defer agentDebugStatusesMu.RUnlock()
	return agentDebugStatuses[agentID]
}

// GetAllAgentDebugStatuses returns debug status for all agents
func GetAllAgentDebugStatuses() map[int]*AgentDebugStatus {
	agentDebugStatusesMu.RLock()
	defer agentDebugStatusesMu.RUnlock()
	result := make(map[int]*AgentDebugStatus, len(agentDebugStatuses))
	for k, v := range agentDebugStatuses {
		result[k] = v
	}
	return result
}

// handleDebugStatusReport processes debug status reports from agents
func (h *Handler) handleDebugStatusReport(client *Client, msg *wsservice.Message) {
	var payload wsservice.DebugStatusReportPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal debug status report: %v", client.agent.ID, err)
		return
	}

	debug.Info("Agent %d: Debug status report - enabled=%v, level=%s, file_logging=%v, buffer_count=%d",
		client.agent.ID, payload.Enabled, payload.Level, payload.FileLoggingEnabled, payload.BufferCount)

	// Store the debug status
	agentDebugStatusesMu.Lock()
	agentDebugStatuses[client.agent.ID] = &AgentDebugStatus{
		AgentID:            client.agent.ID,
		Enabled:            payload.Enabled,
		Level:              payload.Level,
		FileLoggingEnabled: payload.FileLoggingEnabled,
		LogFilePath:        payload.LogFilePath,
		LogFileExists:      payload.LogFileExists,
		LogFileSize:        payload.LogFileSize,
		LogFileModified:    payload.LogFileModified,
		BufferCount:        payload.BufferCount,
		BufferCapacity:     payload.BufferCapacity,
		LastUpdated:        time.Now(),
	}
	agentDebugStatusesMu.Unlock()
}

// handleDebugToggleAck processes debug toggle acknowledgments from agents
func (h *Handler) handleDebugToggleAck(client *Client, msg *wsservice.Message) {
	var payload wsservice.DebugToggleAckPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal debug toggle ack: %v", client.agent.ID, err)
		return
	}

	if payload.Success {
		debug.Info("Agent %d: Debug toggle successful - enabled=%v, restart_required=%v",
			client.agent.ID, payload.Enabled, payload.RestartRequired)
	} else {
		debug.Warning("Agent %d: Debug toggle failed: %s", client.agent.ID, payload.Message)
	}
}

// Callbacks for diagnostic responses (GH Issue #23)
var (
	logDataCallbacks      = make(map[string]chan *wsservice.LogDataPayload)
	logDataCallbacksMu    sync.RWMutex
	logStatusCallbacks    = make(map[string]chan *wsservice.LogStatusResponsePayload)
	logStatusCallbacksMu  sync.RWMutex
	logPurgeCallbacks     = make(map[string]chan *wsservice.LogPurgeAckPayload)
	logPurgeCallbacksMu   sync.RWMutex
)

// RegisterLogDataCallback registers a callback for log data response
func RegisterLogDataCallback(requestID string) <-chan *wsservice.LogDataPayload {
	logDataCallbacksMu.Lock()
	defer logDataCallbacksMu.Unlock()
	ch := make(chan *wsservice.LogDataPayload, 1)
	logDataCallbacks[requestID] = ch
	return ch
}

// UnregisterLogDataCallback removes a log data callback
func UnregisterLogDataCallback(requestID string) {
	logDataCallbacksMu.Lock()
	defer logDataCallbacksMu.Unlock()
	if ch, exists := logDataCallbacks[requestID]; exists {
		close(ch)
		delete(logDataCallbacks, requestID)
	}
}

// handleLogData processes log data from agents
func (h *Handler) handleLogData(client *Client, msg *wsservice.Message) {
	var payload wsservice.LogDataPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal log data: %v", client.agent.ID, err)
		return
	}

	debug.Info("Agent %d: Received log data (request_id: %s, entries: %d, truncated: %v)",
		client.agent.ID, payload.RequestID, len(payload.Entries), payload.Truncated)

	// Check for callback
	logDataCallbacksMu.RLock()
	ch, hasCallback := logDataCallbacks[payload.RequestID]
	logDataCallbacksMu.RUnlock()

	if hasCallback {
		select {
		case ch <- &payload:
			debug.Debug("Agent %d: Sent log data to callback (request_id: %s)", client.agent.ID, payload.RequestID)
		default:
			debug.Warning("Agent %d: Log data callback channel full (request_id: %s)", client.agent.ID, payload.RequestID)
		}
	} else {
		debug.Warning("Agent %d: No callback registered for log data (request_id: %s)", client.agent.ID, payload.RequestID)
	}
}

// RegisterLogStatusCallback registers a callback for log status response
func RegisterLogStatusCallback(requestID string) <-chan *wsservice.LogStatusResponsePayload {
	logStatusCallbacksMu.Lock()
	defer logStatusCallbacksMu.Unlock()
	ch := make(chan *wsservice.LogStatusResponsePayload, 1)
	logStatusCallbacks[requestID] = ch
	return ch
}

// UnregisterLogStatusCallback removes a log status callback
func UnregisterLogStatusCallback(requestID string) {
	logStatusCallbacksMu.Lock()
	defer logStatusCallbacksMu.Unlock()
	if ch, exists := logStatusCallbacks[requestID]; exists {
		close(ch)
		delete(logStatusCallbacks, requestID)
	}
}

// handleLogStatusResponse processes log status responses from agents
func (h *Handler) handleLogStatusResponse(client *Client, msg *wsservice.Message) {
	var payload wsservice.LogStatusResponsePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal log status response: %v", client.agent.ID, err)
		return
	}

	debug.Debug("Agent %d: Received log status (request_id: %s, file_exists: %v, debug_enabled: %v)",
		client.agent.ID, payload.RequestID, payload.LogFileExists, payload.DebugEnabled)

	// Check for callback
	logStatusCallbacksMu.RLock()
	ch, hasCallback := logStatusCallbacks[payload.RequestID]
	logStatusCallbacksMu.RUnlock()

	if hasCallback {
		select {
		case ch <- &payload:
			debug.Debug("Agent %d: Sent log status to callback (request_id: %s)", client.agent.ID, payload.RequestID)
		default:
			debug.Warning("Agent %d: Log status callback channel full (request_id: %s)", client.agent.ID, payload.RequestID)
		}
	}
}

// RegisterLogPurgeCallback registers a callback for log purge acknowledgment
func RegisterLogPurgeCallback(requestID string) <-chan *wsservice.LogPurgeAckPayload {
	logPurgeCallbacksMu.Lock()
	defer logPurgeCallbacksMu.Unlock()
	ch := make(chan *wsservice.LogPurgeAckPayload, 1)
	logPurgeCallbacks[requestID] = ch
	return ch
}

// UnregisterLogPurgeCallback removes a log purge callback
func UnregisterLogPurgeCallback(requestID string) {
	logPurgeCallbacksMu.Lock()
	defer logPurgeCallbacksMu.Unlock()
	if ch, exists := logPurgeCallbacks[requestID]; exists {
		close(ch)
		delete(logPurgeCallbacks, requestID)
	}
}

// handleLogPurgeAck processes log purge acknowledgments from agents
func (h *Handler) handleLogPurgeAck(client *Client, msg *wsservice.Message) {
	var payload wsservice.LogPurgeAckPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		debug.Error("Agent %d: Failed to unmarshal log purge ack: %v", client.agent.ID, err)
		return
	}

	if payload.Success {
		debug.Info("Agent %d: Log purge successful (request_id: %s)", client.agent.ID, payload.RequestID)
	} else {
		debug.Warning("Agent %d: Log purge failed (request_id: %s): %s", client.agent.ID, payload.RequestID, payload.Message)
	}

	// Check for callback
	logPurgeCallbacksMu.RLock()
	ch, hasCallback := logPurgeCallbacks[payload.RequestID]
	logPurgeCallbacksMu.RUnlock()

	if hasCallback {
		select {
		case ch <- &payload:
			debug.Debug("Agent %d: Sent log purge ack to callback (request_id: %s)", client.agent.ID, payload.RequestID)
		default:
			debug.Warning("Agent %d: Log purge callback channel full (request_id: %s)", client.agent.ID, payload.RequestID)
		}
	}
}

// SendDebugToggle sends a debug toggle command to a specific agent
func (h *Handler) SendDebugToggle(agentID int, enable bool) error {
	payload := wsservice.DebugTogglePayload{
		Enable: enable,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal debug toggle payload: %w", err)
	}

	msg := &wsservice.Message{
		Type:    wsservice.TypeDebugToggle,
		Payload: payloadBytes,
	}

	return h.SendMessage(agentID, msg)
}

// SendLogRequest sends a log request to a specific agent
func (h *Handler) SendLogRequest(agentID int, requestID string, hoursBack int, includeAll bool) error {
	payload := wsservice.LogRequestPayload{
		RequestID:  requestID,
		HoursBack:  hoursBack,
		IncludeAll: includeAll,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal log request payload: %w", err)
	}

	msg := &wsservice.Message{
		Type:    wsservice.TypeLogRequest,
		Payload: payloadBytes,
	}

	return h.SendMessage(agentID, msg)
}

// SendLogStatusRequest sends a log status request to a specific agent
func (h *Handler) SendLogStatusRequest(agentID int, requestID string) error {
	payload := wsservice.LogStatusRequestPayload{
		RequestID: requestID,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal log status request payload: %w", err)
	}

	msg := &wsservice.Message{
		Type:    wsservice.TypeLogStatusRequest,
		Payload: payloadBytes,
	}

	return h.SendMessage(agentID, msg)
}

// SendLogPurge sends a log purge command to a specific agent
func (h *Handler) SendLogPurge(agentID int, requestID string) error {
	payload := wsservice.LogPurgePayload{
		RequestID: requestID,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal log purge payload: %w", err)
	}

	msg := &wsservice.Message{
		Type:    wsservice.TypeLogPurge,
		Payload: payloadBytes,
	}

	return h.SendMessage(agentID, msg)
}

// reconcileAgentState compares agent's reported state with backend records and fixes discrepancies
func (h *Handler) reconcileAgentState(client *Client, response *wsservice.StateSyncResponsePayload) {
	ctx := client.ctx
	agentID := client.agent.ID

	// Get agent's current busy status from DB
	agent, err := h.agentService.GetByID(ctx, agentID)
	if err != nil {
		debug.Error("Agent %d: Failed to get agent for reconciliation: %v", agentID, err)
		return
	}

	busyStatus := "false"
	if val, ok := agent.Metadata["busy_status"]; ok {
		busyStatus = val
	}
	backendThinksBusy := busyStatus == "true"

	currentTaskID := ""
	if val, ok := agent.Metadata["current_task_id"]; ok {
		currentTaskID = val
	}

	// Check for state mismatches
	if response.Status == "idle" && !response.HasRunningTask {
		// Agent says it's idle
		if backendThinksBusy {
			debug.Warning("Agent %d: State mismatch - agent idle but backend shows busy (task: %s). Clearing busy status. (GH Issue #12 reconciliation)",
				agentID, currentTaskID)

			// Clear busy status
			if err := h.agentService.ClearAgentBusyStatus(ctx, agentID); err != nil {
				debug.Error("Agent %d: Failed to clear busy status during reconciliation: %v", agentID, err)
			} else {
				debug.Info("Agent %d: Successfully cleared stale busy status via state sync", agentID)
			}
		}
	} else if response.HasRunningTask && response.TaskID != "" {
		// Agent says it's running a task
		if !backendThinksBusy {
			debug.Warning("Agent %d: State mismatch - agent running task %s but backend shows idle. Checking task status.",
				agentID, response.TaskID)
		}

		// Check if the task exists and is in the correct state
		if response.TaskID != currentTaskID && currentTaskID != "" {
			debug.Warning("Agent %d: Task mismatch - agent reports task %s but backend has task %s",
				agentID, response.TaskID, currentTaskID)
		}

		// Verify task is actually assigned to this agent
		taskUUID, err := uuid.Parse(response.TaskID)
		if err == nil {
			task, err := h.jobTaskRepo.GetByID(ctx, taskUUID)
			if err != nil {
				debug.Warning("Agent %d: Reported task %s not found in DB", agentID, response.TaskID)
			} else if task.Status == "completed" || task.Status == "failed" || task.Status == "cancelled" {
				debug.Warning("Agent %d: Agent running task %s but task is in terminal state: %s. Notifying agent to stop.",
					agentID, response.TaskID, task.Status)
				// Could send stop command here if needed
			}
		}
	}

	// Process any pending completions that agent reports
	if len(response.PendingCompletions) > 0 {
		debug.Warning("Agent %d: Agent has %d pending completions that weren't acknowledged: %v",
			agentID, len(response.PendingCompletions), response.PendingCompletions)

		// For each pending completion, check if task is actually completed and send ACK
		for _, taskID := range response.PendingCompletions {
			taskUUID, err := uuid.Parse(taskID)
			if err != nil {
				debug.Error("Agent %d: Invalid pending completion task ID: %s", agentID, taskID)
				continue
			}

			task, err := h.jobTaskRepo.GetByID(ctx, taskUUID)
			if err != nil {
				debug.Error("Agent %d: Failed to get pending completion task %s: %v", agentID, taskID, err)
				continue
			}

			if task.Status == "completed" || task.Status == "failed" {
				// Task was processed, send ACK
				debug.Info("Agent %d: Sending belated ACK for pending completion task %s (status: %s)",
					agentID, taskID, task.Status)

				ackPayload := wsservice.TaskCompleteAckPayload{
					TaskID:    taskID,
					Success:   task.Status == "completed",
					Timestamp: time.Now().Unix(),
					Message:   "Belated ACK from state sync reconciliation",
				}

				payloadBytes, err := json.Marshal(ackPayload)
				if err != nil {
					debug.Error("Agent %d: Failed to marshal belated ACK: %v", agentID, err)
					continue
				}

				msg := &wsservice.Message{
					Type:    wsservice.TypeTaskCompleteAck,
					Payload: payloadBytes,
				}

				select {
				case client.send <- msg:
					debug.Info("Agent %d: Belated ACK sent for task %s", agentID, taskID)
				default:
					debug.Warning("Agent %d: Failed to send belated ACK (buffer full)", agentID)
				}
			}
		}
	}
}
