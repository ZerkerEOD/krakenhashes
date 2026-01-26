package routes

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth/api"
	wordlisthandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// SetupWordlistRoutes configures all wordlist management related routes
func SetupWordlistRoutes(r *mux.Router, sqlDB *sql.DB, cfg *config.Config, agentService *services.AgentService, presetJobService services.AdminPresetJobService, potfileService *services.PotfileService) {
	debug.Info("Setting up wordlist management routes")

	// Create DB wrapper
	database := &db.DB{DB: sqlDB}

	// Initialize repositories
	jobExecRepo := repository.NewJobExecutionRepository(database)
	presetJobRepo := repository.NewPresetJobRepository(sqlDB)
	workflowRepo := repository.NewJobWorkflowRepository(sqlDB)

	// Initialize wordlist store and manager
	store := wordlist.NewStore(sqlDB)
	manager := wordlist.NewManager(
		store,
		filepath.Join(cfg.DataDir, "wordlists"),
		0, // No file size limit
		[]string{"txt", "dict", "lst", "gz", "zip"},                   // Allowed formats
		[]string{"text/plain", "application/gzip", "application/zip"}, // Allowed MIME types
		jobExecRepo,
		presetJobRepo,
		workflowRepo,
	)

	// Create handler
	handler := wordlisthandler.NewHandler(manager, potfileService)

	// User routes (accessible to all authenticated users)
	userRouter := r.PathPrefix("/wordlists").Subrouter()
	userRouter.Use(middleware.RequireAuth(database))

	// Read operations
	userRouter.HandleFunc("", handler.HandleListWordlists).Methods(http.MethodGet)
	userRouter.HandleFunc("/{id:[0-9]+}", handler.HandleGetWordlist).Methods(http.MethodGet)
	userRouter.HandleFunc("/{id:[0-9]+}/download", handler.HandleDownloadWordlist).Methods(http.MethodGet)
	userRouter.HandleFunc("/{id:[0-9]+}/deletion-impact", handler.HandleGetDeletionImpact).Methods(http.MethodGet)

	// Add upload endpoint with special handling
	uploadHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debug.Info("Handling wordlist upload request: %s %s", r.Method, r.URL.Path)

		// Skip CORS handling as it's now handled by GlobalCORSMiddleware
		if r.Method == "OPTIONS" {
			return // OPTIONS requests are handled by GlobalCORSMiddleware
		}

		// Debug log cookies for troubleshooting
		debug.Info("Upload request received with cookies: %v", r.Cookies())

		// Check for authentication token
		cookie, err := r.Cookie("token")
		if err != nil {
			debug.Error("No auth token found in cookies for upload request")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		debug.Info("Found auth token cookie for upload: %s", cookie.Name)

		// Extract user ID from the token
		tokenString := cookie.Value
		userIDStr, err := jwt.ValidateJWT(tokenString)
		if err != nil {
			debug.Error("Invalid token: %v", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Convert user ID string to UUID
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			debug.Error("Failed to parse user ID as UUID: %v", err)
			http.Error(w, "Invalid user ID", http.StatusInternalServerError)
			return
		}

		// Get user role
		role, err := jwt.GetUserRole(tokenString)
		if err != nil {
			debug.Error("Failed to get user role: %v", err)
			role = "user" // Default to regular user role
		}

		// Set user context for the handler
		debug.Info("Setting user context for upload: user_id=%s, role=%s", userID.String(), role)

		// Create a new context with user information
		ctx := context.WithValue(r.Context(), "user_id", userID.String())
		ctx = context.WithValue(ctx, "role", role)

		// Call the handler with the updated context
		handler.HandleAddWordlist(w, r.WithContext(ctx))
		
		// After successful upload, trigger keyspace recalculation for affected preset jobs
		// Get the wordlist ID from the response (this is a bit hacky but works for now)
		// In a production system, we'd modify the handler to return the wordlist ID
		go func() {
			// Use the last uploaded wordlist ID - this would need proper implementation
			// For now, we'll log that recalculation should happen
			debug.Info("Wordlist uploaded - keyspace recalculation should be triggered for affected preset jobs")
			// TODO: Implement proper wordlist ID extraction and recalculation
			// presetJobService.RecalculateKeyspacesForWordlist(context.Background(), wordlistID)
		}()
	})

	// Register upload endpoint with the custom handler - use Handle instead of HandleFunc
	userRouter.Handle("/upload", uploadHandler).Methods(http.MethodPost, http.MethodOptions)

	// Add simplified handler for DELETE operations
	deleteHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debug.Info("Handling wordlist delete request: %s %s", r.Method, r.URL.Path)

		// Skip CORS handling as it's now handled by GlobalCORSMiddleware
		if r.Method == "OPTIONS" {
			return // OPTIONS requests are handled by GlobalCORSMiddleware
		}

		// Call the actual handler
		handler.HandleDeleteWordlist(w, r)
	})

	// Rest of the write operations
	userRouter.HandleFunc("/{id:[0-9]+}", handler.HandleUpdateWordlist).Methods(http.MethodPut)
	userRouter.Handle("/{id:[0-9]+}", deleteHandler).Methods(http.MethodDelete, http.MethodOptions)

	// Add simplified handler for verify operations
	verifyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debug.Info("Handling wordlist verify request: %s %s", r.Method, r.URL.Path)

		// Skip CORS handling as it's now handled by GlobalCORSMiddleware
		if r.Method == "OPTIONS" {
			return // OPTIONS requests are handled by GlobalCORSMiddleware
		}

		// Call the actual handler
		handler.HandleVerifyWordlist(w, r)
	})

	// Register verify endpoint with the custom handler
	userRouter.Handle("/{id:[0-9]+}/verify", verifyHandler).Methods(http.MethodPost, http.MethodOptions)

	// Add refresh endpoint for updating wordlist metadata
	userRouter.HandleFunc("/{id:[0-9]+}/refresh", handler.HandleRefreshWordlist).Methods(http.MethodPost)

	userRouter.HandleFunc("/{id:[0-9]+}/tags", handler.HandleAddWordlistTag).Methods(http.MethodPost)
	userRouter.HandleFunc("/{id:[0-9]+}/tags/{tag}", handler.HandleDeleteWordlistTag).Methods(http.MethodDelete)

	// Agent routes (accessible to agents with API key)
	agentRouter := r.PathPrefix("/agent/wordlists").Subrouter()
	agentRouter.Use(api.APIKeyMiddleware(agentService))

	agentRouter.HandleFunc("", handler.HandleListWordlists).Methods(http.MethodGet)
	agentRouter.HandleFunc("/{id:[0-9]+}/download", handler.HandleDownloadWordlist).Methods(http.MethodGet)

	debug.Info("Registered wordlist management routes")
}
