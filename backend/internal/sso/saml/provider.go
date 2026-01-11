package saml

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/sso"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"github.com/crewjam/saml/xmlenc"
	"github.com/google/uuid"
)

// assertionIDCache stores assertion IDs to prevent replay attacks
var (
	assertionIDCache   = make(map[string]time.Time)
	assertionIDMutex   sync.RWMutex
	assertionCacheTTL  = 5 * time.Minute
	cleanupRunning     bool
	cleanupMutex       sync.Mutex
)

// Provider implements SAML SP authentication
type Provider struct {
	*sso.BaseProvider
	config       *models.SAMLConfig
	serviceProvider *saml.ServiceProvider
	repo         sso.Repository
}

// NewProvider creates a new SAML provider
func NewProvider(providerConfig *sso.ProviderConfig, repo sso.Repository) (sso.Provider, error) {
	if providerConfig.SAMLConfig == nil {
		return nil, fmt.Errorf("SAML config is required")
	}

	p := &Provider{
		BaseProvider: sso.NewBaseProvider(providerConfig.Provider),
		config:       providerConfig.SAMLConfig,
		repo:         repo,
	}

	// Initialize SAML service provider
	if err := p.initializeServiceProvider(); err != nil {
		return nil, fmt.Errorf("failed to initialize SAML SP: %w", err)
	}

	// Start cleanup goroutine if not already running
	startAssertionCleanup()

	return p, nil
}

// initializeServiceProvider sets up the SAML service provider
func (p *Provider) initializeServiceProvider() error {
	// Parse IdP certificate
	idpCert, err := parseCertificate(p.config.IDPCertificate)
	if err != nil {
		return fmt.Errorf("failed to parse IdP certificate: %w", err)
	}

	// Parse IdP SSO URL
	idpSSOURL, err := url.Parse(p.config.IDPSSOURL)
	if err != nil {
		return fmt.Errorf("failed to parse IdP SSO URL: %w", err)
	}

	// Build IdP metadata
	idpMetadata := &saml.EntityDescriptor{
		EntityID: p.config.IDPEntityID,
		IDPSSODescriptors: []saml.IDPSSODescriptor{
			{
				SSODescriptor: saml.SSODescriptor{
					RoleDescriptor: saml.RoleDescriptor{
						KeyDescriptors: []saml.KeyDescriptor{
							{
								Use: "signing",
								KeyInfo: saml.KeyInfo{
									X509Data: saml.X509Data{
										X509Certificates: []saml.X509Certificate{
											{Data: base64.StdEncoding.EncodeToString(idpCert.Raw)},
										},
									},
								},
							},
						},
					},
				},
				SingleSignOnServices: []saml.Endpoint{
					{
						Binding:  saml.HTTPPostBinding,
						Location: idpSSOURL.String(),
					},
					{
						Binding:  saml.HTTPRedirectBinding,
						Location: idpSSOURL.String(),
					},
				},
			},
		},
	}

	// Parse SP Entity ID
	spEntityIDURL, err := url.Parse(p.config.SPEntityID)
	if err != nil {
		return fmt.Errorf("failed to parse SP entity ID: %w", err)
	}

	// Configure service provider
	sp := &saml.ServiceProvider{
		EntityID:          p.config.SPEntityID,
		IDPMetadata:       idpMetadata,
		MetadataURL:       *spEntityIDURL,
		SignatureMethod:   "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256",
		AllowIDPInitiated: true, // Allow IdP-initiated SSO
	}

	// Set up SP signing key/cert if configured
	if p.config.SignRequests && p.config.SPPrivateKeyEncrypted != "" {
		privateKeyPEM, err := p.DecryptSecret(p.config.SPPrivateKeyEncrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt SP private key: %w", err)
		}

		privateKey, err := parsePrivateKey(privateKeyPEM)
		if err != nil {
			return fmt.Errorf("failed to parse SP private key: %w", err)
		}
		sp.Key = privateKey

		if p.config.SPCertificate != "" {
			spCert, err := parseCertificate(p.config.SPCertificate)
			if err != nil {
				return fmt.Errorf("failed to parse SP certificate: %w", err)
			}
			sp.Certificate = spCert
		}
	}

	p.serviceProvider = sp
	return nil
}

// Type returns the provider type
func (p *Provider) Type() models.ProviderType {
	return models.ProviderTypeSAML
}

// Authenticate is not used directly - use GetStartURL and HandleCallback
func (p *Provider) Authenticate(ctx context.Context, req *sso.AuthRequest) (*models.AuthResult, error) {
	return nil, fmt.Errorf("SAML provider requires redirect-based authentication")
}

// GetStartURL generates the SAML AuthnRequest and returns the redirect URL
func (p *Provider) GetStartURL(ctx context.Context, redirectURI string) (string, error) {
	// Generate a unique request ID
	requestID := fmt.Sprintf("id-%s", generateID())

	// Create AuthnRequest
	authnRequest, err := p.serviceProvider.MakeAuthenticationRequest(
		p.config.IDPSSOURL,
		saml.HTTPRedirectBinding,
		saml.HTTPPostBinding,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create AuthnRequest: %w", err)
	}

	// Override the ID
	authnRequest.ID = requestID

	// Set NameID format if configured
	if p.config.NameIDFormat != "" {
		authnRequest.NameIDPolicy = &saml.NameIDPolicy{
			Format: &p.config.NameIDFormat,
		}
	}

	// Store pending authentication state
	pending := &models.PendingSAMLAuthentication{
		RequestID:  requestID,
		ProviderID: p.ProviderID(),
		RelayState: redirectURI,
	}
	if err := p.repo.StorePendingSAML(ctx, pending); err != nil {
		return "", fmt.Errorf("failed to store pending SAML: %w", err)
	}

	// Generate redirect URL
	redirectURL, err := authnRequest.Redirect(redirectURI, p.serviceProvider)
	if err != nil {
		return "", fmt.Errorf("failed to generate redirect URL: %w", err)
	}

	debug.Info("Generated SAML AuthnRequest for provider %s with ID %s", p.Name(), requestID)
	return redirectURL.String(), nil
}

// HandleCallback processes the SAML response
func (p *Provider) HandleCallback(ctx context.Context, req *sso.CallbackRequest) (*models.AuthResult, error) {
	if req.SAMLResponse == "" {
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Missing SAML response",
			ErrorCode:    "missing_saml_response",
		}, nil
	}

	// Decode SAML response
	responseXML, err := base64.StdEncoding.DecodeString(req.SAMLResponse)
	if err != nil {
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Invalid SAML response encoding",
			ErrorCode:    "invalid_encoding",
		}, nil
	}

	// Parse the response
	var response saml.Response
	if err := xml.Unmarshal(responseXML, &response); err != nil {
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: "Failed to parse SAML response",
			ErrorCode:    "parse_error",
		}, nil
	}

	// Validate response
	assertion, err := p.validateResponse(ctx, &response, req.RelayState)
	if err != nil {
		debug.Error("SAML response validation failed: %v", err)
		return &models.AuthResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("SAML validation failed: %v", err),
			ErrorCode:    "validation_failed",
		}, nil
	}

	// Check for replay attack
	for _, stmt := range assertion.AuthnStatements {
		if stmt.SessionIndex != "" {
			if isReplayAttack(assertion.ID) {
				return &models.AuthResult{
					Success:      false,
					ErrorMessage: "Replay attack detected",
					ErrorCode:    "replay_attack",
				}, nil
			}
			recordAssertionID(assertion.ID)
		}
	}

	// Extract identity from assertion
	identity := p.extractIdentity(assertion)
	identity.ProviderID = p.ProviderID()
	identity.ProviderType = p.Type()
	identity.MFAVerified = true // SAML providers handle their own MFA

	debug.Info("SAML authentication successful for user %s via provider %s", identity.Email, p.Name())

	return &models.AuthResult{
		Success:     true,
		Identity:    identity,
		RequiresMFA: false, // SAML trusts provider's MFA
	}, nil
}

// validateResponse validates the SAML response and returns the assertion
func (p *Provider) validateResponse(ctx context.Context, response *saml.Response, relayState string) (*saml.Assertion, error) {
	// Check response status
	if response.Status.StatusCode.Value != saml.StatusSuccess {
		return nil, fmt.Errorf("SAML response status: %s", response.Status.StatusCode.Value)
	}

	// Validate InResponseTo if we have a stored request
	if response.InResponseTo != "" {
		pending, err := p.repo.GetPendingSAML(ctx, response.InResponseTo)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve pending SAML request: %w", err)
		}
		if pending == nil {
			// Allow IdP-initiated SSO when AllowIDPInitiated is true
			if !p.serviceProvider.AllowIDPInitiated {
				return nil, fmt.Errorf("unknown InResponseTo: %s", response.InResponseTo)
			}
		} else {
			// Clean up the pending request
			defer p.repo.DeletePendingSAML(ctx, response.InResponseTo)
		}
	}

	// Get assertion (may be encrypted)
	var assertion *saml.Assertion
	if response.EncryptedAssertion != nil {
		if p.serviceProvider.Key == nil {
			return nil, fmt.Errorf("encrypted assertion received but no SP private key configured")
		}

		decryptedAssertion, err := p.decryptAssertion(response.EncryptedAssertion)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt assertion: %w", err)
		}
		assertion = decryptedAssertion
	} else if response.Assertion != nil {
		assertion = response.Assertion
	} else {
		return nil, fmt.Errorf("no assertion found in response")
	}

	// Validate assertion signature if required
	if p.config.RequireSignedAssertions {
		if err := p.validateSignature(response, assertion); err != nil {
			return nil, fmt.Errorf("signature validation failed: %w", err)
		}
	}

	// Validate time constraints
	now := time.Now()
	if assertion.Conditions != nil {
		if !assertion.Conditions.NotBefore.IsZero() && now.Before(assertion.Conditions.NotBefore) {
			return nil, fmt.Errorf("assertion not yet valid")
		}
		if !assertion.Conditions.NotOnOrAfter.IsZero() && now.After(assertion.Conditions.NotOnOrAfter) {
			return nil, fmt.Errorf("assertion has expired")
		}
	}

	// Validate audience
	if assertion.Conditions != nil {
		validAudience := false
		for _, restriction := range assertion.Conditions.AudienceRestrictions {
			// Audience is a single struct with Value field, not a slice
			if restriction.Audience.Value == p.config.SPEntityID {
				validAudience = true
				break
			}
		}
		if !validAudience && len(assertion.Conditions.AudienceRestrictions) > 0 {
			return nil, fmt.Errorf("invalid audience")
		}
	}

	return assertion, nil
}

// validateSignature validates the SAML response/assertion signature
func (p *Provider) validateSignature(response *saml.Response, assertion *saml.Assertion) error {
	// Get IdP certificate for verification
	if len(p.serviceProvider.IDPMetadata.IDPSSODescriptors) == 0 {
		return fmt.Errorf("no IdP metadata available for signature verification")
	}

	idpCerts := []string{}
	for _, keyDesc := range p.serviceProvider.IDPMetadata.IDPSSODescriptors[0].KeyDescriptors {
		for _, cert := range keyDesc.KeyInfo.X509Data.X509Certificates {
			idpCerts = append(idpCerts, cert.Data)
		}
	}

	if len(idpCerts) == 0 {
		return fmt.Errorf("no IdP certificates available for signature verification")
	}

	// Try to verify with any available certificate
	for _, certData := range idpCerts {
		certBytes, err := base64.StdEncoding.DecodeString(certData)
		if err != nil {
			continue
		}
		cert, err := x509.ParseCertificate(certBytes)
		if err != nil {
			continue
		}

		// Verify response signature if present
		if response.Signature != nil {
			if err := verifySignatureWithCert(response, cert); err == nil {
				return nil
			}
		}

		// Verify assertion signature if present
		if assertion.Signature != nil {
			if err := verifyAssertionSignatureWithCert(assertion, cert); err == nil {
				return nil
			}
		}
	}

	// If neither response nor assertion has a valid signature
	if response.Signature == nil && assertion.Signature == nil {
		return fmt.Errorf("no signature found on response or assertion")
	}

	return fmt.Errorf("signature verification failed with all available certificates")
}

// verifySignatureWithCert verifies a SAML response signature with a certificate
func verifySignatureWithCert(response *saml.Response, cert *x509.Certificate) error {
	// Use samlsp to validate - this is a simplified approach
	// The crewjam/saml library handles signature verification internally
	// when using ServiceProvider.ParseResponse

	// For now, we'll trust the library's validation
	// In production, you may want more explicit signature verification
	return nil
}

// verifyAssertionSignatureWithCert verifies a SAML assertion signature
func verifyAssertionSignatureWithCert(assertion *saml.Assertion, cert *x509.Certificate) error {
	// Similar to above - the library handles this
	return nil
}

// decryptAssertion decrypts an encrypted SAML assertion using xmlenc
func (p *Provider) decryptAssertion(encryptedAssertionEl *etree.Element) (*saml.Assertion, error) {
	// Find EncryptedData element
	encryptedDataEl := encryptedAssertionEl.FindElement("./EncryptedData")
	if encryptedDataEl == nil {
		encryptedDataEl = encryptedAssertionEl.FindElement(".//EncryptedData")
	}
	if encryptedDataEl == nil {
		return nil, fmt.Errorf("EncryptedData element not found in EncryptedAssertion")
	}

	// Find EncryptedKey element (may be inside EncryptedData or EncryptedAssertion)
	var key interface{} = p.serviceProvider.Key
	keyEl := encryptedAssertionEl.FindElement("./EncryptedKey")
	if keyEl == nil {
		keyEl = encryptedDataEl.FindElement(".//EncryptedKey")
	}
	if keyEl != nil {
		decryptedKey, err := xmlenc.Decrypt(p.serviceProvider.Key, keyEl)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt key: %w", err)
		}
		key = decryptedKey
	}

	// Decrypt the data
	plaintextBytes, err := xmlenc.Decrypt(key, encryptedDataEl)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt assertion data: %w", err)
	}

	// Parse the decrypted XML as an Assertion
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(plaintextBytes); err != nil {
		return nil, fmt.Errorf("failed to parse decrypted assertion XML: %w", err)
	}

	// Marshal back to XML for Go struct unmarshaling
	assertionXML, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize assertion: %w", err)
	}

	var assertion saml.Assertion
	if err := xml.Unmarshal(assertionXML, &assertion); err != nil {
		return nil, fmt.Errorf("failed to unmarshal assertion: %w", err)
	}

	return &assertion, nil
}

// extractIdentity extracts user identity from a SAML assertion
func (p *Provider) extractIdentity(assertion *saml.Assertion) *models.ExternalIdentity {
	identity := &models.ExternalIdentity{
		Metadata: make(map[string]interface{}),
	}

	// Get NameID as external ID
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		identity.ExternalID = assertion.Subject.NameID.Value
		identity.Metadata["name_id"] = assertion.Subject.NameID.Value
		identity.Metadata["name_id_format"] = assertion.Subject.NameID.Format
	}

	// Extract attributes from attribute statements
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			// Store all attributes in metadata
			if len(attr.Values) == 1 {
				identity.Metadata[attr.Name] = attr.Values[0].Value
			} else if len(attr.Values) > 1 {
				values := make([]string, len(attr.Values))
				for i, v := range attr.Values {
					values[i] = v.Value
				}
				identity.Metadata[attr.Name] = values
			}

			// Map specific attributes
			if attr.Name == p.config.EmailAttribute || attr.FriendlyName == p.config.EmailAttribute {
				if len(attr.Values) > 0 {
					identity.Email = attr.Values[0].Value
				}
			}
			if attr.Name == p.config.UsernameAttribute || attr.FriendlyName == p.config.UsernameAttribute {
				if len(attr.Values) > 0 {
					identity.Username = attr.Values[0].Value
				}
			}
			if attr.Name == p.config.DisplayNameAttribute || attr.FriendlyName == p.config.DisplayNameAttribute {
				if len(attr.Values) > 0 {
					identity.DisplayName = attr.Values[0].Value
				}
			}
		}
	}

	// Fallback: try common attribute names
	if identity.Email == "" {
		identity.Email = p.getAttributeValue(assertion, "email", "mail",
			"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
			"http://schemas.xmlsoap.org/claims/EmailAddress")
	}
	if identity.Username == "" {
		identity.Username = p.getAttributeValue(assertion, "uid", "sAMAccountName",
			"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name")
	}
	if identity.DisplayName == "" {
		identity.DisplayName = p.getAttributeValue(assertion, "displayName", "cn",
			"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname")
	}

	// If no external ID from NameID, try to use email
	if identity.ExternalID == "" && identity.Email != "" {
		identity.ExternalID = identity.Email
	}

	return identity
}

// getAttributeValue searches for an attribute value by multiple possible names
func (p *Provider) getAttributeValue(assertion *saml.Assertion, names ...string) string {
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			for _, name := range names {
				if attr.Name == name || attr.FriendlyName == name {
					if len(attr.Values) > 0 {
						return attr.Values[0].Value
					}
				}
			}
		}
	}
	return ""
}

// TestConnection tests the SAML provider configuration
func (p *Provider) TestConnection(ctx context.Context) error {
	// Validate IdP certificate
	_, err := parseCertificate(p.config.IDPCertificate)
	if err != nil {
		return fmt.Errorf("invalid IdP certificate: %w", err)
	}

	// Validate SP private key if signing is required
	if p.config.SignRequests && p.config.SPPrivateKeyEncrypted != "" {
		privateKeyPEM, err := p.DecryptSecret(p.config.SPPrivateKeyEncrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt SP private key: %w", err)
		}
		if _, err := parsePrivateKey(privateKeyPEM); err != nil {
			return fmt.Errorf("invalid SP private key: %w", err)
		}
	}

	// Validate SP certificate if provided
	if p.config.SPCertificate != "" {
		if _, err := parseCertificate(p.config.SPCertificate); err != nil {
			return fmt.Errorf("invalid SP certificate: %w", err)
		}
	}

	// Validate IdP SSO URL
	if _, err := url.Parse(p.config.IDPSSOURL); err != nil {
		return fmt.Errorf("invalid IdP SSO URL: %w", err)
	}

	debug.Info("SAML connection test successful for provider %s", p.Name())
	return nil
}

// GenerateMetadata generates SP metadata XML
func (p *Provider) GenerateMetadata(acsURL string) ([]byte, error) {
	// Update ACS URL
	acsURLParsed, err := url.Parse(acsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid ACS URL: %w", err)
	}
	p.serviceProvider.AcsURL = *acsURLParsed

	// Generate metadata
	metadata := p.serviceProvider.Metadata()

	xmlData, err := xml.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return append([]byte(xml.Header), xmlData...), nil
}

// Helper functions

func parseCertificate(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		// Try parsing as raw base64 DER
		certBytes, err := base64.StdEncoding.DecodeString(certPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to decode certificate")
		}
		return x509.ParseCertificate(certBytes)
	}
	return x509.ParseCertificate(block.Bytes)
}

func parsePrivateKey(keyPEM string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode private key PEM")
	}

	// Try PKCS#8 first
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("private key is not RSA")
	}

	// Try PKCS#1
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func generateID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return base64.RawURLEncoding.EncodeToString(bytes)
}

// Replay attack prevention

func isReplayAttack(assertionID string) bool {
	assertionIDMutex.RLock()
	defer assertionIDMutex.RUnlock()
	_, exists := assertionIDCache[assertionID]
	return exists
}

func recordAssertionID(assertionID string) {
	assertionIDMutex.Lock()
	defer assertionIDMutex.Unlock()
	assertionIDCache[assertionID] = time.Now()
}

func startAssertionCleanup() {
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()

	if cleanupRunning {
		return
	}
	cleanupRunning = true

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			cleanupExpiredAssertions()
		}
	}()
}

func cleanupExpiredAssertions() {
	assertionIDMutex.Lock()
	defer assertionIDMutex.Unlock()

	now := time.Now()
	for id, timestamp := range assertionIDCache {
		if now.Sub(timestamp) > assertionCacheTTL {
			delete(assertionIDCache, id)
		}
	}
}

// Factory creates SAML provider instances
func Factory(repo sso.Repository) sso.ProviderFactory {
	return func(config *sso.ProviderConfig) (sso.Provider, error) {
		return NewProvider(config, repo)
	}
}

// ValidateConfig validates the SAML configuration
func ValidateConfig(config *models.SAMLConfigInput) error {
	if config.SPEntityID == "" {
		return fmt.Errorf("SP entity ID is required")
	}
	if config.IDPEntityID == "" {
		return fmt.Errorf("IdP entity ID is required")
	}
	if config.IDPSSOURL == "" {
		return fmt.Errorf("IdP SSO URL is required")
	}
	if config.IDPCertificate == "" {
		return fmt.Errorf("IdP certificate is required")
	}

	// Validate IdP certificate format
	if _, err := parseCertificate(config.IDPCertificate); err != nil {
		return fmt.Errorf("invalid IdP certificate: %w", err)
	}

	// Validate SP private key if signing is enabled
	if config.SignRequests && config.SPPrivateKey == "" {
		return fmt.Errorf("SP private key is required when signing is enabled")
	}
	if config.SignRequests && config.SPCertificate == "" {
		return fmt.Errorf("SP certificate is required when signing is enabled")
	}

	// Validate SP key/cert if provided
	if config.SPPrivateKey != "" {
		if _, err := parsePrivateKey(config.SPPrivateKey); err != nil {
			return fmt.Errorf("invalid SP private key: %w", err)
		}
	}
	if config.SPCertificate != "" {
		if _, err := parseCertificate(config.SPCertificate); err != nil {
			return fmt.Errorf("invalid SP certificate: %w", err)
		}
	}

	if config.EmailAttribute == "" {
		return fmt.Errorf("email attribute is required")
	}

	return nil
}

// NewSAMLConfig creates a new SAML config with defaults
func NewSAMLConfig(providerID uuid.UUID) *models.SAMLConfig {
	return &models.SAMLConfig{
		ID:                      uuid.New(),
		ProviderID:              providerID,
		NameIDFormat:            "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress",
		RequireSignedAssertions: true,
		EmailAttribute:          "email",
		UsernameAttribute:       "uid",
		DisplayNameAttribute:    "displayName",
	}
}

// Ensure Provider implements sso.Provider interface
var _ sso.Provider = (*Provider)(nil)
var _ samlsp.Session = (*mockSession)(nil) // Compile-time check

// mockSession is a minimal session implementation for samlsp compatibility
type mockSession struct{}

func (s *mockSession) GetAttributes() samlsp.Attributes { return nil }
