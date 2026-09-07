/*
 * Package config provides configuration and URL handling for the KrakenHashes agent.
 */
package config

import (
	"fmt"
	"net/url"
	"os"

	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

// URLConfig holds the server URL configuration
type URLConfig struct {
	WebSocketURL string // WebSocket URL (ws:// or wss://)
	BaseURL      string // Base HTTP URL (http:// or https://)
	HTTPPort     string // Plain HTTP port for CA certificate
}

// NewURLConfig creates a new URL configuration from environment variables
func NewURLConfig() *URLConfig {
	// Get host and port
	host := os.Getenv("KH_HOST")
	if host == "" {
		host = "localhost" // Default for development
		debug.Debug("Using default host: %s", host)
	}

	port := os.Getenv("KH_PORT")
	if port == "" {
		port = "31337" // Default for development
		debug.Debug("Using default port: %s", port)
	}

	// Get HTTP port for CA certificate
	httpPort := os.Getenv("KH_HTTP_PORT")
	if httpPort == "" {
		httpPort = "1337" // Default to match backend's HTTP server
		debug.Debug("Using default HTTP port: %s", httpPort)
	}

	// Check TLS setting
	useTLS := os.Getenv("USE_TLS") == "true"

	// Construct base URLs
	wsProtocol := map[bool]string{true: "wss", false: "ws"}[useTLS]
	httpProtocol := map[bool]string{true: "https", false: "http"}[useTLS]

	wsURL := fmt.Sprintf("%s://%s:%s", wsProtocol, host, port)
	baseURL := fmt.Sprintf("%s://%s:%s", httpProtocol, host, port)

	debug.Info("URL Configuration:")
	debug.Info("  Host: %s", host)
	debug.Info("  Port: %s", port)
	debug.Info("  HTTP Port: %s", httpPort)
	debug.Info("  TLS: %v", useTLS)
	debug.Info("  WebSocket URL: %s", wsURL)
	debug.Info("  Base URL: %s", baseURL)

	return &URLConfig{
		WebSocketURL: wsURL,
		BaseURL:      baseURL,
		HTTPPort:     httpPort,
	}
}

// GetWebSocketURL returns the WebSocket URL for agent connections
func (c *URLConfig) GetWebSocketURL() string {
	return fmt.Sprintf("%s/ws/agent", c.WebSocketURL)
}

// GetRegistrationURL returns the URL for agent registration
func (c *URLConfig) GetRegistrationURL() string {
	return fmt.Sprintf("%s/api/agent/register", c.BaseURL)
}

// GetAPIBaseURL returns the base URL for API endpoints
func (c *URLConfig) GetAPIBaseURL() string {
	return fmt.Sprintf("%s/api", c.BaseURL)
}

// GetTLSFailureReportURL returns the URL for reporting a TLS handshake failure.
//
// Always plain HTTP on the HTTP port. This endpoint exists precisely for the case
// where the HTTPS channel cannot be trusted, so it must not depend on it.
func (c *URLConfig) GetTLSFailureReportURL() string {
	return fmt.Sprintf("http://%s:%s/api/agent/tls-failure", c.hostname(), c.HTTPPort)
}

// hostname returns the configured host without its port.
func (c *URLConfig) hostname() string {
	parsedURL, err := url.Parse(c.BaseURL)
	if err != nil {
		debug.Error("Failed to parse base URL: %v", err)
		return "localhost"
	}
	return parsedURL.Hostname()
}

// GetCACertURL returns the URL for downloading the CA certificate
// This endpoint should always be HTTP since we don't have the CA cert yet
func (c *URLConfig) GetCACertURL() string {
	// Parse the base URL to get host
	parsedURL, err := url.Parse(c.BaseURL)
	if err != nil {
		debug.Error("Failed to parse base URL: %v", err)
		return fmt.Sprintf("http://localhost:%s/ca.crt", c.HTTPPort)
	}

	// Get host without port
	host := parsedURL.Hostname()

	// Always use HTTP and the HTTP port for CA cert download
	return fmt.Sprintf("http://%s:%s/ca.crt", host, c.HTTPPort)
}
