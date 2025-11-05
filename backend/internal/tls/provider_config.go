package tls

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/env"
)

// LoadProviderConfig loads the TLS provider configuration from environment variables
func LoadProviderConfig(appConfig *config.Config) (*ProviderConfig, error) {
	debug.Info("Loading TLS provider configuration from environment")

	// Load TLS mode
	mode := ProviderMode(env.GetOrDefault("KH_TLS_MODE", string(ModeSelfSigned)))
	debug.Debug("TLS mode: %s", mode)

	// Get certificates directory from app config
	certsDir := appConfig.GetCertsDir()
	debug.Debug("Using certificates directory: %s", certsDir)

	// Get additional DNS names and IP addresses
	dnsNames, ipAddresses := appConfig.GetTLSConfig()
	debug.Debug("Additional DNS names: %v", dnsNames)
	debug.Debug("Additional IP addresses: %v", ipAddresses)

	// Create validity struct
	validity := struct {
		Server int
		CA     int
	}{
		Server: 365,  // Default 1 year
		CA:     3650, // Default 10 years
	}

	// Create CA details
	caDetails := &CertificateAuthority{
		Country:            env.GetOrDefault("KH_CA_COUNTRY", "US"),
		Organization:       env.GetOrDefault("KH_CA_ORGANIZATION", "KrakenHashes"),
		OrganizationalUnit: env.GetOrDefault("KH_CA_ORGANIZATIONAL_UNIT", "KrakenHashes CA"),
		CommonName:         env.GetOrDefault("KH_CA_COMMON_NAME", "KrakenHashes Root CA"),
	}

	config := &ProviderConfig{
		Mode:                  mode,
		CertsDir:              certsDir,
		CertFile:              env.GetOrDefault("KH_CERT_FILE", filepath.Join(certsDir, "server.crt")),
		KeyFile:               env.GetOrDefault("KH_KEY_FILE", filepath.Join(certsDir, "server.key")),
		CAFile:                env.GetOrDefault("KH_CA_FILE", filepath.Join(certsDir, "ca.crt")),
		KeySize:               4096, // Default key size
		Validity:              validity,
		CADetails:             caDetails,
		AdditionalDNSNames:    dnsNames,
		AdditionalIPAddresses: ipAddresses,
		Host:                  appConfig.Host,
	}
	debug.Debug("Base configuration loaded: certs_dir=%s", config.CertsDir)

	// Load key size from environment if provided
	if keySize := env.GetOrDefault("KH_KEY_SIZE", "4096"); keySize != "" {
		size, err := strconv.Atoi(keySize)
		if err != nil {
			debug.Error("Invalid key size: %v", err)
			return nil, fmt.Errorf("invalid key size: %w", err)
		}
		config.KeySize = size
	}
	debug.Debug("Key size: %d", config.KeySize)

	// Load validity periods from environment if provided
	if serverValidity := env.GetOrDefault("KH_SERVER_CERT_VALIDITY", "365"); serverValidity != "" {
		days, err := strconv.Atoi(serverValidity)
		if err != nil {
			debug.Error("Invalid server certificate validity: %v", err)
			return nil, fmt.Errorf("invalid server certificate validity: %w", err)
		}
		config.Validity.Server = days
	}

	if caValidity := env.GetOrDefault("KH_CA_CERT_VALIDITY", "3650"); caValidity != "" {
		days, err := strconv.Atoi(caValidity)
		if err != nil {
			debug.Error("Invalid CA certificate validity: %v", err)
			return nil, fmt.Errorf("invalid CA certificate validity: %w", err)
		}
		config.Validity.CA = days
	}
	debug.Debug("Certificate validity - Server: %d days, CA: %d days",
		config.Validity.Server, config.Validity.CA)

	// Load mode-specific configuration
	debug.Info("Loading mode-specific configuration for: %s", mode)
	switch mode {
	case ModeSelfSigned:
		debug.Debug("Loading self-signed certificate configuration")
		// CA details already set above

	case ModeProvided:
		debug.Debug("Loading provided certificate configuration")
		if config.CertFile == "" || config.KeyFile == "" {
			debug.Error("Missing required certificate files for provided mode")
			return nil, fmt.Errorf("KH_CERT_FILE and KH_KEY_FILE are required for provided mode")
		}
		debug.Debug("Certificate files - Cert: %s, Key: %s, CA: %s",
			config.CertFile,
			config.KeyFile,
			config.CAFile)

	case ModeCertbot:
		debug.Debug("Loading certbot configuration")
		domain := env.GetOrDefault("KH_CERTBOT_DOMAIN", "")
		email := env.GetOrDefault("KH_CERTBOT_EMAIL", "")
		certbotConfig := &CertbotConfig{
			Domain:    domain,
			Email:     email,
			Staging:   env.GetBool("KH_CERTBOT_STAGING"),
			AutoRenew: env.GetBool("KH_CERTBOT_AUTO_RENEW"),
			RenewHook: env.GetOrDefault("KH_CERTBOT_RENEW_HOOK", ""),
			Server:    env.GetOrDefault("KH_CERTBOT_SERVER", ""),
			ExtraArgs: env.GetOrDefault("KH_CERTBOT_EXTRA_ARGS", ""),
		}

		if domain == "" || email == "" {
			debug.Error("Missing required certbot configuration")
			return nil, fmt.Errorf("KH_CERTBOT_DOMAIN and KH_CERTBOT_EMAIL are required for certbot mode")
		}
		debug.Debug("Certbot configuration - Domain: %s, Email: %s, Staging: %v, AutoRenew: %v",
			domain,
			email,
			certbotConfig.Staging,
			certbotConfig.AutoRenew)

		config.CertbotConfig = certbotConfig

	default:
		err := fmt.Errorf("unsupported TLS mode: %s", mode)
		debug.Error("Configuration error: %v", err)
		return nil, err
	}

	if config.CertFile == "" {
		config.CertFile = filepath.Join(certsDir, "cert.pem")
	}
	if config.KeyFile == "" {
		config.KeyFile = filepath.Join(certsDir, "key.pem")
	}
	if config.CAFile == "" {
		config.CAFile = filepath.Join(certsDir, "ca.pem")
	}

	debug.Info("Successfully loaded TLS provider configuration")
	return config, nil
}
