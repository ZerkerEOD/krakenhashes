package agent

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/auth"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistration_StoreCredentials(t *testing.T) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "store-creds-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		apiKey        string
		agentID       int
		clientCert    string
		clientKey     string
		setupDir      func()
		wantErr       bool
		errorContains string
	}{
		{
			name:       "successful credential storage",
			apiKey:     "test-api-key",
			agentID:    123,
			clientCert: testClientCert,
			clientKey:  testClientKey,
			setupDir:   func() {},
			wantErr:    false,
		},
		{
			name:       "empty api key",
			apiKey:     "",
			agentID:    123,
			clientCert: testClientCert,
			clientKey:  testClientKey,
			setupDir:   func() {},
			wantErr:    true,
			errorContains: "API key is required",
		},
		{
			name:       "invalid agent ID",
			apiKey:     "test-api-key",
			agentID:    0,
			clientCert: testClientCert,
			clientKey:  testClientKey,
			setupDir:   func() {},
			wantErr:    true,
			errorContains: "invalid agent ID",
		},
		{
			name:       "directory creation failure",
			apiKey:     "test-api-key",
			agentID:    123,
			clientCert: testClientCert,
			clientKey:  testClientKey,
			setupDir: func() {
				// Create a file where directory should be
				err := ioutil.WriteFile(tempDir, []byte("block"), 0644)
				require.NoError(t, err)
			},
			wantErr:       true,
			errorContains: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test directory
			testDir := filepath.Join(tempDir, tt.name)
			if tt.setupDir != nil {
				tt.setupDir()
			}

			// Test store credentials
			err := storeCredentials(testDir, tt.apiKey, tt.agentID, tt.clientCert, tt.clientKey)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)

				// Verify files were created
				agentKeyPath := filepath.Join(testDir, "agent.key")
				clientCertPath := filepath.Join(testDir, "client.crt")
				clientKeyPath := filepath.Join(testDir, "client.key")

				assert.FileExists(t, agentKeyPath)
				assert.FileExists(t, clientCertPath)
				assert.FileExists(t, clientKeyPath)

				// Verify agent key content
				apiKey, agentID, err := auth.LoadAgentKey(testDir)
				assert.NoError(t, err)
				assert.Equal(t, tt.apiKey, apiKey)
				assert.Equal(t, "123", agentID)

				// Verify permissions
				info, _ := os.Stat(agentKeyPath)
				assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
			}
		})
	}
}

func TestRegistration_LoadCredentials(t *testing.T) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "load-creds-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		setupFiles  func()
		wantErr     bool
		wantAPIKey  string
		wantAgentID int
	}{
		{
			name: "successful load",
			setupFiles: func() {
				// Store valid credentials
				err := storeCredentials(tempDir, "test-api-key", 123, testClientCert, testClientKey)
				require.NoError(t, err)
			},
			wantErr:     false,
			wantAPIKey:  "test-api-key",
			wantAgentID: 123,
		},
		{
			name: "missing agent key file",
			setupFiles: func() {
				// Don't create any files
			},
			wantErr: true,
		},
		{
			name: "missing client certificate",
			setupFiles: func() {
				// Only create agent key
				err := auth.SaveAgentKey(tempDir, "test-api-key", "123")
				require.NoError(t, err)
			},
			wantErr: true,
		},
		{
			name: "corrupt agent key",
			setupFiles: func() {
				agentKeyPath := filepath.Join(tempDir, "agent.key")
				err := ioutil.WriteFile(agentKeyPath, []byte("invalid"), 0600)
				require.NoError(t, err)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean directory
			os.RemoveAll(tempDir)
			os.MkdirAll(tempDir, 0755)

			// Setup files
			if tt.setupFiles != nil {
				tt.setupFiles()
			}

			// Test load
			apiKey, agentID, clientCert, clientKey, err := LoadCredentials(tempDir)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantAPIKey, apiKey)
				assert.Equal(t, tt.wantAgentID, agentID)
				assert.NotEmpty(t, clientCert)
				assert.NotEmpty(t, clientKey)
			}
		})
	}
}

func TestRegistration_SendRegistrationRequest(t *testing.T) {
	tests := []struct {
		name          string
		serverURL     string
		claimCode     string
		hostname      string
		serverHandler func(w http.ResponseWriter, r *http.Request)
		wantErr       bool
		errorContains string
	}{
		{
			name:      "successful registration",
			serverURL: "", // Will be set by test server
			claimCode: "TEST-CLAIM-CODE",
			hostname:  "test-host",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "/api/agent/register", r.URL.Path)

				// Parse request body
				var req RegistrationRequest
				json.NewDecoder(r.Body).Decode(&req)
				assert.Equal(t, "TEST-CLAIM-CODE", req.ClaimCode)
				assert.Equal(t, "test-host", req.Hostname)

				// Send response
				resp := RegistrationResponse{
					Success:    true,
					APIKey:     "new-api-key",
					AgentID:    456,
					ClientCert: testClientCert,
					ClientKey:  testClientKey,
				}
				json.NewEncoder(w).Encode(resp)
			},
			wantErr: false,
		},
		{
			name:      "server error",
			serverURL: "",
			claimCode: "TEST-CLAIM-CODE",
			hostname:  "test-host",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "internal server error",
				})
			},
			wantErr:       true,
			errorContains: "registration failed",
		},
		{
			name:      "invalid response",
			serverURL: "",
			claimCode: "TEST-CLAIM-CODE",
			hostname:  "test-host",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("invalid json"))
			},
			wantErr:       true,
			errorContains: "failed to decode",
		},
		{
			name:      "registration rejected",
			serverURL: "",
			claimCode: "TEST-CLAIM-CODE",
			hostname:  "test-host",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := RegistrationResponse{
					Success: false,
					Error:   "invalid claim code",
				}
				json.NewEncoder(w).Encode(resp)
			},
			wantErr:       true,
			errorContains: "registration failed: invalid claim code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
			defer server.Close()

			// Set server URL if not provided
			serverURL := tt.serverURL
			if serverURL == "" {
				serverURL = server.URL
			}

			// Test registration request
			resp, err := sendRegistrationRequest(serverURL, tt.claimCode, tt.hostname)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.True(t, resp.Success)
				assert.NotEmpty(t, resp.APIKey)
			}
		})
	}
}

func TestRegistration_RegisterAgent(t *testing.T) {
	// Create temporary directory
	tempDir, err := ioutil.TempDir("", "register-agent-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		setupServer   func() *httptest.Server
		claimCode     string
		wantErr       bool
		errorContains string
	}{
		{
			name: "successful registration",
			setupServer: func() *httptest.Server {
				mux := http.NewServeMux()
				
				// CA certificate endpoint
				mux.HandleFunc("/ca-cert", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(testCACert))
				})

				// Registration endpoint
				mux.HandleFunc("/api/agent/register", func(w http.ResponseWriter, r *http.Request) {
					resp := RegistrationResponse{
						Success:    true,
						APIKey:     "new-api-key",
						AgentID:    789,
						ClientCert: testClientCert,
						ClientKey:  testClientKey,
					}
					json.NewEncoder(w).Encode(resp)
				})

				return httptest.NewServer(mux)
			},
			claimCode: "VALID-CODE",
			wantErr:   false,
		},
		{
			name: "CA download failure",
			setupServer: func() *httptest.Server {
				mux := http.NewServeMux()
				
				// CA certificate endpoint returns error
				mux.HandleFunc("/ca-cert", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				})

				return httptest.NewServer(mux)
			},
			claimCode:     "VALID-CODE",
			wantErr:       true,
			errorContains: "failed to download",
		},
		{
			name: "registration failure",
			setupServer: func() *httptest.Server {
				mux := http.NewServeMux()
				
				// CA certificate endpoint
				mux.HandleFunc("/ca-cert", func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(testCACert))
				})

				// Registration endpoint returns error
				mux.HandleFunc("/api/agent/register", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusBadRequest)
					json.NewEncoder(w).Encode(map[string]string{
						"error": "invalid claim code",
					})
				})

				return httptest.NewServer(mux)
			},
			claimCode:     "INVALID-CODE",
			wantErr:       true,
			errorContains: "registration request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup server
			server := tt.setupServer()
			defer server.Close()

			// Create config
			cfg := &config.Config{
				ServerURL: server.URL,
			}

			// Test registration
			err := RegisterAgent(cfg, tempDir, tt.claimCode)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)

				// Verify credentials were stored
				apiKey, agentID, clientCert, clientKey, err := LoadCredentials(tempDir)
				assert.NoError(t, err)
				assert.Equal(t, "new-api-key", apiKey)
				assert.Equal(t, 789, agentID)
				assert.NotEmpty(t, clientCert)
				assert.NotEmpty(t, clientKey)
			}
		})
	}
}

func TestRegistration_DownloadCACertificate(t *testing.T) {
	tests := []struct {
		name          string
		serverHandler func(w http.ResponseWriter, r *http.Request)
		wantErr       bool
		errorContains string
	}{
		{
			name: "successful download",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(testCACert))
			},
			wantErr: false,
		},
		{
			name: "server error",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:       true,
			errorContains: "unexpected status code: 500",
		},
		{
			name: "invalid certificate",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("invalid certificate"))
			},
			wantErr:       true,
			errorContains: "failed to parse",
		},
		{
			name: "empty response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Send nothing
			},
			wantErr:       true,
			errorContains: "empty response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
			defer server.Close()

			// Create temp directory
			tempDir, err := ioutil.TempDir("", "ca-cert-*")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)

			// Test download
			err = downloadCACertificate(server.URL, tempDir)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)

				// Verify certificate was saved
				caPath := filepath.Join(tempDir, "ca.crt")
				assert.FileExists(t, caPath)

				// Verify it's a valid certificate
				data, err := ioutil.ReadFile(caPath)
				assert.NoError(t, err)
				block, _ := pem.Decode(data)
				assert.NotNil(t, block)
				_, err = x509.ParseCertificate(block.Bytes)
				assert.NoError(t, err)
			}
		})
	}
}

// Test certificates
const testClientCert = `-----BEGIN CERTIFICATE-----
MIIDQTCCAimgAwIBAgIUJeohtgk8nnt8ofratXJg7kUJsI8wDQYJKoZIhvcNAQEL
BQAwRTELMAkGA1UEBhMCVVMxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDAeFw0yMTA5MTQwMzQ0NTVaFw0yMjA5
MTQwMzQ0NTVaMEUxCzAJBgNVBAYTAlVTMRMwEQYDVQQIDApTb21lLVN0YXRlMSEw
HwYDVQQKDBhJbnRlcm5ldCBXaWRnaXRzIFB0eSBMdGQwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQDJQg5NpJ8nFE5V4x3tNb7UqQIQYGPQBKXmQzMh6Xwd
W7+nj+IwnRnL7g4DZgQGKYMO3TnJQusXVyE3ehVkSFdeGJhuxTwRQ9MZXvZOAkLF
ORBUSqtk5HlhYtuMn/OjGH3f7SXqVe0aGTq7P8bxhh4n1PhTkKE3cBD2bJ6vfFWB
CKCnFHvGNdT0Kds8B0k7uPZsBv1qjB5rF2AwOGX8v4lL+2S2F7jAXVQpfTBGE7Eg
8FxqmNAYQSB2lNMJRcr6CCCZUlLqAKKZ6OiXRbfIPBwqIEqmGKGPxS8wFsFWlObU
KYNGJl8nKmcfhEuVN5kZIiWVmRBfp0FdYGWQysyPDGqpAgMBAAGjOjA4MAkGA1Ud
EwQCMAAwCwYDVR0PBAQDAgTwMB4GA1UdEQQXMBWCCWxvY2FsaG9zdIcEfwAAAYcE
wKgBATANBgkqhkiG9w0BAQsFAAOCAQEAY3VrEB3bHCAbZ+jDTxAC8rqMUVD8KU2O
V6Z9N4G/4E7LMH3o2HfY4VQPRkXl4w/j4Qm7Z3u5jXkPCNaKLlSmQX2L9kNP0DaI
Tc9kcjZdWk+bvTO4kGmF8JNQxFKCB3V8huj7cJ3qEbCuVr8OPpVn3ThHWBWJGGVj
7z0A/TNM/0czXKfLRVmMzbsVrfDV3L+WxVPH7RqZrpBCYI7tz+c5uQru6Y3e0V7T
TGmeNhMKqPkUPJ+x4JLUGGm3Qp0B9Hk/Lva9KqHC4fVA8uJ3LphMNHCvV9uw0Zl1
WHBqluA9GyaE8lwVSsFmOJ4EJnY5gvMKLBZhSxhPTnFBj4F/BpRG6Q==
-----END CERTIFICATE-----`

const testClientKey = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDJQg5NpJ8nFE5V
4x3tNb7UqQIQYGPQBKXmQzMh6XwdW7+nj+IwnRnL7g4DZgQGKYMO3TnJQusXVyE3
ehVkSFdeGJhuxTwRQ9MZXvZOAkLFORBUSqtk5HlhYtuMn/OjGH3f7SXqVe0aGTq7
P8bxhh4n1PhTkKE3cBD2bJ6vfFWBCKCnFHvGNdT0Kds8B0k7uPZsBv1qjB5rF2Aw
OGX8v4lL+2S2F7jAXVQpfTBGE7Eg8FxqmNAYQSB2lNMJRcr6CCCZUlLqAKKZ6OiX
RbfIPBwqIEqmGKGPxS8wFsFWlObUKYNGJl8nKmcfhEuVN5kZIiWVmRBfp0FdYGWQ
ysyPDGqpAgMBAAECggEADVlhk5pa1GyHEv4LdspQxQORh3p3tvCBLdP0S7LfQyLx
+jrGYlsH3XQCaESM8b1r9IdbrE7lnIHvLxATdmtD+sY1PHlcCcqhMGq9stsXDJTb
Cn4qRjVVEqLXRZLGAvaVQ7m6IWPVRaJ6omivywB3zxC8gW0DLTBO1ESFjQ1aP4TF
NCmFAglt2QHlL8QwpKDLOuKgs1NTjDDVmNnu3Qt1GMo5Q7jRLGvyIvLjdXTt9QNs
2IwIR+fSOj1acES3wzqqWsKYWD+6yRXdCZzdmVJ9xCGEk8hwcOCX0vLDjCJxESSd
q5FdP8UBu8vqbF4vtMB3coGsDvLfMCnw4LnAIllqAQKBgQDmUpKCM/JJ2Xz+VlXR
rsthgLyBKgNOtGfY63vMXXBlj9koQYEj7gn0P6oNVzzGJY8Dx1roJNaEcqiD6/IG
aVF6voW1ALZPIxj3vPpuQy5e1oPKlZBX8RLxC6YpD1bGR1RrE8pdHDYp6kATMHLZ
1f+x9ntpxYDnKMF2enWNBqhOKQKBgQDf9N6ntHxVjDrfJK0XiLSb6BB6iVLXwEHR
9qKxtJqARPpd3sAGuqPMEOY2QE3HOQ3DWFm7lyGjTB0F7IISV7hcpQzatdPZ+bkN
3TkEPVJkwKKTZPxQKMUmFxLFaFjTMAjHXYjFMgJ4MxJlLB2VVDEjMYaB8A2h8vEW
P9N9wfvVwQKBgG5l+1TaFMdXNKE3cBL21LbqHaZKFS5aAdi0aoYFfQCMpllBpKxl
/rIT7YkGfE3P5nYXBLAD0bN2f5kWa8iR6BwGG+vJDxqqBDL7SgbETyLTy0JG6cVd
b5K7cpOosskL7TPpGhvI7V7NPEHdjeR3G3dGSIldqJnpMR3S2xFgP5QxAoGBAMPt
gCID+pVATk1dNX0HhnbKufauhbzvHJQEP7jCH1+ly4mqYurJOm/tOPHmaVBJXpyy
+p8BDQDyZxHLyr4F2Xg6NC5cl4odE6HKHwjhz/kbs05lpwFK7KApVCoiF7vDZArr
SCIzVP9zQfXFH5rEMGK0VJlYdZkkCGB5RgCB9EgBAoGAU9looci0t4exmaterialized
HwZ8dNoDimD7Ew/Ckqvg8q+G8xPTuVQkRJMYVQOWhmQf2E5SrR9LSVFR8PH3akz0
iHfMALEPgfS3UEQF/tclPXLRGJ8GWLOOM3bDFxSxH9L3xfqpXW9RKvnVCGrQZFjP
L3aXxlSJkyS8iUAV2B5Hp2A=
-----END PRIVATE KEY-----`