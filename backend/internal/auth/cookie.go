package auth

import (
	"net/http"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// GetCookieDomain extracts the domain from the request host for cookie setting
func GetCookieDomain(host string) string {
	debug.Debug("Getting cookie domain from host: %s", host)

	// Always strip port number since frontend and backend are on different ports
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	// For development environments (localhost/127.0.0.1), don't set domain
	if host == "localhost" || host == "127.0.0.1" {
		debug.Debug("Development environment detected, not setting cookie domain")
		return ""
	}

	debug.Debug("Using cookie domain: %s", host)
	return host
}

// SetAuthCookie sets the authentication cookie with the given token
// maxAge is in seconds
func SetAuthCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	debug.Debug("[COOKIE] Setting auth cookie - MaxAge: %d", maxAge)

	// Check if this is a development environment
	isDevelopment := strings.Contains(r.Host, "localhost") || strings.Contains(r.Host, "127.0.0.1")

	// For cross-port development (frontend:3000, backend:31337) we need special handling
	var sameSite http.SameSite
	var secure bool

	if isDevelopment {
		// For localhost development with HTTPS, use Lax for better compatibility
		sameSite = http.SameSiteLaxMode
		secure = true // We're using HTTPS even in development
		debug.Info("[COOKIE] Development environment: using SameSite=Lax, Secure=true for HTTPS localhost")
	} else {
		// Production settings
		sameSite = http.SameSiteLaxMode
		secure = true
		debug.Debug("[COOKIE] Production environment: using SameSite=Lax, Secure=true")
	}

	cookie := &http.Cookie{
		Name:     "token",
		Value:    token,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Path:     "/",
		MaxAge:   maxAge,
	}

	// For development, don't set domain to allow cross-port cookie sharing
	domain := GetCookieDomain(r.Host)
	if domain != "" {
		cookie.Domain = domain
		debug.Debug("[COOKIE] Setting cookie domain: %s", domain)
	} else {
		debug.Info("[COOKIE] No domain set for cookie (allows cross-port sharing in development)")
	}

	// Log the complete cookie configuration for debugging
	debug.Info("[COOKIE] Cookie configuration: name=%s, secure=%v, sameSite=%v, httpOnly=%v, path=%s, domain=%s, maxAge=%d",
		cookie.Name, cookie.Secure, cookie.SameSite, cookie.HttpOnly, cookie.Path, cookie.Domain, cookie.MaxAge)

	http.SetCookie(w, cookie)
	debug.Info("[COOKIE] Auth cookie set successfully")
}

// GetClientInfo extracts client IP address and User-Agent from request
func GetClientInfo(r *http.Request) (ipAddress string, userAgent string) {
	// Try to get real IP from X-Forwarded-For header (for proxied requests)
	ipAddress = r.Header.Get("X-Forwarded-For")
	if ipAddress != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		if idx := strings.Index(ipAddress, ","); idx != -1 {
			ipAddress = strings.TrimSpace(ipAddress[:idx])
		}
	}

	// Fallback to X-Real-IP header
	if ipAddress == "" {
		ipAddress = r.Header.Get("X-Real-IP")
	}

	// Fallback to RemoteAddr
	if ipAddress == "" {
		ipAddress = r.RemoteAddr
		// Remove port if present
		if idx := strings.LastIndex(ipAddress, ":"); idx != -1 {
			ipAddress = ipAddress[:idx]
		}
	}

	// Get User-Agent
	userAgent = r.Header.Get("User-Agent")
	if userAgent == "" {
		userAgent = "Unknown"
	}

	return ipAddress, userAgent
}

// GetClientIP extracts only the client IP address from request
func GetClientIP(r *http.Request) string {
	ipAddress, _ := GetClientInfo(r)
	return ipAddress
}
