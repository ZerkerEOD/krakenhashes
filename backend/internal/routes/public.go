package routes

import (
	"encoding/json"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/email"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers"
	agenthandlers "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/agent"
	authhandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/public"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupPublicRoutes configures all public routes that don't require authentication
// Returns the SSO manager for use by admin routes
func SetupPublicRoutes(apiRouter *mux.Router, database *db.DB, agentService *services.AgentService, binaryService *services.AgentBinaryService, appConfig *config.Config, tlsProvider tls.Provider) *sso.Manager {
	debug.Debug("Setting up public routes")

	// Auth endpoints
	emailService := email.NewService(database.DB)
	authHandler := authhandler.NewHandler(database, emailService)
	apiRouter.HandleFunc("/login", authHandler.LoginHandler).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/logout", authHandler.LogoutHandler).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/check-auth", authHandler.CheckAuthHandler).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/verify-mfa", authHandler.VerifyMFAHandler).Methods("POST", "OPTIONS")
	debug.Info("Configured authentication endpoints: /login, /logout, /check-auth, /verify-mfa")

	// Setup SSO routes (LDAP, SAML, OAuth/OIDC)
	ssoManager := SetupSSORoutes(apiRouter, database.DB)

	// Health check endpoint - publicly accessible
	publicRouter := apiRouter.PathPrefix("").Subrouter()
	publicRouter.Use(CORSMiddleware)
	publicRouter.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		debug.Info("Health check request from %s", r.RemoteAddr)
		debug.Debug("Health check request headers: %s", debug.SanitizeHeaders(r.Header))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET", "OPTIONS")
	debug.Info("Configured health check endpoint: /health")

	// Version endpoint - publicly accessible
	publicRouter.HandleFunc("/version", handlers.GetVersion).Methods("GET", "OPTIONS")
	debug.Info("Configured version endpoint: /version")

	// Agent registration endpoint
	registrationHandler := handlers.NewRegistrationHandler(agentService, appConfig, tlsProvider)
	apiRouter.HandleFunc("/agent/register", registrationHandler.HandleRegistration).Methods("POST", "OPTIONS")
	debug.Info("Configured agent registration endpoint: /agent/register")

	// Agent configuration endpoint - publicly accessible for agents to get WebSocket config
	apiRouter.HandleFunc("/agent/config", agenthandlers.GetConfig).Methods("GET", "OPTIONS")
	debug.Info("Configured agent configuration endpoint: /agent/config")

	// Get password policy endpoint
	publicRouter.HandleFunc("/password/policy", func(w http.ResponseWriter, r *http.Request) {
		settings, err := database.GetAuthSettings()
		if err != nil {
			debug.Error("Failed to get password policy: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		response := struct {
			MinPasswordLength   int  `json:"minPasswordLength"`
			RequireUppercase    bool `json:"requireUppercase"`
			RequireLowercase    bool `json:"requireLowercase"`
			RequireNumbers      bool `json:"requireNumbers"`
			RequireSpecialChars bool `json:"requireSpecialChars"`
		}{
			MinPasswordLength:   settings.MinPasswordLength,
			RequireUppercase:    settings.RequireUppercase,
			RequireLowercase:    settings.RequireLowercase,
			RequireNumbers:      settings.RequireNumbers,
			RequireSpecialChars: settings.RequireSpecialChars,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}).Methods("GET")
	debug.Info("Configured password policy endpoint: /password/policy")

	// Agent download endpoints - publicly accessible
	agentDownloadHandler := public.NewAgentDownloadHandler(binaryService)
	apiRouter.HandleFunc("/public/agent/platforms", agentDownloadHandler.GetAvailablePlatforms).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/public/agent/download/{os}/{arch}", agentDownloadHandler.DownloadAgent).Methods("GET", "OPTIONS")
	debug.Info("Configured agent download endpoints: /public/agent/platforms, /public/agent/download/{os}/{arch}")

	return ssoManager
}
