package httputil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// MaxFormFieldSize bounds in-memory reads of individual multipart form field
// values (metadata such as names, descriptions, and tags). Uploaded file content
// is streamed separately and is not subject to this limit.
const MaxFormFieldSize = 1 << 20 // 1 MiB

// ReadFormField reads a single multipart form field value with a hard cap of
// MaxFormFieldSize, returning an error if the field exceeds it. Use it in place
// of io.ReadAll on multipart field readers so an oversized non-file field cannot
// force unbounded memory allocation.
func ReadFormField(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFormFieldSize+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxFormFieldSize {
		return "", fmt.Errorf("form field exceeds maximum size of %d bytes", MaxFormFieldSize)
	}
	return string(data), nil
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// RespondWithError sends an error response with the given status code and message
func RespondWithError(w http.ResponseWriter, code int, message string) {
	RespondWithJSON(w, code, ErrorResponse{Error: message})
}

// RespondWithJSON sends a JSON response with the given status code and data
func RespondWithJSON(w http.ResponseWriter, code int, data interface{}) {
	// Set content type
	w.Header().Set("Content-Type", "application/json")

	// Set status code
	w.WriteHeader(code)

	// Encode response
	if err := json.NewEncoder(w).Encode(data); err != nil {
		debug.Error("Failed to encode JSON response: %v", err)
		// If we can't encode the response, send a plain text error
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ParseJSONBody parses the request body into the given struct
func ParseJSONBody(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// GetQueryParam gets a query parameter from the request
func GetQueryParam(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

// GetQueryParamWithDefault gets a query parameter from the request with a default value
func GetQueryParamWithDefault(r *http.Request, key, defaultValue string) string {
	value := r.URL.Query().Get(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// GetBoolQueryParam gets a boolean query parameter from the request
func GetBoolQueryParam(r *http.Request, key string) bool {
	value := r.URL.Query().Get(key)
	return value == "true" || value == "1" || value == "yes"
}

// GetIntQueryParam gets an integer query parameter from the request
func GetIntQueryParam(r *http.Request, key string, defaultValue int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return defaultValue
	}

	intValue, err := json.Number(value).Int64()
	if err != nil {
		return defaultValue
	}

	return int(intValue)
}
