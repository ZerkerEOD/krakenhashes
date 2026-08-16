package sso

import (
	"github.com/ZerkerEOD/krakenhashes/backend/internal/crypto"
)

// The secret-encryption implementation now lives in internal/crypto, because
// cloud GPU provisioning needs the same primitive for provider API keys and VPN
// enrollment credentials, and the SSO package is the wrong owner of a
// process-wide encryption key.
//
// These aliases keep every existing SSO caller compiling unchanged, and — more
// importantly — the singleton and key resolution are shared, so a secret
// encrypted by one subsystem decrypts in the other. internal/crypto still
// honors SSO_ENCRYPTION_KEY, so existing deployments are unaffected.

const (
	// EnvSSOEncryptionKey is the legacy environment variable name for the
	// encryption key. KH_ENCRYPTION_KEY is preferred; both are honored.
	EnvSSOEncryptionKey = crypto.EnvSSOEncryptionKey
	// KeySize is the required key size for AES-256 (32 bytes)
	KeySize = crypto.KeySize
)

var (
	// ErrInvalidKeySize is returned when the encryption key is not the correct size
	ErrInvalidKeySize = crypto.ErrInvalidKeySize
	// ErrInvalidCiphertext is returned when decryption fails due to invalid ciphertext
	ErrInvalidCiphertext = crypto.ErrInvalidCiphertext
	// ErrEncryptionNotInitialized is returned when encryption is used before initialization
	ErrEncryptionNotInitialized = crypto.ErrEncryptionNotInitialized
)

// EncryptionService handles encryption and decryption of sensitive data.
// Aliased to crypto.EncryptionService so both packages share one instance.
type EncryptionService = crypto.EncryptionService

// GetEncryptionService returns the process-wide encryption service.
func GetEncryptionService() *EncryptionService {
	return crypto.GetEncryptionService()
}

// GenerateKey generates a new random 32-byte key and returns it base64-encoded.
func GenerateKey() (string, error) {
	return crypto.GenerateKey()
}
