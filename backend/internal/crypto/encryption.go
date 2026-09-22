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
 3. <ConfigDir>/secrets/encryption.key — generated on first boot and reused
    thereafter, so a server with no configuration still keeps its secrets
 4. an ephemeral random key

Tier 3 is why this package needs a directory handed to it; see keyfile.go and
InitializeWithKeyDir. Before it existed, an unconfigured server silently used a
throwaway key and every secret it wrote became unrecoverable on exit.

Tier 4 is reached in two situations, which differ deliberately:

  - No key directory was supplied. Only the lazy GetEncryptionService accessor
    does that, i.e. tests and tooling, so it is ephemeral with a warning and no
    opt-in. A server never lands here — cmd/server always passes ConfigDir.
  - A directory was supplied but its key cannot be read or created. That is a
    real server misconfiguration, so it is FATAL unless the operator sets
    KH_ALLOW_EPHEMERAL_KEY=true. An existing key is never replaced.

IsEphemeral() and KeySource() are surfaced in the admin UI.
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
	ephemeral   bool // true if key was generated at startup AND not persisted
	source      string
	// keyDir is the config directory under which the key file is kept. Empty
	// means the caller did not ask for persistence, which only happens for tests
	// and tooling; cmd/server always supplies it.
	keyDir string
	mu     sync.RWMutex
}

var (
	encryptionService *EncryptionService
	encryptionOnce    sync.Once
)

// GetEncryptionService returns the singleton encryption service instance.
//
// This accessor cannot report an error, so it is NOT the way a server should
// initialise the service -- a failure to obtain a persistent key would be
// swallowed here and surface much later as a decryption error. cmd/server calls
// InitializeWithKeyDir first, which returns the error and aborts startup; by the
// time anything calls this, the singleton already exists.
//
// Reached first (tests, tooling), this still yields a usable service: it
// initialises with no key directory, which takes an ephemeral key with a
// warning. That keeps callers who just want Encrypt/Decrypt working without
// setup, and cannot make a server ephemeral, because cmd/server always calls
// InitializeWithKeyDir before anything else.
func GetEncryptionService() *EncryptionService {
	encryptionOnce.Do(func() {
		encryptionService = &EncryptionService{}
		if err := encryptionService.Initialize(); err != nil {
			// Should not happen on this path (no key directory means ephemeral,
			// which only fails if the system RNG does), but never leave a
			// half-initialised service silently in place: Encrypt/Decrypt return
			// ErrEncryptionNotInitialized instead.
			debug.Error("Encryption service unavailable: %v", err)
		}
	})
	return encryptionService
}

/*
InitializeWithKeyDir initialises the singleton, persisting a generated key under
configDir when no key is configured in the environment.

Call this once during startup, before anything can reach GetEncryptionService,
and treat a returned error as fatal. That ordering is what makes "refuse to
start" possible: the accessor above has no way to report failure.
*/
func InitializeWithKeyDir(configDir string) error {
	var initErr error
	ran := false

	encryptionOnce.Do(func() {
		ran = true
		encryptionService = &EncryptionService{keyDir: configDir}
		initErr = encryptionService.Initialize()
	})

	if !ran {
		// Something already initialised the singleton -- with a different key
		// directory, or none. Silently keeping that key would mean the server
		// runs on whichever key happened to be resolved first, so make the
		// ordering mistake loud here instead.
		if encryptionService != nil && encryptionService.keyDir == configDir {
			return nil
		}
		return fmt.Errorf("encryption service was already initialized before %s was configured; "+
			"InitializeWithKeyDir must run before any use of GetEncryptionService", configDir)
	}

	return initErr
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
		return e.initializeWithoutEnvKey()
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
initializeWithoutEnvKey handles the case where neither environment variable is
set: persist a generated key under the config directory, or fail.

Caller holds e.mu.

Once a directory is configured -- which is every server start -- a key that
cannot be read or created is fatal unless KH_ALLOW_EPHEMERAL_KEY=true. A server
that silently falls back to a throwaway key looks healthy while discarding every
credential it is asked to store, which is strictly worse than not starting.
*/
func (e *EncryptionService) initializeWithoutEnvKey() error {
	// No key directory means nobody asked for persistence, which only happens
	// via the lazy GetEncryptionService accessor -- tests and tooling. The
	// server always supplies one: config.NewConfig resolves ConfigDir to the env
	// var, then $HOME/.krakenhashes, then ./.krakenhashes, so it is never empty.
	if e.keyDir == "" {
		return e.useEphemeralKey("no key directory configured")
	}

	key, created, err := loadOrCreateKeyFile(e.keyDir)
	if err != nil {
		if ephemeralAllowed() {
			debug.Warning("Falling back to an ephemeral key: %v", err)
			return e.useEphemeralKey(err.Error())
		}
		return fmt.Errorf("%w: %v. Fix the path above, set %s, or set %s=true to accept "+
			"losing every stored secret on restart",
			ErrNoPersistentKey, err, EnvEncryptionKey, EnvAllowEphemeralKey)
	}

	if created {
		debug.Warning("Generated a new encryption key at %s. Back this file up: "+
			"without it, stored cloud provider credentials, VPN credentials and SSO "+
			"secrets cannot be decrypted.", keyFilePath(e.keyDir))
	}

	e.key = key
	e.ephemeral = false
	e.initialized = true
	e.source = "keyfile"
	debug.Info("Encryption service initialized with key from %s", keyFilePath(e.keyDir))
	return nil
}

// useEphemeralKey installs a throwaway key. Caller holds e.mu.
func (e *EncryptionService) useEphemeralKey(reason string) error {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return fmt.Errorf("failed to generate ephemeral key: %w", err)
	}

	e.key = key
	e.ephemeral = true
	e.initialized = true
	e.source = "ephemeral"

	debug.Warning("Using an EPHEMERAL encryption key (%s). Every secret written by this "+
		"process becomes unrecoverable when it exits. Set %s for a server.",
		reason, EnvEncryptionKey)
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
// SSO_ENCRYPTION_KEY, "keyfile" (persisted under the config directory), or
// "ephemeral". Intended for admin diagnostics.
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
