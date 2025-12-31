package routes

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth/api"
	rulehandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// SetupRuleRoutes configures all rule management related routes
func SetupRuleRoutes(r *mux.Router, sqlDB *sql.DB, cfg *config.Config, agentService *services.AgentService, presetJobService services.AdminPresetJobService) {
	debug.Info("Setting up rule management routes")

	// Create DB wrapper
	database := &db.DB{DB: sqlDB}

	// Initialize repositories
	jobExecRepo := repository.NewJobExecutionRepository(database)
	presetJobRepo := repository.NewPresetJobRepository(sqlDB)
	workflowRepo := repository.NewJobWorkflowRepository(sqlDB)

	// Initialize rule store and manager
	store := rule.NewStore(sqlDB)
	manager := rule.NewManager(
		store,
		filepath.Join(cfg.DataDir, "rules"),
		0,                       // No file size limit
		[]string{"rule", "txt"}, // Allowed formats
		[]string{"text/plain"},  // Allowed MIME types
		jobExecRepo,
		presetJobRepo,
		workflowRepo,
	)

	// Create handler
	handler := rulehandler.NewHandler(manager, cfg)

	// User routes (accessible to all authenticated users)
	userRouter := r.PathPrefix("/rules").Subrouter()
	userRouter.Use(middleware.RequireAuth(database))

	// Read operations
	userRouter.HandleFunc("", handler.HandleListRules).Methods(http.MethodGet)
	userRouter.HandleFunc("/{id:[0-9]+}", handler.HandleGetRule).Methods(http.MethodGet)
	userRouter.HandleFunc("/{id:[0-9]+}/download", handler.HandleDownloadRule).Methods(http.MethodGet)
	userRouter.HandleFunc("/{id:[0-9]+}/deletion-impact", handler.HandleGetDeletionImpact).Methods(http.MethodGet)

	// Add upload endpoint with special handling
	uploadHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debug.Info("Handling rule upload request: %s %s", r.Method, r.URL.Path)

		// Skip CORS handling as it's now handled by GlobalCORSMiddleware
		if r.Method == "OPTIONS" {
			return // OPTIONS requests are handled by GlobalCORSMiddleware
		}

		// Debug log cookies for troubleshooting
		debug.Info("Rule upload request received with cookies: %v", r.Cookies())

		// Check for authentication token
		cookie, err := r.Cookie("token")
		if err != nil {
			debug.Error("No auth token found in cookies for rule upload request")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		debug.Info("Found auth token cookie for rule upload: %s", cookie.Name)

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
		debug.Info("Setting user context for rule upload: user_id=%s, role=%s", userID.String(), role)

		// Create a new context with user information
		ctx := context.WithValue(r.Context(), "user_id", userID.String())
		ctx = context.WithValue(ctx, "role", role)

		// Call the handler with the updated context
		handler.HandleAddRule(w, r.WithContext(ctx))
		
		// After successful upload, trigger keyspace recalculation for affected preset jobs
		go func() {
			// Log that recalculation should happen
			debug.Info("Rule uploaded - keyspace recalculation should be triggered for affected preset jobs")
			// TODO: Implement proper rule ID extraction and recalculation
			// presetJobService.RecalculateKeyspacesForRule(context.Background(), ruleID)
		}()
	})

	// Register upload endpoint with the custom handler - use Handle instead of HandleFunc
	userRouter.Handle("/upload", uploadHandler).Methods(http.MethodPost, http.MethodOptions)

	// Rest of the write operations
	userRouter.HandleFunc("/{id:[0-9]+}", handler.HandleUpdateRule).Methods(http.MethodPut)

	// Add simplified handler for DELETE operations
	deleteHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debug.Info("Handling rule delete request: %s %s", r.Method, r.URL.Path)

		// Skip CORS handling as it's now handled by GlobalCORSMiddleware
		if r.Method == "OPTIONS" {
			return // OPTIONS requests are handled by GlobalCORSMiddleware
		}

		// Call the actual handler
		handler.HandleDeleteRule(w, r)
	})

	// Register delete endpoint with the custom handler
	userRouter.Handle("/{id:[0-9]+}", deleteHandler).Methods(http.MethodDelete, http.MethodOptions)

	userRouter.HandleFunc("/{id:[0-9]+}/tags", handler.HandleAddRuleTag).Methods(http.MethodPost)
	userRouter.HandleFunc("/{id:[0-9]+}/tags/{tag}", handler.HandleDeleteRuleTag).Methods(http.MethodDelete)

	// Add simplified handler for verify operations
	verifyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debug.Info("Handling rule verify request: %s %s", r.Method, r.URL.Path)

		// Skip CORS handling as it's now handled by GlobalCORSMiddleware
		if r.Method == "OPTIONS" {
			return // OPTIONS requests are handled by GlobalCORSMiddleware
		}

		// Call the actual handler
		handler.HandleVerifyRule(w, r)
	})

	// Register verify endpoint with the custom handler
	userRouter.Handle("/{id:[0-9]+}/verify", verifyHandler).Methods(http.MethodPost, http.MethodOptions)

	// Agent routes (accessible to agents with API key)
	agentRouter := r.PathPrefix("/agent/rules").Subrouter()
	agentRouter.Use(api.APIKeyMiddleware(agentService))

	agentRouter.HandleFunc("", handler.HandleListRulesForAgent).Methods(http.MethodGet)
	agentRouter.HandleFunc("/{filename}", handler.HandleDownloadRule).Methods(http.MethodGet)

	debug.Info("Registered rule management routes")
}
