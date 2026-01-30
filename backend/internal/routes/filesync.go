package routes

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/auth/api"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// SetupFileDownloadRoutes configures routes for agent file downloads
func SetupFileDownloadRoutes(r *mux.Router, sqlDB *sql.DB, cfg *config.Config, agentService *services.AgentService) *http.ServeMux {
	debug.Info("Setting up file download routes for agents")

	// Create repositories
	dbWrapper := &db.DB{DB: sqlDB}
	fileRepo := repository.NewFileRepository(dbWrapper, cfg.DataDir)
	jobTaskRepo := repository.NewJobTaskRepository(dbWrapper)
	hashRepo := repository.NewHashRepository(dbWrapper)
	hashlistRepo := repository.NewHashListRepository(dbWrapper)

	// Create rule split manager for chunk recreation
	ruleSplitDir := filepath.Join(cfg.DataDir, "temp", "rule_chunks")
	ruleSplitManager := services.NewRuleSplitManager(ruleSplitDir, fileRepo)

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
			// Check if this is a rule chunk file
			if category == "chunks" {
				// Rule chunks are stored in temp/rule_chunks/job_<ID>/<filename>
				// Extract job ID from filename if it contains it
				if strings.Contains(filename, "/") {
					parts := strings.Split(filename, "/")
					if len(parts) == 2 {
						jobID := parts[0]
						chunkFile := parts[1]
						filePath = filepath.Join(cfg.DataDir, "temp", "rule_chunks", jobID, chunkFile)
					} else {
						// Fallback to old format
						filePath = filepath.Join(cfg.DataDir, "temp", "rule_chunks", filename)
					}
				} else {
					// Direct chunk file without job ID
					filePath = filepath.Join(cfg.DataDir, "temp", "rule_chunks", filename)
				}

				// Check if chunk file exists, if not, try to recreate it
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					debug.Info("Rule chunk not found, attempting to recreate: %s", filePath)

					ctx := r.Context()
					if recreateErr := recreateMissingChunk(ctx, filePath, jobTaskRepo, ruleSplitManager, fileRepo, sqlDB, cfg.DataDir); recreateErr != nil {
						debug.Error("Failed to recreate missing chunk %s: %v", filePath, recreateErr)
						http.Error(w, fmt.Sprintf("Chunk file not found and recreation failed: %v", recreateErr), http.StatusNotFound)
						return
					}
					debug.Info("Successfully recreated missing chunk: %s", filePath)
				}
			} else {
				filePath = filepath.Join(cfg.DataDir, "rules", category, filename)
			}
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
			if os.IsNotExist(err) {
				debug.Error("File not found: %s (requested: %s)", filePath, filename)
				http.Error(w, fmt.Sprintf("File not found: %s", filename), http.StatusNotFound)
			} else {
				debug.Error("Failed to open file: %s - %v", filePath, err)
				http.Error(w, fmt.Sprintf("Failed to open file: %s - %v", filename, err), http.StatusInternalServerError)
			}
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
			// Check if this is a rule chunk file
			if category == "chunks" {
				// Handle chunks/job_<ID>/chunk_<N>.rule format
				if len(parts) >= 3 {
					jobID := parts[1]
					chunkFile := parts[2]
					filePath = filepath.Join(cfg.DataDir, "temp", "rule_chunks", jobID, chunkFile)
				} else {
					baseName := parts[len(parts)-1]
					filePath = filepath.Join(cfg.DataDir, "temp", "rule_chunks", baseName)
				}

				// Check if chunk file exists, if not, try to recreate it
				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					debug.Info("Rule chunk not found (legacy route), attempting to recreate: %s", filePath)

					ctx := r.Context()
					if recreateErr := recreateMissingChunk(ctx, filePath, jobTaskRepo, ruleSplitManager, fileRepo, sqlDB, cfg.DataDir); recreateErr != nil {
						debug.Error("Failed to recreate missing chunk %s: %v", filePath, recreateErr)
						http.Error(w, fmt.Sprintf("Chunk file not found and recreation failed: %v", recreateErr), http.StatusNotFound)
						return
					}
					debug.Info("Successfully recreated missing chunk (legacy route): %s", filePath)
				}
			} else {
				baseName := parts[len(parts)-1]
				filePath = filepath.Join(cfg.DataDir, "rules", category, baseName)
			}
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
			if os.IsNotExist(err) {
				debug.Error("File not found: %s (requested: %s)", filePath, filename)
				http.Error(w, fmt.Sprintf("File not found: %s", filename), http.StatusNotFound)
			} else {
				debug.Error("Failed to open file: %s - %v", filePath, err)
				http.Error(w, fmt.Sprintf("Failed to open file: %s - %v", filename, err), http.StatusInternalServerError)
			}
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

	// Add hashlist download route for agents
	// Create hashlist routes with API key authentication
	hashlistRouter := r.PathPrefix("/api/agent/hashlists").Subrouter()
	hashlistRouter.Use(api.APIKeyMiddleware(agentService))

	// Handler for /api/agent/hashlists/{id}/download
	hashlistRouter.HandleFunc("/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		hashlistIDStr := vars["id"]

		debug.Info("Hashlist download request from agent: id=%s", hashlistIDStr)

		// Parse hashlist ID
		var hashlistID int64
		if _, err := fmt.Sscanf(hashlistIDStr, "%d", &hashlistID); err != nil {
			debug.Error("Invalid hashlist ID: %s", hashlistIDStr)
			http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		// Verify hashlist exists
		hashlist, err := hashlistRepo.GetByID(ctx, hashlistID)
		if err != nil {
			debug.Error("Failed to get hashlist %d: %v", hashlistID, err)
			http.Error(w, "Hashlist not found", http.StatusNotFound)
			return
		}

		debug.Info("Streaming uncracked hashes for hashlist %d to agent", hashlist.ID)

		// Set response headers for streaming
		filename := fmt.Sprintf("%d.hash", hashlist.ID)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Transfer-Encoding", "chunked")

		// Use buffered writer for much better performance
		// 256KB buffer with 32KB flush interval reduces flushes from millions to thousands
		const flushInterval = 32 * 1024 // 32KB flush interval
		var bytesWritten int

		bufWriter := bufio.NewWriterSize(w, 256*1024) // 256KB buffer
		defer bufWriter.Flush()

		// Stream uncracked hash values to agent
		hashCount := 0
		err = hashRepo.StreamUncrackedHashValuesForHashlist(ctx, hashlist.ID, func(hashValue string) error {
			hashCount++
			n, err := bufWriter.WriteString(hashValue)
			if err != nil {
				return fmt.Errorf("failed to write hash: %w", err)
			}
			bufWriter.WriteByte('\n')
			bytesWritten += n + 1

			// Flush periodically for streaming (every 32KB, not every line)
			if bytesWritten >= flushInterval {
				if err := bufWriter.Flush(); err != nil {
					return fmt.Errorf("failed to flush buffer: %w", err)
				}
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				bytesWritten = 0
			}
			return nil
		})

		// Final flush is handled by defer bufWriter.Flush()
		if err != nil {
			debug.Error("Failed to stream hashlist %d to agent: %v", hashlist.ID, err)
			// Can't send error response here as headers are already sent
		} else {
			debug.Info("Successfully streamed %d uncracked hashes from hashlist %d to agent", hashCount, hashlist.ID)
		}
	}).Methods(http.MethodGet)

	// Handler for /api/agent/hashlists/{id}/original - download original hashlist file for association attacks
	hashlistRouter.HandleFunc("/{id}/original", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		hashlistIDStr := vars["id"]

		debug.Info("Original hashlist download request from agent: id=%s", hashlistIDStr)

		// Parse hashlist ID
		var hashlistID int64
		if _, err := fmt.Sscanf(hashlistIDStr, "%d", &hashlistID); err != nil {
			debug.Error("Invalid hashlist ID: %s", hashlistIDStr)
			http.Error(w, "Invalid hashlist ID", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		// Get the hashlist to find the original file path
		hashlist, err := hashlistRepo.GetByID(ctx, hashlistID)
		if err != nil {
			debug.Error("Failed to get hashlist %d: %v", hashlistID, err)
			http.Error(w, "Hashlist not found", http.StatusNotFound)
			return
		}

		// Check if original file exists
		if hashlist.OriginalFilePath == nil || *hashlist.OriginalFilePath == "" {
			debug.Error("Original file path not available for hashlist %d", hashlistID)
			http.Error(w, "Original hashlist file not available", http.StatusNotFound)
			return
		}

		originalPath := *hashlist.OriginalFilePath
		debug.Info("Serving original hashlist file: %s", originalPath)

		// Check if file exists
		fileInfo, err := os.Stat(originalPath)
		if err != nil {
			debug.Error("Original hashlist file not found: %s", originalPath)
			http.Error(w, "Original hashlist file not found", http.StatusNotFound)
			return
		}

		// Open the file
		file, err := os.Open(originalPath)
		if err != nil {
			debug.Error("Failed to open original hashlist file: %v", err)
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Set response headers
		originalFileName := filepath.Base(originalPath)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%d_%s\"", hashlistID, originalFileName))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

		// Stream the file
		if _, err := io.Copy(w, file); err != nil {
			debug.Error("Failed to stream original hashlist file: %v", err)
		} else {
			debug.Info("Successfully streamed original hashlist %d (%s) to agent", hashlistID, originalFileName)
		}
	}).Methods(http.MethodGet)

	// Add client potfile download route for agents
	clientPotfileRepo := repository.NewClientPotfileRepository(dbWrapper)
	clientPotfileRouter := r.PathPrefix("/api/agent/client-potfiles").Subrouter()
	clientPotfileRouter.Use(api.APIKeyMiddleware(agentService))

	// Handler for /api/agent/client-potfiles/{client_id}
	clientPotfileRouter.HandleFunc("/{client_id}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		clientIDStr := vars["client_id"]

		debug.Info("Client potfile download request from agent: client_id=%s", clientIDStr)

		// Parse client ID
		clientID, err := uuid.Parse(clientIDStr)
		if err != nil {
			debug.Error("Invalid client ID: %s", clientIDStr)
			http.Error(w, "Invalid client ID", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		// Get client potfile record
		potfile, err := clientPotfileRepo.GetByClientID(ctx, clientID)
		if err != nil {
			debug.Error("Failed to get client potfile for client %s: %v", clientID, err)
			http.Error(w, "Client potfile not found", http.StatusNotFound)
			return
		}

		if potfile == nil {
			debug.Info("No potfile exists for client %s", clientID)
			http.Error(w, "Client potfile not found", http.StatusNotFound)
			return
		}

		debug.Info("Serving client potfile: %s (size=%d, lines=%d)", potfile.FilePath, potfile.FileSize, potfile.LineCount)

		// Check if file exists
		fileInfo, err := os.Stat(potfile.FilePath)
		if err != nil {
			debug.Error("Client potfile file not found: %s", potfile.FilePath)
			http.Error(w, "Client potfile file not found", http.StatusNotFound)
			return
		}

		// Open the file
		file, err := os.Open(potfile.FilePath)
		if err != nil {
			debug.Error("Failed to open client potfile file: %v", err)
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Set response headers
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s_potfile.txt\"", clientID))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

		// Stream the file
		if _, err := io.Copy(w, file); err != nil {
			debug.Error("Failed to stream client potfile file: %v", err)
		} else {
			debug.Info("Successfully streamed client potfile for client %s to agent", clientID)
		}
	}).Methods(http.MethodGet)

	// Add client wordlist download route for agents
	clientWordlistRepo := repository.NewClientWordlistRepository(dbWrapper)
	clientWordlistRouter := r.PathPrefix("/api/agent/client-wordlists").Subrouter()
	clientWordlistRouter.Use(api.APIKeyMiddleware(agentService))

	// Handler for /api/agent/client-wordlists/{wordlist_id}
	clientWordlistRouter.HandleFunc("/{wordlist_id}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		wordlistIDStr := vars["wordlist_id"]

		debug.Info("Client wordlist download request from agent: wordlist_id=%s", wordlistIDStr)

		// Parse wordlist ID
		wordlistID, err := uuid.Parse(wordlistIDStr)
		if err != nil {
			debug.Error("Invalid wordlist ID: %s", wordlistIDStr)
			http.Error(w, "Invalid wordlist ID", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		// Get wordlist file path
		filePath, err := clientWordlistRepo.GetFilePath(ctx, wordlistID)
		if err != nil {
			debug.Error("Failed to get client wordlist path for %s: %v", wordlistID, err)
			http.Error(w, "Client wordlist not found", http.StatusNotFound)
			return
		}

		debug.Info("Serving client wordlist: %s", filePath)

		// Check if file exists
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			debug.Error("Client wordlist file not found: %s", filePath)
			http.Error(w, "Client wordlist file not found", http.StatusNotFound)
			return
		}

		// Open the file
		file, err := os.Open(filePath)
		if err != nil {
			debug.Error("Failed to open client wordlist file: %v", err)
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Set response headers
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(filePath)))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

		// Stream the file
		if _, err := io.Copy(w, file); err != nil {
			debug.Error("Failed to stream client wordlist file: %v", err)
		} else {
			debug.Info("Successfully streamed client wordlist %s to agent", wordlistID)
		}
	}).Methods(http.MethodGet)

	debug.Info("Registered file download routes for agents (including hashlists, client potfiles, client wordlists)")
	return nil
}

// recreateMissingChunk attempts to recreate a missing rule chunk file
func recreateMissingChunk(ctx context.Context, chunkPath string, jobTaskRepo *repository.JobTaskRepository,
	ruleSplitManager *services.RuleSplitManager, fileRepo *repository.FileRepository, sqlDB *sql.DB, dataDir string) error {

	debug.Info("Attempting to recreate missing chunk: %s", chunkPath)

	// Extract job ID and chunk info from the path
	// Expected formats:
	// - temp/rule_chunks/job_<uuid>/chunk_<n>.rule
	// - temp/rule_chunks/job_<uuid>/chunk_<start>_<end>.rule

	re := regexp.MustCompile(`job_([a-f0-9-]+)/chunk_(\d+)(?:_(\d+))?\.rule`)
	matches := re.FindStringSubmatch(chunkPath)

	if len(matches) < 3 {
		return fmt.Errorf("cannot parse job ID and chunk info from path: %s", chunkPath)
	}

	jobIDStr := matches[1]
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		return fmt.Errorf("invalid job ID in path: %s", jobIDStr)
	}

	// Look for tasks with this chunk path to get the rule indices
	tasks, err := jobTaskRepo.GetTasksByJobExecution(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get tasks for job %s: %w", jobID, err)
	}

	// Find the task with this chunk path
	var targetTask *models.JobTask
	chunkFileName := filepath.Base(chunkPath)

	for _, task := range tasks {
		if task.RuleChunkPath != nil && strings.Contains(*task.RuleChunkPath, chunkFileName) {
			targetTask = &task
			break
		}
	}

	if targetTask == nil {
		return fmt.Errorf("no task found with chunk path containing: %s", chunkFileName)
	}

	// Check if this task has rule indices for recreation
	if targetTask.RuleStartIndex == nil || targetTask.RuleEndIndex == nil {
		return fmt.Errorf("task %s has no rule indices for chunk recreation", targetTask.ID)
	}

	// Get the preset job to find the original rule file
	dbWrapper := &db.DB{DB: sqlDB}
	jobExecRepo := repository.NewJobExecutionRepository(dbWrapper)
	jobExec, err := jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job execution: %w", err)
	}

	if jobExec.PresetJobID == nil {
		return fmt.Errorf("job %s has no preset job ID for rule file lookup", jobID)
	}

	// Get preset job
	presetJobRepo := repository.NewPresetJobRepository(sqlDB)
	presetJob, err := presetJobRepo.GetByID(ctx, *jobExec.PresetJobID)
	if err != nil {
		return fmt.Errorf("failed to get preset job: %w", err)
	}

	// Get rule files from preset job
	if len(presetJob.RuleIDs) == 0 {
		return fmt.Errorf("preset job has no rule files")
	}

	// Get the rule file path (assuming first rule for now)
	rules, err := fileRepo.GetRules(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to get rules: %w", err)
	}

	var ruleFilePath string
	// Find the rule file path based on the rule IDs from the preset job
	// presetJob.RuleIDs contains string IDs, rules have int IDs
	for _, rule := range rules {
		for _, ruleIDStr := range presetJob.RuleIDs {
			// Convert string ID to int for comparison
			if fmt.Sprintf("%d", rule.ID) == ruleIDStr {
				// Use the same path construction as resolveRulePath in job_execution_service.go
				// rule.Name already contains the relative path from rules directory
				// e.g., "hashcat/00-primary-merged.rule"
				ruleFilePath = filepath.Join(dataDir, "rules", rule.Name)

				debug.Log("Resolved rule file path for chunk recreation", map[string]interface{}{
					"rule_id":     rule.ID,
					"rule_name":   rule.Name,
					"constructed_path": ruleFilePath,
				})
				break
			}
		}
		if ruleFilePath != "" {
			break
		}
	}

	if ruleFilePath == "" {
		debug.Log("Could not find rule file path for preset job rules", map[string]interface{}{
			"preset_job_id": *jobExec.PresetJobID,
			"rule_ids":      presetJob.RuleIDs,
			"available_rules": len(rules),
			"error": "rule not found in database",
		})
		return fmt.Errorf("could not find rule file path for preset job rules")
	}

	// Defensive check: verify the rule file exists before attempting to recreate
	if _, err := os.Stat(ruleFilePath); err != nil {
		if os.IsNotExist(err) {
			debug.Log("Rule file does not exist at resolved path", map[string]interface{}{
				"rule_path": ruleFilePath,
				"error":     err.Error(),
			})
			return fmt.Errorf("rule file not found at path %s: %w", ruleFilePath, err)
		}
		return fmt.Errorf("failed to check rule file existence: %w", err)
	}

	debug.Info("Recreating chunk from rule file %s (indices %d-%d)",
		ruleFilePath, *targetTask.RuleStartIndex, *targetTask.RuleEndIndex)

	// Recreate the chunk using the rule split manager
	numRules := *targetTask.RuleEndIndex - *targetTask.RuleStartIndex
	chunk, err := ruleSplitManager.CreateSingleRuleChunk(ctx, jobID, ruleFilePath,
		*targetTask.RuleStartIndex, numRules)
	if err != nil {
		return fmt.Errorf("failed to recreate chunk: %w", err)
	}

	debug.Info("Successfully recreated chunk at: %s", chunk.Path)

	// Verify the recreated chunk path matches what we expected
	if !strings.Contains(chunk.Path, chunkFileName) {
		// The chunk was created but with a different name, try to rename it
		expectedPath := filepath.Join(filepath.Dir(chunk.Path), chunkFileName)
		if err := os.Rename(chunk.Path, expectedPath); err != nil {
			debug.Error("Created chunk at %s but could not rename to expected %s: %v",
				chunk.Path, expectedPath, err)
			// Still return the created chunk path
		}
	}

	return nil
}
