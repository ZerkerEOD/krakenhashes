package routes

import (
	"context"
	cryptotls "crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth/api"
	wshandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/integration"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// JobIntegrationManager is a global reference to the job integration manager
// This is a temporary solution until we refactor main to properly manage lifecycle
var JobIntegrationManager *integration.JobIntegrationManager

// WSHandler is a global reference to the WebSocket handler for access by other handlers
// This is a temporary solution until we refactor to use proper dependency injection
var WSHandler *wshandler.Handler

// wsHandlerAdapter adapts the WebSocket handler to the WSHandler interface
type wsHandlerAdapter struct {
	handler *wshandler.Handler
}

// SendMessage implements the WSHandler interface
func (a *wsHandlerAdapter) SendMessage(agentID int, msg interface{}) error {
	// Convert the interface{} to *wsservice.Message
	wsMsg, ok := msg.(*wsservice.Message)
	if !ok {
		// Try to convert a map to Message
		if msgMap, ok := msg.(map[string]interface{}); ok {
			msgType, _ := msgMap["type"].(string)
			payload, _ := msgMap["payload"].(json.RawMessage)
			wsMsg = &wsservice.Message{
				Type:    wsservice.MessageType(msgType),
				Payload: payload,
			}
		} else {
			return fmt.Errorf("invalid message type: expected *wsservice.Message or map[string]interface{}")
		}
	}
	return a.handler.SendMessage(agentID, wsMsg)
}

// SetupWebSocketWithJobRoutes configures WebSocket routes with job execution integration
func SetupWebSocketWithJobRoutes(
	r *mux.Router,
	agentService *services.AgentService,
	tlsProvider tls.Provider,
	sqlDB *sql.DB,
	appConfig *config.Config,
	wordlistManager wordlist.Manager,
	ruleManager rule.Manager,
	binaryManager binary.Manager,
	potfileService *services.PotfileService,
) {
	debug.Debug("Setting up WebSocket routes with job integration")

	// Create database wrapper
	database := &db.DB{DB: sqlDB}

	// Create repositories
	benchmarkRepo := repository.NewBenchmarkRepository(database)
	presetJobRepo := repository.NewPresetJobRepository(sqlDB)
	hashlistRepo := repository.NewHashListRepository(database)
	hashRepo := repository.NewHashRepository(database)
	lmHashRepo := repository.NewLMHashRepository(database)
	jobTaskRepo := repository.NewJobTaskRepository(database)
	agentRepo := repository.NewAgentRepository(database)
	jobExecutionRepo := repository.NewJobExecutionRepository(database)
	systemSettingsRepo := repository.NewSystemSettingsRepository(database)
	agentHashlistRepo := repository.NewAgentHashlistRepository(database)
	fileRepo := repository.NewFileRepository(database, appConfig.DataDir)
	deviceRepo := repository.NewAgentDeviceRepository(database)
	scheduleRepo := repository.NewAgentScheduleRepository(database)
	clientRepo := repository.NewClientRepository(database)
	jobIncrementLayerRepo := repository.NewJobIncrementLayerRepository(database)
	presetIncrementLayerRepo := repository.NewPresetIncrementLayerRepository(database)
	assocWordlistRepo := repository.NewAssociationWordlistRepository(database)
	hashTypeRepo := repository.NewHashTypeRepository(database)

	// Create services
	jobExecutionService := services.NewJobExecutionService(
		database,
		jobExecutionRepo,
		jobTaskRepo,
		jobIncrementLayerRepo,
		presetIncrementLayerRepo,
		benchmarkRepo,
		agentHashlistRepo,
		agentRepo,
		deviceRepo,
		presetJobRepo,
		hashlistRepo,
		hashTypeRepo,
		systemSettingsRepo,
		fileRepo,
		scheduleRepo,
		binaryManager,
		assocWordlistRepo,
		"/usr/bin/hashcat", // hashcat binary path (deprecated, using binary manager now)
		appConfig.DataDir,
	)

	jobChunkingService := services.NewJobChunkingService(
		benchmarkRepo,
		jobTaskRepo,
		systemSettingsRepo,
	)

	hashlistSyncService := services.NewHashlistSyncService(
		agentHashlistRepo,
		hashlistRepo,
		systemSettingsRepo,
		jobExecutionRepo,
		appConfig.DataDir,
		sqlDB,
	)

	jobSchedulingService := services.NewJobSchedulingService(
		jobExecutionService,
		jobChunkingService,
		hashlistSyncService,
		agentRepo,
		jobTaskRepo,
		systemSettingsRepo,
	)

	// Create WebSocket service
	wsService := wsservice.NewService(agentService)

	// Get TLS configuration for WebSocket handler
	tlsConfig, err := tlsProvider.GetTLSConfig()
	if err != nil {
		debug.Error("Failed to get TLS configuration: %v", err)
		return
	}

	// Create a new TLS config for agent connections
	agentTLSConfig := &cryptotls.Config{
		Certificates:             tlsConfig.Certificates,
		RootCAs:                  tlsConfig.RootCAs,
		MinVersion:               tlsConfig.MinVersion,
		MaxVersion:               tlsConfig.MaxVersion,
		CipherSuites:             tlsConfig.CipherSuites,
		PreferServerCipherSuites: true,
	}

	// Create WebSocket handler
	// Pass potfileHistory for handling potfile sync race conditions during heavy ingestion
	wsHandler := wshandler.NewHandler(wsService, agentService, jobExecutionService, systemSettingsRepo, jobTaskRepo, jobExecutionRepo, agentTLSConfig, potfileService.GetPotfileHistory())

	// Store WebSocket handler globally for access by other handlers
	WSHandler = wsHandler

	// Set the WebSocket handler in the UserJobsHandler if it was already created
	if UserJobsHandlerInstance != nil {
		debug.Info("Setting WebSocket handler in UserJobsHandler")
		// Create an adapter that implements the WSHandler interface
		adapter := &wsHandlerAdapter{handler: wsHandler}
		UserJobsHandlerInstance.SetWSHandler(adapter)
	}

	// Create notification service for job completion
	notificationService := services.NewNotificationService(sqlDB)

	// Create hashlist completion service for handling fully cracked hashlists
	hashlistCompletionService := services.NewHashlistCompletionService(
		database,
		jobExecutionRepo,
		jobTaskRepo,
		jobIncrementLayerRepo,
		hashlistRepo,
		notificationService,
		&wsHandlerAdapter{handler: wsHandler},
	)

	// Create job integration manager
	jobIntegration := integration.NewJobIntegrationManager(
		wsHandler,
		jobSchedulingService,
		jobExecutionService,
		hashlistSyncService,
		benchmarkRepo,
		presetJobRepo,
		hashlistRepo,
		hashRepo,
		lmHashRepo,
		jobTaskRepo,
		jobIncrementLayerRepo,
		agentRepo,
		deviceRepo,
		clientRepo,
		systemSettingsRepo,
		assocWordlistRepo,
		potfileService,
		hashlistCompletionService,
		sqlDB,
		wordlistManager,
		ruleManager,
		binaryManager,
	)

	// Set the job handler in the WebSocket service
	wsService.SetJobHandler(jobIntegration)

	// Setup WebSocket routes
	wsRouter := r.PathPrefix("/ws").Subrouter()
	wsRouter.Use(api.APIKeyMiddleware(agentService))
	wsRouter.Use(loggingMiddleware)

	wsRouter.HandleFunc("/agent", wsHandler.ServeWS)
	debug.Info("Configured WebSocket endpoint: /ws/agent with job integration and TLS: %v", tlsConfig != nil)

	// Return the job integration manager so it can be started from main
	// Store it globally for now until we refactor the main function
	JobIntegrationManager = jobIntegration

	// Initialize and start metrics cleanup service
	metricsCleanupService := services.NewMetricsCleanupService(benchmarkRepo, systemSettingsRepo)
	go metricsCleanupService.StartCleanupScheduler(context.Background())
	debug.Info("Metrics cleanup service started")

	if tlsConfig != nil {
		debug.Debug("WebSocket TLS Configuration:")
		debug.Debug("- Min Version: %v", agentTLSConfig.MinVersion)
		debug.Debug("- Certificates: %d", len(agentTLSConfig.Certificates))
	}
}
