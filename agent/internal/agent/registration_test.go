package agent

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationRequest_JSON(t *testing.T) {
	req := RegistrationRequest{
		ClaimCode: "test-claim-code",
		Hostname:  "test-host",
	}

	// Marshal
	data, err := json.Marshal(req)
	require.NoError(t, err)

	// Unmarshal
	var decoded RegistrationRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, req.ClaimCode, decoded.ClaimCode)
	assert.Equal(t, req.Hostname, decoded.Hostname)
}

func TestRegistrationResponse_JSON(t *testing.T) {
	resp := RegistrationResponse{
		AgentID:       123,
		DownloadToken: "download-token",
		Endpoints: map[string]string{
			"websocket": "wss://test.example.com/ws",
			"api":       "https://test.example.com/api",
		},
		APIKey:        "test-api-key",
		Certificate:   "test-cert",
		PrivateKey:    "test-key",
		CACertificate: "test-ca",
	}

	// Marshal
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	// Unmarshal
	var decoded RegistrationResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, resp.AgentID, decoded.AgentID)
	assert.Equal(t, resp.APIKey, decoded.APIKey)
	assert.Equal(t, resp.Endpoints["websocket"], decoded.Endpoints["websocket"])
}

func TestGetHostname(t *testing.T) {
	hostname, err := getHostname()
	require.NoError(t, err)
	assert.NotEmpty(t, hostname)
}

func TestCalculateChecksum(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "empty data",
			data:     []byte{},
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "test data",
			data:     []byte("test data"),
			expected: "916f0027a575074ce72a331777c3478d6513f786a591bd892da1a577bf2335f9",
		},
		{
			name:     "binary data",
			data:     []byte{0x00, 0x01, 0x02, 0x03},
			expected: "054edec1d0211f624fed0cbca9d4f9400b0e491c43742af2c5b0abebf0c990d8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checksum := calculateChecksum(tt.data)
			assert.Equal(t, tt.expected, checksum)
		})
	}
}

func TestVerifyChecksum(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		checksum string
		expected bool
	}{
		{
			name:     "valid checksum",
			data:     []byte("test data"),
			checksum: "916f0027a575074ce72a331777c3478d6513f786a591bd892da1a577bf2335f9",
			expected: true,
		},
		{
			name:     "invalid checksum",
			data:     []byte("test data"),
			checksum: "invalid",
			expected: false,
		},
		{
			name:     "mismatched checksum",
			data:     []byte("test data"),
			checksum: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := verifyChecksum(tt.data, tt.checksum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFileWithChecksum(t *testing.T) {
	data := []byte("test file content")
	expectedChecksum := calculateChecksum(data)

	file := FileWithChecksum{
		Data:     data,
		Checksum: expectedChecksum,
	}

	// Verify checksum
	assert.True(t, verifyChecksum(file.Data, file.Checksum))

	// Modify data and verify checksum fails
	file.Data = append(file.Data, byte('x'))
	assert.False(t, verifyChecksum(file.Data, file.Checksum))
}

func TestCleanupStaleLocks(t *testing.T) {
	// Save current state
	oldFileLocks := fileLocks
	oldLastUsed := lastUsed
	oldLockTimeout := lockTimeout
	defer func() {
		fileLocks = oldFileLocks
		lastUsed = oldLastUsed
		lockTimeout = oldLockTimeout
	}()

	// Set short timeout for testing
	lockTimeout = 100 * time.Millisecond

	// Reset for test
	fileLocks = make(map[string]*sync.Mutex)
	lastUsed = make(map[string]time.Time)

	// Add some locks
	path1 := "/test/path1"
	path2 := "/test/path2"
	path3 := "/test/path3"

	fileLocks[path1] = &sync.Mutex{}
	fileLocks[path2] = &sync.Mutex{}
	fileLocks[path3] = &sync.Mutex{}

	// Set timestamps
	lastUsed[path1] = time.Now().Add(-2 * lockTimeout)                // Stale
	lastUsed[path2] = time.Now()                                      // Fresh
	lastUsed[path3] = time.Now().Add(-lockTimeout - time.Millisecond) // Stale

	// Cleanup
	cleanupStaleLocks()

	// Verify stale locks removed
	_, exists1 := fileLocks[path1]
	assert.False(t, exists1, "stale lock 1 should be removed")

	_, exists2 := fileLocks[path2]
	assert.True(t, exists2, "fresh lock should remain")

	_, exists3 := fileLocks[path3]
	assert.False(t, exists3, "stale lock 3 should be removed")
}

func TestGetFileLock(t *testing.T) {
	// Save current state
	oldFileLocks := fileLocks
	oldLastUsed := lastUsed
	defer func() {
		fileLocks = oldFileLocks
		lastUsed = oldLastUsed
	}()

	// Reset for test
	fileLocks = make(map[string]*sync.Mutex)
	lastUsed = make(map[string]time.Time)

	path := "/test/file.txt"

	// First call - should create new lock
	lock1 := getFileLock(path)
	assert.NotNil(t, lock1)
	assert.Len(t, fileLocks, 1)

	// Second call - should return same lock
	lock2 := getFileLock(path)
	assert.Equal(t, lock1, lock2)
	assert.Len(t, fileLocks, 1)

	// Verify last used time updated
	time1 := lastUsed[path]
	time.Sleep(10 * time.Millisecond)
	_ = getFileLock(path)
	time2 := lastUsed[path]
	assert.True(t, time2.After(time1))
}

/*
 * generateTestCert produces a real self-signed certificate and key.
 *
 * The literals this replaced were hand-assembled PEM that no X.509 parser
 * accepts — openssl rejects them too — so RegisterAgent failed at "failed to
 * parse CA certificate" and the test had been red for as long as it existed.
 * A fixture that cannot be parsed tests only the error path.
 *
 * Generated per run rather than checked in, so there is no expiry date to come
 * back and break this in a few years.
 */
func generateTestCert(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

func TestRegistrationIntegration(t *testing.T) {
	// RegisterAgent writes its config relative to the working directory, which
	// for a Go test is the package directory — so without this the run leaves
	// an untracked config/ca.crt in the source tree that then shows up in
	// everyone's `git status`.
	//
	// t.Chdir would be the obvious tool but needs go1.24, and this module
	// declares go1.23.1; bumping a module's language version as a side effect
	// of fixing a test is not a trade worth making.
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	certPEM, keyPEM := generateTestCert(t)

	// Create test server
	var registrationCalled bool
	var caCertRequested bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ca.crt":
			caCertRequested = true
			// Return a test CA certificate (self-signed for testing)
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Write([]byte(certPEM))

		case "/api/agent/register":
			registrationCalled = true

			// Verify request
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var req RegistrationRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			assert.Equal(t, "test-claim-code", req.ClaimCode)
			assert.NotEmpty(t, req.Hostname)

			// Send response with valid PEM certificates
			resp := RegistrationResponse{
				AgentID:       123,
				APIKey:        "test-api-key",
				DownloadToken: "test-download-token",
				Certificate:   certPEM,
				PrivateKey:    keyPEM,
				CACertificate: certPEM,
				Endpoints: map[string]string{
					"websocket": "wss://test.example.com/ws",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create URL config
	// Extract just the port number from server.URL (http://127.0.0.1:PORT)
	serverHost := server.URL[7:] // Remove "http://"
	httpPort := serverHost[strings.LastIndex(serverHost, ":")+1:]

	urlConfig := &config.URLConfig{
		BaseURL:      server.URL,
		WebSocketURL: "ws://localhost:8080/ws",
		HTTPPort:     httpPort,
	}

	// Register agent
	err = RegisterAgent("test-claim-code", urlConfig)
	assert.NoError(t, err)
	assert.True(t, caCertRequested, "CA certificate should be requested")
	assert.True(t, registrationCalled, "Registration endpoint should be called")
}
