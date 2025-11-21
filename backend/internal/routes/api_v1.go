package routes

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	v1handlers "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/api/v1"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/processor"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupV1Routes configures all /api/v1 routes for the User API
// Note: Job endpoints are currently disabled until proper service dependencies can be passed in
func SetupV1Routes(r *mux.Router, database *db.DB) {
	debug.Info("Setting up /api/v1 User API routes")

	// Get data directory from environment
	dataDirectory := os.Getenv("KH_DATA_DIR")
	if dataDirectory == "" {
		dataDirectory = "/data/krakenhashes"
	}
	dataDirectory = filepath.Clean(dataDirectory)

	// Create repositories
	userRepo := repository.NewUserRepository(database)
	clientRepo := repository.NewClientRepository(database)
	hashlistRepo := repository.NewHashListRepository(database)
	hashRepo := repository.NewHashRepository(database)
	hashTypeRepo := repository.NewHashTypeRepository(database)

	// Create services
	userAPIService := services.NewUserAPIService(userRepo)

	// Create config for hashlist processor
	cfg := &config.Config{
		HashlistBatchSize: 1000, // Default batch size
	}

	// Create hashlist processor
	hashlistProcessor := processor.NewHashlistDBProcessor(hashlistRepo, hashTypeRepo, hashRepo, cfg)

	// Define hashlists storage directory
	hashlistDataDir := filepath.Join(dataDirectory, "hashlists")
	if err := os.MkdirAll(hashlistDataDir, 0755); err != nil {
		debug.Error("Failed to create hashlist directory %s: %v", hashlistDataDir, err)
	}

	// Create v1 router with User API Key authentication middleware
	v1Router := r.PathPrefix("/api/v1").Subrouter()
	v1Router.Use(middleware.UserAPIKeyMiddleware(userAPIService))
	v1Router.Use(loggingMiddleware)

	// Placeholder endpoint for testing
	v1Router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","message":"User API v1 is operational"}`))
	}).Methods("GET", "OPTIONS")

	// Client endpoints
	clientHandler := v1handlers.NewClientHandler(clientRepo, hashlistRepo, database)
	v1Router.HandleFunc("/clients", clientHandler.CreateClient).Methods("POST", "OPTIONS")
	v1Router.HandleFunc("/clients", clientHandler.ListClients).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/clients/{id}", clientHandler.GetClient).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/clients/{id}", clientHandler.UpdateClient).Methods("PATCH", "OPTIONS")
	v1Router.HandleFunc("/clients/{id}", clientHandler.DeleteClient).Methods("DELETE", "OPTIONS")

	// Hashlist endpoints
	hashlistHandler := v1handlers.NewHashlistHandler(hashlistRepo, clientRepo, hashTypeRepo, hashlistProcessor, hashlistDataDir)
	v1Router.HandleFunc("/hashlists", hashlistHandler.CreateHashlist).Methods("POST", "OPTIONS")
	v1Router.HandleFunc("/hashlists", hashlistHandler.ListHashlists).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/hashlists/{id:[0-9]+}", hashlistHandler.GetHashlist).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/hashlists/{id:[0-9]+}", hashlistHandler.DeleteHashlist).Methods("DELETE", "OPTIONS")

	// TODO: Add job endpoints here once we can properly initialize job services
	// This requires:
	// - JobExecutionService
	// - JobSchedulingService
	// - PresetJobRepository
	// - WorkflowRepository
	// These services have complex dependencies that should be passed in from main.go

	debug.Info("/api/v1 User API routes configured successfully")
	debug.Info("User API authentication requires X-User-Email and X-API-Key headers")
}
