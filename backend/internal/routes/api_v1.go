package routes

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
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
func SetupV1Routes(r *mux.Router, database *db.DB, dataDir string, binaryManager binary.Manager) {
	debug.Info("Setting up /api/v1 User API routes")

	// Use provided data directory
	dataDirectory := filepath.Clean(dataDir)

	// Create repositories
	userRepo := repository.NewUserRepository(database)
	clientRepo := repository.NewClientRepository(database)
	clientSettingsRepo := repository.NewClientSettingsRepository(database)
	hashlistRepo := repository.NewHashListRepository(database)
	hashRepo := repository.NewHashRepository(database)
	hashTypeRepo := repository.NewHashTypeRepository(database)
	agentRepo := repository.NewAgentRepository(database)
	voucherRepo := repository.NewClaimVoucherRepository(database)
	workflowRepo := repository.NewJobWorkflowRepository(database.DB)
	presetJobRepo := repository.NewPresetJobRepository(database.DB)
	systemSettingsRepo := repository.NewSystemSettingsRepository(database)

	// Create services
	userAPIService := services.NewUserAPIService(userRepo)
	voucherService := services.NewClaimVoucherService(voucherRepo)

	// Create config for hashlist processor
	cfg := &config.Config{}

	// Create hashlist processor (nil progress service for API v1 - progress tracking is for web UI)
	hashlistProcessor := processor.NewHashlistDBProcessor(hashlistRepo, hashTypeRepo, hashRepo, systemSettingsRepo, cfg, nil)

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
	clientHandler := v1handlers.NewClientHandler(clientRepo, hashlistRepo, clientSettingsRepo, database)
	v1Router.HandleFunc("/clients", clientHandler.CreateClient).Methods("POST", "OPTIONS")
	v1Router.HandleFunc("/clients", clientHandler.ListClients).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/clients/{id}", clientHandler.GetClient).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/clients/{id}", clientHandler.UpdateClient).Methods("PATCH", "OPTIONS")
	v1Router.HandleFunc("/clients/{id}", clientHandler.DeleteClient).Methods("DELETE", "OPTIONS")

	// Hashlist endpoints
	hashlistHandler := v1handlers.NewHashlistHandler(hashlistRepo, clientRepo, hashTypeRepo, systemSettingsRepo, hashlistProcessor, hashlistDataDir)
	v1Router.HandleFunc("/hashlists", hashlistHandler.CreateHashlist).Methods("POST", "OPTIONS")
	v1Router.HandleFunc("/hashlists", hashlistHandler.ListHashlists).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/hashlists/{id:[0-9]+}", hashlistHandler.GetHashlist).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/hashlists/{id:[0-9]+}", hashlistHandler.DeleteHashlist).Methods("DELETE", "OPTIONS")

	// Agent endpoints
	agentHandler := v1handlers.NewAgentHandler(agentRepo, voucherService)
	v1Router.HandleFunc("/agents/vouchers", agentHandler.GenerateVoucher).Methods("POST", "OPTIONS")
	v1Router.HandleFunc("/agents", agentHandler.ListAgents).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/agents/{id:[0-9]+}", agentHandler.GetAgent).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/agents/{id:[0-9]+}", agentHandler.UpdateAgent).Methods("PATCH", "OPTIONS")
	v1Router.HandleFunc("/agents/{id:[0-9]+}", agentHandler.DeleteAgent).Methods("DELETE", "OPTIONS")

	// Helper/Metadata endpoints
	helperHandler := v1handlers.NewHelperHandler(hashTypeRepo, workflowRepo, presetJobRepo)
	v1Router.HandleFunc("/hash-types", helperHandler.ListHashTypes).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/workflows", helperHandler.ListWorkflows).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/preset-jobs", helperHandler.ListPresetJobs).Methods("GET", "OPTIONS")

	// Job endpoints - create necessary repositories and services
	jobExecRepo := repository.NewJobExecutionRepository(database)
	jobTaskRepo := repository.NewJobTaskRepository(database)
	jobIncrementLayerRepo := repository.NewJobIncrementLayerRepository(database)
	presetIncrementLayerRepo := repository.NewPresetIncrementLayerRepository(database)
	benchmarkRepo := repository.NewBenchmarkRepository(database)
	agentHashlistRepo := repository.NewAgentHashlistRepository(database)
	deviceRepo := repository.NewAgentDeviceRepository(database)
	scheduleRepo := repository.NewAgentScheduleRepository(database)
	fileRepo := repository.NewFileRepository(database, dataDirectory)

	// Create job execution service
	jobExecutionService := services.NewJobExecutionService(
		database,
		jobExecRepo,
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
		nil, // assocWordlistRepo - not needed, API v1 only supports preset jobs (not association attacks)
		"",  // hashcatBinaryPath - not needed for keyspace calculation
		dataDirectory,
	)

	// Create job handler (schedulingService is nil as it's only needed for Delete/Stop operations)
	jobHandler := v1handlers.NewJobHandler(
		jobExecutionService,
		jobExecRepo,
		jobTaskRepo,
		hashlistRepo,
		clientRepo,
		presetJobRepo,
		workflowRepo,
		nil, // schedulingService not needed for core CRUD operations
		jobIncrementLayerRepo,
		systemSettingsRepo,
	)

	v1Router.HandleFunc("/jobs", jobHandler.CreateJob).Methods("POST", "OPTIONS")
	v1Router.HandleFunc("/jobs", jobHandler.ListJobs).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/jobs/{id}", jobHandler.GetJob).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/jobs/{id}", jobHandler.UpdateJob).Methods("PATCH", "OPTIONS")
	v1Router.HandleFunc("/jobs/{id}/layers", jobHandler.GetJobLayers).Methods("GET", "OPTIONS")
	v1Router.HandleFunc("/jobs/{id}/layers/{layer_id}", jobHandler.GetJobLayerTasks).Methods("GET", "OPTIONS")

	debug.Info("/api/v1 User API routes configured successfully")
	debug.Info("User API authentication requires X-User-Email and X-API-Key headers")
}
