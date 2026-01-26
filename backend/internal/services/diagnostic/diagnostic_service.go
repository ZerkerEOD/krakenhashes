package diagnostic

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wshandler "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/websocket"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// sanitizeLogFilePath extracts only the last 2 path components to avoid exposing directory structure
// e.g., "/home/user/Programming/passwordCracking/agent/logs/agent.log" -> "logs/agent.log"
func sanitizeLogFilePath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, string(os.PathSeparator))
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], string(os.PathSeparator))
	}
	return filepath.Base(path)
}

// DiagnosticService handles collection and packaging of diagnostic data
type DiagnosticService struct {
	dbExport  *DBExportService
	wsHandler *wshandler.Handler
	logsDir   string
}

// NewDiagnosticService creates a new diagnostic service
func NewDiagnosticService(db *sql.DB, wsHandler *wshandler.Handler, logsDir string) *DiagnosticService {
	return &DiagnosticService{
		dbExport:  NewDBExportService(db),
		wsHandler: wsHandler,
		logsDir:   logsDir,
	}
}

// DiagnosticReport contains all diagnostic data
type DiagnosticReport struct {
	GeneratedAt      time.Time                           `json:"generated_at"`
	Version          string                              `json:"version"`
	SystemInfo       map[string]interface{}              `json:"system_info"`
	DatabaseExport   map[string]*TableExport             `json:"database_export,omitempty"`
	BackendLogs      string                              `json:"backend_logs,omitempty"`
	NginxLogs        string                              `json:"nginx_logs,omitempty"`
	PostgresLogs     string                              `json:"postgres_logs,omitempty"`
	AgentDebugStatus map[int]*wshandler.AgentDebugStatus `json:"agent_debug_status,omitempty"`
	AgentLogs        map[int]*AgentLogData               `json:"agent_logs,omitempty"`
	Errors           []string                            `json:"errors,omitempty"`
}

// AgentLogData contains log data from an agent
type AgentLogData struct {
	AgentID     int         `json:"agent_id"`
	Entries     interface{} `json:"entries,omitempty"`
	FileContent string      `json:"file_content,omitempty"`
	TotalCount  int         `json:"total_count"`
	Truncated   bool        `json:"truncated"`
	Error       string      `json:"error,omitempty"`
	CollectedAt time.Time   `json:"collected_at"`
}

// CollectDiagnostics collects all diagnostic data
func (s *DiagnosticService) CollectDiagnostics(ctx context.Context, includeAgentLogs, includeNginxLogs, includePostgresLogs bool, hoursBack int) (*DiagnosticReport, error) {
	debug.Info("Starting diagnostic data collection (hoursBack=%d, nginx=%v, postgres=%v)", hoursBack, includeNginxLogs, includePostgresLogs)
	startTime := time.Now()

	report := &DiagnosticReport{
		GeneratedAt: startTime,
		Version:     "1.0.0",
		Errors:      []string{},
	}

	// Collect system info
	sysInfo, err := s.collectSystemInfo(ctx)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("System info error: %v", err))
	} else {
		report.SystemInfo = sysInfo
	}

	// Collect database export
	dbExport, err := s.dbExport.ExportAllTables(ctx)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("Database export error: %v", err))
	} else {
		report.DatabaseExport = dbExport
	}

	// Collect backend logs (filtered by modification time)
	backendLogs, err := s.collectBackendLogs(hoursBack)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("Backend logs error: %v", err))
	} else {
		report.BackendLogs = backendLogs
	}

	// Collect nginx logs if requested (contains sensitive data)
	if includeNginxLogs {
		nginxLogs, err := s.collectNginxLogs(hoursBack)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("Nginx logs error: %v", err))
		} else {
			report.NginxLogs = nginxLogs
		}
	}

	// Collect postgres logs if requested (contains sensitive data)
	if includePostgresLogs {
		postgresLogs, err := s.collectPostgresLogs(hoursBack)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("PostgreSQL logs error: %v", err))
		} else {
			report.PostgresLogs = postgresLogs
		}
	}

	// Collect agent debug statuses
	report.AgentDebugStatus = wshandler.GetAllAgentDebugStatuses()

	// Collect agent logs if requested
	if includeAgentLogs && s.wsHandler != nil {
		agentLogs, errs := s.collectAgentLogs(ctx, hoursBack)
		report.AgentLogs = agentLogs
		for _, e := range errs {
			report.Errors = append(report.Errors, e)
		}
	}

	debug.Info("Diagnostic data collection complete in %v", time.Since(startTime))
	return report, nil
}

// collectSystemInfo gathers system-level diagnostic information
func (s *DiagnosticService) collectSystemInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})

	// Runtime info
	info["go_version"] = runtime.Version()
	info["go_os"] = runtime.GOOS
	info["go_arch"] = runtime.GOARCH
	info["num_cpu"] = runtime.NumCPU()
	info["num_goroutine"] = runtime.NumGoroutine()

	// Memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	info["memory"] = map[string]interface{}{
		"alloc_mb":        memStats.Alloc / 1024 / 1024,
		"total_alloc_mb":  memStats.TotalAlloc / 1024 / 1024,
		"sys_mb":          memStats.Sys / 1024 / 1024,
		"num_gc":          memStats.NumGC,
		"heap_objects":    memStats.HeapObjects,
		"heap_alloc_mb":   memStats.HeapAlloc / 1024 / 1024,
	}

	// Hostname
	if hostname, err := os.Hostname(); err == nil {
		info["hostname"] = hostname
	}

	// Working directory
	if wd, err := os.Getwd(); err == nil {
		info["working_directory"] = wd
	}

	// Environment (filtered for security)
	safeEnvVars := []string{
		"KH_TLS_MODE",
		"LOG_LEVEL",
		"DEBUG",
		"SERVER_PORT",
		"KH_PONG_WAIT",
		"KH_PING_PERIOD",
	}
	envInfo := make(map[string]string)
	for _, key := range safeEnvVars {
		if val := os.Getenv(key); val != "" {
			envInfo[key] = val
		}
	}
	info["environment"] = envInfo

	// Database info
	if s.dbExport != nil {
		dbInfo, err := s.dbExport.GetSystemInfo(ctx)
		if err == nil {
			info["database"] = dbInfo
		}
	}

	// Connected agents count
	if s.wsHandler != nil {
		info["connected_agents"] = len(s.wsHandler.GetConnectedAgents())
	}

	info["collected_at"] = time.Now()

	return info, nil
}

// GetSystemInfoOnly returns only lightweight system information (for page display).
// Use this instead of CollectDiagnostics for the /system-info endpoint.
func (s *DiagnosticService) GetSystemInfoOnly(ctx context.Context) (map[string]interface{}, error) {
	return s.collectSystemInfo(ctx)
}

// collectBackendLogs reads backend log files
func (s *DiagnosticService) collectBackendLogs(hoursBack int) (string, error) {
	backendDir := filepath.Join(s.logsDir, "backend")
	return s.collectLogsFromDir(backendDir, "Backend", hoursBack)
}

// collectNginxLogs reads nginx log files
func (s *DiagnosticService) collectNginxLogs(hoursBack int) (string, error) {
	nginxDir := filepath.Join(s.logsDir, "nginx")
	return s.collectLogsFromDir(nginxDir, "Nginx", hoursBack)
}

// collectPostgresLogs reads PostgreSQL log files
func (s *DiagnosticService) collectPostgresLogs(hoursBack int) (string, error) {
	postgresDir := filepath.Join(s.logsDir, "postgres")
	return s.collectLogsFromDir(postgresDir, "PostgreSQL", hoursBack)
}

// collectLogsFromDir reads log files from a specific directory
func (s *DiagnosticService) collectLogsFromDir(logDir, dirName string, hoursBack int) (string, error) {
	if s.logsDir == "" {
		debug.Warning("collectLogsFromDir: logsDir is empty")
		return "", nil
	}

	// Calculate cutoff time for filtering logs by modification time
	cutoffTime := time.Now().Add(-time.Duration(hoursBack) * time.Hour)

	// Find log files
	var logContent strings.Builder
	logContent.WriteString(fmt.Sprintf("=== %s Logs ===\n", dirName))
	logContent.WriteString(fmt.Sprintf("Logs directory: %s\n", logDir))
	logContent.WriteString(fmt.Sprintf("Time range: files modified in last %d hour(s)\n\n", hoursBack))

	// Track which files we've processed
	processedFiles := make(map[string]bool)

	// Use filepath.WalkDir for proper recursive file search (Go's Glob doesn't support **)
	err := filepath.WalkDir(logDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			debug.Warning("collectLogsFromDir: error walking path %s: %v", path, err)
			return nil // Continue walking
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Skip compressed archives (.gz) - they're already archived, older data
		if strings.HasSuffix(path, ".gz") {
			return nil
		}

		// Include .log files and rotated .log.N files (e.g., backend.log, backend.log.1, backend.log.2)
		filename := filepath.Base(path)
		if !strings.Contains(filename, ".log") {
			return nil
		}

		// Get file info for modification time check
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Skip files not modified within the time range
		if info.ModTime().Before(cutoffTime) {
			return nil
		}

		processedFiles[path] = true

		// Read entire file (no truncation for downloads)
		content, err := readLogFileFullContent(path)
		if err != nil {
			debug.Warning("collectLogsFromDir: failed to read %s: %v", path, err)
			return nil
		}

		logContent.WriteString(fmt.Sprintf("=== %s (modified: %s, size: %d bytes) ===\n", path, info.ModTime().Format(time.RFC3339), info.Size()))
		logContent.WriteString(content)
		logContent.WriteString("\n")

		return nil
	})

	if err != nil {
		debug.Error("collectLogsFromDir: walk error: %v", err)
	}

	if len(processedFiles) == 0 {
		logContent.WriteString("No log files found modified within the specified time range.\n")
		debug.Info("collectLogsFromDir: no files found in %s modified after %s", logDir, cutoffTime.Format(time.RFC3339))
	} else {
		debug.Info("collectLogsFromDir: collected %d log files from %s", len(processedFiles), logDir)
	}

	return logContent.String(), nil
}

// readLogFile reads a log file with size limit (used for frontend display)
func readLogFile(path string, maxSize int64) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var content []byte
	if info.Size() > maxSize {
		// Read only the last maxSize bytes
		if _, err := file.Seek(-maxSize, io.SeekEnd); err != nil {
			return "", err
		}
		content, err = io.ReadAll(file)
		if err != nil {
			return "", err
		}
		return "... [truncated] ...\n" + string(content), nil
	}

	content, err = io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// readLogFileFullContent reads an entire log file without truncation (used for download packages)
func readLogFileFullContent(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// collectAgentLogs collects logs from all connected agents
func (s *DiagnosticService) collectAgentLogs(ctx context.Context, hoursBack int) (map[int]*AgentLogData, []string) {
	if s.wsHandler == nil {
		return nil, nil
	}

	connectedAgents := s.wsHandler.GetConnectedAgents()
	if len(connectedAgents) == 0 {
		return nil, nil
	}

	results := make(map[int]*AgentLogData)
	var errors []string

	// Request logs from each agent (with timeout)
	for _, agentID := range connectedAgents {
		requestID := uuid.New().String()

		// Register callback for response
		responseCh := wshandler.RegisterLogDataCallback(requestID)
		defer wshandler.UnregisterLogDataCallback(requestID)

		// Send log request with the specified time range
		if err := s.wsHandler.SendLogRequest(agentID, requestID, hoursBack, false); err != nil {
			errors = append(errors, fmt.Sprintf("Agent %d: failed to send log request: %v", agentID, err))
			continue
		}

		// Wait for response with timeout
		select {
		case response := <-responseCh:
			if response != nil {
				results[agentID] = &AgentLogData{
					AgentID:     agentID,
					Entries:     response.Entries,
					FileContent: response.FileContent,
					TotalCount:  response.TotalCount,
					Truncated:   response.Truncated,
					Error:       response.Error,
					CollectedAt: time.Now(),
				}
			}
		case <-time.After(10 * time.Second):
			errors = append(errors, fmt.Sprintf("Agent %d: log request timed out", agentID))
		case <-ctx.Done():
			errors = append(errors, fmt.Sprintf("Agent %d: context cancelled", agentID))
			return results, errors
		}
	}

	return results, errors
}

// PackageDiagnostics creates a ZIP archive containing all diagnostic data
func (s *DiagnosticService) PackageDiagnostics(ctx context.Context, includeAgentLogs, includeNginxLogs, includePostgresLogs bool, hoursBack int) ([]byte, error) {
	debug.Info("Creating diagnostic package (hoursBack=%d, nginx=%v, postgres=%v)", hoursBack, includeNginxLogs, includePostgresLogs)

	// Collect all data
	report, err := s.CollectDiagnostics(ctx, includeAgentLogs, includeNginxLogs, includePostgresLogs, hoursBack)
	if err != nil {
		return nil, fmt.Errorf("failed to collect diagnostics: %w", err)
	}

	// Create ZIP archive in memory
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add summary JSON
	summaryJSON, _ := json.MarshalIndent(map[string]interface{}{
		"generated_at":     report.GeneratedAt,
		"version":          report.Version,
		"connected_agents": len(report.AgentDebugStatus),
		"tables_exported":  len(report.DatabaseExport),
		"errors":           report.Errors,
	}, "", "  ")
	if err := addFileToZip(zipWriter, "summary.json", summaryJSON); err != nil {
		return nil, err
	}

	// Add system info
	if report.SystemInfo != nil {
		sysInfoJSON, _ := json.MarshalIndent(report.SystemInfo, "", "  ")
		if err := addFileToZip(zipWriter, "system_info.json", sysInfoJSON); err != nil {
			return nil, err
		}
	}

	// Add database exports
	if report.DatabaseExport != nil {
		// Full JSON export
		dbJSON, _ := json.MarshalIndent(report.DatabaseExport, "", "  ")
		if err := addFileToZip(zipWriter, "database/full_export.json", dbJSON); err != nil {
			return nil, err
		}

		// Text export
		dbText, _ := s.dbExport.ExportToText(ctx)
		if err := addFileToZip(zipWriter, "database/export.txt", []byte(dbText)); err != nil {
			return nil, err
		}

		// Individual table files
		for tableName, export := range report.DatabaseExport {
			tableJSON, _ := json.MarshalIndent(export, "", "  ")
			fileName := fmt.Sprintf("database/tables/%s.json", tableName)
			if err := addFileToZip(zipWriter, fileName, tableJSON); err != nil {
				debug.Warning("Failed to add %s to zip: %v", fileName, err)
			}
		}
	}

	// Add backend logs (sanitize user home paths)
	if report.BackendLogs != "" {
		sanitizedLogs := debug.SanitizeLogContent(report.BackendLogs)
		if err := addFileToZip(zipWriter, "logs/backend.log", []byte(sanitizedLogs)); err != nil {
			return nil, err
		}
	}

	// Add nginx logs (if included - contains sensitive data)
	if report.NginxLogs != "" {
		if err := addFileToZip(zipWriter, "logs/nginx.log", []byte(report.NginxLogs)); err != nil {
			return nil, err
		}
	}

	// Add postgres logs (if included - contains sensitive data)
	if report.PostgresLogs != "" {
		if err := addFileToZip(zipWriter, "logs/postgres.log", []byte(report.PostgresLogs)); err != nil {
			return nil, err
		}
	}

	// Add agent debug status (with sanitized paths)
	if len(report.AgentDebugStatus) > 0 {
		// Create sanitized copy to avoid exposing user home paths
		sanitizedStatus := make(map[int]map[string]interface{})
		for id, status := range report.AgentDebugStatus {
			if status == nil {
				continue
			}
			sanitizedStatus[id] = map[string]interface{}{
				"agent_id":             status.AgentID,
				"enabled":              status.Enabled,
				"level":                status.Level,
				"file_logging_enabled": status.FileLoggingEnabled,
				"log_file_path":        sanitizeLogFilePath(status.LogFilePath),
				"log_file_exists":      status.LogFileExists,
				"log_file_size":        status.LogFileSize,
				"log_file_modified":    status.LogFileModified,
				"buffer_count":         status.BufferCount,
				"buffer_capacity":      status.BufferCapacity,
				"last_updated":         status.LastUpdated,
			}
		}
		statusJSON, _ := json.MarshalIndent(sanitizedStatus, "", "  ")
		if err := addFileToZip(zipWriter, "agents/debug_status.json", statusJSON); err != nil {
			return nil, err
		}
	}

	// Add agent logs
	if report.AgentLogs != nil {
		for agentID, logData := range report.AgentLogs {
			// JSON format
			logJSON, _ := json.MarshalIndent(logData, "", "  ")
			fileName := fmt.Sprintf("agents/agent_%d_logs.json", agentID)
			if err := addFileToZip(zipWriter, fileName, logJSON); err != nil {
				debug.Warning("Failed to add %s to zip: %v", fileName, err)
			}

			// Text format for file content (sanitize user home paths)
			if logData.FileContent != "" {
				sanitizedContent := debug.SanitizeLogContent(logData.FileContent)
				textFileName := fmt.Sprintf("agents/agent_%d.log", agentID)
				if err := addFileToZip(zipWriter, textFileName, []byte(sanitizedContent)); err != nil {
					debug.Warning("Failed to add %s to zip: %v", textFileName, err)
				}
			}
		}
	}

	// Add errors summary
	if len(report.Errors) > 0 {
		errorsText := strings.Join(report.Errors, "\n")
		if err := addFileToZip(zipWriter, "errors.txt", []byte(errorsText)); err != nil {
			return nil, err
		}
	}

	// Add README
	readme := generateREADME(report)
	if err := addFileToZip(zipWriter, "README.txt", []byte(readme)); err != nil {
		return nil, err
	}

	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip: %w", err)
	}

	debug.Info("Diagnostic package created: %d bytes", buf.Len())
	return buf.Bytes(), nil
}

// addFileToZip adds a file to a ZIP archive
func addFileToZip(zw *zip.Writer, filename string, content []byte) error {
	w, err := zw.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create %s in zip: %w", filename, err)
	}
	if _, err := w.Write(content); err != nil {
		return fmt.Errorf("failed to write %s to zip: %w", filename, err)
	}
	return nil
}

// generateREADME creates a README file for the diagnostic package
func generateREADME(report *DiagnosticReport) string {
	var sb strings.Builder
	sb.WriteString("KrakenHashes Diagnostic Report\n")
	sb.WriteString("==============================\n\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n", report.GeneratedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Version: %s\n\n", report.Version))

	sb.WriteString("Contents:\n")
	sb.WriteString("---------\n")
	sb.WriteString("- summary.json: Overview of the diagnostic report\n")
	sb.WriteString("- system_info.json: System and runtime information\n")
	sb.WriteString("- database/: Database table exports (sanitized)\n")
	sb.WriteString("  - full_export.json: Complete database export in JSON\n")
	sb.WriteString("  - export.txt: Human-readable database export\n")
	sb.WriteString("  - tables/: Individual table exports\n")
	sb.WriteString("- logs/: Backend log files\n")
	sb.WriteString("- agents/: Agent debug status and logs\n")
	sb.WriteString("  - debug_status.json: Debug status for all agents\n")
	sb.WriteString("  - agent_N_logs.json: Log data from agent N\n")
	sb.WriteString("- errors.txt: Any errors during collection (if any)\n\n")

	sb.WriteString("Privacy Notice:\n")
	sb.WriteString("---------------\n")
	sb.WriteString("Sensitive information has been redacted from this export.\n")
	sb.WriteString("Names, hostnames, file paths, and other identifying information\n")
	sb.WriteString("have been replaced with [REDACTED] placeholders.\n")
	sb.WriteString("IDs are preserved for correlation purposes.\n\n")

	if len(report.Errors) > 0 {
		sb.WriteString("Errors During Collection:\n")
		sb.WriteString("-------------------------\n")
		for _, err := range report.Errors {
			sb.WriteString(fmt.Sprintf("- %s\n", err))
		}
	}

	return sb.String()
}

// LogDirStats contains statistics for a log directory
type LogDirStats struct {
	Files int   `json:"files"`
	Size  int64 `json:"size"`
}

// AllLogStats contains stats for all log directories
type AllLogStats struct {
	Backend  LogDirStats `json:"backend"`
	Nginx    LogDirStats `json:"nginx"`
	Postgres LogDirStats `json:"postgres"`
}

// GetLogStats returns statistics for all log directories
func (s *DiagnosticService) GetLogStats() (*AllLogStats, error) {
	stats := &AllLogStats{}

	// Calculate stats for each directory
	stats.Backend = s.getLogDirStats(filepath.Join(s.logsDir, "backend"))
	stats.Nginx = s.getLogDirStats(filepath.Join(s.logsDir, "nginx"))
	stats.Postgres = s.getLogDirStats(filepath.Join(s.logsDir, "postgres"))

	return stats, nil
}

// getLogDirStats calculates log file statistics for a directory
func (s *DiagnosticService) getLogDirStats(dir string) LogDirStats {
	stats := LogDirStats{}

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		filename := filepath.Base(path)
		// Count all log files including rotated ones (*.log, *.log.1, etc.) and compressed (*.gz)
		if strings.Contains(filename, ".log") {
			stats.Files++
			if info, err := d.Info(); err == nil {
				stats.Size += info.Size()
			}
		}
		return nil
	})

	return stats
}

// PurgeLogs deletes all log files in the specified directory
func (s *DiagnosticService) PurgeLogs(directory string) error {
	var targetDir string
	switch directory {
	case "backend":
		targetDir = filepath.Join(s.logsDir, "backend")
	case "nginx":
		targetDir = filepath.Join(s.logsDir, "nginx")
	case "postgres":
		targetDir = filepath.Join(s.logsDir, "postgres")
	case "all":
		// Purge all directories
		if err := s.PurgeLogs("backend"); err != nil {
			debug.Warning("PurgeLogs: backend purge error: %v", err)
		}
		if err := s.PurgeLogs("nginx"); err != nil {
			debug.Warning("PurgeLogs: nginx purge error: %v", err)
		}
		if err := s.PurgeLogs("postgres"); err != nil {
			debug.Warning("PurgeLogs: postgres purge error: %v", err)
		}
		return nil
	default:
		return fmt.Errorf("invalid directory: %s", directory)
	}

	debug.Info("PurgeLogs: purging logs in %s", targetDir)

	// Truncate active log files (.log) and delete backup files (.log.N, .log.N.gz)
	// Truncating active logs preserves file handles so supervisord continues writing
	return filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		filename := filepath.Base(path)

		// Check if this is an active log file (ends with .log exactly)
		isActiveLog := strings.HasSuffix(filename, ".log")

		if isActiveLog {
			// TRUNCATE active logs - preserves file handle, process continues writing from byte 0
			if err := os.Truncate(path, 0); err != nil {
				debug.Warning("PurgeLogs: failed to truncate %s: %v", path, err)
			} else {
				debug.Info("PurgeLogs: truncated %s", path)
			}
		} else if strings.Contains(filename, ".log") {
			// DELETE backup files (.log.1, .log.2.gz, etc.) - no process has these open
			if err := os.Remove(path); err != nil {
				debug.Warning("PurgeLogs: failed to delete %s: %v", path, err)
			} else {
				debug.Info("PurgeLogs: deleted %s", path)
			}
		}
		return nil
	})
}

// CheckPostgresLogsExist checks if there are any postgres log files
func (s *DiagnosticService) CheckPostgresLogsExist() bool {
	postgresDir := filepath.Join(s.logsDir, "postgres")
	stats := s.getLogDirStats(postgresDir)
	return stats.Files > 0
}

// CheckNginxLogsExist checks if there are any nginx log files
func (s *DiagnosticService) CheckNginxLogsExist() bool {
	nginxDir := filepath.Join(s.logsDir, "nginx")
	stats := s.getLogDirStats(nginxDir)
	return stats.Files > 0
}

// GetDiskUsage returns disk usage information for the data directory
func (s *DiagnosticService) GetDiskUsage(dataDir string) (map[string]interface{}, error) {
	info := make(map[string]interface{})

	// Calculate directory sizes
	dirs := map[string]string{
		"wordlists": filepath.Join(dataDir, "wordlists"),
		"rules":     filepath.Join(dataDir, "rules"),
		"hashlists": filepath.Join(dataDir, "hashlists"),
		"binaries":  filepath.Join(dataDir, "binaries"),
		"logs":      filepath.Join(dataDir, "logs"),
	}

	for name, dir := range dirs {
		size, count, err := getDirStats(dir)
		if err == nil {
			info[name] = map[string]interface{}{
				"size_bytes": size,
				"size_mb":    size / 1024 / 1024,
				"file_count": count,
			}
		}
	}

	return info, nil
}

// getDirStats calculates directory size and file count
func getDirStats(path string) (int64, int64, error) {
	var size int64
	var count int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !info.IsDir() {
			size += info.Size()
			count++
		}
		return nil
	})

	return size, count, err
}

// ReloadNginx sends a HUP signal to nginx via supervisorctl to trigger a hot-reload.
// This causes nginx to gracefully reload its configuration without dropping connections.
func (s *DiagnosticService) ReloadNginx() error {
	debug.Info("Reloading nginx configuration via supervisorctl")

	cmd := exec.Command("supervisorctl", "signal", "HUP", "nginx")
	output, err := cmd.CombinedOutput()
	if err != nil {
		debug.Error("Failed to reload nginx: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to reload nginx: %w (output: %s)", err, string(output))
	}

	debug.Info("Nginx reload successful: %s", strings.TrimSpace(string(output)))
	return nil
}

