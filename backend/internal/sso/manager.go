package sso

import (
	"context"
	"fmt"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// Repository interface for SSO data access
type Repository interface {
	// Provider operations
	GetProvider(ctx context.Context, id uuid.UUID) (*models.SSOProvider, error)
	GetProviderByType(ctx context.Context, providerType models.ProviderType) ([]*models.SSOProvider, error)
	GetEnabledProviders(ctx context.Context) ([]*models.EnabledSSOProvider, error)
	ListProviders(ctx context.Context) ([]*models.SSOProvider, error)
	CreateProvider(ctx context.Context, provider *models.SSOProvider) error
	UpdateProvider(ctx context.Context, provider *models.SSOProvider) error
	DeleteProvider(ctx context.Context, id uuid.UUID) error

	// Config operations
	GetLDAPConfig(ctx context.Context, providerID uuid.UUID) (*models.LDAPConfig, error)
	CreateLDAPConfig(ctx context.Context, config *models.LDAPConfig) error
	UpdateLDAPConfig(ctx context.Context, config *models.LDAPConfig) error

	GetSAMLConfig(ctx context.Context, providerID uuid.UUID) (*models.SAMLConfig, error)
	CreateSAMLConfig(ctx context.Context, config *models.SAMLConfig) error
	UpdateSAMLConfig(ctx context.Context, config *models.SAMLConfig) error

	GetOAuthConfig(ctx context.Context, providerID uuid.UUID) (*models.OAuthConfig, error)
	CreateOAuthConfig(ctx context.Context, config *models.OAuthConfig) error
	UpdateOAuthConfig(ctx context.Context, config *models.OAuthConfig) error

	// Identity operations
	GetUserIdentity(ctx context.Context, providerID uuid.UUID, externalID string) (*models.UserIdentity, error)
	GetUserIdentityByEmail(ctx context.Context, providerID uuid.UUID, email string) (*models.UserIdentity, error)
	GetUserIdentities(ctx context.Context, userID uuid.UUID) ([]*models.UserIdentityWithProvider, error)
	CreateUserIdentity(ctx context.Context, identity *models.UserIdentity) error
	UpdateUserIdentity(ctx context.Context, identity *models.UserIdentity) error
	DeleteUserIdentity(ctx context.Context, id uuid.UUID) error

	// Pending auth state operations
	StorePendingOAuth(ctx context.Context, pending *models.PendingOAuthAuthentication) error
	GetPendingOAuth(ctx context.Context, state string) (*models.PendingOAuthAuthentication, error)
	DeletePendingOAuth(ctx context.Context, state string) error

	StorePendingSAML(ctx context.Context, pending *models.PendingSAMLAuthentication) error
	GetPendingSAML(ctx context.Context, requestID string) (*models.PendingSAMLAuthentication, error)
	DeletePendingSAML(ctx context.Context, requestID string) error

	// Settings operations
	GetSSOSettings(ctx context.Context) (*models.SSOAuthSettings, error)
	UpdateSSOSettings(ctx context.Context, settings *models.SSOAuthSettings) error
}

// ProviderFactory creates provider instances
type ProviderFactory func(config *ProviderConfig) (Provider, error)

// Manager orchestrates SSO providers and handles authentication flows
type Manager struct {
	repo       Repository
	providers  map[uuid.UUID]Provider
	factories  map[models.ProviderType]ProviderFactory
	encryption *EncryptionService
	mu         sync.RWMutex
}

// NewManager creates a new SSO manager
func NewManager(repo Repository) *Manager {
	m := &Manager{
		repo:       repo,
		providers:  make(map[uuid.UUID]Provider),
		factories:  make(map[models.ProviderType]ProviderFactory),
		encryption: GetEncryptionService(),
	}
	return m
}

// RegisterFactory registers a provider factory for a given provider type
func (m *Manager) RegisterFactory(providerType models.ProviderType, factory ProviderFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[providerType] = factory
	debug.Info("Registered SSO provider factory for type: %s", providerType)
}

// LoadProviders loads all enabled providers from the database
func (m *Manager) LoadProviders(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	providers, err := m.repo.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("failed to list providers: %w", err)
	}

	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}

		if err := m.loadProviderLocked(ctx, provider); err != nil {
			debug.Warning("Failed to load provider %s (%s): %v", provider.Name, provider.ID, err)
			continue
		}
	}

	debug.Info("Loaded %d SSO providers", len(m.providers))
	return nil
}

// loadProviderLocked loads a single provider (must be called with lock held)
func (m *Manager) loadProviderLocked(ctx context.Context, provider *models.SSOProvider) error {
	factory, ok := m.factories[provider.ProviderType]
	if !ok {
		return fmt.Errorf("no factory registered for provider type: %s", provider.ProviderType)
	}

	config := &ProviderConfig{Provider: provider}

	// Load type-specific config
	switch provider.ProviderType {
	case models.ProviderTypeLDAP:
		ldapConfig, err := m.repo.GetLDAPConfig(ctx, provider.ID)
		if err != nil {
			return fmt.Errorf("failed to get LDAP config: %w", err)
		}
		config.LDAPConfig = ldapConfig

	case models.ProviderTypeSAML:
		samlConfig, err := m.repo.GetSAMLConfig(ctx, provider.ID)
		if err != nil {
			return fmt.Errorf("failed to get SAML config: %w", err)
		}
		config.SAMLConfig = samlConfig

	case models.ProviderTypeOIDC, models.ProviderTypeOAuth2:
		oauthConfig, err := m.repo.GetOAuthConfig(ctx, provider.ID)
		if err != nil {
			return fmt.Errorf("failed to get OAuth config: %w", err)
		}
		config.OAuthConfig = oauthConfig
	}

	p, err := factory(config)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	m.providers[provider.ID] = p
	debug.Info("Loaded SSO provider: %s (%s)", provider.Name, provider.ProviderType)
	return nil
}

// GetProvider returns a loaded provider by ID
func (m *Manager) GetProvider(id uuid.UUID) (Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.providers[id]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return p, nil
}

// ReloadProvider reloads a specific provider from the database
func (m *Manager) ReloadProvider(ctx context.Context, id uuid.UUID) error {
	provider, err := m.repo.GetProvider(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get provider: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove existing provider if any
	delete(m.providers, id)

	// Only load if enabled
	if provider.Enabled {
		return m.loadProviderLocked(ctx, provider)
	}

	return nil
}

// UnloadProvider removes a provider from the loaded set
func (m *Manager) UnloadProvider(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.providers, id)
}

// GetEnabledProviders returns all enabled providers for the login page
func (m *Manager) GetEnabledProviders(ctx context.Context) ([]*models.EnabledSSOProvider, error) {
	settings, err := m.repo.GetSSOSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSO settings: %w", err)
	}

	providers, err := m.repo.GetEnabledProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled providers: %w", err)
	}

	// Filter by global type enablement
	var filtered []*models.EnabledSSOProvider
	for _, p := range providers {
		switch p.ProviderType {
		case models.ProviderTypeLDAP:
			if settings.LDAPAuthEnabled {
				filtered = append(filtered, p)
			}
		case models.ProviderTypeSAML:
			if settings.SAMLAuthEnabled {
				filtered = append(filtered, p)
			}
		case models.ProviderTypeOIDC, models.ProviderTypeOAuth2:
			if settings.OAuthAuthEnabled {
				filtered = append(filtered, p)
			}
		}
	}

	return filtered, nil
}

// Authenticate handles authentication via a specific provider
func (m *Manager) Authenticate(ctx context.Context, providerID uuid.UUID, req *AuthRequest) (*models.AuthResult, error) {
	// Check if provider type is enabled globally
	settings, err := m.repo.GetSSOSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSO settings: %w", err)
	}

	provider, err := m.GetProvider(providerID)
	if err != nil {
		return nil, err
	}

	// Check global type enablement
	switch provider.Type() {
	case models.ProviderTypeLDAP:
		if !settings.LDAPAuthEnabled {
			return nil, ErrProviderTypeDisabled
		}
	case models.ProviderTypeSAML:
		if !settings.SAMLAuthEnabled {
			return nil, ErrProviderTypeDisabled
		}
	case models.ProviderTypeOIDC, models.ProviderTypeOAuth2:
		if !settings.OAuthAuthEnabled {
			return nil, ErrProviderTypeDisabled
		}
	}

	return provider.Authenticate(ctx, req)
}

// GetStartURL returns the URL to redirect the user to for authentication
func (m *Manager) GetStartURL(ctx context.Context, providerID uuid.UUID, redirectURI string) (string, error) {
	provider, err := m.GetProvider(providerID)
	if err != nil {
		return "", err
	}

	return provider.GetStartURL(ctx, redirectURI)
}

// HandleCallback processes the callback from an identity provider
func (m *Manager) HandleCallback(ctx context.Context, providerID uuid.UUID, req *CallbackRequest) (*models.AuthResult, error) {
	provider, err := m.GetProvider(providerID)
	if err != nil {
		return nil, err
	}

	return provider.HandleCallback(ctx, req)
}

// TestConnection tests the connection to a provider
func (m *Manager) TestConnection(ctx context.Context, providerID uuid.UUID) error {
	provider, err := m.GetProvider(providerID)
	if err != nil {
		return err
	}

	return provider.TestConnection(ctx)
}

// GetSSOSettings returns the global SSO settings
func (m *Manager) GetSSOSettings(ctx context.Context) (*models.SSOAuthSettings, error) {
	return m.repo.GetSSOSettings(ctx)
}

// UpdateSSOSettings updates the global SSO settings
func (m *Manager) UpdateSSOSettings(ctx context.Context, settings *models.SSOAuthSettings) error {
	return m.repo.UpdateSSOSettings(ctx, settings)
}

// IsLocalAuthEnabled checks if local authentication is enabled
func (m *Manager) IsLocalAuthEnabled(ctx context.Context) (bool, error) {
	settings, err := m.repo.GetSSOSettings(ctx)
	if err != nil {
		return false, err
	}
	return settings.LocalAuthEnabled, nil
}

// LinkIdentity links an external identity to a user
func (m *Manager) LinkIdentity(ctx context.Context, userID uuid.UUID, identity *models.ExternalIdentity) (*models.UserIdentity, error) {
	ui := &models.UserIdentity{
		ID:                  uuid.New(),
		UserID:              userID,
		ProviderID:          identity.ProviderID,
		ExternalID:          identity.ExternalID,
		ExternalEmail:       identity.Email,
		ExternalUsername:    identity.Username,
		ExternalDisplayName: identity.DisplayName,
		Metadata:            identity.Metadata,
	}

	if err := m.repo.CreateUserIdentity(ctx, ui); err != nil {
		return nil, fmt.Errorf("failed to create user identity: %w", err)
	}

	return ui, nil
}

// GetUserIdentities returns all linked identities for a user
func (m *Manager) GetUserIdentities(ctx context.Context, userID uuid.UUID) ([]*models.UserIdentityWithProvider, error) {
	return m.repo.GetUserIdentities(ctx, userID)
}

// UnlinkIdentity removes a linked identity
func (m *Manager) UnlinkIdentity(ctx context.Context, identityID uuid.UUID) error {
	return m.repo.DeleteUserIdentity(ctx, identityID)
}
