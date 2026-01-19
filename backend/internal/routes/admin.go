package routes

import (
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/email"
	adminhandlers "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin/auth"
	adminsettings "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin/settings"
	adminuser "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin/user"
	binaryhandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/binary"
	emailhandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/email"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupAdminRoutes configures all admin-related routes
// It now accepts an AdminJobsHandler to set up job and workflow routes.
func SetupAdminRoutes(router *mux.Router, database *db.DB, emailService *email.Service, jobHandler *AdminJobsHandler, binaryManager binary.Manager, ssoManager *sso.Manager) *mux.Router {
	debug.Debug("Setting up admin routes")

	// Create Repositories needed by handlers/services
	clientSettingsRepo := repository.NewClientSettingsRepository(database)
	systemSettingsRepo := repository.NewSystemSettingsRepository(database)
	userRepo := repository.NewUserRepository(database)
	ssoRepo := repository.NewSSORepository(database)

	// Create Services (retention service no longer needed in admin routes)
	// Client management moved to regular authenticated users

	// Get preset job repository from handler - we need to find a way to access this
	// For now, create it again here since we can't access it from the handler
	presetJobRepo := repository.NewPresetJobRepository(database.DB)

	// Create Handlers
	authSettingsHandler := auth.NewAuthSettingsHandler(database)
	emailHandler := emailhandler.NewHandler(emailService)
	retentionSettingsHandler := adminsettings.NewRetentionSettingsHandler(clientSettingsRepo)
	systemSettingsHandler := adminsettings.NewSystemSettingsHandler(systemSettingsRepo, presetJobRepo)
	jobSettingsHandler := adminsettings.NewJobSettingsHandler(systemSettingsRepo)
	monitoringSettingsHandler := adminsettings.NewMonitoringSettingsHandler(systemSettingsRepo)
	// clientHandler removed - client management moved to regular authenticated users
	userHandler := adminuser.NewUserHandler(userRepo, database)
	ssoAdminHandler := adminhandlers.NewSSOAdminHandler(database, ssoManager, ssoRepo)

	// Create admin router
	adminRouter := router.PathPrefix("/admin").Subrouter()

	// Apply admin middleware
	adminRouter.Use(middleware.AdminOnly)

	// Auth settings routes
	adminRouter.HandleFunc("/auth/settings", authSettingsHandler.GetSettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/auth/settings", authSettingsHandler.UpdateSettings).Methods(http.MethodPut, http.MethodOptions)
	adminRouter.HandleFunc("/auth/settings/mfa", authSettingsHandler.GetMFASettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/auth/settings/mfa", authSettingsHandler.UpdateMFASettings).Methods(http.MethodPut, http.MethodOptions)
	adminRouter.HandleFunc("/auth/settings/password", authSettingsHandler.GetPasswordPolicy).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/auth/settings/security", authSettingsHandler.GetAccountSecurity).Methods(http.MethodGet, http.MethodOptions)

	// SSO Admin routes
	adminRouter.HandleFunc("/sso/settings", ssoAdminHandler.GetSSOSettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/sso/settings", ssoAdminHandler.UpdateSSOSettings).Methods(http.MethodPut, http.MethodOptions)
	adminRouter.HandleFunc("/sso/providers", ssoAdminHandler.ListProviders).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/sso/providers", ssoAdminHandler.CreateProvider).Methods(http.MethodPost, http.MethodOptions)
	adminRouter.HandleFunc("/sso/providers/{id:[0-9a-fA-F-]+}", ssoAdminHandler.GetProvider).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/sso/providers/{id:[0-9a-fA-F-]+}", ssoAdminHandler.UpdateProvider).Methods(http.MethodPut, http.MethodOptions)
	adminRouter.HandleFunc("/sso/providers/{id:[0-9a-fA-F-]+}", ssoAdminHandler.DeleteProvider).Methods(http.MethodDelete, http.MethodOptions)
	adminRouter.HandleFunc("/sso/providers/{id:[0-9a-fA-F-]+}/test", ssoAdminHandler.TestProvider).Methods(http.MethodPost, http.MethodOptions)
	adminRouter.HandleFunc("/sso/users/{id:[0-9a-fA-F-]+}/identities", ssoAdminHandler.GetUserIdentities).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/sso/identities/{id:[0-9a-fA-F-]+}", ssoAdminHandler.UnlinkIdentity).Methods(http.MethodDelete, http.MethodOptions)
	debug.Info("Configured SSO admin routes: /admin/sso/*")

	// Data Retention settings routes (New)
	adminRouter.HandleFunc("/settings/retention", retentionSettingsHandler.GetDefaultRetention).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/settings/retention", retentionSettingsHandler.UpdateDefaultRetention).Methods(http.MethodPut, http.MethodOptions)

	// System settings routes (New)
	adminRouter.HandleFunc("/settings/max-priority", systemSettingsHandler.GetMaxPriority).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/settings/max-priority", systemSettingsHandler.UpdateMaxPriority).Methods(http.MethodPut, http.MethodOptions)
	
	// Job execution settings routes (New) - Must be before generic {key} route
	adminRouter.HandleFunc("/settings/job-execution", jobSettingsHandler.GetJobExecutionSettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/settings/job-execution", jobSettingsHandler.UpdateJobExecutionSettings).Methods(http.MethodPut, http.MethodOptions)
	
	// Monitoring settings routes - Must be before generic {key} route
	adminRouter.HandleFunc("/settings/monitoring", monitoringSettingsHandler.GetMonitoringSettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/settings/monitoring", monitoringSettingsHandler.UpdateMonitoringSettings).Methods(http.MethodPut, http.MethodOptions)

	// Agent download settings routes - Must be before generic {key} route
	agentSettingsHandler := adminsettings.NewAgentSettingsHandler(systemSettingsRepo)
	adminRouter.HandleFunc("/settings/agent-download", agentSettingsHandler.GetAgentDownloadSettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/settings/agent-download", agentSettingsHandler.UpdateAgentDownloadSettings).Methods(http.MethodPut, http.MethodOptions)

	// General system settings routes for listing and updating individual settings - Must be after specific routes
	adminRouter.HandleFunc("/settings", systemSettingsHandler.ListSettings).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/settings/{key}", systemSettingsHandler.GetSetting).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/settings/{key}", systemSettingsHandler.UpdateSetting).Methods(http.MethodPut, http.MethodOptions)

	// Client Management routes moved to regular authenticated users at /api/clients

	// User Management routes (New)
	adminRouter.HandleFunc("/users", userHandler.ListUsers).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/users", userHandler.CreateUser).Methods(http.MethodPost, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}", userHandler.GetUser).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}", userHandler.UpdateUser).Methods(http.MethodPut, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}", userHandler.DeleteUser).Methods(http.MethodDelete, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/disable", userHandler.DisableUser).Methods(http.MethodPost, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/enable", userHandler.EnableUser).Methods(http.MethodPost, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/reset-password", userHandler.ResetUserPassword).Methods(http.MethodPost, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/disable-mfa", userHandler.DisableUserMFA).Methods(http.MethodPost, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/unlock", userHandler.UnlockUser).Methods(http.MethodPost, http.MethodOptions)
	// Login attempts and session management routes
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/login-attempts", userHandler.GetUserLoginAttempts).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/sessions", userHandler.GetUserSessions).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/sessions", userHandler.TerminateAllUserSessions).Methods(http.MethodDelete, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/sessions/{sessionId:[0-9a-fA-F-]+}", userHandler.TerminateSession).Methods(http.MethodDelete, http.MethodOptions)

	// User API Key management routes (admin)
	userAPIService := services.NewUserAPIService(userRepo)
	apiKeyAdminHandler := adminuser.NewAPIKeyAdminHandler(userAPIService)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/api-key/info", apiKeyAdminHandler.GetUserAPIKeyInfo).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/users/{id:[0-9a-fA-F-]+}/api-key", apiKeyAdminHandler.RevokeUserAPIKey).Methods(http.MethodDelete, http.MethodOptions)
	debug.Info("Configured admin user API key management routes")

	// Email configuration endpoints
	adminRouter.HandleFunc("/email/config", emailHandler.GetConfig).Methods("GET", "OPTIONS")
	adminRouter.HandleFunc("/email/config", emailHandler.UpdateConfig).Methods("POST", "PUT", "OPTIONS")
	adminRouter.HandleFunc("/email/test", emailHandler.TestConfig).Methods("POST", "OPTIONS")

	// Email template endpoints
	adminRouter.HandleFunc("/email/templates", emailHandler.ListTemplates).Methods("GET", "OPTIONS")
	adminRouter.HandleFunc("/email/templates", emailHandler.CreateTemplate).Methods("POST", "OPTIONS")
	adminRouter.HandleFunc("/email/templates/{id:[0-9]+}", emailHandler.GetTemplate).Methods("GET", "OPTIONS")
	adminRouter.HandleFunc("/email/templates/{id:[0-9]+}", emailHandler.UpdateTemplate).Methods("PUT", "OPTIONS")
	adminRouter.HandleFunc("/email/templates/{id:[0-9]+}", emailHandler.DeleteTemplate).Methods("DELETE", "OPTIONS")

	// Email usage statistics endpoint
	adminRouter.HandleFunc("/email/usage", emailHandler.GetUsage).Methods("GET", "OPTIONS")

	// Binary management endpoints
	if binaryManager != nil {
		binaryHandler := binaryhandler.NewHandler(binaryManager)
		adminRouter.HandleFunc("/binary", binaryHandler.HandleListVersions).Methods(http.MethodGet, http.MethodOptions)
		adminRouter.HandleFunc("/binary", binaryHandler.HandleAddVersion).Methods(http.MethodPost, http.MethodOptions)
		adminRouter.HandleFunc("/binary/upload", binaryHandler.HandleUploadVersion).Methods(http.MethodPost, http.MethodOptions)
		adminRouter.HandleFunc("/binary/{id}", binaryHandler.HandleGetVersion).Methods(http.MethodGet, http.MethodOptions)
		adminRouter.HandleFunc("/binary/{id}", binaryHandler.HandleDeleteVersion).Methods(http.MethodDelete, http.MethodOptions)
		adminRouter.HandleFunc("/binary/{id}/verify", binaryHandler.HandleVerifyVersion).Methods(http.MethodPost, http.MethodOptions)
		adminRouter.HandleFunc("/binary/{id}/set-default", binaryHandler.HandleSetDefaultVersion).Methods(http.MethodPut, http.MethodOptions)
		debug.Info("Configured admin binary management routes: /admin/binary/*")
	} else {
		debug.Error("Binary manager not provided to SetupAdminRoutes")
	}

	// Setup Preset Job and Job Workflow routes using the passed handler
	SetupAdminJobRoutes(adminRouter, jobHandler)
	debug.Info("Configured admin preset job and workflow routes: /admin/preset-jobs/*, /admin/job-workflows/*")

	// Note: Diagnostics routes (GH Issue #23) are configured separately via SetupDiagnosticsRoutes
	// after WebSocket handler is initialized

	debug.Info("Configured admin routes: /admin/* (including user management at /admin/users/*)")

	return adminRouter
}
