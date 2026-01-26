package routes

import (
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/gorilla/mux"
)

// SetupPasskeyRoutes sets up the protected passkey/WebAuthn-related routes (require authentication)
func SetupPasskeyRoutes(router *mux.Router, authHandler *auth.Handler, database *db.DB) {
	// Protected routes (require authentication) - for passkey management
	protected := router.PathPrefix("/user/passkeys").Subrouter()
	protected.Use(middleware.RequireAuth(database))

	// List user's passkeys
	protected.HandleFunc("", authHandler.ListPasskeys).Methods("GET", "OPTIONS")

	// Begin passkey registration
	protected.HandleFunc("/register/begin", authHandler.BeginPasskeyRegistration).Methods("POST", "OPTIONS")

	// Finish passkey registration
	protected.HandleFunc("/register/finish", authHandler.FinishPasskeyRegistration).Methods("POST", "OPTIONS")

	// Delete a passkey
	protected.HandleFunc("/{id}", authHandler.DeletePasskey).Methods("DELETE", "OPTIONS")

	// Rename a passkey
	protected.HandleFunc("/{id}/rename", authHandler.RenamePasskey).Methods("PUT", "OPTIONS")
}

// SetupPublicPasskeyRoutes sets up public passkey routes for MFA authentication flow
// These routes must be on a public router (no RequireAuth middleware) because they are
// called during the MFA verification step when the user only has a session token, not a JWT
func SetupPublicPasskeyRoutes(router *mux.Router, authHandler *auth.Handler) {
	// Public routes (for MFA authentication flow)
	// These routes are called during the MFA verification step after password login
	public := router.PathPrefix("/auth/passkey").Subrouter()

	// Begin passkey authentication (MFA flow - requires valid MFA session token)
	public.HandleFunc("/authenticate/begin", authHandler.BeginPasskeyAuthentication).Methods("POST", "OPTIONS")

	// Finish passkey authentication (MFA flow)
	public.HandleFunc("/authenticate/finish", authHandler.FinishPasskeyAuthentication).Methods("POST", "OPTIONS")
}

// SetupAdminPasskeyRoutes sets up admin-only passkey configuration routes
func SetupAdminPasskeyRoutes(router *mux.Router, authHandler *auth.Handler, database *db.DB) {
	// Admin routes (require admin role)
	admin := router.PathPrefix("/admin/webauthn").Subrouter()
	admin.Use(middleware.RequireAuth(database))
	admin.Use(middleware.AdminOnly)

	// Get WebAuthn settings
	admin.HandleFunc("/settings", authHandler.GetWebAuthnSettings).Methods("GET", "OPTIONS")

	// Update WebAuthn settings
	admin.HandleFunc("/settings", authHandler.UpdateWebAuthnSettings).Methods("PUT", "OPTIONS")
}
