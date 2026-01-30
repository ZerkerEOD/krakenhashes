package routes

import (
	"bufio"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/email"
	adminsettings "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin/settings"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

/*
 * Package routes handles the setup and configuration of all application routes.
 * It includes middleware for CORS and authentication, and organizes routes into
 * logical groups for different parts of the application.
 */

/*
 * CORSMiddleware handles CORS headers for all requests.
 * It configures cross-origin resource sharing based on environment settings.
 *
 * Configuration:
 *   - Uses CORS_ALLOWED_ORIGIN environment variable
 *   - Falls back to http://localhost:3000 if not set
 *
 * Headers Set:
 *   - Access-Control-Allow-Origin
 *   - Access-Control-Allow-Methods
 *   - Access-Control-Allow-Headers
 *   - Access-Control-Allow-Credentials
 *
 * Parameters:
 *   - next: The next handler in the middleware chain
 *
 * Returns:
 *   - http.Handler: Middleware handler that processes CORS
 */
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		debug.Info("Processing CORS for %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		// Get request origin
		origin := r.Header.Get("Origin")
		debug.Debug("Request origin: %s", origin)

		// Always allow the request origin if it's present
		if origin != "" {
			debug.Debug("Setting CORS origin to match request: %s", origin)
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			debug.Warning("No origin header present in request")
		}

		// Set standard CORS headers
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-API-Key, X-Agent-ID, Origin, Cookie, Cache-Control")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "3600")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Authorization")

		// Debug log cookies for troubleshooting
		debug.Debug("Request cookies: %v", r.Cookies())

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			debug.Info("Handling OPTIONS preflight request from origin: %s for path: %s", origin, r.URL.Path)

			// Check if this is a preflight for a multipart/form-data upload
			if strings.Contains(r.Header.Get("Access-Control-Request-Headers"), "content-type") {
				debug.Info("Detected potential multipart/form-data preflight request")
				// Ensure we explicitly allow content-type header for multipart uploads
				w.Header().Set("Access-Control-Allow-Headers",
					"Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-API-Key, X-Agent-ID, Origin, Cookie, Cache-Control")
			}

			// Special handling for upload endpoints
			if strings.Contains(r.URL.Path, "/upload") {
				debug.Info("Detected preflight for upload endpoint: %s", r.URL.Path)

				// Ensure we explicitly allow content-type header for multipart uploads
				w.Header().Set("Access-Control-Allow-Headers",
					"Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-API-Key, X-Agent-ID, Origin, Cookie, Cache-Control")

				// Ensure origin is set for upload endpoints
				if origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			}

			w.WriteHeader(http.StatusOK)
			return
		}

		// For POST requests that might be multipart/form-data uploads
		if r.Method == "POST" && (strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") || strings.Contains(r.URL.Path, "/upload")) {
			debug.Info("Processing upload request to: %s with Content-Type: %s", r.URL.Path, r.Header.Get("Content-Type"))
			// Ensure CORS headers are properly set for uploads
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		// Log final headers for debugging
		debug.Debug("Final response headers: %v", w.Header())

		next.ServeHTTP(w, r)
	})
}

/*
 * SetupRoutes configures all application routes and middleware.
 *
 * Route Groups:
 *   - Public Routes (/api/login, /api/logout, /api/check-auth)
 *   - Protected Routes (requires authentication)
 *     - Dashboard (/api/dashboard)
 *     - Hashlists (/api/hashlists)
 *     - Jobs (/api/jobs)
 *     - API endpoints (/api/api/...)
 *     - Agent endpoints (/api/agent/...)
 *
 * Middleware Applied:
 *   - CORS middleware (all routes)
 *   - JWT authentication (protected routes)
 *   - API Key authentication (agent routes)
 */
func SetupRoutes(r *mux.Router, sqlDB *sql.DB, tlsProvider tls.Provider, agentService *services.AgentService, wordlistManager wordlist.Manager, ruleManager rule.Manager, binaryManager binary.Manager, potfileService *services.PotfileService, clientPotfileService *services.ClientPotfileService, analyticsQueueService *services.AnalyticsQueueService) {
	debug.Info("Initializing route configuration")

	// Create our custom DB wrapper
	database := &db.DB{DB: sqlDB}
	debug.Debug("Created custom DB wrapper")

	// Apply GlobalCORSMiddleware at the root level for consistent CORS handling
	r.Use(GlobalCORSMiddleware)
	debug.Info("Applied GlobalCORSMiddleware to root router")

	// Initialize email service
	emailService := email.NewService(sqlDB)

	// Initialize configuration
	appConfig := config.NewConfig()
	debug.Info("Application configuration initialized")

	// Create API router with logging
	apiRouter := r.PathPrefix("/api").Subrouter()
	apiRouter.Use(loggingMiddleware)
	debug.Info("Created API router with logging middleware")

	// Setup TLS routes - Disabled to prevent duplicate HTTP server on port 1337
	// SetupTLSRoutes(r, tlsProvider)

	// Create MFA handler
	mfaHandler := auth.NewMFAHandler(database, emailService)

	// Initialize Repositories needed for new services
	presetJobRepo := repository.NewPresetJobRepository(sqlDB)
	presetIncrementLayerRepo := repository.NewPresetIncrementLayerRepository(database)
	systemSettingsRepo := repository.NewSystemSettingsRepository(database)
	workflowRepo := repository.NewJobWorkflowRepository(sqlDB)
	fileRepository := repository.NewFileRepository(database, appConfig.DataDir)
	hashRepo := repository.NewHashRepository(database)
	hashlistRepo := repository.NewHashListRepository(database)
	clientRepo := repository.NewClientRepository(database)
	clientSettingsRepo := repository.NewClientSettingsRepository(database)
	jobExecutionRepo := repository.NewJobExecutionRepository(database)
	debug.Info("Initialized PresetJob, PresetIncrementLayer, SystemSettings, JobWorkflow, File, Hash, Hashlist, Client, and ClientSettings repositories")

	// Initialize Services for preset jobs and workflows
	presetJobService := services.NewAdminPresetJobService(presetJobRepo, presetIncrementLayerRepo, systemSettingsRepo, binaryManager, fileRepository, appConfig.DataDir)
	workflowService := services.NewAdminJobWorkflowService(sqlDB, workflowRepo, presetJobRepo) // Pass db, workflowRepo, presetJobRepo
	debug.Info("Initialized AdminPresetJobService and AdminJobWorkflowService")

	// Initialize Handler for new admin job routes
	adminJobsHandler := NewAdminJobsHandler(presetJobService, workflowService)
	debug.Info("Initialized AdminJobsHandler")

	// Initialize binary service for agent downloads
	binaryService := services.NewAgentBinaryService(appConfig.DataDir)
	if err := binaryService.Initialize(); err != nil {
		debug.Error("Failed to initialize binary service: %v", err)
	}
	debug.Info("Initialized AgentBinaryService")

	// Create auth handler (needed for both public and protected routes)
	authHandler := auth.NewHandler(database, emailService)

	// Setup public routes (returns SSO manager for admin routes)
	ssoManager := SetupPublicRoutes(apiRouter, database, agentService, binaryService, appConfig, tlsProvider)

	// Setup public passkey routes (MFA authentication flow - no JWT required)
	SetupPublicPasskeyRoutes(apiRouter, authHandler)

	// Setup JWT protected routes
	jwtRouter := apiRouter.PathPrefix("").Subrouter()
	jwtRouter.Use(middleware.RequireAuth(database))
	jwtRouter.Use(loggingMiddleware)

	// Add token refresh endpoint (requires authentication)
	jwtRouter.HandleFunc("/refresh-token", authHandler.RefreshTokenHandler).Methods("POST", "OPTIONS")
	debug.Info("Configured token refresh endpoint: /refresh-token")

	// Create system settings handler for users (read-only access to max priority)
	userSystemSettingsHandler := adminsettings.NewSystemSettingsHandler(systemSettingsRepo, presetJobRepo)

	// Create retention settings handler for users (read-only access to default retention)
	userRetentionSettingsHandler := adminsettings.NewRetentionSettingsHandler(clientSettingsRepo)

	// Setup feature-specific routes
	SetupDashboardRoutes(jwtRouter)
	SetupHashlistRoutes(jwtRouter)
	// Note: Skipping SetupJobRoutes(jwtRouter) as it conflicts with SetupUserRoutes - the real job routes are in SetupUserRoutes
	SetupAgentRoutes(jwtRouter, agentService, database)
	SetupVoucherRoutes(jwtRouter, services.NewClaimVoucherService(repository.NewClaimVoucherRepository(database)))
	SetupPotRoutes(jwtRouter, hashRepo, hashlistRepo, clientRepo, jobExecutionRepo)

	// Add user accessible routes for settings (read-only)
	jwtRouter.HandleFunc("/settings/max-priority", userSystemSettingsHandler.GetMaxPriorityForUsers).Methods(http.MethodGet, http.MethodOptions)
	jwtRouter.HandleFunc("/settings/retention", userRetentionSettingsHandler.GetDefaultRetention).Methods(http.MethodGet, http.MethodOptions)

	SetupAdminRoutes(jwtRouter, database, emailService, adminJobsHandler, binaryManager, ssoManager) // Pass adminJobsHandler, binaryManager, and ssoManager
	SetupUserRoutes(jwtRouter, database, appConfig.DataDir, binaryManager, agentService)
	SetupNotificationRoutes(jwtRouter, database, emailService) // Enhanced notification system
	SetupMFARoutes(jwtRouter, mfaHandler, database, emailService)
	SetupPasskeyRoutes(jwtRouter, authHandler, database)
	SetupAdminPasskeyRoutes(jwtRouter, authHandler, database)
	// Use the enhanced WebSocket setup with job integration
	SetupWebSocketWithJobRoutes(r, agentService, tlsProvider, sqlDB, appConfig, wordlistManager, ruleManager, binaryManager, potfileService, clientPotfileService)
	SetupBinaryRoutes(jwtRouter, sqlDB, appConfig, agentService)

	// Setup wordlist and rule routes
	SetupWordlistRoutes(jwtRouter, sqlDB, appConfig, agentService, presetJobService, potfileService)
	SetupRuleRoutes(jwtRouter, sqlDB, appConfig, agentService, presetJobService)

	// Setup analytics routes
	SetupAnalyticsRoutes(jwtRouter, database, analyticsQueueService)

	// Setup file download routes for agents
	SetupFileDownloadRoutes(r, sqlDB, appConfig, agentService)

	// Create jobs handler for hashlist routes
	jobsHandler := CreateJobsHandler(database, appConfig.DataDir, binaryManager)

	// Register Hashlist Management Routes (includes user/agent hashlist, clients, hash types, hash search)
	registerHashlistRoutes(jwtRouter, sqlDB, appConfig, agentService, clientPotfileService, jobsHandler)

	// Setup WebSocket Routes
	debug.Info("Setting up WebSocket routes...")

	// Setup User API v1 routes
	SetupV1Routes(r, database, appConfig.DataDir, binaryManager)

	debug.Info("Route configuration completed successfully")
	logRegisteredRoutes(r)
}

// loggingMiddleware logs details about each request
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// For OPTIONS requests, just log minimally to avoid interference
		if r.Method == "OPTIONS" {
			debug.Debug("OPTIONS request received: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		debug.Info("Request received: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		debug.Debug("Request headers: %s", debug.SanitizeHeaders(r.Header))

		// Create a response wrapper to capture the status code
		rw := &responseWriter{w, http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		debug.Info("Request completed: %s %s - Status: %d - Duration: %v",
			r.Method, r.URL.Path, rw.statusCode, duration)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements the http.Hijacker interface to support WebSocket connections
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// logRegisteredRoutes prints all registered routes for debugging
func logRegisteredRoutes(r *mux.Router) {
	debug.Info("Registered routes:")
	r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err != nil {
			pathTemplate = "<unknown>"
		}
		methods, err := route.GetMethods()
		if err != nil {
			methods = []string{"ANY"}
		}
		debug.Info("Route: %s [%s]", pathTemplate, strings.Join(methods, ", "))
		return nil
	})
}

// TODO: Implement agent-related functionality
// func unusedAgentPlaceholder() {
// 	_ = agent.SomeFunction // Replace SomeFunction with an actual function from the agent package
// }
