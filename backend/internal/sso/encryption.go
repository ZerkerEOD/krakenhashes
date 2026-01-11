package sso

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

const (
	// EnvSSOEncryptionKey is the environment variable name for the encryption key
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

// EncryptionService handles encryption and decryption of sensitive SSO data
type EncryptionService struct {
	key         []byte
	initialized bool
	ephemeral   bool // true if key was generated at startup (not from env)
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

// Initialize sets up the encryption service with the key from environment
// or generates an ephemeral key if not set
func (e *EncryptionService) Initialize() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		return nil
	}

	keyStr := os.Getenv(EnvSSOEncryptionKey)
	if keyStr == "" {
		// Generate ephemeral key for development/testing
		debug.Warning("SSO_ENCRYPTION_KEY not set - generating ephemeral key. SSO secrets will NOT persist across restarts!")
		key := make([]byte, KeySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return fmt.Errorf("failed to generate ephemeral key: %w", err)
		}
		e.key = key
		e.ephemeral = true
		e.initialized = true
		debug.Warning("Using ephemeral encryption key. Set SSO_ENCRYPTION_KEY environment variable for production.")
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
	debug.Info("SSO encryption service initialized with configured key")
	return nil
}

// IsEphemeral returns true if the encryption key was generated at startup
func (e *EncryptionService) IsEphemeral() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.ephemeral
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

// GenerateKey generates a new random 32-byte key and returns it base64-encoded
// This is a helper for generating keys to set in environment variables
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
