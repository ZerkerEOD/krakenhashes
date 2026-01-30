package diagnostic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// DBExportService handles exporting sanitized database tables for diagnostics
type DBExportService struct {
	db *sql.DB
}

// NewDBExportService creates a new database export service
func NewDBExportService(db *sql.DB) *DBExportService {
	return &DBExportService{db: db}
}

// TableExport represents an exported table
type TableExport struct {
	TableName  string                   `json:"table_name"`
	RowCount   int                      `json:"row_count"`
	ExportedAt time.Time                `json:"exported_at"`
	Columns    []string                 `json:"columns"`
	Rows       []map[string]interface{} `json:"rows"`
}

// DiagnosticTables lists tables to export for diagnostics (non-sensitive)
var DiagnosticTables = []string{
	"agents",
	"agent_devices",
	"agent_physical_devices",
	"agent_schedules",
	"agent_settings",
	"binary_versions",
	"hashlists",
	"job_executions",
	"job_tasks",
	"job_workflows",
	"preset_jobs",
	"rules",
	"wordlists",
	"system_settings",
}

// SensitiveColumns maps table names to columns that should be censored
var SensitiveColumns = map[string][]string{
	"agents":          {"name", "hostname", "api_key"},
	"hashlists":       {"name", "file_path", "original_filename"},
	"job_workflows":   {"name", "description"},
	"job_executions":  {"error_message"},
	"preset_jobs":     {"name", "description"},
	"wordlists":       {"name", "file_path", "description"},
	"rules":           {"name", "file_path", "description"},
	"binary_versions": {"file_path"},
	"users":           {"username", "email", "first_name", "last_name"},
	"clients":         {"name", "description"},
	"teams":           {"name", "description"},
}

// ExportAllTables exports all diagnostic tables with sanitization
func (s *DBExportService) ExportAllTables(ctx context.Context) (map[string]*TableExport, error) {
	debug.Info("Starting database export for diagnostics")

	// Start a read-only transaction for consistent snapshot
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	results := make(map[string]*TableExport)

	for _, tableName := range DiagnosticTables {
		export, err := s.exportTable(ctx, tx, tableName)
		if err != nil {
			debug.Warning("Failed to export table %s: %v", tableName, err)
			// Continue with other tables
			continue
		}
		results[tableName] = export
		debug.Debug("Exported table %s: %d rows", tableName, export.RowCount)
	}

	debug.Info("Database export complete: %d tables exported", len(results))
	return results, nil
}

// ExportTable exports a single table with sanitization
func (s *DBExportService) ExportTable(ctx context.Context, tableName string) (*TableExport, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	return s.exportTable(ctx, tx, tableName)
}

// exportTable exports a table within a transaction
func (s *DBExportService) exportTable(ctx context.Context, tx *sql.Tx, tableName string) (*TableExport, error) {
	// Validate table name (prevent SQL injection)
	if !isValidTableName(tableName) {
		return nil, fmt.Errorf("invalid table name: %s", tableName)
	}

	// Get column names
	columns, err := s.getTableColumns(ctx, tx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get columns for %s: %w", tableName, err)
	}

	// Query all rows (limit to 10000 for safety)
	query := fmt.Sprintf("SELECT * FROM %s LIMIT 10000", tableName)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query %s: %w", tableName, err)
	}
	defer rows.Close()

	// Get sensitive columns for this table
	sensitiveColsMap := make(map[string]bool)
	if cols, ok := SensitiveColumns[tableName]; ok {
		for _, col := range cols {
			sensitiveColsMap[col] = true
		}
	}

	// Scan rows
	var exportedRows []map[string]interface{}
	for rows.Next() {
		// Create a slice of interface{} to scan into
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Convert to map with sanitization
		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]

			// Sanitize sensitive columns
			if sensitiveColsMap[col] {
				val = sanitizeValue(val, col)
			} else if col == "os_info" {
				// Special handling for os_info JSON field - redact hostname only
				val = sanitizeOsInfo(val)
			} else {
				// Convert byte arrays to strings for readability
				if b, ok := val.([]byte); ok {
					val = string(b)
				}
			}

			row[col] = val
		}

		exportedRows = append(exportedRows, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return &TableExport{
		TableName:  tableName,
		RowCount:   len(exportedRows),
		ExportedAt: time.Now(),
		Columns:    columns,
		Rows:       exportedRows,
	}, nil
}

// getTableColumns returns the column names for a table
func (s *DBExportService) getTableColumns(ctx context.Context, tx *sql.Tx, tableName string) ([]string, error) {
	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_name = $1 AND table_schema = 'public'
		ORDER BY ordinal_position
	`
	rows, err := tx.QueryContext(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		columns = append(columns, col)
	}

	return columns, rows.Err()
}

// sanitizeValue sanitizes a sensitive value
func sanitizeValue(val interface{}, colName string) interface{} {
	if val == nil {
		return nil
	}

	// Convert to string for processing
	var strVal string
	switch v := val.(type) {
	case string:
		strVal = v
	case []byte:
		strVal = string(v)
	default:
		// For non-string types, return a placeholder
		return fmt.Sprintf("[REDACTED:%s]", colName)
	}

	if strVal == "" {
		return ""
	}

	// Return hash of the value for correlation without exposing actual data
	// Use first 8 chars of value + length for basic anonymization
	if len(strVal) > 8 {
		return fmt.Sprintf("[REDACTED:%s:len=%d]", colName, len(strVal))
	}
	return fmt.Sprintf("[REDACTED:%s]", colName)
}

// sanitizeOsInfo sanitizes the os_info JSON field to redact hostname
// Returns the sanitized JSON string or the original if parsing fails
func sanitizeOsInfo(val interface{}) interface{} {
	if val == nil {
		return nil
	}

	// Convert to string
	var jsonStr string
	switch v := val.(type) {
	case string:
		jsonStr = v
	case []byte:
		jsonStr = string(v)
	default:
		return val
	}

	if jsonStr == "" {
		return ""
	}

	// Parse JSON
	var osInfo map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &osInfo); err != nil {
		// Return original if not valid JSON
		return jsonStr
	}

	// Redact hostname if present
	if _, ok := osInfo["hostname"]; ok {
		osInfo["hostname"] = "[REDACTED:hostname]"
	}

	// Re-serialize
	sanitized, err := json.Marshal(osInfo)
	if err != nil {
		return jsonStr
	}
	return string(sanitized)
}

// isValidTableName validates table name to prevent SQL injection
func isValidTableName(name string) bool {
	// Only allow alphanumeric and underscore
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return len(name) > 0 && len(name) <= 64
}

// ExportToJSON exports tables to JSON format
func (s *DBExportService) ExportToJSON(ctx context.Context) ([]byte, error) {
	exports, err := s.ExportAllTables(ctx)
	if err != nil {
		return nil, err
	}

	return json.MarshalIndent(exports, "", "  ")
}

// ExportToText exports tables to a readable text format
func (s *DBExportService) ExportToText(ctx context.Context) (string, error) {
	exports, err := s.ExportAllTables(ctx)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("=== KrakenHashes Database Export ===\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	for _, tableName := range DiagnosticTables {
		export, ok := exports[tableName]
		if !ok {
			continue
		}

		sb.WriteString(fmt.Sprintf("=== Table: %s (%d rows) ===\n", export.TableName, export.RowCount))

		if len(export.Rows) == 0 {
			sb.WriteString("(empty)\n\n")
			continue
		}

		// Write column headers
		sb.WriteString(strings.Join(export.Columns, " | "))
		sb.WriteString("\n")
		sb.WriteString(strings.Repeat("-", 80))
		sb.WriteString("\n")

		// Write rows (limit to first 100 for text format)
		maxRows := 100
		if len(export.Rows) < maxRows {
			maxRows = len(export.Rows)
		}

		for i := 0; i < maxRows; i++ {
			row := export.Rows[i]
			var values []string
			for _, col := range export.Columns {
				val := row[col]
				if val == nil {
					values = append(values, "NULL")
				} else {
					values = append(values, fmt.Sprintf("%v", val))
				}
			}
			sb.WriteString(strings.Join(values, " | "))
			sb.WriteString("\n")
		}

		if len(export.Rows) > maxRows {
			sb.WriteString(fmt.Sprintf("... and %d more rows\n", len(export.Rows)-maxRows))
		}

		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// GetSystemInfo returns system-level diagnostic information
func (s *DBExportService) GetSystemInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})

	// Get database version
	var dbVersion string
	if err := s.db.QueryRowContext(ctx, "SELECT version()").Scan(&dbVersion); err == nil {
		info["database_version"] = dbVersion
	}

	// Get table counts
	tableCounts := make(map[string]int64)
	for _, table := range DiagnosticTables {
		var count int64
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		if err := s.db.QueryRowContext(ctx, query).Scan(&count); err == nil {
			tableCounts[table] = count
		}
	}
	info["table_counts"] = tableCounts

	// Get database size
	var dbSize string
	if err := s.db.QueryRowContext(ctx, "SELECT pg_size_pretty(pg_database_size(current_database()))").Scan(&dbSize); err == nil {
		info["database_size"] = dbSize
	}

	// Get connection info
	var maxConnections int
	if err := s.db.QueryRowContext(ctx, "SELECT setting::int FROM pg_settings WHERE name = 'max_connections'").Scan(&maxConnections); err == nil {
		info["max_connections"] = maxConnections
	}

	stats := s.db.Stats()
	info["connection_stats"] = map[string]interface{}{
		"open_connections":  stats.OpenConnections,
		"in_use":            stats.InUse,
		"idle":              stats.Idle,
		"max_open":          stats.MaxOpenConnections,
		"wait_count":        stats.WaitCount,
		"wait_duration_ms":  stats.WaitDuration.Milliseconds(),
		"max_idle_closed":   stats.MaxIdleClosed,
		"max_lifetime_closed": stats.MaxLifetimeClosed,
	}

	info["exported_at"] = time.Now()

	return info, nil
}
