package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/jwt"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// SetTestJWTSecret sets the JWT_SECRET environment variable for testing
func SetTestJWTSecret(t *testing.T) {
	t.Helper()

	oldSecret := os.Getenv("JWT_SECRET")
	os.Setenv("JWT_SECRET", TestJWTSecret)

	t.Cleanup(func() {
		if oldSecret != "" {
			os.Setenv("JWT_SECRET", oldSecret)
		} else {
			os.Unsetenv("JWT_SECRET")
		}
	})
}

// MakeAuthenticatedRequest creates an HTTP request with a valid auth token
func MakeAuthenticatedRequest(t *testing.T, method, url string, body interface{}, userID, role string) *http.Request {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req := httptest.NewRequest(method, url, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Generate a valid token with 60 minute expiry for tests
	token, err := jwt.GenerateToken(userID, role, 60)
	if err != nil {
		t.Fatalf("Failed to generate auth token: %v", err)
	}

	// Set auth cookie
	req.AddCookie(&http.Cookie{
		Name:  "token",
		Value: token,
	})

	// Add user_id to context (mimics what auth middleware does)
	ctx := context.WithValue(req.Context(), "user_id", userID)
	req = req.WithContext(ctx)

	return req
}

// MakeRequest creates a basic HTTP request
func MakeRequest(t *testing.T, method, url string, body interface{}) *http.Request {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req := httptest.NewRequest(method, url, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req
}

// AssertJSONResponse checks that the response has the expected status and decodes JSON
func AssertJSONResponse(t *testing.T, rr *httptest.ResponseRecorder, expectedStatus int, v interface{}) {
	t.Helper()

	if rr.Code != expectedStatus {
		t.Errorf("Expected status %d, got %d. Body: %s", expectedStatus, rr.Code, rr.Body.String())
	}

	if v != nil && rr.Body.Len() > 0 {
		if err := json.NewDecoder(rr.Body).Decode(v); err != nil {
			t.Errorf("Failed to decode JSON response: %v. Body: %s", err, rr.Body.String())
		}
	}
}

// AssertCookieSet checks that a cookie with the given name was set
func AssertCookieSet(t *testing.T, rr *httptest.ResponseRecorder, cookieName string) *http.Cookie {
	t.Helper()

	cookies := rr.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Name == cookieName {
			return cookie
		}
	}

	// Fail fast: returning nil here made every caller nil-deref the result and
	// panic, which aborts the whole test binary and is a root cause of the
	// varying failure count (GH #89).
	t.Fatalf("Expected cookie %s to be set, but it was not", cookieName)
	return nil
}

// AssertCookieDeleted checks that a cookie was deleted (MaxAge < 0)
func AssertCookieDeleted(t *testing.T, rr *httptest.ResponseRecorder, cookieName string) {
	t.Helper()

	cookie := AssertCookieSet(t, rr, cookieName)
	if cookie != nil && cookie.MaxAge >= 0 {
		t.Errorf("Expected cookie %s to be deleted (MaxAge < 0), but MaxAge was %d", cookieName, cookie.MaxAge)
	}
}

// GenerateTOTPCode generates a valid TOTP code for a base32 secret using the
// SAME parameters production validates with (SHA512, 6 digits, 30s period) —
// see handlers/auth/mfa.go. The old stub returned a fixed "123456", which never
// validated against SHA512 and made every TOTP test fail (GH #89).
func GenerateTOTPCode(secret string) (string, error) {
	return GenerateTOTPCodeAt(secret, time.Now().UTC())
}

// GenerateTOTPCodeAt is GenerateTOTPCode for a specific instant (time-window tests).
func GenerateTOTPCodeAt(secret string, at time.Time) (string, error) {
	return totp.GenerateCodeCustom(secret, at.UTC(), totp.ValidateOpts{
		Period:    30,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA512,
	})
}

// GetRespBool returns resp[key] as a bool, failing the test (not panicking)
// when the key is missing or not a bool. A bare resp[key].(bool) panics on an
// unexpected response shape (for example an error body that never carries the
// field), and a panic in a Go test aborts the whole test binary — so the number
// of tests that get to run, and thus the reported failure count, varies from run
// to run. That nondeterminism is the core of GH #89; failing here keeps the
// fault local to the one assertion instead.
func GetRespBool(t *testing.T, resp map[string]interface{}, key string) bool {
	t.Helper()
	v, ok := resp[key].(bool)
	if !ok {
		t.Fatalf("expected response field %q to be a bool, got %T (full response: %v)", key, resp[key], resp)
	}
	return v
}

// GetRespString is GetRespBool for a string field.
func GetRespString(t *testing.T, resp map[string]interface{}, key string) string {
	t.Helper()
	v, ok := resp[key].(string)
	if !ok {
		t.Fatalf("expected response field %q to be a string, got %T (full response: %v)", key, resp[key], resp)
	}
	return v
}

// GetRespFloat64 is GetRespBool for a JSON number field (encoding/json decodes
// every JSON number into a float64).
func GetRespFloat64(t *testing.T, resp map[string]interface{}, key string) float64 {
	t.Helper()
	v, ok := resp[key].(float64)
	if !ok {
		t.Fatalf("expected response field %q to be a number, got %T (full response: %v)", key, resp[key], resp)
	}
	return v
}

// WaitForCondition waits for a condition to be true or times out
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("Timeout waiting for condition: %s", message)
}
