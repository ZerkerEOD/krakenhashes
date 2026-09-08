package sync

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileSync_ScanAllDirectories(t *testing.T) {
	// Create test directories
	tempDir := t.TempDir()
	dataDirs := &config.DataDirs{
		Binaries:  filepath.Join(tempDir, "binaries"),
		Wordlists: filepath.Join(tempDir, "wordlists"),
		Rules:     filepath.Join(tempDir, "rules"),
		Hashlists: filepath.Join(tempDir, "hashlists"),
	}

	// Create directories
	for _, dir := range []string{dataDirs.Binaries, dataDirs.Wordlists, dataDirs.Rules, dataDirs.Hashlists} {
		err := os.MkdirAll(dir, 0755)
		require.NoError(t, err)
	}

	// Create test files
	testFiles := map[string][]string{
		dataDirs.Binaries:  {"hashcat.7z", "john.7z"},
		dataDirs.Wordlists: {"rockyou.txt", "common.txt"},
		dataDirs.Rules:     {"best64.rule", "dive.rule"},
	}

	for dir, files := range testFiles {
		for _, file := range files {
			path := filepath.Join(dir, file)
			err := ioutil.WriteFile(path, []byte("test content"), 0644)
			require.NoError(t, err)
		}
	}

	fs := &FileSync{
		dataDirs: dataDirs,
	}

	// Test scanning specific file types
	result, err := fs.ScanAllDirectories([]string{"wordlist", "rule"})
	assert.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Len(t, result["wordlist"], 2)
	assert.Len(t, result["rule"], 2)

	// Test scanning all directories
	result, err = fs.ScanAllDirectories([]string{"binary", "wordlist", "rule", "hashlist"})
	assert.NoError(t, err)
	assert.Len(t, result, 4)
}

func TestFileSync_FindExtractedExecutables(t *testing.T) {
	// Create test directory structure
	tempDir := t.TempDir()
	binaryDir := filepath.Join(tempDir, "binaries")

	// Create test directories and files
	testStructure := []struct {
		dir  string
		file string
	}{
		{filepath.Join(binaryDir, "hashcat"), "hashcat.bin"},
		{filepath.Join(binaryDir, "hashcat"), "hashcat.exe"},
		{filepath.Join(binaryDir, "john"), "john.exe"},
		{filepath.Join(binaryDir, "subdir"), "tool.bin"},
		{binaryDir, "not_executable.txt"},
	}

	for _, ts := range testStructure {
		err := os.MkdirAll(ts.dir, 0755)
		require.NoError(t, err)

		filePath := filepath.Join(ts.dir, ts.file)
		err = ioutil.WriteFile(filePath, []byte("executable"), 0755)
		require.NoError(t, err)
	}

	fs := &FileSync{}

	executables, err := fs.FindExtractedExecutables(binaryDir)
	assert.NoError(t, err)
	assert.Len(t, executables, 4) // Should find 4 .bin/.exe files

	// Verify the found executables
	for _, exec := range executables {
		assert.True(t, strings.HasSuffix(exec, ".bin") || strings.HasSuffix(exec, ".exe"))
	}
}

func TestFileSync_getBinaryIDFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected int
	}{
		{
			name:     "valid ID in path",
			path:     "/data/binaries/123/hashcat.exe",
			expected: 123,
		},
		{
			name:     "ID in filename",
			path:     "/data/binaries/binary_456.7z",
			expected: 456,
		},
		{
			name:     "no ID in path",
			path:     "/data/binaries/hashcat/hashcat.exe",
			expected: 0,
		},
		{
			name:     "complex path with ID",
			path:     "/home/user/data/binaries/789/subfolder/tool.bin",
			expected: 789,
		},
	}

	fs := &FileSync{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := fs.getBinaryIDFromPath(tt.path)
			assert.Equal(t, tt.expected, id)
		})
	}
}

func TestFileSync_DownloadFileFromInfo(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authentication headers
		assert.Equal(t, "test-api-key", r.Header.Get("X-API-Key"))
		assert.Equal(t, "agent-123", r.Header.Get("X-Agent-ID"))

		switch r.URL.Path {
		case "/api/sync/download/wordlist/1":
			// Successful download
			content := []byte("test wordlist content")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
			w.Header().Set("X-MD5-Hash", calculateMD5Hash(content))
			w.Write(content)
		case "/api/sync/download/rule/2":
			// Server error
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/sync/download/binary/3":
			// Wrong checksum
			content := []byte("binary content")
			w.Header().Set("X-MD5-Hash", "wrong_checksum")
			w.Write(content)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create test file sync
	tempDir := t.TempDir()
	fs := &FileSync{
		client: &http.Client{Timeout: 30 * time.Second},
		urlConfig: &config.URLConfig{
			BaseURL: server.URL,
		},
		dataDirs: &config.DataDirs{
			Wordlists: filepath.Join(tempDir, "wordlists"),
			Rules:     filepath.Join(tempDir, "rules"),
			Binaries:  filepath.Join(tempDir, "binaries"),
		},
		apiKey:  "test-api-key",
		agentID: "agent-123",
	}

	// Create directories
	os.MkdirAll(fs.dataDirs.Wordlists, 0755)
	os.MkdirAll(fs.dataDirs.Rules, 0755)
	os.MkdirAll(fs.dataDirs.Binaries, 0755)

	ctx := context.Background()

	tests := []struct {
		name          string
		fileInfo      FileInfo
		wantErr       bool
		errorContains string
	}{
		{
			name: "successful download",
			fileInfo: FileInfo{
				Name:     "test.txt",
				FileType: "wordlist",
				ID:       1,
				MD5Hash:  calculateMD5Hash([]byte("test wordlist content")),
			},
			wantErr: false,
		},
		{
			name: "server error",
			fileInfo: FileInfo{
				Name:     "test.rule",
				FileType: "rule",
				ID:       2,
			},
			wantErr:       true,
			errorContains: "unexpected status code: 500",
		},
		{
			name: "checksum mismatch",
			fileInfo: FileInfo{
				Name:     "binary.exe",
				FileType: "binary",
				ID:       3,
				MD5Hash:  "expected_checksum",
			},
			wantErr:       true,
			errorContains: "checksum mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fs.DownloadFileFromInfo(ctx, &tt.fileInfo)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				// Verify file was created
				filePath := filepath.Join(fs.dataDirs.Wordlists, tt.fileInfo.Name)
				assert.FileExists(t, filePath)
			}
		})
	}
}

func TestFileSync_DownloadFileWithInfoRetry(t *testing.T) {
	attempts := 0
	// Create test server that fails first attempts
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Fail first 2 attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed on 3rd attempt
		content := []byte("success after retries")
		w.Header().Set("X-MD5-Hash", calculateMD5Hash(content))
		w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	fs := &FileSync{
		client: &http.Client{Timeout: 30 * time.Second},
		urlConfig: &config.URLConfig{
			BaseURL: server.URL,
		},
		dataDirs: &config.DataDirs{
			Wordlists: filepath.Join(tempDir, "wordlists"),
		},
		maxRetries: 3,
		apiKey:     "test-key",
		agentID:    "123",
	}

	os.MkdirAll(fs.dataDirs.Wordlists, 0755)

	fileInfo := FileInfo{
		Name:     "retry.txt",
		FileType: "wordlist",
		ID:       1,
		MD5Hash:  calculateMD5Hash([]byte("success after retries")),
	}

	ctx := context.Background()
	err := fs.DownloadFileWithInfoRetry(ctx, &fileInfo, 0)
	assert.NoError(t, err)
	assert.Equal(t, 3, attempts) // Should have tried 3 times
}

func TestFileSync_SyncDirectory(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/files/wordlist/list":
			// Return server file list
			files := []FileInfo{
				{
					Name:     "new-file.txt",
					FileType: "wordlist",
					ID:       1,
					MD5Hash:  calculateMD5Hash([]byte("new content")),
					Size:     11,
				},
				{
					Name:     "existing.txt",
					FileType: "wordlist",
					ID:       2,
					MD5Hash:  calculateMD5Hash([]byte("existing content")),
					Size:     16,
				},
			}
			json.NewEncoder(w).Encode(files)
		case "/api/sync/download/wordlist/1":
			// Download new file
			content := []byte("new content")
			w.Header().Set("X-MD5-Hash", calculateMD5Hash(content))
			w.Write(content)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Setup file sync
	tempDir := t.TempDir()
	wordlistDir := filepath.Join(tempDir, "wordlists")
	os.MkdirAll(wordlistDir, 0755)

	// Create existing file with correct hash
	existingContent := []byte("existing content")
	err := ioutil.WriteFile(filepath.Join(wordlistDir, "existing.txt"), existingContent, 0644)
	require.NoError(t, err)

	fs := &FileSync{
		client: &http.Client{Timeout: 30 * time.Second},
		urlConfig: &config.URLConfig{
			BaseURL: server.URL,
		},
		dataDirs: &config.DataDirs{
			Wordlists: wordlistDir,
		},
		apiKey:  "test-key",
		agentID: "123",
		sem:     make(chan struct{}, 3),
	}

	// Sync directory
	ctx := context.Background()
	err = fs.SyncDirectory(ctx, "wordlist")
	assert.NoError(t, err)

	// Verify new file exists
	assert.FileExists(t, filepath.Join(wordlistDir, "new-file.txt"))
}

// Superseded by TestEnsureBinaryExtracted_* in extract_test.go, which drive a
// real checked-in archive rather than skipping. Kept only for the error path.
func TestFileSync_ExtractBinary7z(t *testing.T) {

	fs := &FileSync{
		dataDirs: &config.DataDirs{
			Binaries: t.TempDir(),
		},
	}

	// A missing archive must be an error, not a silent success that leaves the
	// caller believing a binary is available.
	err := fs.ExtractBinary7z("/nonexistent.7z", t.TempDir())
	assert.Error(t, err)
}

func TestFileSync_LoadCACertificate(t *testing.T) {
	// Create test config directory
	tempDir := t.TempDir()
	t.Setenv("KH_CONFIG_DIR", tempDir)

	// Test missing certificate
	pool, err := loadCACertificate()
	assert.Error(t, err)
	assert.Nil(t, pool)

	// Create valid certificate
	certPath := filepath.Join(tempDir, "ca.crt")
	err = ioutil.WriteFile(certPath, []byte(testCACert), 0644)
	require.NoError(t, err)

	// Test loading valid certificate
	pool, err = loadCACertificate()
	assert.NoError(t, err)
	assert.NotNil(t, pool)

	// Create invalid certificate
	err = ioutil.WriteFile(certPath, []byte("invalid cert"), 0644)
	require.NoError(t, err)

	// Test loading invalid certificate
	pool, err = loadCACertificate()
	assert.Error(t, err)
	assert.Nil(t, pool)
}

// Helper function to calculate MD5
func calculateMD5Hash(data []byte) string {
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

// Test CA certificate
const testCACert = `-----BEGIN CERTIFICATE-----
MIIDazCCAlOgAwIBAgIUJeohtgk8nnt8ofratXJg7kUJsI4wDQYJKoZIhvcNAQEL
BQAwRTELMAkGA1UEBhMCVVMxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDAeFw0yMTA5MTQwMzQ0NTRaFw0zMTA5
MTIwMzQ0NTRaMEUxCzAJBgNVBAYTAlVTMRMwEQYDVQQIDApTb21lLVN0YXRlMSEw
HwYDVQQKDBhJbnRlcm5ldCBXaWRnaXRzIFB0eSBMdGQwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQC7W8rjAhMRbxLaDmEzFTe7PfGPcXgY6/zV+0U8DbwG
F0rAFJyQOCDqxLV7isCBcdtik6dZH8cnDfnYPJgsP8Ga8cp6JTLD0EbB5gILlYfQ
Nq1P5CJ7BF4rXBKvzn8nKOvqkuOa3p7cPQPjYYZPaHYMTDw7vPNgmgWZK6nYLKNP
8UIZp7gGXfBi1qQi9kzAP3YbEvxQDPj0P9RNGEdKGvabL9zPlnLVRLWVI0khM6xw
p7bR4GGacRuDVojL4p2vLdCSW+r4A1eFWYgvfRcT5yzCGKBeUMA6hxR7SlFebSMk
Ojr0965l4rJWqx1yfLJh0J0dHnxXJmZKUlEUkwQcpXQJAgMBAAGjUzBRMB0GA1Ud
DgQWBBR+7+AHor818sn8kUgBa1HNZRqbkzAfBgNVHSMEGDAWgBR+7+AHor818sn8
kUgBa1HNZRqbkzAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQBo
5BjMUrnPM6hgKnCT2XRmP/XaT9pxTzT0xJCqBJpwCCqBnZdQYdNNjp+RWBmVxh6d
FdMVJCLmeYiegsSJ3XnFD8J13zO5PK6cQqF4C0VuHlHJxAqcZiiX0GJLLc3lOKNb
J2WzLy2qc0DteM8vBUHIu6x5bmiTNe/E9Z1nU1wENWezQzGBtQNFEiN6Ot+lUzma
sF+I2hqpbVPj3qYGxDGkJeFrF5d9dC1vwqTSFwmJP1F6xiVPAjPZCPCK3cd0qnSW
uxFPb0pPFHJPdCNhHQfjeKfwEOQdX7KdMPBdAX8N6cEisU4R5LoGwfOJVPW6xDH0
gL4HgWOvn6zG9SrXrZH+
-----END CERTIFICATE-----`
