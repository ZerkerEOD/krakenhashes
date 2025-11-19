package services

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// AgentService handles agent-related operations
type AgentService struct {
	agentRepo       *repository.AgentRepository
	voucherRepo     *repository.ClaimVoucherRepository
	fileRepo        *repository.FileRepository
	deviceRepo      *repository.AgentDeviceRepository
	jobTaskRepo     *repository.JobTaskRepository
	jobExecutionRepo *repository.JobExecutionRepository
	tokens          map[string]downloadToken
	tokenMutex      sync.RWMutex
}

type downloadToken struct {
	AgentID   int
	ExpiresAt time.Time
}

// NewAgentService creates a new instance of AgentService
func NewAgentService(agentRepo *repository.AgentRepository, voucherRepo *repository.ClaimVoucherRepository, fileRepo *repository.FileRepository, deviceRepo *repository.AgentDeviceRepository, jobTaskRepo *repository.JobTaskRepository, jobExecutionRepo *repository.JobExecutionRepository) *AgentService {
	return &AgentService{
		agentRepo:        agentRepo,
		voucherRepo:      voucherRepo,
		fileRepo:         fileRepo,
		deviceRepo:       deviceRepo,
		jobTaskRepo:      jobTaskRepo,
		jobExecutionRepo: jobExecutionRepo,
		tokens:           make(map[string]downloadToken),
	}
}

// ValidateClaimCode validates a claim code
func (s *AgentService) ValidateClaimCode(ctx context.Context, claimCode string) error {
	// Normalize claim code by removing hyphens and converting to uppercase
	normalizedCode := strings.ToUpper(strings.ReplaceAll(claimCode, "-", ""))

	voucher, err := s.voucherRepo.GetByCode(ctx, normalizedCode)
	if err != nil {
		return fmt.Errorf("invalid claim code")
	}

	if !voucher.IsValid() {
		return fmt.Errorf("claim code expired")
	}

	return nil
}

// RegisterAgent registers a new agent using a claim code.
// This is a single-step process that:
// 1. Validates the claim code
// 2. Generates a unique agent name
// 3. Generates a secure API key
// 4. Creates the agent record
//
// Parameters:
//   - ctx: Context for the operation
//   - claimCode: The voucher code for agent registration
//   - hostname: The agent's hostname for identification
//
// Returns:
//   - *models.Agent: The newly registered agent
//   - error: Any errors encountered during registration
func (s *AgentService) RegisterAgent(ctx context.Context, claimCode, hostname string) (*models.Agent, error) {
	debug.Info("Starting agent registration with claim code: %s, hostname: %s", claimCode, hostname)

	// Normalize claim code by removing hyphens and converting to uppercase
	normalizedCode := strings.ToUpper(strings.ReplaceAll(claimCode, "-", ""))
	debug.Debug("Normalized claim code: %s", normalizedCode)

	// Validate claim code
	voucher, err := s.voucherRepo.GetByCode(ctx, normalizedCode)
	if err != nil {
		debug.Error("Invalid claim code: %v", err)
		return nil, fmt.Errorf("invalid claim code")
	}

	if !voucher.IsValid() {
		debug.Error("Claim code is not active")
		return nil, fmt.Errorf("claim code is not active")
	}
	debug.Info("Claim code validated successfully")

	// Check for existing agent with same name and modify if needed
	name := hostname
	for i := 2; ; i++ {
		exists, err := s.agentRepo.ExistsByName(ctx, name)
		if err != nil {
			debug.Error("Failed to check agent name: %v", err)
			return nil, fmt.Errorf("failed to check agent name: %w", err)
		}
		if !exists {
			break
		}
		debug.Debug("Agent name %s already exists, trying %s%d", name, hostname, i)
		name = fmt.Sprintf("%s%d", hostname, i)
	}
	debug.Info("Using agent name: %s", name)

	// Generate API key (64-character hex string)
	apiKeyBytes := make([]byte, 32) // 32 bytes = 64 hex characters
	if _, err := rand.Read(apiKeyBytes); err != nil {
		debug.Error("Failed to generate API key: %v", err)
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}
	apiKey := hex.EncodeToString(apiKeyBytes)
	debug.Debug("Generated API key for agent")

	now := time.Now()

	// Create new agent
	agent := &models.Agent{
		Name:          name,
		Status:        models.AgentStatusPending,
		CreatedByID:   voucher.CreatedByID,
		OwnerID:       &voucher.CreatedByID, // Set owner to creator initially
		CreatedAt:     now,
		UpdatedAt:     now,
		LastHeartbeat: now,     // Initialize heartbeat timestamp
		Version:       "1.0.0", // Set initial version
		APIKey: sql.NullString{
			String: apiKey,
			Valid:  true,
		},
		APIKeyCreatedAt: sql.NullTime{
			Time:  now,
			Valid: true,
		},
	}

	// Create agent record
	if err := s.agentRepo.Create(ctx, agent); err != nil {
		debug.Error("Failed to create agent: %v", err)
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}
	debug.Info("Successfully created agent with ID: %d", agent.ID)

	// Mark claim code as used if it's a single-use code
	if !voucher.IsContinuous {
		if err := s.MarkClaimCodeUsed(ctx, claimCode, agent.ID); err != nil {
			debug.Warning("Failed to mark claim code as used: %v", err)
			// Continue anyway as the agent is already created
		}
	}

	return agent, nil
}

// RegisterAgentWithVersion registers a new agent with a claim code and includes the agent version
//
// This function validates the claim code, generates API credentials, and creates the agent record
// with the specified version. If no version is provided, it defaults to "unknown".
//
// Parameters:
//   - ctx: The context for the operation
//   - claimCode: The voucher code for registering the agent
//   - hostname: The hostname of the agent
//   - version: The version of the agent software
//
// Returns:
//   - *models.Agent: The newly registered agent
//   - error: Any errors encountered during registration
func (s *AgentService) RegisterAgentWithVersion(ctx context.Context, claimCode, hostname, version string) (*models.Agent, error) {
	debug.Info("Starting agent registration with claim code: %s, hostname: %s, version: %s", claimCode, hostname, version)

	// Normalize claim code by removing hyphens and converting to uppercase
	normalizedCode := strings.ToUpper(strings.ReplaceAll(claimCode, "-", ""))
	debug.Debug("Normalized claim code: %s", normalizedCode)

	// Validate claim code
	voucher, err := s.voucherRepo.GetByCode(ctx, normalizedCode)
	if err != nil {
		debug.Error("Invalid claim code: %v", err)
		return nil, fmt.Errorf("invalid claim code")
	}

	if !voucher.IsValid() {
		debug.Error("Claim code is not active")
		return nil, fmt.Errorf("claim code is not active")
	}
	debug.Info("Claim code validated successfully")

	// Check for existing agent with same name and modify if needed
	name := hostname
	for i := 2; ; i++ {
		exists, err := s.agentRepo.ExistsByName(ctx, name)
		if err != nil {
			debug.Error("Failed to check agent name: %v", err)
			return nil, fmt.Errorf("failed to check agent name: %w", err)
		}
		if !exists {
			break
		}
		debug.Debug("Agent name %s already exists, trying %s%d", name, hostname, i)
		name = fmt.Sprintf("%s%d", hostname, i)
	}
	debug.Info("Using agent name: %s", name)

	// Generate API key (64-character hex string)
	apiKeyBytes := make([]byte, 32) // 32 bytes = 64 hex characters
	if _, err := rand.Read(apiKeyBytes); err != nil {
		debug.Error("Failed to generate API key: %v", err)
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}
	apiKey := hex.EncodeToString(apiKeyBytes)
	debug.Debug("Generated API key for agent")

	now := time.Now()

	// Use provided version or default to "unknown"
	if version == "" {
		version = "unknown"
		debug.Debug("No version provided, defaulting to 'unknown'")
	}

	// Create new agent
	agent := &models.Agent{
		Name:          name,
		Status:        models.AgentStatusPending,
		CreatedByID:   voucher.CreatedByID,
		OwnerID:       &voucher.CreatedByID, // Set owner to creator initially
		CreatedAt:     now,
		UpdatedAt:     now,
		LastHeartbeat: now,     // Initialize heartbeat timestamp
		Version:       version, // Use provided version
		APIKey: sql.NullString{
			String: apiKey,
			Valid:  true,
		},
		APIKeyCreatedAt: sql.NullTime{
			Time:  now,
			Valid: true,
		},
	}

	// Create agent record
	if err := s.agentRepo.Create(ctx, agent); err != nil {
		debug.Error("Failed to create agent: %v", err)
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}
	debug.Info("Successfully created agent with ID: %d and version: %s", agent.ID, agent.Version)

	// Mark claim code as used if it's a single-use code
	if !voucher.IsContinuous {
		if err := s.MarkClaimCodeUsed(ctx, claimCode, agent.ID); err != nil {
			debug.Warning("Failed to mark claim code as used: %v", err)
			// Continue anyway as the agent is already created
		}
	}

	return agent, nil
}

// MarkClaimCodeUsed marks a claim code as used by an agent after successful connection
func (s *AgentService) MarkClaimCodeUsed(ctx context.Context, claimCode string, agentID int) error {
	// Normalize claim code
	normalizedCode := strings.ToUpper(strings.ReplaceAll(claimCode, "-", ""))

	// Get voucher
	voucher, err := s.voucherRepo.GetByCode(ctx, normalizedCode)
	if err != nil {
		return fmt.Errorf("invalid claim code")
	}

	// Only mark as used if it's a single-use code
	if !voucher.IsContinuous {
		if err := s.voucherRepo.UseByAgent(ctx, normalizedCode, agentID); err != nil {
			debug.Error("failed to mark voucher as used: %v", err)
			return fmt.Errorf("failed to mark voucher as used: %v", err)
		}
		// Deactivate single-use vouchers after use
		if err := s.voucherRepo.Deactivate(ctx, normalizedCode); err != nil {
			debug.Error("failed to deactivate voucher: %v", err)
			return fmt.Errorf("failed to deactivate voucher: %v", err)
		}
		debug.Info("Successfully marked single-use claim code as used for agent %d", agentID)
	}

	return nil
}

// CreateDownloadToken generates a temporary token for certificate download
func (s *AgentService) CreateDownloadToken(ctx context.Context, agentID int) (string, error) {
	s.tokenMutex.Lock()
	defer s.tokenMutex.Unlock()

	// Generate token
	token := uuid.New().String()

	// Store token with expiration
	s.tokens[token] = downloadToken{
		AgentID:   agentID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	return token, nil
}

// ValidateDownloadToken validates a download token and returns the associated agent ID
func (s *AgentService) ValidateDownloadToken(ctx context.Context, token string) (int, error) {
	s.tokenMutex.RLock()
	defer s.tokenMutex.RUnlock()

	dt, exists := s.tokens[token]
	if !exists {
		return 0, fmt.Errorf("invalid download token")
	}

	if time.Now().After(dt.ExpiresAt) {
		delete(s.tokens, token)
		return 0, fmt.Errorf("download token expired")
	}

	return dt.AgentID, nil
}

// InvalidateDownloadToken invalidates a download token
func (s *AgentService) InvalidateDownloadToken(ctx context.Context, token string) error {
	s.tokenMutex.Lock()
	defer s.tokenMutex.Unlock()

	delete(s.tokens, token)
	return nil
}

// GetAgent retrieves a single agent by ID
func (s *AgentService) GetAgent(ctx context.Context, id int) (*models.Agent, error) {
	debug.Info("Getting agent: %d", id)
	return s.agentRepo.GetByID(ctx, id)
}

// ListAgents retrieves all agents with optional filters
func (s *AgentService) ListAgents(ctx context.Context, filters map[string]interface{}) ([]models.Agent, error) {
	debug.Info("Listing agents with filters: %v", filters)
	return s.agentRepo.List(ctx, filters)
}

// DeleteAgent deletes an agent by ID
func (s *AgentService) DeleteAgent(ctx context.Context, id int) error {
	debug.Info("Deleting agent: %d", id)
	return s.agentRepo.Delete(ctx, id)
}

// HandleAgentConnection handles the WebSocket connection for an agent
func (s *AgentService) HandleAgentConnection(ctx context.Context, conn *websocket.Conn, agent *models.Agent) error {
	debug.Info("Agent %d connected via WebSocket", agent.ID)

	// Update agent status to connected
	now := time.Now()
	agent.Status = models.AgentStatusActive
	agent.LastSeen = now
	agent.APIKeyLastUsed = sql.NullTime{
		Time:  now,
		Valid: true,
	}
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		debug.Error("failed to update agent status: %v", err)
		return fmt.Errorf("failed to update agent status: %v", err)
	}

	// Set up connection handling
	done := make(chan struct{})
	errChan := make(chan error)

	// Start heartbeat handler
	go s.handleHeartbeat(ctx, agent.ID, done, errChan)

	// Start metrics handler
	go s.handleMetrics(ctx, conn, agent.ID, done, errChan)

	// Wait for error or context cancellation
	select {
	case <-ctx.Done():
		close(done)
		return ctx.Err()
	case err := <-errChan:
		close(done)
		return err
	}
}

// handleHeartbeat processes agent heartbeat
func (s *AgentService) handleHeartbeat(ctx context.Context, agentID int, done chan struct{}, errChan chan error) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := s.agentRepo.UpdateHeartbeat(ctx, agentID); err != nil {
				debug.Error("failed to update heartbeat: %v", err)
				errChan <- err
				return
			}
		}
	}
}

// handleMetrics processes agent metrics
func (s *AgentService) handleMetrics(ctx context.Context, conn *websocket.Conn, agentID int, done chan struct{}, errChan chan error) {
	for {
		select {
		case <-done:
			return
		default:
			var metrics models.AgentMetrics
			if err := conn.ReadJSON(&metrics); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					debug.Error("websocket error: %v", err)
					errChan <- err
				}
				return
			}

			metrics.AgentID = agentID
			metrics.Timestamp = time.Now()

			if err := s.agentRepo.SaveMetrics(ctx, &metrics); err != nil {
				debug.Error("failed to save metrics: %v", err)
				errChan <- err
				return
			}
		}
	}
}

// UpdateAgentStatus updates an agent's status and last error
func (s *AgentService) UpdateAgentStatus(ctx context.Context, id int, status string, lastError *string) error {
	return s.agentRepo.UpdateStatus(ctx, id, status, lastError)
}

// UpdateAgentVersion updates an agent's version
func (s *AgentService) UpdateAgentVersion(ctx context.Context, id int, version string) error {
	// Don't update if version is empty
	if version == "" {
		debug.Debug("Skipping version update for agent %d - empty version", id)
		return nil
	}

	debug.Debug("Updating agent %d version to %s", id, version)
	return s.agentRepo.UpdateVersion(ctx, id, version)
}

// UpdateAgentMetadata updates an agent's metadata
func (s *AgentService) UpdateAgentMetadata(ctx context.Context, id int, metadata map[string]string) error {
	return s.agentRepo.UpdateMetadata(ctx, id, metadata)
}

// UpdateAgentSyncStatus updates the sync status for an agent
func (s *AgentService) UpdateAgentSyncStatus(ctx context.Context, id int, status string, errorMsg string) error {
	debug.Debug("Updating agent %d sync status to %s", id, status)

	agent, err := s.agentRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	agent.SyncStatus = status
	if errorMsg != "" {
		agent.SyncError = sql.NullString{String: errorMsg, Valid: true}
	} else {
		agent.SyncError = sql.NullString{Valid: false}
	}

	// Update timestamps based on status
	now := time.Now()
	if status == models.AgentSyncStatusInProgress {
		agent.SyncStartedAt = sql.NullTime{Time: now, Valid: true}
		agent.SyncCompletedAt = sql.NullTime{Valid: false}
	} else if status == models.AgentSyncStatusCompleted {
		agent.SyncCompletedAt = sql.NullTime{Time: now, Valid: true}
	}

	return s.agentRepo.Update(ctx, agent)
}

// UpdateLastSeen updates the last seen timestamp for an agent
func (s *AgentService) UpdateLastSeen(agentID int) error {
	now := time.Now()
	agent, err := s.agentRepo.GetByID(context.Background(), agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	agent.LastSeen = now
	agent.APIKeyLastUsed = sql.NullTime{
		Time:  now,
		Valid: true,
	}

	if err := s.agentRepo.Update(context.Background(), agent); err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	return nil
}

// UpdateHardwareInfo updates the hardware info for an agent
func (s *AgentService) UpdateHardwareInfo(agentID int, hardwareInfo *models.Hardware, osInfo json.RawMessage) error {
	debug.Info("Updating hardware info for agent: %d", agentID)
	agent, err := s.agentRepo.GetByID(context.Background(), agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	// Update hardware info
	agent.Hardware = *hardwareInfo
	agent.OSInfo = osInfo
	agent.UpdatedAt = time.Now()

	// Update the agent in the database
	if err := s.agentRepo.Update(context.Background(), agent); err != nil {
		return fmt.Errorf("failed to update agent hardware: %w", err)
	}

	debug.Info("Successfully updated hardware info for agent: %d", agentID)
	return nil
}

// GetByID retrieves an agent by ID
func (s *AgentService) GetByID(ctx context.Context, id int) (*models.Agent, error) {
	return s.agentRepo.GetByID(ctx, id)
}

// Delete deletes an agent
func (s *AgentService) Delete(ctx context.Context, id int) error {
	return s.agentRepo.Delete(ctx, id)
}

// ProcessHeartbeat processes a heartbeat update for an agent
func (s *AgentService) ProcessHeartbeat(ctx context.Context, agentID int) error {
	return s.agentRepo.UpdateHeartbeat(ctx, agentID)
}

// ProcessMetrics processes metrics for an agent
func (s *AgentService) ProcessMetrics(ctx context.Context, agentID int, metrics *models.AgentMetrics) error {
	now := time.Now()
	metrics.Timestamp = now

	// Update agent's last seen time
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	agent.LastSeen = now
	agent.APIKeyLastUsed = sql.NullTime{
		Time:  now,
		Valid: true,
	}

	if err := s.agentRepo.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	// Save metrics
	if err := s.agentRepo.SaveMetrics(ctx, metrics); err != nil {
		return fmt.Errorf("failed to save metrics: %w", err)
	}

	return nil
}

// Update updates an agent's information
func (s *AgentService) Update(ctx context.Context, agent *models.Agent) error {
	agent.UpdatedAt = time.Now()
	return s.agentRepo.Update(ctx, agent)
}

// ValidateAgentAPIKey validates an agent's API key and returns the agent ID if valid
func (s *AgentService) ValidateAgentAPIKey(ctx context.Context, apiKey string) (int, error) {
	agent, err := s.agentRepo.GetByAPIKey(ctx, apiKey)
	if err != nil {
		return 0, fmt.Errorf("invalid API key")
	}

	// Update last used timestamp (use specialized method to avoid sync_status errors)
	now := time.Now()
	if err := s.agentRepo.UpdateAPIKeyLastUsedByID(ctx, agent.ID, now); err != nil {
		debug.Error("failed to update API key last used: %v", err)
		// Continue anyway as this is not critical
	}

	return agent.ID, nil
}

// GetByAPIKey retrieves an agent by their API key
func (s *AgentService) GetByAPIKey(ctx context.Context, apiKey string) (*models.Agent, error) {
	agent, err := s.agentRepo.GetByAPIKey(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent by API key: %w", err)
	}

	// Update last used timestamp (use specialized method to avoid sync_status errors)
	now := time.Now()
	if err := s.agentRepo.UpdateAPIKeyLastUsedByID(ctx, agent.ID, now); err != nil {
		debug.Error("failed to update API key last used: %v", err)
		// Continue anyway as this is not critical
	}

	// Update the agent object for the caller
	agent.APIKeyLastUsed = sql.NullTime{
		Time:  now,
		Valid: true,
	}

	return agent, nil
}

// UpdateHeartbeat updates the last heartbeat timestamp for an agent
func (s *AgentService) UpdateHeartbeat(ctx context.Context, agentID int) error {
	debug.Info("Updating heartbeat for agent: %d", agentID)

	// First check if the agent has a zero timestamp and fix it
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	// Check for zero timestamp (0001-01-01)
	if agent.LastHeartbeat.Year() < 2000 {
		debug.Info("Agent %d has zero/invalid heartbeat timestamp, fixing it", agentID)
		// Use the repository's UpdateHeartbeat which sets CURRENT_TIMESTAMP
	}

	if err := s.agentRepo.UpdateHeartbeat(ctx, agentID); err != nil {
		return fmt.Errorf("failed to update agent heartbeat: %w", err)
	}

	debug.Info("Successfully updated heartbeat for agent: %d", agentID)
	return nil
}

// GetFiles retrieves files of specified types and category from the database
func (s *AgentService) GetFiles(ctx context.Context, fileTypes []string, category string) ([]repository.FileInfo, error) {
	debug.Info("Getting files of types %v, category %s", fileTypes, category)
	return s.fileRepo.GetFiles(ctx, fileTypes, category)
}

// GetDB returns the database connection from the repository
func (s *AgentService) GetDB() *sql.DB {
	return s.agentRepo.GetDB()
}

// UpdateAgentDevices updates the devices for an agent (DEPRECATED - use UpdateAgentPhysicalDevices)
func (s *AgentService) UpdateAgentDevices(agentID int, devices []models.Device) error {
	debug.Info("Updating devices for agent %d with %d devices", agentID, len(devices))
	return s.deviceRepo.UpsertDevices(agentID, devices)
}

// UpdateAgentPhysicalDevices updates the physical devices for an agent
func (s *AgentService) UpdateAgentPhysicalDevices(agentID int, devices []models.PhysicalDevice) error {
	debug.Info("Updating physical devices for agent %d with %d devices", agentID, len(devices))
	return s.deviceRepo.UpsertPhysicalDevices(agentID, devices)
}

// GetAgentDevices retrieves all devices for an agent
func (s *AgentService) GetAgentDevices(agentID int) ([]models.AgentDevice, error) {
	return s.deviceRepo.GetByAgentID(agentID)
}

// UpdateDeviceStatus updates the enabled status of a device
func (s *AgentService) UpdateDeviceStatus(agentID int, deviceID int, enabled bool) error {
	debug.Info("Updating device %d for agent %d to enabled=%v", deviceID, agentID, enabled)
	return s.deviceRepo.UpdateDeviceStatus(agentID, deviceID, enabled)
}

// UpdateDeviceRuntime updates the selected runtime for a device
func (s *AgentService) UpdateDeviceRuntime(agentID int, deviceID int, runtime string) error {
	debug.Info("Updating device %d for agent %d to runtime=%s", deviceID, agentID, runtime)
	return s.deviceRepo.UpdateDeviceRuntime(agentID, deviceID, runtime)
}

// UpdateDeviceDetectionStatus updates the device detection status for an agent
func (s *AgentService) UpdateDeviceDetectionStatus(agentID int, status string, errorMsg *string) error {
	debug.Info("Updating device detection status for agent %d to %s", agentID, status)
	return s.deviceRepo.UpdateAgentDeviceDetectionStatus(agentID, status, errorMsg)
}

// GetEnabledDevices retrieves only enabled devices for an agent
func (s *AgentService) GetEnabledDevices(agentID int) ([]models.AgentDevice, error) {
	return s.deviceRepo.GetEnabledDevicesByAgentID(agentID)
}

// GetAgentDeviceMetrics retrieves device metrics for an agent
func (s *AgentService) GetAgentDeviceMetrics(ctx context.Context, agentID int, timeRange string, metricsParam string) (map[string]interface{}, error) {
	// Parse time range
	var duration time.Duration
	switch timeRange {
	case "10m":
		duration = 10 * time.Minute
	case "20m":
		duration = 20 * time.Minute
	case "1h":
		duration = 1 * time.Hour
	case "5h":
		duration = 5 * time.Hour
	case "24h":
		duration = 24 * time.Hour
	default:
		duration = 10 * time.Minute
	}
	
	endTime := time.Now()
	startTime := endTime.Add(-duration)
	
	// Parse metrics types
	metricTypeMap := map[string]models.MetricType{
		"temperature": models.MetricTypeTemperature,
		"utilization": models.MetricTypeUtilization,
		"fanspeed":    models.MetricTypePowerUsage, // Using power_usage as placeholder for fan speed
		"hashrate":    models.MetricTypeHashRate,
	}
	
	var requestedMetrics []models.MetricType
	for _, metric := range strings.Split(metricsParam, ",") {
		metric = strings.TrimSpace(metric)
		if metricType, ok := metricTypeMap[metric]; ok {
			requestedMetrics = append(requestedMetrics, metricType)
		}
	}
	
	// Get benchmark repository for metrics
	// Create a db.DB wrapper from the agent repository's database
	dbWrapper := &db.DB{DB: s.agentRepo.GetDB()}
	benchmarkRepo := repository.NewBenchmarkRepository(dbWrapper)
	
	// Fetch metrics from database
	metrics, err := benchmarkRepo.GetAgentDeviceMetrics(ctx, agentID, requestedMetrics, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get device metrics: %w", err)
	}
	
	// Transform metrics for frontend consumption
	// Group by device and metric type
	deviceMetrics := make(map[int]map[string][]map[string]interface{})
	deviceNames := make(map[int]string)
	
	for _, metric := range metrics {
		if metric.DeviceID == nil {
			continue
		}
		
		deviceID := *metric.DeviceID
		if metric.DeviceName != nil {
			deviceNames[deviceID] = *metric.DeviceName
		}
		
		if _, ok := deviceMetrics[deviceID]; !ok {
			deviceMetrics[deviceID] = make(map[string][]map[string]interface{})
		}
		
		// Map metric type to frontend name
		var metricName string
		switch metric.MetricType {
		case models.MetricTypeTemperature:
			metricName = "temperature"
		case models.MetricTypeUtilization:
			metricName = "utilization"
		case models.MetricTypePowerUsage:
			metricName = "fanspeed"
		case models.MetricTypeHashRate:
			metricName = "hashrate"
		}
		
		dataPoint := map[string]interface{}{
			"timestamp": metric.Timestamp.Unix() * 1000, // Convert to milliseconds for JavaScript
			"value":     metric.Value,
		}
		
		deviceMetrics[deviceID][metricName] = append(deviceMetrics[deviceID][metricName], dataPoint)
	}
	
	// Build response structure
	response := map[string]interface{}{
		"timeRange": timeRange,
		"startTime": startTime.Unix() * 1000,
		"endTime":   endTime.Unix() * 1000,
		"devices":   []map[string]interface{}{},
	}
	
	// Convert to array format for frontend
	for deviceID, metrics := range deviceMetrics {
		deviceData := map[string]interface{}{
			"deviceId":   deviceID,
			"deviceName": deviceNames[deviceID],
			"metrics":    metrics,
		}
		response["devices"] = append(response["devices"].([]map[string]interface{}), deviceData)
	}
	
	return response, nil
}

// HasEnabledDevices checks if an agent has at least one enabled device
func (s *AgentService) HasEnabledDevices(agentID int) (bool, error) {
	return s.deviceRepo.HasEnabledDevices(agentID)
}

// UpdateAgent updates agent settings including owner and extra parameters
func (s *AgentService) UpdateAgent(ctx context.Context, agentID int, isEnabled bool, ownerID *string, extraParameters string, binaryVersionID *int64, binaryOverride bool) error {
	// First check if agent exists
	_, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return err
	}

	// If binary_version_id is provided, validate it exists and is hashcat type
	if binaryVersionID != nil && *binaryVersionID > 0 {
		// TODO: Add binary version validation here
		// For now, we'll trust the handler to validate
	}

	// Update agent in database
	return s.agentRepo.UpdateAgentSettings(ctx, agentID, isEnabled, ownerID, extraParameters, binaryVersionID, binaryOverride)
}

// UpdateAgentOSInfo updates an agent's OS information
func (s *AgentService) UpdateAgentOSInfo(ctx context.Context, agentID int, osInfo map[string]interface{}) error {
	return s.agentRepo.UpdateOSInfo(ctx, agentID, osInfo)
}

// GetUserAgentsWithTasks retrieves agents owned by a user along with their current task information
func (s *AgentService) GetUserAgentsWithTasks(ctx context.Context, userID string) ([]models.AgentWithTask, error) {
	debug.Info("Getting agents with tasks for user: %s", userID)

	// Parse user ID
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	// Get agents owned by the user
	agents, err := s.agentRepo.GetByOwnerID(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user agents: %w", err)
	}

	// Create result slice
	result := make([]models.AgentWithTask, 0, len(agents))

	// For each agent, get their current active task
	for _, agent := range agents {
		agentWithTask := models.AgentWithTask{
			Agent: agent,
		}

		// Get active tasks for this agent
		activeTasks, err := s.jobTaskRepo.GetActiveTasksByAgent(ctx, agent.ID)
		if err != nil {
			debug.Warning("Failed to get active tasks for agent %d: %v", agent.ID, err)
			// Continue with nil task
		} else if len(activeTasks) > 0 {
			// Take the first active task (should typically be only one)
			task := activeTasks[0]
			agentWithTask.CurrentTask = &task

			// Get job execution details for the task
			jobExecution, err := s.jobExecutionRepo.GetByID(ctx, task.JobExecutionID)
			if err != nil {
				debug.Warning("Failed to get job execution for task %s: %v", task.ID, err)
			} else {
				agentWithTask.JobExecution = jobExecution
			}
		}

		result = append(result, agentWithTask)
	}

	return result, nil
}

// ClearAgentBusyStatus manually clears the busy_status metadata for an agent
// This validates that the agent isn't actually running a task before clearing
func (s *AgentService) ClearAgentBusyStatus(ctx context.Context, agentID int) error {
	// Get the agent
	agent, err := s.agentRepo.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	// Check if agent has metadata
	if agent.Metadata == nil {
		return fmt.Errorf("agent has no metadata")
	}

	// Check if agent is marked as busy
	if agent.Metadata["busy_status"] != "true" {
		return fmt.Errorf("agent is not marked as busy")
	}

	// Validate that the agent doesn't have an active task
	activeTasks, err := s.jobTaskRepo.GetActiveTasksByAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to check active tasks: %w", err)
	}

	if len(activeTasks) > 0 {
		return fmt.Errorf("agent has %d active tasks, cannot clear busy status", len(activeTasks))
	}

	// Clear the busy status
	agent.Metadata["busy_status"] = "false"
	delete(agent.Metadata, "current_task_id")
	delete(agent.Metadata, "current_job_id")

	// Update metadata using specialized method to avoid sync_status errors
	if err := s.agentRepo.UpdateMetadata(ctx, agentID, agent.Metadata); err != nil {
		return fmt.Errorf("failed to update agent metadata: %w", err)
	}

	debug.Info("Manually cleared busy status for agent %d", agentID)
	return nil
}
