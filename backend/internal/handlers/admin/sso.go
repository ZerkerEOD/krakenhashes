package admin

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// SSOAdminHandler handles SSO administration requests
type SSOAdminHandler struct {
	db         *db.DB
	ssoManager *sso.Manager
	ssoRepo    *repository.SSORepository
}

// NewSSOAdminHandler creates a new SSO admin handler
func NewSSOAdminHandler(database *db.DB, ssoManager *sso.Manager, ssoRepo *repository.SSORepository) *SSOAdminHandler {
	return &SSOAdminHandler{
		db:         database,
		ssoManager: ssoManager,
		ssoRepo:    ssoRepo,
	}
}

// ============================================================================
// SSO Settings Handlers
// ============================================================================

// GetSSOSettings returns the global SSO settings
func (h *SSOAdminHandler) GetSSOSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.ssoManager.GetSSOSettings(r.Context())
	if err != nil {
		debug.Error("Failed to get SSO settings: %v", err)
		http.Error(w, "Failed to get SSO settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// UpdateSSOSettingsRequest represents the update SSO settings request body
type UpdateSSOSettingsRequest struct {
	LocalAuthEnabled   *bool `json:"local_auth_enabled,omitempty"`
	LDAPAuthEnabled    *bool `json:"ldap_auth_enabled,omitempty"`
	SAMLAuthEnabled    *bool `json:"saml_auth_enabled,omitempty"`
	OAuthAuthEnabled   *bool `json:"oauth_auth_enabled,omitempty"`
	SSOAutoCreateUsers *bool `json:"sso_auto_create_users,omitempty"`
	SSOAutoEnableUsers *bool `json:"sso_auto_enable_users,omitempty"`
}

// UpdateSSOSettings updates the global SSO settings
func (h *SSOAdminHandler) UpdateSSOSettings(w http.ResponseWriter, r *http.Request) {
	var req UpdateSSOSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Warning("Failed to decode SSO settings update request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Get current settings
	settings, err := h.ssoManager.GetSSOSettings(r.Context())
	if err != nil {
		debug.Error("Failed to get current SSO settings: %v", err)
		http.Error(w, "Failed to get current settings", http.StatusInternalServerError)
		return
	}

	// Apply updates
	if req.LocalAuthEnabled != nil {
		settings.LocalAuthEnabled = *req.LocalAuthEnabled
	}
	if req.LDAPAuthEnabled != nil {
		settings.LDAPAuthEnabled = *req.LDAPAuthEnabled
	}
	if req.SAMLAuthEnabled != nil {
		settings.SAMLAuthEnabled = *req.SAMLAuthEnabled
	}
	if req.OAuthAuthEnabled != nil {
		settings.OAuthAuthEnabled = *req.OAuthAuthEnabled
	}
	if req.SSOAutoCreateUsers != nil {
		settings.SSOAutoCreateUsers = *req.SSOAutoCreateUsers
	}
	if req.SSOAutoEnableUsers != nil {
		settings.SSOAutoEnableUsers = *req.SSOAutoEnableUsers
	}

	// Save settings
	if err := h.ssoManager.UpdateSSOSettings(r.Context(), settings); err != nil {
		debug.Error("Failed to update SSO settings: %v", err)
		http.Error(w, "Failed to update SSO settings", http.StatusInternalServerError)
		return
	}

	debug.Info("SSO settings updated")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// ============================================================================
// SSO Provider Handlers
// ============================================================================

// ListProviders returns all SSO providers
func (h *SSOAdminHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.ssoRepo.ListProviders(r.Context())
	if err != nil {
		debug.Error("Failed to list SSO providers: %v", err)
		http.Error(w, "Failed to list providers", http.StatusInternalServerError)
		return
	}

	// Ensure nil slice becomes empty array in JSON
	if providers == nil {
		providers = []*models.SSOProvider{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers": providers,
	})
}

// GetProvider returns a specific SSO provider with its config
func (h *SSOAdminHandler) GetProvider(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	provider, err := h.ssoRepo.GetProvider(r.Context(), providerID)
	if err != nil {
		debug.Error("Failed to get SSO provider: %v", err)
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	// Get type-specific config
	response := map[string]interface{}{
		"provider": provider,
	}

	switch provider.ProviderType {
	case models.ProviderTypeLDAP:
		config, err := h.ssoRepo.GetLDAPConfig(r.Context(), providerID)
		if err == nil {
			response["ldap_config"] = config
		}
	case models.ProviderTypeSAML:
		config, err := h.ssoRepo.GetSAMLConfig(r.Context(), providerID)
		if err == nil {
			response["saml_config"] = config
		}
	case models.ProviderTypeOIDC, models.ProviderTypeOAuth2:
		config, err := h.ssoRepo.GetOAuthConfig(r.Context(), providerID)
		if err == nil {
			response["oauth_config"] = config
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// CreateProviderRequest represents the create provider request body
type CreateProviderRequest struct {
	Name            string                   `json:"name"`
	ProviderType    models.ProviderType      `json:"provider_type"`
	Enabled         bool                     `json:"enabled"`
	DisplayOrder    int                      `json:"display_order"`
	AutoCreateUsers *bool                    `json:"auto_create_users,omitempty"`
	AutoEnableUsers *bool                    `json:"auto_enable_users,omitempty"`
	LDAPConfig      *models.LDAPConfigInput  `json:"ldap_config,omitempty"`
	SAMLConfig      *models.SAMLConfigInput  `json:"saml_config,omitempty"`
	OAuthConfig     *models.OAuthConfigInput `json:"oauth_config,omitempty"`
}

// CreateProvider creates a new SSO provider
func (h *SSOAdminHandler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req CreateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Warning("Failed to decode create provider request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Name == "" {
		http.Error(w, "Provider name is required", http.StatusBadRequest)
		return
	}
	if req.ProviderType == "" {
		http.Error(w, "Provider type is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Create provider
	provider := &models.SSOProvider{
		ID:              uuid.New(),
		Name:            req.Name,
		ProviderType:    req.ProviderType,
		Enabled:         req.Enabled,
		DisplayOrder:    req.DisplayOrder,
		AutoCreateUsers: req.AutoCreateUsers,
		AutoEnableUsers: req.AutoEnableUsers,
	}

	if err := h.ssoRepo.CreateProvider(ctx, provider); err != nil {
		debug.Error("Failed to create SSO provider: %v", err)
		http.Error(w, "Failed to create provider", http.StatusInternalServerError)
		return
	}

	// Create type-specific config
	var configErr error
	switch req.ProviderType {
	case models.ProviderTypeLDAP:
		if req.LDAPConfig == nil {
			configErr = deleteAndError(ctx, h.ssoRepo, provider.ID, "LDAP config is required for LDAP providers")
		} else {
			configErr = h.createLDAPConfig(ctx, provider.ID, req.LDAPConfig)
		}
	case models.ProviderTypeSAML:
		if req.SAMLConfig == nil {
			configErr = deleteAndError(ctx, h.ssoRepo, provider.ID, "SAML config is required for SAML providers")
		} else {
			configErr = h.createSAMLConfig(ctx, provider.ID, req.SAMLConfig)
		}
	case models.ProviderTypeOIDC, models.ProviderTypeOAuth2:
		if req.OAuthConfig == nil {
			configErr = deleteAndError(ctx, h.ssoRepo, provider.ID, "OAuth config is required for OAuth/OIDC providers")
		} else {
			configErr = h.createOAuthConfig(ctx, provider.ID, req.OAuthConfig)
		}
	default:
		configErr = deleteAndError(ctx, h.ssoRepo, provider.ID, "Unknown provider type")
	}

	if configErr != nil {
		debug.Error("Failed to create provider config: %v", configErr)
		http.Error(w, configErr.Error(), http.StatusBadRequest)
		return
	}

	// Reload provider if enabled
	var loadError string
	if provider.Enabled {
		if err := h.ssoManager.ReloadProvider(ctx, provider.ID); err != nil {
			debug.Warning("Failed to load new provider: %v", err)
			loadError = err.Error()
		}
	}

	debug.Info("Created SSO provider ID %s (type: %s)", provider.ID, provider.ProviderType)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	// Return provider with load error if any
	response := map[string]interface{}{
		"provider": provider,
	}
	if loadError != "" {
		response["load_error"] = loadError
		response["warning"] = "Provider created but failed to load. Please check configuration and try test connection."
	}
	json.NewEncoder(w).Encode(response)
}

// UpdateProvider updates an existing SSO provider
func (h *SSOAdminHandler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	var req CreateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		debug.Warning("Failed to decode update provider request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get existing provider
	provider, err := h.ssoRepo.GetProvider(ctx, providerID)
	if err != nil {
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	// Update provider fields
	if req.Name != "" {
		provider.Name = req.Name
	}
	provider.Enabled = req.Enabled
	provider.DisplayOrder = req.DisplayOrder
	provider.AutoCreateUsers = req.AutoCreateUsers
	provider.AutoEnableUsers = req.AutoEnableUsers

	if err := h.ssoRepo.UpdateProvider(ctx, provider); err != nil {
		debug.Error("Failed to update SSO provider: %v", err)
		http.Error(w, "Failed to update provider", http.StatusInternalServerError)
		return
	}

	// Update type-specific config
	switch provider.ProviderType {
	case models.ProviderTypeLDAP:
		if req.LDAPConfig != nil {
			if err := h.updateLDAPConfig(ctx, providerID, req.LDAPConfig); err != nil {
				debug.Error("Failed to update LDAP config: %v", err)
				http.Error(w, "Failed to update LDAP config", http.StatusInternalServerError)
				return
			}
		}
	case models.ProviderTypeSAML:
		if req.SAMLConfig != nil {
			if err := h.updateSAMLConfig(ctx, providerID, req.SAMLConfig); err != nil {
				debug.Error("Failed to update SAML config: %v", err)
				http.Error(w, "Failed to update SAML config", http.StatusInternalServerError)
				return
			}
		}
	case models.ProviderTypeOIDC, models.ProviderTypeOAuth2:
		if req.OAuthConfig != nil {
			if err := h.updateOAuthConfig(ctx, providerID, req.OAuthConfig); err != nil {
				debug.Error("Failed to update OAuth config: %v", err)
				http.Error(w, "Failed to update OAuth config", http.StatusInternalServerError)
				return
			}
		}
	}

	// Reload provider
	if err := h.ssoManager.ReloadProvider(ctx, providerID); err != nil {
		debug.Warning("Failed to reload provider: %v", err)
	}

	debug.Info("Updated SSO provider ID %s", provider.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(provider)
}

// DeleteProvider deletes an SSO provider
func (h *SSOAdminHandler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get provider for logging
	provider, err := h.ssoRepo.GetProvider(ctx, providerID)
	if err != nil {
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	// Unload from manager
	h.ssoManager.UnloadProvider(providerID)

	// Delete from database
	if err := h.ssoRepo.DeleteProvider(ctx, providerID); err != nil {
		debug.Error("Failed to delete SSO provider: %v", err)
		http.Error(w, "Failed to delete provider", http.StatusInternalServerError)
		return
	}

	debug.Info("Deleted SSO provider ID %s", provider.ID)
	w.WriteHeader(http.StatusNoContent)
}

// TestProvider tests the connection to an SSO provider
func (h *SSOAdminHandler) TestProvider(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerIDStr := vars["id"]

	providerID, err := uuid.Parse(providerIDStr)
	if err != nil {
		http.Error(w, "Invalid provider ID", http.StatusBadRequest)
		return
	}

	// Try to load/reload the provider first to ensure it's in memory
	// This also validates the provider configuration
	if err := h.ssoManager.ReloadProvider(r.Context(), providerID); err != nil {
		debug.Warning("Failed to load provider for test: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load provider: " + err.Error(),
		})
		return
	}

	if err := h.ssoManager.TestConnection(r.Context(), providerID); err != nil {
		debug.Warning("Provider connection test failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Connection test successful",
	})
}

// ============================================================================
// User Identity Handlers
// ============================================================================

// GetUserIdentities returns all linked identities for a user
func (h *SSOAdminHandler) GetUserIdentities(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr := vars["user_id"]

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	identities, err := h.ssoRepo.GetUserIdentities(r.Context(), userID)
	if err != nil {
		debug.Error("Failed to get user identities: %v", err)
		http.Error(w, "Failed to get identities", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(identities)
}

// UnlinkIdentity removes a linked identity from a user
func (h *SSOAdminHandler) UnlinkIdentity(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	identityIDStr := vars["identity_id"]

	identityID, err := uuid.Parse(identityIDStr)
	if err != nil {
		http.Error(w, "Invalid identity ID", http.StatusBadRequest)
		return
	}

	if err := h.ssoRepo.DeleteUserIdentity(r.Context(), identityID); err != nil {
		debug.Error("Failed to unlink identity: %v", err)
		http.Error(w, "Failed to unlink identity", http.StatusInternalServerError)
		return
	}

	debug.Info("Unlinked user identity: %s", identityID)
	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// Helper Methods
// ============================================================================

func deleteAndError(ctx context.Context, repo *repository.SSORepository, providerID uuid.UUID, message string) error {
	repo.DeleteProvider(ctx, providerID)
	return &httpError{message: message}
}

type httpError struct {
	message string
}

func (e *httpError) Error() string {
	return e.message
}

func (h *SSOAdminHandler) createLDAPConfig(ctx context.Context, providerID uuid.UUID, input *models.LDAPConfigInput) error {
	// Encrypt bind password
	encryptedPassword := ""
	if input.BindPassword != "" {
		encrypted, err := sso.GetEncryptionService().Encrypt(input.BindPassword)
		if err != nil {
			return err
		}
		encryptedPassword = encrypted
	}

	config := &models.LDAPConfig{
		ID:                       uuid.New(),
		ProviderID:               providerID,
		ServerURL:                input.ServerURL,
		BaseDN:                   input.BaseDN,
		BindDN:                   input.BindDN,
		BindPasswordEncrypted:    encryptedPassword,
		UserSearchFilter:         input.UserSearchFilter,
		UseStartTLS:              input.UseStartTLS,
		SkipCertVerify:           input.SkipCertVerify,
		CACertificate:            input.CACertificate,
		EmailAttribute:           input.EmailAttribute,
		UsernameAttribute:        input.UsernameAttribute,
		DisplayNameAttribute:     input.DisplayNameAttribute,
		ConnectionTimeoutSeconds: input.ConnectionTimeoutSeconds,
	}

	return h.ssoRepo.CreateLDAPConfig(ctx, config)
}

func (h *SSOAdminHandler) updateLDAPConfig(ctx context.Context, providerID uuid.UUID, input *models.LDAPConfigInput) error {
	config, err := h.ssoRepo.GetLDAPConfig(ctx, providerID)
	if err != nil {
		return err
	}

	// Update fields
	config.ServerURL = input.ServerURL
	config.BaseDN = input.BaseDN
	config.BindDN = input.BindDN
	config.UserSearchFilter = input.UserSearchFilter
	config.UseStartTLS = input.UseStartTLS
	config.SkipCertVerify = input.SkipCertVerify
	config.CACertificate = input.CACertificate
	config.EmailAttribute = input.EmailAttribute
	config.UsernameAttribute = input.UsernameAttribute
	config.DisplayNameAttribute = input.DisplayNameAttribute
	config.ConnectionTimeoutSeconds = input.ConnectionTimeoutSeconds

	// Only update password if provided
	if input.BindPassword != "" {
		encrypted, err := sso.GetEncryptionService().Encrypt(input.BindPassword)
		if err != nil {
			return err
		}
		config.BindPasswordEncrypted = encrypted
	}

	return h.ssoRepo.UpdateLDAPConfig(ctx, config)
}

func (h *SSOAdminHandler) createSAMLConfig(ctx context.Context, providerID uuid.UUID, input *models.SAMLConfigInput) error {
	var encryptedKey, spCertificate string

	// Auto-generate key pair if not provided (always required for SAML signing)
	if input.SPPrivateKey == "" {
		privateKeyPEM, certPEM, err := generateSAMLSPKeyPair(input.SPEntityID)
		if err != nil {
			return fmt.Errorf("failed to generate SP key pair: %w", err)
		}
		encrypted, err := sso.GetEncryptionService().Encrypt(privateKeyPEM)
		if err != nil {
			return fmt.Errorf("failed to encrypt SP key: %w", err)
		}
		encryptedKey = encrypted
		spCertificate = certPEM
		debug.Info("Auto-generated SP key pair for SAML provider")
	} else {
		// Use provided key
		encrypted, err := sso.GetEncryptionService().Encrypt(input.SPPrivateKey)
		if err != nil {
			return err
		}
		encryptedKey = encrypted
		spCertificate = input.SPCertificate
	}

	config := &models.SAMLConfig{
		ID:                         uuid.New(),
		ProviderID:                 providerID,
		SPEntityID:                 input.SPEntityID,
		IDPEntityID:                input.IDPEntityID,
		IDPSSOURL:                  input.IDPSSOURL,
		IDPSLOURL:                  input.IDPSLOURL,
		IDPCertificate:             input.IDPCertificate,
		SPPrivateKeyEncrypted:      encryptedKey,
		SPCertificate:              spCertificate,
		SignRequests:               true, // Always enable signing since we have keys
		RequireSignedAssertions:    input.RequireSignedAssertions,
		RequireEncryptedAssertions: input.RequireEncryptedAssertions,
		NameIDFormat:               input.NameIDFormat,
		EmailAttribute:             input.EmailAttribute,
		UsernameAttribute:          input.UsernameAttribute,
		DisplayNameAttribute:       input.DisplayNameAttribute,
	}

	return h.ssoRepo.CreateSAMLConfig(ctx, config)
}

func (h *SSOAdminHandler) updateSAMLConfig(ctx context.Context, providerID uuid.UUID, input *models.SAMLConfigInput) error {
	config, err := h.ssoRepo.GetSAMLConfig(ctx, providerID)
	if err != nil {
		return err
	}

	// Update fields
	config.SPEntityID = input.SPEntityID
	config.IDPEntityID = input.IDPEntityID
	config.IDPSSOURL = input.IDPSSOURL
	config.IDPSLOURL = input.IDPSLOURL
	config.IDPCertificate = input.IDPCertificate
	config.RequireSignedAssertions = input.RequireSignedAssertions
	config.RequireEncryptedAssertions = input.RequireEncryptedAssertions
	config.NameIDFormat = input.NameIDFormat
	config.EmailAttribute = input.EmailAttribute
	config.UsernameAttribute = input.UsernameAttribute
	config.DisplayNameAttribute = input.DisplayNameAttribute

	// Always enable signing
	config.SignRequests = true

	// Update key if provided, or generate if missing
	if input.SPPrivateKey != "" {
		encrypted, err := sso.GetEncryptionService().Encrypt(input.SPPrivateKey)
		if err != nil {
			return err
		}
		config.SPPrivateKeyEncrypted = encrypted
		config.SPCertificate = input.SPCertificate
	} else if config.SPPrivateKeyEncrypted == "" {
		// Auto-generate key pair if existing config has no keys
		privateKeyPEM, certPEM, err := generateSAMLSPKeyPair(input.SPEntityID)
		if err != nil {
			return fmt.Errorf("failed to generate SP key pair: %w", err)
		}
		encrypted, err := sso.GetEncryptionService().Encrypt(privateKeyPEM)
		if err != nil {
			return fmt.Errorf("failed to encrypt SP key: %w", err)
		}
		config.SPPrivateKeyEncrypted = encrypted
		config.SPCertificate = certPEM
		debug.Info("Auto-generated SP key pair for existing SAML provider")
	}

	return h.ssoRepo.UpdateSAMLConfig(ctx, config)
}

func (h *SSOAdminHandler) createOAuthConfig(ctx context.Context, providerID uuid.UUID, input *models.OAuthConfigInput) error {
	// Encrypt client secret
	encryptedSecret := ""
	if input.ClientSecret != "" {
		encrypted, err := sso.GetEncryptionService().Encrypt(input.ClientSecret)
		if err != nil {
			return err
		}
		encryptedSecret = encrypted
	}

	config := &models.OAuthConfig{
		ID:                    uuid.New(),
		ProviderID:            providerID,
		IsOIDC:                input.IsOIDC,
		ClientID:              input.ClientID,
		ClientSecretEncrypted: encryptedSecret,
		DiscoveryURL:          input.DiscoveryURL,
		AuthorizationURL:      input.AuthorizationURL,
		TokenURL:              input.TokenURL,
		UserinfoURL:           input.UserinfoURL,
		JWKSURL:               input.JWKSURL,
		Scopes:                input.Scopes,
		EmailAttribute:        input.EmailAttribute,
		UsernameAttribute:     input.UsernameAttribute,
		DisplayNameAttribute:  input.DisplayNameAttribute,
		ExternalIDAttribute:   input.ExternalIDAttribute,
	}

	return h.ssoRepo.CreateOAuthConfig(ctx, config)
}

func (h *SSOAdminHandler) updateOAuthConfig(ctx context.Context, providerID uuid.UUID, input *models.OAuthConfigInput) error {
	config, err := h.ssoRepo.GetOAuthConfig(ctx, providerID)
	if err != nil {
		return err
	}

	// Update fields
	config.IsOIDC = input.IsOIDC
	config.ClientID = input.ClientID
	config.DiscoveryURL = input.DiscoveryURL
	config.AuthorizationURL = input.AuthorizationURL
	config.TokenURL = input.TokenURL
	config.UserinfoURL = input.UserinfoURL
	config.JWKSURL = input.JWKSURL
	config.Scopes = input.Scopes
	config.EmailAttribute = input.EmailAttribute
	config.UsernameAttribute = input.UsernameAttribute
	config.DisplayNameAttribute = input.DisplayNameAttribute
	config.ExternalIDAttribute = input.ExternalIDAttribute

	// Only update secret if provided
	if input.ClientSecret != "" {
		encrypted, err := sso.GetEncryptionService().Encrypt(input.ClientSecret)
		if err != nil {
			return err
		}
		config.ClientSecretEncrypted = encrypted
	}

	return h.ssoRepo.UpdateOAuthConfig(ctx, config)
}

// generateSAMLSPKeyPair generates an RSA key pair and self-signed certificate for SAML SP signing
func generateSAMLSPKeyPair(commonName string) (privateKeyPEM, certificatePEM string, err error) {
	// Generate 2048-bit RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"KrakenHashes SAML SP"},
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(10, 0, 0), // 10 years validity
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode private key to PEM
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}
	privateKeyPEM = string(pem.EncodeToMemory(privateKeyBlock))

	// Encode certificate to PEM
	certBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	}
	certificatePEM = string(pem.EncodeToMemory(certBlock))

	return privateKeyPEM, certificatePEM, nil
}
