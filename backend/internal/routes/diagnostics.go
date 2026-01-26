package routes

import (
	"database/sql"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	admindiagnostics "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin/diagnostics"
	wshandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/diagnostic"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupDiagnosticsRoutes configures the admin diagnostics routes (GH Issue #23)
// This is called after WebSocket handler is initialized since it requires access to agent connections.
func SetupDiagnosticsRoutes(router *mux.Router, sqlDB *sql.DB, wsHandler *wshandler.Handler, logsDir string) {
	debug.Debug("Setting up diagnostics routes")

	if wsHandler == nil {
		debug.Error("WebSocket handler is nil, cannot setup diagnostics routes")
		return
	}

	// Create database wrapper for middleware
	database := &db.DB{DB: sqlDB}

	// Create the diagnostic service
	diagnosticService := diagnostic.NewDiagnosticService(sqlDB, wsHandler, logsDir)

	// Create the handler
	diagnosticHandler := admindiagnostics.NewDiagnosticHandler(diagnosticService, wsHandler)

	// Create admin diagnostics subrouter (requires admin authentication)
	// Note: We use /api/admin/diagnostics because SetupDiagnosticsRoutes is called with root router
	// but frontend expects /api/admin/... paths like other admin routes
	diagnosticsRouter := router.PathPrefix("/api/admin/diagnostics").Subrouter()
	diagnosticsRouter.Use(middleware.RequireAuth(database))
	diagnosticsRouter.Use(middleware.AdminOnly)

	// Download full diagnostic package
	diagnosticsRouter.HandleFunc("/download", diagnosticHandler.DownloadDiagnostics).Methods(http.MethodGet, http.MethodOptions)

	// System info
	diagnosticsRouter.HandleFunc("/system-info", diagnosticHandler.GetSystemInfo).Methods(http.MethodGet, http.MethodOptions)

	// Agent debug status endpoints
	diagnosticsRouter.HandleFunc("/agents", diagnosticHandler.GetAgentDebugStatuses).Methods(http.MethodGet, http.MethodOptions)
	diagnosticsRouter.HandleFunc("/agents/{id:[0-9]+}", diagnosticHandler.GetAgentDebugStatus).Methods(http.MethodGet, http.MethodOptions)

	// Toggle debug mode for specific agent
	diagnosticsRouter.HandleFunc("/agents/{id:[0-9]+}/debug", diagnosticHandler.ToggleAgentDebug).Methods(http.MethodPost, http.MethodOptions)

	// Request/purge logs for specific agent
	diagnosticsRouter.HandleFunc("/agents/{id:[0-9]+}/logs", diagnosticHandler.RequestAgentLogs).Methods(http.MethodGet, http.MethodOptions)
	diagnosticsRouter.HandleFunc("/agents/{id:[0-9]+}/logs", diagnosticHandler.PurgeAgentLogs).Methods(http.MethodDelete, http.MethodOptions)

	// Toggle debug mode for all agents
	diagnosticsRouter.HandleFunc("/agents/debug", diagnosticHandler.ToggleAllAgentsDebug).Methods(http.MethodPost, http.MethodOptions)

	// Server/backend debug status and toggle
	diagnosticsRouter.HandleFunc("/server/debug", diagnosticHandler.GetServerDebugStatus).Methods(http.MethodGet, http.MethodOptions)
	diagnosticsRouter.HandleFunc("/server/debug", diagnosticHandler.ToggleServerDebug).Methods(http.MethodPost, http.MethodOptions)

	// Server log stats and purge
	diagnosticsRouter.HandleFunc("/logs/stats", diagnosticHandler.GetLogStats).Methods(http.MethodGet, http.MethodOptions)
	diagnosticsRouter.HandleFunc("/logs/{directory}", diagnosticHandler.PurgeServerLogs).Methods(http.MethodDelete, http.MethodOptions)

	// PostgreSQL logs check
	diagnosticsRouter.HandleFunc("/postgres-logs-exist", diagnosticHandler.CheckPostgresLogsExist).Methods(http.MethodGet, http.MethodOptions)

	// Nginx logs check
	diagnosticsRouter.HandleFunc("/nginx-logs-exist", diagnosticHandler.CheckNginxLogsExist).Methods(http.MethodGet, http.MethodOptions)

	// Nginx hot-reload
	diagnosticsRouter.HandleFunc("/nginx/reload", diagnosticHandler.ReloadNginx).Methods(http.MethodPost, http.MethodOptions)

	debug.Info("Configured admin diagnostics routes: /api/admin/diagnostics/*")
}
