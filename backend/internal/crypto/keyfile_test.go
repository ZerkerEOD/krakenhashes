package crypto

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearKeyEnv removes both key variables and the ephemeral opt-in so a test
// exercises the file tier rather than whatever the developer has exported.
func clearKeyEnv(t *testing.T) {
	t.Helper()
	t.Setenv(EnvEncryptionKey, "")
	t.Setenv(EnvSSOEncryptionKey, "")
	t.Setenv(EnvAllowEphemeralKey, "")
}

func TestLoadOrCreateKeyFile_GeneratesOnFirstBoot(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()

	key, created, err := loadOrCreateKeyFile(dir)
	if err != nil {
		t.Fatalf("loadOrCreateKeyFile: %v", err)
	}
	if !created {
		t.Error("created = false on first boot, want true")
	}
	if len(key) != KeySize {
		t.Errorf("key is %d bytes, want %d", len(key), KeySize)
	}

	path := keyFilePath(dir)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("key file was not written: %v", err)
	}
	// The whole point of persisting it is that it stays private.
	if perm := info.Mode().Perm(); perm != keyFileMode {
		t.Errorf("key file mode = %04o, want %04o", perm, keyFileMode)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat key dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != keyDirMode {
		t.Errorf("key dir mode = %04o, want %04o", perm, keyDirMode)
	}
}

// The config directory is bind-mounted in production, so its mode comes from the
// host: /etc/krakenhashes is observed as 0755 in the running container even
// though the Dockerfile asks for tighter. The key must not inherit that.
func TestLoadOrCreateKeyFile_TightensAgainstALooseParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: umask/mode behaviour is not representative")
	}
	clearKeyEnv(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, _, err := loadOrCreateKeyFile(dir); err != nil {
		t.Fatalf("loadOrCreateKeyFile: %v", err)
	}

	secretsDir := filepath.Dir(keyFilePath(dir))
	info, err := os.Stat(secretsDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != keyDirMode {
		t.Errorf("secrets dir mode = %04o under a 0755 parent, want %04o", perm, keyDirMode)
	}

	fileInfo, err := os.Stat(keyFilePath(dir))
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != keyFileMode {
		t.Errorf("key file mode = %04o, want %04o", perm, keyFileMode)
	}
}

// An existing secrets dir left too wide by an older version must be tightened
// rather than accepted as-is.
func TestLoadOrCreateKeyFile_TightensAnExistingWideDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode behaviour is not representative")
	}
	clearKeyEnv(t)
	dir := t.TempDir()
	secretsDir := filepath.Dir(keyFilePath(dir))
	if err := os.MkdirAll(secretsDir, 0777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(secretsDir, 0777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, _, err := loadOrCreateKeyFile(dir); err != nil {
		t.Fatalf("loadOrCreateKeyFile: %v", err)
	}

	info, err := os.Stat(secretsDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != keyDirMode {
		t.Errorf("pre-existing 0777 secrets dir left at %04o, want %04o", perm, keyDirMode)
	}
}

func TestLoadOrCreateKeyFile_ReusesExistingKey(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()

	first, created, err := loadOrCreateKeyFile(dir)
	if err != nil || !created {
		t.Fatalf("first call: key=%v created=%v err=%v", first, created, err)
	}
	raw, err := os.ReadFile(keyFilePath(dir))
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}

	second, created, err := loadOrCreateKeyFile(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if created {
		t.Error("created = true on second call; the key was regenerated")
	}
	if string(first) != string(second) {
		t.Error("second call returned a different key — secrets written before it would be lost")
	}

	rawAfter, err := os.ReadFile(keyFilePath(dir))
	if err != nil {
		t.Fatalf("re-read key file: %v", err)
	}
	if string(raw) != string(rawAfter) {
		t.Error("key file contents changed on a read-only path")
	}
}

// An existing-but-unusable key must NEVER be replaced: regenerating would turn
// one bad file into every stored secret being permanently undecryptable.
func TestLoadOrCreateKeyFile_RefusesToReplaceBadKey(t *testing.T) {
	cases := []struct {
		name     string
		contents string
	}{
		{"empty file", ""},
		{"whitespace only", "   \n\t "},
		{"not base64 and wrong length", "this is definitely not a key"},
		{"base64 but too short", base64.StdEncoding.EncodeToString([]byte("tooshort"))},
		{"base64 but too long", base64.StdEncoding.EncodeToString(make([]byte, KeySize+1))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearKeyEnv(t)
			dir := t.TempDir()
			path := keyFilePath(dir)
			if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte(tc.contents), keyFileMode); err != nil {
				t.Fatalf("seed key file: %v", err)
			}

			_, _, err := loadOrCreateKeyFile(dir)
			if err == nil {
				t.Fatal("loadOrCreateKeyFile succeeded, want an error")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the path, so an operator cannot act on it", err)
			}

			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("key file disappeared: %v", readErr)
			}
			if string(after) != tc.contents {
				t.Errorf("key file was rewritten (%q -> %q); an unusable key must be left alone",
					tc.contents, string(after))
			}
		})
	}
}

func TestLoadOrCreateKeyFile_AcceptsRawAndTrimmedKeys(t *testing.T) {
	// Bytes 0x01..0x20 deliberately. The last byte is 0x20 — a space — and the
	// range also covers 0x09 (tab), 0x0A (LF) and 0x0D (CR). An earlier version
	// trimmed whitespace from raw key material and silently dropped that final
	// byte, rejecting the key as 31 bytes. Roughly 3% of random raw keys begin or
	// end with one of those four values, so do not "tidy" this to 0x00..0x1F.
	raw := make([]byte, KeySize)
	for i := range raw {
		raw[i] = byte(i + 1)
	}

	cases := []struct {
		name     string
		contents string
	}{
		{"base64 with trailing newline", base64.StdEncoding.EncodeToString(raw) + "\n"},
		{"base64 with surrounding whitespace", "  " + base64.StdEncoding.EncodeToString(raw) + "  \r\n"},
		{"raw 32 bytes", string(raw)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearKeyEnv(t)
			dir := t.TempDir()
			path := keyFilePath(dir)
			if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte(tc.contents), keyFileMode); err != nil {
				t.Fatalf("seed: %v", err)
			}

			key, created, err := loadOrCreateKeyFile(dir)
			if err != nil {
				t.Fatalf("loadOrCreateKeyFile: %v", err)
			}
			if created {
				t.Error("created = true, want false for an existing usable key")
			}
			if string(key) != string(raw) {
				t.Error("decoded key does not match the seeded key")
			}
		})
	}
}

func TestLoadOrCreateKeyFile_UnreadableFileIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file modes do not restrict access")
	}
	clearKeyEnv(t)
	dir := t.TempDir()
	path := keyFilePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	valid, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if err := os.WriteFile(path, []byte(valid), 0000); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, _, err := loadOrCreateKeyFile(dir); err == nil {
		t.Fatal("loadOrCreateKeyFile succeeded on an unreadable key file, want an error")
	}
}

func TestLoadOrCreateKeyFile_UnwritableDirIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory modes do not restrict writes")
	}
	clearKeyEnv(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	if _, _, err := loadOrCreateKeyFile(dir); err == nil {
		t.Fatal("loadOrCreateKeyFile succeeded with an unwritable config dir, want an error")
	}
}

// Initialize's tiering, exercised on a non-singleton instance so each case is
// independent of sync.Once.
func TestInitialize_KeyResolutionOrder(t *testing.T) {
	envKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	t.Run("env key wins and no file is created", func(t *testing.T) {
		clearKeyEnv(t)
		t.Setenv(EnvEncryptionKey, envKey)
		dir := t.TempDir()

		e := &EncryptionService{keyDir: dir}
		if err := e.Initialize(); err != nil {
			t.Fatalf("Initialize: %v", err)
		}
		if e.KeySource() != EnvEncryptionKey {
			t.Errorf("KeySource = %q, want %q", e.KeySource(), EnvEncryptionKey)
		}
		if e.IsEphemeral() {
			t.Error("IsEphemeral = true for an env-configured key")
		}
		if _, err := os.Stat(keyFilePath(dir)); !os.IsNotExist(err) {
			t.Error("a key file was created even though the environment supplied a key")
		}
	})

	t.Run("falls through to the key file", func(t *testing.T) {
		clearKeyEnv(t)
		dir := t.TempDir()

		e := &EncryptionService{keyDir: dir}
		if err := e.Initialize(); err != nil {
			t.Fatalf("Initialize: %v", err)
		}
		if e.KeySource() != "keyfile" {
			t.Errorf("KeySource = %q, want \"keyfile\"", e.KeySource())
		}
		if e.IsEphemeral() {
			t.Error("IsEphemeral = true for a persisted key")
		}
	})

	// No key directory means nobody asked for persistence, which only happens
	// through the lazy GetEncryptionService accessor in tests and tooling. The
	// server always passes a directory, so this path cannot make a server
	// silently ephemeral.
	t.Run("no key dir is ephemeral for tooling", func(t *testing.T) {
		clearKeyEnv(t)

		e := &EncryptionService{}
		if err := e.Initialize(); err != nil {
			t.Fatalf("Initialize: %v", err)
		}
		if !e.IsEphemeral() {
			t.Error("IsEphemeral = false, want true")
		}
		if e.KeySource() != "ephemeral" {
			t.Errorf("KeySource = %q, want \"ephemeral\"", e.KeySource())
		}
	})

	// The guarantee that matters: once a directory IS configured (every server
	// start), an unusable key is fatal rather than quietly replaced.
	t.Run("a bad key file refuses to start when a dir is set", func(t *testing.T) {
		clearKeyEnv(t)
		dir := t.TempDir()
		path := keyFilePath(dir)
		if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("garbage"), keyFileMode); err != nil {
			t.Fatalf("seed: %v", err)
		}

		e := &EncryptionService{keyDir: dir}
		err := e.Initialize()
		if err == nil {
			t.Fatal("Initialize succeeded on a corrupt key file, want an error")
		}
		if !strings.Contains(err.Error(), EnvAllowEphemeralKey) {
			t.Errorf("error %q does not mention the opt-in, so the way forward is unclear", err)
		}
	})

	t.Run("a bad key file with the opt-in degrades to ephemeral", func(t *testing.T) {
		clearKeyEnv(t)
		t.Setenv(EnvAllowEphemeralKey, "true")
		dir := t.TempDir()
		path := keyFilePath(dir)
		if err := os.MkdirAll(filepath.Dir(path), keyDirMode); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("garbage"), keyFileMode); err != nil {
			t.Fatalf("seed: %v", err)
		}

		e := &EncryptionService{keyDir: dir}
		if err := e.Initialize(); err != nil {
			t.Fatalf("Initialize: %v", err)
		}
		if !e.IsEphemeral() {
			t.Error("IsEphemeral = false, want true")
		}
		// The corrupt file must still be left exactly as found.
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(after) != "garbage" {
			t.Errorf("key file was rewritten to %q even under the ephemeral opt-in", after)
		}
	})
}

// GetEncryptionService is the accessor tests and tooling reach without any
// setup. It must yield a usable service, because it cannot report an error --
// the server uses InitializeWithKeyDir instead. Guards against regressing
// existing callers such as internal/services/cloud's tests.
func TestGetEncryptionServiceIsUsableWithoutSetup(t *testing.T) {
	svc := GetEncryptionService()
	if svc == nil {
		t.Fatal("GetEncryptionService returned nil")
	}

	ciphertext, err := svc.Encrypt("tskey-reusable-test")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	plaintext, err := svc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "tskey-reusable-test" {
		t.Errorf("round trip = %q, want %q", plaintext, "tskey-reusable-test")
	}
}

// The end-to-end property the whole change exists for: a restart must still
// decrypt what the previous process wrote.
func TestKeyFileSurvivesRestart(t *testing.T) {
	clearKeyEnv(t)
	dir := t.TempDir()

	first := &EncryptionService{keyDir: dir}
	if err := first.Initialize(); err != nil {
		t.Fatalf("first Initialize: %v", err)
	}
	ciphertext, err := first.Encrypt("cloud-provider-api-key")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// A new process, same config directory.
	second := &EncryptionService{keyDir: dir}
	if err := second.Initialize(); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
	plaintext, err := second.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt after restart: %v", err)
	}
	if plaintext != "cloud-provider-api-key" {
		t.Errorf("Decrypt = %q, want %q", plaintext, "cloud-provider-api-key")
	}
}

func TestWriteFileAtomic_SetsModeAndContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.txt")

	if err := writeFileAtomic(path, []byte("payload"), keyFileMode); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("contents = %q, want %q", got, "payload")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != keyFileMode {
		t.Errorf("mode = %04o, want %04o", perm, keyFileMode)
	}

	// No temp files left behind on success.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".kh-key-") {
			t.Errorf("temp file %s was not cleaned up", e.Name())
		}
	}
}
