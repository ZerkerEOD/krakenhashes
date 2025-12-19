package binary

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// Handler handles binary management HTTP requests
type Handler struct {
	manager binary.Manager
}

// NewHandler creates a new binary management handler
func NewHandler(manager binary.Manager) *Handler {
	return &Handler{manager: manager}
}

// Request/Response types
type AddVersionRequest struct {
	BinaryType      binary.BinaryType      `json:"binary_type"`
	CompressionType binary.CompressionType `json:"compression_type"`
	SourceURL       string                 `json:"source_url"`
	FileName        string                 `json:"file_name"`
	MD5Hash         string                 `json:"md5_hash,omitempty"`
	SetAsDefault    bool                   `json:"set_as_default,omitempty"`
	Description     string                 `json:"description,omitempty"`
	Version         string                 `json:"version,omitempty"`
}

// HandleAddVersion handles adding a new binary version
func (h *Handler) HandleAddVersion(w http.ResponseWriter, r *http.Request) {
	var req AddVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get user ID from context
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse user ID to UUID
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		debug.Error("Invalid user ID format: %v", err)
		http.Error(w, "Invalid user ID", http.StatusInternalServerError)
		return
	}

	// Handle optional description and version
	var description *string
	if req.Description != "" {
		description = &req.Description
	}
	var versionStr *string
	if req.Version != "" {
		versionStr = &req.Version
	}

	version := &binary.BinaryVersion{
		BinaryType:      req.BinaryType,
		CompressionType: req.CompressionType,
		SourceURL:       req.SourceURL,
		FileName:        req.FileName,
		MD5Hash:         req.MD5Hash,
		CreatedBy:       userUUID,
		IsActive:        true,
		IsDefault:       req.SetAsDefault,
		SourceType:      binary.SourceTypeURL,
		Description:     description,
		Version:         versionStr,
	}

	if err := h.manager.AddVersion(r.Context(), version); err != nil {
		debug.Error("Failed to add binary version: %v", err)
		http.Error(w, "Failed to add binary version", http.StatusInternalServerError)
		return
	}

	// If requested to set as default, do it after successful creation
	if req.SetAsDefault && version.ID > 0 {
		if err := h.manager.SetDefaultVersion(r.Context(), version.ID); err != nil {
			debug.Warning("Failed to set new binary as default: %v", err)
			// Don't fail the whole operation, just log the warning
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(version)
}

// HandleListVersions handles listing binary versions with optional filters
func (h *Handler) HandleListVersions(w http.ResponseWriter, r *http.Request) {
	filters := make(map[string]interface{})

	// Parse query parameters
	if binaryType := r.URL.Query().Get("type"); binaryType != "" {
		filters["binary_type"] = binary.BinaryType(binaryType)
	}
	if isActive := r.URL.Query().Get("active"); isActive != "" {
		filters["is_active"] = isActive == "true"
	}
	if status := r.URL.Query().Get("status"); status != "" {
		filters["verification_status"] = binary.VerificationStatus(status)
	}

	versions, err := h.manager.ListVersions(r.Context(), filters)
	if err != nil {
		debug.Error("Failed to list binary versions: %v", err)
		http.Error(w, "Failed to list binary versions", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(versions)
}

// HandleGetVersion handles retrieving a specific binary version
func (h *Handler) HandleGetVersion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid version ID", http.StatusBadRequest)
		return
	}

	version, err := h.manager.GetVersion(r.Context(), id)
	if err != nil {
		debug.Error("Failed to get binary version: %v", err)
		http.Error(w, "Failed to get binary version", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(version)
}

// HandleDeleteVersion handles deleting/deactivating a binary version
func (h *Handler) HandleDeleteVersion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid version ID", http.StatusBadRequest)
		return
	}

	if err := h.manager.DeleteVersion(r.Context(), id); err != nil {
		// Check if it's a protection error
		if strings.Contains(err.Error(), "cannot delete the only remaining binary") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		debug.Error("Failed to delete binary version: %v", err)
		http.Error(w, "Failed to delete binary version", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleVerifyVersion handles verifying a binary version's integrity
func (h *Handler) HandleVerifyVersion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid version ID", http.StatusBadRequest)
		return
	}

	if err := h.manager.VerifyVersion(r.Context(), id); err != nil {
		debug.Error("Failed to verify binary version: %v", err)
		http.Error(w, "Failed to verify binary version", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// HandleSetDefaultVersion handles setting a binary version as default
func (h *Handler) HandleSetDefaultVersion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid version ID", http.StatusBadRequest)
		return
	}

	if err := h.manager.SetDefaultVersion(r.Context(), id); err != nil {
		debug.Error("Failed to set default binary version: %v", err)
		http.Error(w, "Failed to set default binary version", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Binary version set as default successfully",
	})
}

// HandleGetLatestVersion handles retrieving the latest active version of a binary type
func (h *Handler) HandleGetLatestVersion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	binaryType := binary.BinaryType(vars["type"])

	version, err := h.manager.GetLatestActive(r.Context(), binaryType)
	if err != nil {
		debug.Error("Failed to get latest binary version: %v", err)
		http.Error(w, "Failed to get latest binary version", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(version)
}

// HandleDownloadBinary handles binary file downloads
func (h *Handler) HandleDownloadBinary(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid version ID", http.StatusBadRequest)
		return
	}

	// Get binary version details
	version, err := h.manager.GetVersion(r.Context(), id)
	if err != nil {
		debug.Error("Failed to get binary version: %v", err)
		http.Error(w, "Failed to get binary version", http.StatusInternalServerError)
		return
	}

	// Open the binary file
	filePath := filepath.Join("data", "binaries", fmt.Sprintf("%d", version.ID), version.FileName)
	file, err := os.Open(filePath)
	if err != nil {
		debug.Error("Failed to open binary file: %v", err)
		http.Error(w, "Failed to open binary file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Set response headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", version.FileName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", version.FileSize))

	// Stream the file
	if _, err := io.Copy(w, file); err != nil {
		debug.Error("Failed to stream binary file: %v", err)
		return
	}
}

// HandleUploadVersion handles uploading a new binary version directly
func (h *Handler) HandleUploadVersion(w http.ResponseWriter, r *http.Request) {
	// 200MB limit for binary uploads
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)

	// Parse multipart form (32MB memory limit, rest goes to temp files)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		debug.Error("Failed to parse multipart form: %v", err)
		http.Error(w, "File too large or invalid form", http.StatusBadRequest)
		return
	}

	// Get user ID from context
	userID, ok := r.Context().Value("user_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse user ID to UUID
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		debug.Error("Invalid user ID format: %v", err)
		http.Error(w, "Invalid user ID", http.StatusInternalServerError)
		return
	}

	// Get form fields
	binaryType := r.FormValue("binary_type")
	compressionType := r.FormValue("compression_type")
	fileName := r.FormValue("file_name")
	versionStr := r.FormValue("version")
	description := r.FormValue("description")
	setAsDefault := r.FormValue("set_as_default") == "true"

	// Validate required fields
	if binaryType == "" || compressionType == "" || fileName == "" {
		http.Error(w, "binary_type, compression_type, and file_name are required", http.StatusBadRequest)
		return
	}

	// Get uploaded file
	file, header, err := r.FormFile("file")
	if err != nil {
		debug.Error("Failed to get uploaded file: %v", err)
		http.Error(w, "No file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Handle optional fields
	var descriptionPtr *string
	if description != "" {
		descriptionPtr = &description
	}
	var versionPtr *string
	if versionStr != "" {
		versionPtr = &versionStr
	}

	// Create version struct
	version := &binary.BinaryVersion{
		BinaryType:      binary.BinaryType(binaryType),
		CompressionType: binary.CompressionType(compressionType),
		FileName:        fileName,
		CreatedBy:       userUUID,
		IsActive:        true,
		IsDefault:       setAsDefault,
		SourceType:      binary.SourceTypeUpload,
		Description:     descriptionPtr,
		Version:         versionPtr,
	}

	// Call manager to handle the upload
	if err := h.manager.AddVersionFromUpload(r.Context(), version, file, header.Size); err != nil {
		debug.Error("Failed to upload binary version: %v", err)
		http.Error(w, "Failed to upload binary", http.StatusInternalServerError)
		return
	}

	// If requested to set as default, do it after successful creation
	if setAsDefault && version.ID > 0 {
		if err := h.manager.SetDefaultVersion(r.Context(), version.ID); err != nil {
			debug.Warning("Failed to set new binary as default: %v", err)
			// Don't fail the whole operation, just log the warning
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(version)
}
