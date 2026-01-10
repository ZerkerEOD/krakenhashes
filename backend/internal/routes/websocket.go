package routes

import (
	cryptotls "crypto/tls"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth/api"
	wshandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	wsservice "github.com/ZerkerEOD/krakenhashes/backend/internal/services/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupWebSocketRoutes configures WebSocket-related routes
func SetupWebSocketRoutes(r *mux.Router, database *db.DB, agentService *services.AgentService, tlsProvider tls.Provider) {
	debug.Debug("Setting up API key protected routes")
	wsService := wsservice.NewService(agentService)

	// Get TLS configuration for WebSocket handler
	tlsConfig, err := tlsProvider.GetTLSConfig()
	if err != nil {
		debug.Error("Failed to get TLS configuration: %v", err)
		return
	}

	// Create system settings repository
	systemSettingsRepo := repository.NewSystemSettingsRepository(database)
	jobTaskRepo := repository.NewJobTaskRepository(database)
	jobExecutionRepo := repository.NewJobExecutionRepository(database)

	wsRouter := r.PathPrefix("/ws").Subrouter()
	wsRouter.Use(api.APIKeyMiddleware(agentService))
	wsRouter.Use(loggingMiddleware)

	// Create a new TLS config for agent connections instead of copying
	agentTLSConfig := &cryptotls.Config{
		Certificates:             tlsConfig.Certificates,
		RootCAs:                  tlsConfig.RootCAs,
		MinVersion:               tlsConfig.MinVersion,
		MaxVersion:               tlsConfig.MaxVersion,
		CipherSuites:             tlsConfig.CipherSuites,
		PreferServerCipherSuites: true,
	}
	// NOTE: This route setup appears to be legacy/unused. SetupWebSocketWithJobRoutes is used instead.
	// Passing nil for JobExecutionService and potfileHistory since this handler doesn't have access to them.
	wsHandler := wshandler.NewHandler(wsService, agentService, nil, systemSettingsRepo, jobTaskRepo, jobExecutionRepo, agentTLSConfig, nil)
	wsRouter.HandleFunc("/agent", wsHandler.ServeWS)
	debug.Info("Configured WebSocket endpoint: /ws/agent with TLS: %v", tlsConfig != nil)

	if tlsConfig != nil {
		debug.Debug("WebSocket TLS Configuration:")
		debug.Debug("- Min Version: %v", agentTLSConfig.MinVersion)
		debug.Debug("- Certificates: %d", len(agentTLSConfig.Certificates))
	}
}
