package tls

import (
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// Modes for the two classes of file in the certs directory.
const (
	certFileMode = os.FileMode(0644)
	keyFileMode  = os.FileMode(0600)
)

// writePEMAtomic encodes a DER blob as PEM and replaces path with it atomically.
//
// A certificate reissue rewrites files that the running server and nginx both
// read. A plain os.Create truncates first, so a crash or a full disk between
// truncate and write leaves a zero-length certificate on disk -- which is an
// unbootable server on the next restart. Writing to a sibling temp file and
// renaming means a reader either sees the whole old file or the whole new one.
//
// The mode is applied before any bytes are written, so a private key is never
// briefly world-readable.
func writePEMAtomic(path, blockType string, der []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".kh-*.tmp")
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

	// Chmod before writing: os.CreateTemp makes the file 0600, which is correct
	// for keys but must be widened for certificates, and doing it first means a
	// key is never momentarily readable at a wider mode.
	if err = tmp.Chmod(mode); err != nil {
		return fmt.Errorf("failed to set mode on %s: %w", tmpName, err)
	}

	if err = pem.Encode(tmp, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		return fmt.Errorf("failed to encode PEM to %s: %w", tmpName, err)
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

	// Sync the directory so the rename itself is durable. Without this the file
	// contents survive a power loss but the directory entry may not.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}

// copyFile duplicates src to dst with the given mode, used to take the .bak
// snapshots that a failed mid-reissue write is restored from.
func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", src, err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("failed to write %s: %w", dst, err)
	}
	return nil
}
