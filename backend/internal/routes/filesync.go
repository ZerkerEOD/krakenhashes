package routes

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth/api"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// SetupFileDownloadRoutes configures routes for agent file downloads
func SetupFileDownloadRoutes(r *mux.Router, sqlDB *sql.DB, cfg *config.Config, agentService *services.AgentService) *http.ServeMux {
	debug.Info("Setting up file download routes for agents")

	// Create file repository
	dbWrapper := &db.DB{DB: sqlDB}
	fileRepo := repository.NewFileRepository(dbWrapper)

	// Create file download handlers
	fileRouter := r.PathPrefix("/api/files").Subrouter()
	fileRouter.Use(api.APIKeyMiddleware(agentService))

	// Handler for /api/files/{file_type}/{category}/{filename}
	fileRouter.HandleFunc("/{file_type}/{category}/{filename}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		fileType := vars["file_type"]
		category := vars["category"]
		filename := vars["filename"]

		debug.Info("File download request: type=%s, category=%s, name=%s", fileType, category, filename)

		var filePath string
		var fileSize int64
		var contentType string = "application/octet-stream"

		// Determine file path based on type
		switch fileType {
		case "wordlist":
			filePath = filepath.Join(cfg.DataDir, "wordlists", category, filename)
		case "rule":
			filePath = filepath.Join(cfg.DataDir, "rules", category, filename)
		case "binary":
			// Binary files are stored in directories named by their ID in the database
			// First try the provided category (might be an ID)
			filePath = filepath.Join(cfg.DataDir, "binaries", category, filename)

			// If the file doesn't exist at that path, query the database
			if _, err := os.Stat(filePath); err != nil {
				debug.Info("Binary file not found at primary path, querying database")

				// Create a short timeout context for the database query
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()

				// Query database for binary info
				binaries, err := fileRepo.GetBinaries(ctx, "")
				if err != nil {
					debug.Error("Failed to query database for binaries: %v", err)
					http.Error(w, "Server error", http.StatusInternalServerError)
					return
				}

				// Look for matching filename
				var binaryID int
				found := false
				for _, binary := range binaries {
					if binary.Name == filename {
						binaryID = binary.ID
						found = true
						break
					}
				}

				if found {
					filePath = filepath.Join(cfg.DataDir, "binaries", fmt.Sprintf("%d", binaryID), filename)
					debug.Info("Found binary ID %d for file %s", binaryID, filename)
				} else {
					debug.Error("Binary file not found in database: %s", filename)
					http.Error(w, "File not found", http.StatusNotFound)
					return
				}
			}
		default:
			debug.Error("Unknown file type: %s", fileType)
			http.Error(w, "Unknown file type", http.StatusBadRequest)
			return
		}

		debug.Info("Looking for file at path: %s", filePath)

		// Check if file exists
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			debug.Error("File not found: %s", filePath)
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		fileSize = fileInfo.Size()

		// Open file
		file, err := os.Open(filePath)
		if err != nil {
			debug.Error("Failed to open file: %s - %v", filePath, err)
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Set headers
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(filename)))
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))

		// Stream file to response
		if _, err := io.Copy(w, file); err != nil {
			debug.Error("Failed to stream file: %v", err)
			// Can't send error response here as headers are already sent
		}
	}).Methods(http.MethodGet)

	// Fallback handler for legacy format: /api/files/{file_type}/{filename}
	// where filename might contain path information
	fileRouter.HandleFunc("/{file_type}/{filename:.*}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		fileType := vars["file_type"]
		filename := vars["filename"]

		debug.Info("Legacy file download request: type=%s, name=%s", fileType, filename)

		var filePath string
		var fileSize int64
		var contentType string = "application/octet-stream"

		// Determine file path based on type
		switch fileType {
		case "wordlist":
			// Extract category from filename (e.g., general/file.txt -> general)
			parts := strings.Split(filename, "/")
			if len(parts) < 2 {
				debug.Error("Invalid wordlist filename format: %s", filename)
				http.Error(w, "Invalid filename format", http.StatusBadRequest)
				return
			}
			category := parts[0]
			baseName := parts[len(parts)-1]
			filePath = filepath.Join(cfg.DataDir, "wordlists", category, baseName)
		case "rule":
			// Extract category from filename (e.g., hashcat/file.txt -> hashcat)
			parts := strings.Split(filename, "/")
			if len(parts) < 2 {
				debug.Error("Invalid rule filename format: %s", filename)
				http.Error(w, "Invalid filename format", http.StatusBadRequest)
				return
			}
			category := parts[0]
			baseName := parts[len(parts)-1]
			filePath = filepath.Join(cfg.DataDir, "rules", category, baseName)
		case "binary":
			// For binary files without a category, query the database
			if !strings.Contains(filename, "/") {
				// Create a short timeout context for the database query
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()

				// Query database for binary info
				binaries, err := fileRepo.GetBinaries(ctx, "")
				if err != nil {
					debug.Error("Failed to query database for binaries: %v", err)
					http.Error(w, "Server error", http.StatusInternalServerError)
					return
				}

				// Look for matching filename
				var binaryID int
				found := false
				for _, binary := range binaries {
					if binary.Name == filename {
						binaryID = binary.ID
						found = true
						break
					}
				}

				if found {
					filePath = filepath.Join(cfg.DataDir, "binaries", fmt.Sprintf("%d", binaryID), filename)
					debug.Info("Found binary ID %d for file %s", binaryID, filename)
				} else {
					debug.Error("Binary file not found in database: %s", filename)
					http.Error(w, "File not found", http.StatusNotFound)
					return
				}
			} else {
				// If filename contains a path separator, extract the parts
				parts := strings.Split(filename, "/")
				category := parts[0]
				baseName := parts[len(parts)-1]
				filePath = filepath.Join(cfg.DataDir, "binaries", category, baseName)
			}
		default:
			debug.Error("Unknown file type: %s", fileType)
			http.Error(w, "Unknown file type", http.StatusBadRequest)
			return
		}

		debug.Info("Looking for file at path: %s", filePath)

		// Check if file exists
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			debug.Error("File not found: %s", filePath)
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		fileSize = fileInfo.Size()

		// Open file
		file, err := os.Open(filePath)
		if err != nil {
			debug.Error("Failed to open file: %s - %v", filePath, err)
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Set headers
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(filename)))
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))

		// Stream file to response
		if _, err := io.Copy(w, file); err != nil {
			debug.Error("Failed to stream file: %v", err)
			// Can't send error response here as headers are already sent
		}
	}).Methods(http.MethodGet)

	debug.Info("Registered file download routes for agents")
	return nil
}
