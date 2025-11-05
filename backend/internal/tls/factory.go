package tls

import (
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// NewProvider creates a new TLS provider based on the configuration
func NewProvider(config *ProviderConfig) (Provider, error) {
	debug.Info("Creating new TLS provider with mode: %s", config.Mode)

	switch config.Mode {
	case ModeSelfSigned:
		debug.Debug("Initializing self-signed certificate provider")
		return NewSelfSignedProvider(config), nil
	case ModeProvided:
		debug.Debug("Initializing user-provided certificate provider")
		return NewProvidedProvider(config), nil
	case ModeCertbot:
		debug.Debug("Initializing certbot certificate provider")
		return NewCertbotProvider(config), nil
	default:
		err := fmt.Errorf("unsupported TLS mode: %s", config.Mode)
		debug.Error("Failed to create provider: %v", err)
		return nil, err
	}
}

// InitializeProvider creates and initializes a new TLS provider
func InitializeProvider(appConfig *config.Config) (Provider, error) {
	debug.Info("Loading TLS provider configuration")
	config, err := LoadProviderConfig(appConfig)
	if err != nil {
		debug.Error("Failed to load TLS provider configuration: %v", err)
		return nil, fmt.Errorf("failed to load TLS provider configuration: %w", err)
	}
	debug.Debug("Successfully loaded TLS configuration: mode=%s, certs_dir=%s", config.Mode, config.CertsDir)

	debug.Info("Creating TLS provider")
	provider, err := NewProvider(config)
	if err != nil {
		debug.Error("Failed to create TLS provider: %v", err)
		return nil, fmt.Errorf("failed to create TLS provider: %w", err)
	}
	debug.Debug("Successfully created TLS provider")

	debug.Info("Initializing TLS provider")
	if err := provider.Initialize(); err != nil {
		debug.Error("Failed to initialize TLS provider: %v", err)
		return nil, fmt.Errorf("failed to initialize TLS provider: %w", err)
	}
	debug.Info("Successfully initialized TLS provider")

	return provider, nil
}
