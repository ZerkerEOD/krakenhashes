package routes

import (
	"context"
	"database/sql"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso/ldap"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso/oauth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso/saml"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupSSORoutes configures all SSO authentication routes
func SetupSSORoutes(apiRouter *mux.Router, sqlDB *sql.DB) *sso.Manager {
	debug.Debug("Setting up SSO routes")

	// Create db wrapper
	database := &db.DB{DB: sqlDB}

	// Create SSO repository
	ssoRepo := repository.NewSSORepository(database)

	// Create SSO manager
	ssoManager := sso.NewManager(ssoRepo)

	// Register provider factories
	ssoManager.RegisterFactory("ldap", ldap.Factory)
	ssoManager.RegisterFactory("saml", saml.Factory(ssoRepo))
	ssoManager.RegisterFactory("oidc", oauth.Factory(ssoRepo))
	ssoManager.RegisterFactory("oauth2", oauth.Factory(ssoRepo))

	// Load providers from database
	if err := ssoManager.LoadProviders(context.Background()); err != nil {
		debug.Warning("Failed to load SSO providers: %v", err)
	}

	// Create SSO handler
	ssoHandler := auth.NewSSOHandler(database, ssoManager, ssoRepo)

	// Public SSO routes (no authentication required)
	// List enabled providers for login page
	apiRouter.HandleFunc("/auth/providers", ssoHandler.GetEnabledProviders).Methods("GET", "OPTIONS")
	debug.Info("Configured SSO endpoint: GET /auth/providers")

	// LDAP authentication
	apiRouter.HandleFunc("/auth/ldap/{id}", ssoHandler.LDAPLogin).Methods("POST", "OPTIONS")
	debug.Info("Configured SSO endpoint: POST /auth/ldap/{id}")

	// OAuth flow
	apiRouter.HandleFunc("/auth/oauth/{id}/start", ssoHandler.OAuthStart).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/auth/oauth/{id}/callback", ssoHandler.OAuthCallback).Methods("GET", "OPTIONS")
	debug.Info("Configured SSO endpoints: GET /auth/oauth/{id}/start, GET /auth/oauth/{id}/callback")

	// SAML flow
	apiRouter.HandleFunc("/auth/saml/{id}/start", ssoHandler.SAMLStart).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/auth/saml/{id}/acs", ssoHandler.SAMLACS).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/auth/saml/{id}/metadata", ssoHandler.SAMLMetadata).Methods("GET", "OPTIONS")
	debug.Info("Configured SSO endpoints: GET /auth/saml/{id}/start, POST /auth/saml/{id}/acs, GET /auth/saml/{id}/metadata")

	debug.Info("SSO routes configured successfully")
	return ssoManager
}
