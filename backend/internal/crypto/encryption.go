/*
Package crypto provides AES-256-GCM encryption for secrets that must be stored
in the database — SSO bind passwords and client secrets, cloud provider API
credentials, VPN enrollment credentials.

This logic began life in internal/sso. It was promoted here because cloud GPU
provisioning needs the same primitive, and "the SSO package" is the wrong home
for the process-wide secret-encryption key.

The key is resolved once, in this order:

 1. KH_ENCRYPTION_KEY   — preferred, covers every subsystem
 2. SSO_ENCRYPTION_KEY  — legacy name, still fully supported so existing
    deployments keep decrypting their stored SSO secrets untouched
 3. an ephemeral random key, with loud warnings

Option 3 exists so a developer can boot without configuration, but it means
every secret written during that process becomes unrecoverable the moment it
exits. IsEphemeral() is surfaced in the admin UI for exactly that reason.
*/
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

const (
	// EnvEncryptionKey is the preferred environment variable for the
	// process-wide secret encryption key.
	EnvEncryptionKey = "KH_ENCRYPTION_KEY"
	// EnvSSOEncryptionKey is the original, SSO-scoped name. Still honored so
	// that upgrading does not orphan already-encrypted SSO secrets.
	EnvSSOEncryptionKey = "SSO_ENCRYPTION_KEY"
	// KeySize is the required key size for AES-256 (32 bytes)
	KeySize = 32
)

var (
	// ErrInvalidKeySize is returned when the encryption key is not the correct size
	ErrInvalidKeySize = errors.New("encryption key must be 32 bytes (256 bits)")
	// ErrInvalidCiphertext is returned when decryption fails due to invalid ciphertext
	ErrInvalidCiphertext = errors.New("invalid ciphertext")
	// ErrEncryptionNotInitialized is returned when encryption is used before initialization
	ErrEncryptionNotInitialized = errors.New("encryption service not initialized")
)

// EncryptionService handles encryption and decryption of sensitive data at rest.
type EncryptionService struct {
	key         []byte
	initialized bool
	ephemeral   bool // true if key was generated at startup (not from env)
	source      string
	mu          sync.RWMutex
}

var (
	encryptionService *EncryptionService
	encryptionOnce    sync.Once
)

// GetEncryptionService returns the singleton encryption service instance
func GetEncryptionService() *EncryptionService {
	encryptionOnce.Do(func() {
		encryptionService = &EncryptionService{}
		encryptionService.Initialize()
	})
	return encryptionService
}

// Initialize sets up the encryption service with the key from the environment,
// or generates an ephemeral key if none is configured.
func (e *EncryptionService) Initialize() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		return nil
	}

	keyStr, source := resolveKeyMaterial()
	if keyStr == "" {
		// Generate ephemeral key for development/testing
		debug.Warning("%s not set - generating ephemeral key. Encrypted secrets will NOT persist across restarts!", EnvEncryptionKey)
		key := make([]byte, KeySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return fmt.Errorf("failed to generate ephemeral key: %w", err)
		}
		e.key = key
		e.ephemeral = true
		e.initialized = true
		e.source = "ephemeral"
		debug.Warning("Using ephemeral encryption key. Set %s for production.", EnvEncryptionKey)
		return nil
	}

	// Decode base64 key from environment
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		// Try raw bytes if not base64
		key = []byte(keyStr)
	}

	if len(key) != KeySize {
		return fmt.Errorf("%w: got %d bytes", ErrInvalidKeySize, len(key))
	}

	e.key = key
	e.ephemeral = false
	e.initialized = true
	e.source = source
	debug.Info("Encryption service initialized with key from %s", source)
	return nil
}

/*
 * resolveKeyMaterial picks the configured key, preferring KH_ENCRYPTION_KEY and
 * falling back to the legacy SSO_ENCRYPTION_KEY.
 *
 * If both are set to DIFFERENT values we keep KH_ENCRYPTION_KEY but warn
 * loudly: that combination silently makes every previously-stored SSO secret
 * undecryptable, and the failure would otherwise surface much later as
 * "decryption failed" on an unrelated login attempt.
 */
func resolveKeyMaterial() (key string, source string) {
	primary := os.Getenv(EnvEncryptionKey)
	legacy := os.Getenv(EnvSSOEncryptionKey)

	switch {
	case primary != "" && legacy != "":
		if subtle.ConstantTimeCompare([]byte(primary), []byte(legacy)) != 1 {
			debug.Warning("Both %s and %s are set to different values. Using %s. "+
				"Any secret previously encrypted under %s (SSO bind passwords, SAML private keys, "+
				"OAuth client secrets) will fail to decrypt until it is re-entered.",
				EnvEncryptionKey, EnvSSOEncryptionKey, EnvEncryptionKey, EnvSSOEncryptionKey)
		}
		return primary, EnvEncryptionKey
	case primary != "":
		return primary, EnvEncryptionKey
	case legacy != "":
		debug.Info("Using legacy %s; %s is preferred and covers all subsystems.",
			EnvSSOEncryptionKey, EnvEncryptionKey)
		return legacy, EnvSSOEncryptionKey
	default:
		return "", ""
	}
}

// IsEphemeral returns true if the encryption key was generated at startup
func (e *EncryptionService) IsEphemeral() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.ephemeral
}

// KeySource reports where the key came from: KH_ENCRYPTION_KEY,
// SSO_ENCRYPTION_KEY, or "ephemeral". Intended for admin diagnostics.
func (e *EncryptionService) KeySource() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.source
}

// Encrypt encrypts plaintext using AES-256-GCM and returns base64-encoded ciphertext
func (e *EncryptionService) Encrypt(plaintext string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.initialized {
		return "", ErrEncryptionNotInitialized
	}

	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and prepend nonce to ciphertext
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Return base64-encoded result
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext using AES-256-GCM
func (e *EncryptionService) Decrypt(ciphertext string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.initialized {
		return "", ErrEncryptionNotInitialized
	}

	if ciphertext == "" {
		return "", nil
	}

	// Decode base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Validate ciphertext length
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	// Extract nonce and ciphertext
	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// GenerateKey generates a new random 32-byte key and returns it base64-encoded.
// This is a helper for generating keys to set in environment variables.
func GenerateKey() (string, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// MustEncrypt encrypts plaintext and panics on error (for use in tests/init)
func (e *EncryptionService) MustEncrypt(plaintext string) string {
	encrypted, err := e.Encrypt(plaintext)
	if err != nil {
		panic(fmt.Sprintf("encryption failed: %v", err))
	}
	return encrypted
}

// MustDecrypt decrypts ciphertext and panics on error (for use in tests/init)
func (e *EncryptionService) MustDecrypt(ciphertext string) string {
	decrypted, err := e.Decrypt(ciphertext)
	if err != nil {
		panic(fmt.Sprintf("decryption failed: %v", err))
	}
	return decrypted
}
