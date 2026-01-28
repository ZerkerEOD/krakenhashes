package services

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// AgentOfflineMonitor monitors agents and sends notifications after buffer period
type AgentOfflineMonitor struct {
	db                     *db.DB
	bufferRepo             *repository.AgentOfflineBufferRepository
	agentRepo              *repository.AgentRepository
	systemSettingsRepo     *repository.SystemSettingsRepository
	notificationDispatcher *NotificationDispatcher

	bufferMinutes int
	ticker        *time.Ticker
	stopCh        chan struct{}
	running       bool
	mu            sync.Mutex
}

// NewAgentOfflineMonitor creates a new agent offline monitor
func NewAgentOfflineMonitor(
	dbConn *sql.DB,
	notificationDispatcher *NotificationDispatcher,
) *AgentOfflineMonitor {
	database := &db.DB{DB: dbConn}
	return &AgentOfflineMonitor{
		db:                     database,
		bufferRepo:             repository.NewAgentOfflineBufferRepository(database),
		agentRepo:              repository.NewAgentRepository(database),
		systemSettingsRepo:     repository.NewSystemSettingsRepository(database),
		notificationDispatcher: notificationDispatcher,
		bufferMinutes:          10, // Default, will be loaded from settings
		stopCh:                 make(chan struct{}),
	}
}

// Start begins the monitoring goroutine
func (m *AgentOfflineMonitor) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("monitor already running")
	}
	m.running = true
	m.mu.Unlock()

	// Load buffer minutes from system settings
	if err := m.loadBufferMinutes(ctx); err != nil {
		debug.Warning("Failed to load buffer minutes from settings, using default: %v", err)
	}

	debug.Info("Starting agent offline monitor with %d minute buffer", m.bufferMinutes)

	// Check every minute for pending notifications
	m.ticker = time.NewTicker(1 * time.Minute)

	go m.monitorLoop(ctx)

	return nil
}

// Stop stops the monitoring goroutine
func (m *AgentOfflineMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	close(m.stopCh)
	if m.ticker != nil {
		m.ticker.Stop()
	}
	m.running = false

	debug.Info("Agent offline monitor stopped")
}

// OnAgentDisconnect is called when an agent disconnects
func (m *AgentOfflineMonitor) OnAgentDisconnect(ctx context.Context, agentID int) error {
	// Check if there's already a pending buffer for this agent
	existing, err := m.bufferRepo.GetPendingByAgentID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to check existing buffer: %w", err)
	}

	if existing != nil {
		debug.Log("Agent already has pending offline buffer", map[string]interface{}{
			"agent_id":  agentID,
			"buffer_id": existing.ID,
		})
		return nil
	}

	// Create new buffer entry
	buffer := models.NewAgentOfflineBuffer(agentID, m.bufferMinutes)

	if err := m.bufferRepo.Create(ctx, buffer); err != nil {
		return fmt.Errorf("failed to create offline buffer: %w", err)
	}

	debug.Log("Agent offline buffer created", map[string]interface{}{
		"agent_id":            agentID,
		"buffer_id":           buffer.ID,
		"notification_due_at": buffer.NotificationDueAt,
	})

	return nil
}

// OnAgentReconnect cancels pending notification
func (m *AgentOfflineMonitor) OnAgentReconnect(ctx context.Context, agentID int) error {
	if err := m.bufferRepo.MarkAsReconnected(ctx, agentID, time.Now()); err != nil {
		return fmt.Errorf("failed to mark as reconnected: %w", err)
	}

	debug.Log("Agent reconnected, offline notification cancelled", map[string]interface{}{
		"agent_id": agentID,
	})

	return nil
}

// monitorLoop runs the main monitoring loop
func (m *AgentOfflineMonitor) monitorLoop(ctx context.Context) {
	// Process immediately on start
	m.processBufferedNotifications(ctx)

	for {
		select {
		case <-ctx.Done():
			debug.Info("Agent offline monitor context cancelled")
			return
		case <-m.stopCh:
			debug.Info("Agent offline monitor stop signal received")
			return
		case <-m.ticker.C:
			m.processBufferedNotifications(ctx)
		}
	}
}

// processBufferedNotifications checks for due notifications and sends them
func (m *AgentOfflineMonitor) processBufferedNotifications(ctx context.Context) {
	now := time.Now()

	pending, err := m.bufferRepo.GetPendingDue(ctx, now)
	if err != nil {
		debug.Error("Failed to get pending offline notifications: %v", err)
		return
	}

	if len(pending) == 0 {
		return
	}

	debug.Log("Processing pending offline notifications", map[string]interface{}{
		"count": len(pending),
	})

	for _, buffer := range pending {
		if err := m.sendOfflineNotification(ctx, buffer); err != nil {
			debug.Error("Failed to send offline notification for agent %d: %v", buffer.AgentID, err)
			continue
		}

		// Mark as sent
		if err := m.bufferRepo.MarkAsSent(ctx, buffer.ID, time.Now()); err != nil {
			debug.Error("Failed to mark buffer as sent: %v", err)
		}
	}

	// Clean up old entries (older than 7 days)
	cleanupTime := time.Now().AddDate(0, 0, -7)
	deleted, err := m.bufferRepo.DeleteOld(ctx, cleanupTime)
	if err != nil {
		debug.Warning("Failed to cleanup old buffer entries: %v", err)
	} else if deleted > 0 {
		debug.Log("Cleaned up old buffer entries", map[string]interface{}{
			"deleted": deleted,
		})
	}
}

// sendOfflineNotification sends the offline notification for an agent
func (m *AgentOfflineMonitor) sendOfflineNotification(ctx context.Context, buffer *models.AgentOfflineBuffer) error {
	// Get agent details
	agent, err := m.agentRepo.GetByID(ctx, buffer.AgentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}
	if agent == nil {
		debug.Warning("Agent not found, skipping notification", map[string]interface{}{
			"agent_id": buffer.AgentID,
		})
		return nil
	}

	// Get agent owner
	if agent.OwnerID == nil {
		debug.Warning("Agent has no owner, skipping notification", map[string]interface{}{
			"agent_id": buffer.AgentID,
		})
		return nil
	}

	// Calculate offline duration for display
	offlineDuration := time.Since(buffer.DisconnectedAt).Round(time.Minute)
	offlineDurationStr := formatDuration(offlineDuration)

	// Dispatch notification to agent owner
	params := models.NotificationDispatchParams{
		UserID:  *agent.OwnerID,
		Type:    models.NotificationTypeAgentOffline,
		Title:   fmt.Sprintf("Agent '%s' Offline", agent.Name),
		Message: fmt.Sprintf("Agent has been offline for over %d minutes", m.bufferMinutes),
		Data: map[string]interface{}{
			"AgentID":         agent.ID,
			"AgentName":       agent.Name,
			"LastSeen":        buffer.DisconnectedAt.Format(time.RFC3339),
			"BufferMinutes":   m.bufferMinutes,
			"DisconnectedAt":  buffer.DisconnectedAt,
			"OfflineDuration": offlineDurationStr,
		},
		SourceType: "agent",
		SourceID:   buffer.ID.String(), // Unique per offline event - each disconnect creates a new buffer entry
	}

	if err := m.notificationDispatcher.Dispatch(ctx, params); err != nil {
		return fmt.Errorf("failed to dispatch notification: %w", err)
	}

	debug.Log("Agent offline notification sent", map[string]interface{}{
		"agent_id":   agent.ID,
		"agent_name": agent.Name,
		"owner_id":   agent.OwnerID,
	})

	return nil
}

// loadBufferMinutes loads the buffer duration from system settings
func (m *AgentOfflineMonitor) loadBufferMinutes(ctx context.Context) error {
	setting, err := m.systemSettingsRepo.GetSetting(ctx, "agent_offline_buffer_minutes")
	if err != nil {
		return err
	}

	if setting.Value != nil {
		var minutes int
		if _, err := fmt.Sscanf(*setting.Value, "%d", &minutes); err == nil && minutes > 0 {
			m.bufferMinutes = minutes
		}
	}

	return nil
}

// GetBufferMinutes returns the current buffer duration
func (m *AgentOfflineMonitor) GetBufferMinutes() int {
	return m.bufferMinutes
}

// SetBufferMinutes updates the buffer duration
func (m *AgentOfflineMonitor) SetBufferMinutes(minutes int) {
	if minutes > 0 {
		m.bufferMinutes = minutes
	}
}
