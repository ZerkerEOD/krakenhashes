package crypto

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
Persistence for the process-wide secret-encryption key.

Without this, a deployment that never set KH_ENCRYPTION_KEY ran on a random
in-memory key, and every secret it wrote -- cloud provider API credentials, VPN
enrollment credentials, SSO bind passwords, SAML private keys, OAuth client
secrets -- became unrecoverable the moment the process exited. The failure was
invisible until something tried to decrypt one, which on a cloud-provisioning
build meant an instance launch failing long after the restart that caused it.

The key lives under the CONFIG directory rather than the data directory, next to
the TLS private keys (config.go puts those at <ConfigDir>/certs). The data
directory holds wordlists, hashlists and binaries -- bulk content that gets
backed up, rsynced and browsed -- which is the wrong company for the one file
that decrypts every stored credential.
*/

const (
	// EnvAllowEphemeralKey opts in to the old behaviour of generating a
	// throwaway key when none can be persisted. Intended for tests and
	// short-lived local runs; a server that sets it cannot keep any secret
	// across a restart.
	EnvAllowEphemeralKey = "KH_ALLOW_EPHEMERAL_KEY"

	// keyFileName is the file holding the base64-encoded key, inside keyDirName.
	keyFileName = "encryption.key"

	// keyDirName is a dedicated subdirectory of the config directory.
	//
	// It exists so the mode is ours rather than inherited. Dockerfile.prod
	// creates the data directory 0750, but a host bind-mount's permissions win
	// at runtime (observed as 0755), so neither the config nor the data
	// directory can be assumed tight. Creating our own at 0700 means the key is
	// protected by the directory as well as by its own 0600.
	keyDirName = "secrets"

	keyDirMode  = os.FileMode(0700)
	keyFileMode = os.FileMode(0600)
)

// ErrNoPersistentKey is returned when no key could be loaded or created and
// ephemeral keys have not been explicitly permitted.
var ErrNoPersistentKey = errors.New("no persistent encryption key available")

// ephemeralAllowed reports whether the operator has opted in to throwaway keys.
func ephemeralAllowed() bool {
	return os.Getenv(EnvAllowEphemeralKey) == "true"
}

// keyFilePath returns the key file's location within a config directory.
func keyFilePath(configDir string) string {
	return filepath.Join(configDir, keyDirName, keyFileName)
}

/*
loadOrCreateKeyFile returns the key stored at <configDir>/secrets/encryption.key,
creating it on first use.

The distinction that matters: a MISSING key file is first boot and we generate
one, but a key file that exists and cannot be used is a hard error. Regenerating
over it would convert one unreadable file into every stored secret being
silently undecryptable, and the operator would not find out until a cloud launch
or an SSO login failed. Refusing to start is recoverable; a silent rewrite is
not.
*/
func loadOrCreateKeyFile(configDir string) (key []byte, created bool, err error) {
	path := keyFilePath(configDir)

	contents, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		key, err := decodeKeyMaterial(contents)
		if err != nil {
			// Exists but unusable. Do NOT replace it -- see above.
			return nil, false, fmt.Errorf("encryption key file %s is unusable (%w); "+
				"refusing to replace it, because generating a new key would make every "+
				"already-stored secret permanently undecryptable. Restore the file from "+
				"backup, or delete it deliberately to start over with new secrets", path, err)
		}
		return key, false, nil

	case errors.Is(readErr, os.ErrNotExist):
		// First boot: fall through and create one.

	default:
		// Exists but unreadable (permissions, I/O error, a directory in its
		// place). Same reasoning -- never write over something we cannot read.
		return nil, false, fmt.Errorf("cannot read encryption key file %s: %w", path, readErr)
	}

	encoded, err := GenerateKey()
	if err != nil {
		return nil, false, fmt.Errorf("failed to generate an encryption key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
		return nil, false, fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	// MkdirAll is a no-op on an existing directory, including one created by an
	// older version at a wider mode, so tighten it explicitly.
	if err := os.Chmod(filepath.Dir(path), keyDirMode); err != nil {
		debug.Warning("Could not tighten permissions on %s: %v", filepath.Dir(path), err)
	}

	if err := writeFileAtomic(path, []byte(encoded), keyFileMode); err != nil {
		return nil, false, fmt.Errorf("failed to persist an encryption key to %s: %w", path, err)
	}

	decoded, err := decodeKeyMaterial([]byte(encoded))
	if err != nil {
		return nil, false, fmt.Errorf("generated an unusable key: %w", err)
	}

	return decoded, true, nil
}

/*
decodeKeyMaterial accepts the base64 form written by GenerateKey, and falls back
to raw bytes for parity with how the environment variable is parsed.

The two forms cannot collide: 32 base64 characters decode to 24 bytes, so a
KeySize-byte raw key is never also valid base64 for a KeySize-byte key.

Whitespace is trimmed ONLY when interpreting the contents as base64. A raw
32-byte key is passed through untouched, because 0x20, 0x09, 0x0A and 0x0D are
perfectly ordinary bytes in random key material -- trimming them would silently
corrupt roughly 3% of raw keys (any that happen to begin or end with one).
*/
func decodeKeyMaterial(contents []byte) ([]byte, error) {
	trimmed := trimKeyBytes(contents)

	if len(trimmed) > 0 {
		if decoded, err := base64.StdEncoding.DecodeString(string(trimmed)); err == nil && len(decoded) == KeySize {
			return decoded, nil
		}
	}

	if len(contents) == KeySize {
		return contents, nil
	}

	if len(trimmed) == 0 {
		return nil, errors.New("file is empty")
	}
	return nil, fmt.Errorf("%w: file holds %d bytes, which is neither a raw %d-byte key "+
		"nor base64 for one", ErrInvalidKeySize, len(contents), KeySize)
}

// trimKeyBytes strips surrounding whitespace so a base64 key written with a
// trailing newline, or edited by hand, still parses. Only ever applied to the
// base64 interpretation — see decodeKeyMaterial.
func trimKeyBytes(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isKeySpace(b[start]) {
		start++
	}
	for end > start && isKeySpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isKeySpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

/*
writeFileAtomic replaces path with data, never leaving a partial or
wide-permission file behind.

Mirrors writePEMAtomic in internal/tls: write to a sibling temp file, chmod
BEFORE any bytes are written so the key is never briefly world-readable, fsync,
then rename. os.CreateTemp already makes the file 0600, but the explicit chmod
keeps that a guarantee rather than a detail of the standard library.
*/
func writeFileAtomic(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".kh-key-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if err = tmp.Chmod(mode); err != nil {
		return fmt.Errorf("failed to set mode on %s: %w", tmpName, err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write %s: %w", tmpName, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync %s: %w", tmpName, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", tmpName, err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", tmpName, path, err)
	}

	return nil
}
