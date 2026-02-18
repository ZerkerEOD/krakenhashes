package routes

import (
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/analytics"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupAnalyticsRoutes configures routes for password analytics
func SetupAnalyticsRoutes(router *mux.Router, database *db.DB, queueService *services.AnalyticsQueueService, teamService *services.TeamService, clientTeamRepo *repository.ClientTeamRepository) {
	debug.Info("Setting up analytics routes...")

	// Create handler
	handler := analytics.NewHandler(database, queueService, teamService, clientTeamRepo)

	// Analytics routes (all require authentication via JWT middleware)
	router.HandleFunc("/analytics/clients", handler.GetClients).Methods("GET", "OPTIONS")
	router.HandleFunc("/analytics/reports", handler.CreateReport).Methods("POST", "OPTIONS")
	router.HandleFunc("/analytics/reports/{id}", handler.GetReport).Methods("GET", "OPTIONS")
	router.HandleFunc("/analytics/reports/client/{clientId}", handler.GetClientReports).Methods("GET", "OPTIONS")
	router.HandleFunc("/analytics/reports/{id}", handler.DeleteReport).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/analytics/reports/{id}/retry", handler.RetryReport).Methods("POST", "OPTIONS")
	router.HandleFunc("/analytics/queue-status", handler.GetQueueStatus).Methods("GET", "OPTIONS")

	debug.Info("Analytics routes configured successfully")
}
