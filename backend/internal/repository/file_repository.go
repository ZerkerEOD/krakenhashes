package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// FileInfo represents information about a file for synchronization
type FileInfo struct {
	Name      string `json:"name"`
	MD5Hash   string `json:"md5_hash"` // MD5 hash used for synchronization
	Size      int64  `json:"size"`
	FileType  string `json:"file_type"` // "wordlist", "rule", "binary", "hashlist"
	Category  string `json:"category,omitempty"`
	ID        int    `json:"id,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
	RuleCount int64  `json:"rule_count,omitempty"` // For rules: number of rules in file
}

// FileRepository handles database operations for files (wordlists, rules, binaries)
// and file storage operations.
type FileRepository struct {
	db       *db.DB
	basePath string // Base directory for storing files (e.g., configured upload dir)
}

// NewFileRepository creates a new file repository
func NewFileRepository(db *db.DB, basePath string) *FileRepository {
	debug.Info("Initializing FileRepository with base path: %s", basePath)
	return &FileRepository{db: db, basePath: basePath}
}

// GetWordlists retrieves wordlists matching the specified category
func (r *FileRepository) GetWordlists(ctx context.Context, category string) ([]FileInfo, error) {
	// Check if category is valid for wordlist_type enum
	var query string
	var rows *sql.Rows
	var err error

	if category == "" {
		// If category is empty, return all verified wordlists
		query = `
			SELECT id, name, file_name, md5_hash, file_size, wordlist_type, updated_at 
			FROM wordlists 
			WHERE verification_status = 'verified'
		`
		rows, err = r.db.QueryContext(ctx, query)
	} else if category == "general" || category == "specialized" || category == "targeted" || category == "custom" {
		// Category is a valid enum value, use it for filtering
		query = `
			SELECT id, name, file_name, md5_hash, file_size, wordlist_type, updated_at 
			FROM wordlists 
			WHERE wordlist_type = $1::wordlist_type
			AND verification_status = 'verified'
		`
		rows, err = r.db.QueryContext(ctx, query, category)
	} else {
		// Category is not a valid enum value, return empty set
		debug.Info("Invalid wordlist_type category: %s, returning empty set", category)
		return []FileInfo{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("error querying wordlists: %w", err)
	}
	defer rows.Close()

	var files []FileInfo

	for rows.Next() {
		var id int
		var name, fileName, md5Hash, wordlistType string
		var size int64
		var updatedAt time.Time

		if err := rows.Scan(&id, &name, &fileName, &md5Hash, &size, &wordlistType, &updatedAt); err != nil {
			debug.Error("Error scanning wordlist row: %v", err)
			continue
		}

		// Use fileName directly as it already contains the correct relative path
		// e.g., "general/crackstation.txt"
		files = append(files, FileInfo{
			Name:      fileName,
			MD5Hash:   md5Hash,
			Size:      size,
			FileType:  "wordlist",
			Category:  wordlistType,
			ID:        id,
			Timestamp: updatedAt.Unix(),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating wordlist rows: %w", err)
	}

	return files, nil
}

// GetRules retrieves rules matching the specified category
func (r *FileRepository) GetRules(ctx context.Context, category string) ([]FileInfo, error) {
	// Check if category is valid for rule_type enum
	var query string
	var rows *sql.Rows
	var err error

	if category == "" {
		// If category is empty, return all verified rules
		query = `
			SELECT id, name, file_name, md5_hash, file_size, rule_type, updated_at, rule_count
			FROM rules
			WHERE verification_status = 'verified'
		`
		rows, err = r.db.QueryContext(ctx, query)
	} else if category == "hashcat" || category == "john" {
		// Category is a valid enum value, use it for filtering
		query = `
			SELECT id, name, file_name, md5_hash, file_size, rule_type, updated_at, rule_count
			FROM rules
			WHERE rule_type = $1::rule_type
			AND verification_status = 'verified'
		`
		rows, err = r.db.QueryContext(ctx, query, category)
	} else {
		// Category is not a valid enum value, return empty set
		debug.Info("Invalid rule_type category: %s, returning empty set", category)
		return []FileInfo{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("error querying rules: %w", err)
	}
	defer rows.Close()

	var files []FileInfo

	for rows.Next() {
		var id int
		var name, fileName, md5Hash, ruleType string
		var size int64
		var updatedAt time.Time
		var ruleCount int64

		if err := rows.Scan(&id, &name, &fileName, &md5Hash, &size, &ruleType, &updatedAt, &ruleCount); err != nil {
			debug.Error("Error scanning rule row: %v", err)
			continue
		}

		// Use fileName directly as it already contains the correct relative path
		// e.g., "hashcat/_nsakey.v2.dive.rule"
		files = append(files, FileInfo{
			Name:      fileName,
			MD5Hash:   md5Hash,
			Size:      size,
			FileType:  "rule",
			Category:  ruleType,
			ID:        id,
			Timestamp: updatedAt.Unix(),
			RuleCount: ruleCount,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rule rows: %w", err)
	}

	return files, nil
}

// GetBinaries retrieves binary versions matching the specified category
func (r *FileRepository) GetBinaries(ctx context.Context, category string) ([]FileInfo, error) {
	// Check if category is valid for binary_type enum
	var query string
	var rows *sql.Rows
	var err error

	if category == "" {
		// If category is empty, return all verified active binaries
		query = `
			SELECT id, file_name, md5_hash, file_size, binary_type, created_at 
			FROM binary_versions 
			WHERE verification_status = 'verified'
			AND is_active = true
		`
		rows, err = r.db.QueryContext(ctx, query)
	} else if category == "hashcat" || category == "john" {
		// Category is a valid enum value, use it for filtering
		query = `
			SELECT id, file_name, md5_hash, file_size, binary_type, created_at 
			FROM binary_versions 
			WHERE binary_type = $1::binary_type
			AND verification_status = 'verified'
			AND is_active = true
		`
		rows, err = r.db.QueryContext(ctx, query, category)
	} else {
		// Category is not a valid enum value, return empty set
		debug.Info("Invalid binary_type category: %s, returning empty set", category)
		return []FileInfo{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("error querying binary versions: %w", err)
	}
	defer rows.Close()

	var files []FileInfo

	for rows.Next() {
		var id int
		var fileName, md5Hash, binaryType string
		var size int64
		var createdAt time.Time

		if err := rows.Scan(&id, &fileName, &md5Hash, &size, &binaryType, &createdAt); err != nil {
			debug.Error("Error scanning binary row: %v", err)
			continue
		}

		files = append(files, FileInfo{
			Name:      fileName,
			MD5Hash:   md5Hash,
			Size:      size,
			FileType:  "binary",
			Category:  binaryType,
			ID:        id,
			Timestamp: createdAt.Unix(),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating binary rows: %w", err)
	}

	return files, nil
}

// GetFiles retrieves all files of the specified types and category
func (r *FileRepository) GetFiles(ctx context.Context, fileTypes []string, category string) ([]FileInfo, error) {
	debug.Info("Retrieving files for types: %v, category: %s", fileTypes, category)

	var files []FileInfo
	var err error
	var typeFiles []FileInfo

	for _, fileType := range fileTypes {
		switch fileType {
		case "wordlist":
			typeFiles, err = r.GetWordlists(ctx, category)
			if err != nil {
				debug.Error("Error getting wordlists: %v", err)
				continue
			}
			files = append(files, typeFiles...)

		case "rule":
			typeFiles, err = r.GetRules(ctx, category)
			if err != nil {
				debug.Error("Error getting rules: %v", err)
				continue
			}
			files = append(files, typeFiles...)

		case "binary":
			typeFiles, err = r.GetBinaries(ctx, category)
			if err != nil {
				debug.Error("Error getting binaries: %v", err)
				continue
			}
			files = append(files, typeFiles...)
		}
	}

	debug.Info("Retrieved %d files from database", len(files))
	return files, nil
}

// Save stores an uploaded file to a specified directory relative to the base path.
// It ensures the target directory exists and returns the final saved filename.
// It performs basic sanitization on the original filename.
func (r *FileRepository) Save(file multipart.File, targetDir, originalFilename string) (string, error) {
	// Ensure targetDir is relative to basePath or handle absolute paths appropriately?
	// For now, assume targetDir is intended to be *within* basePath or an absolute path itself.
	// Let's treat targetDir as the full intended directory path.
	destDir := targetDir
	if !filepath.IsAbs(targetDir) {
		destDir = filepath.Join(r.basePath, targetDir)
	}

	// Create the target directory if it doesn't exist
	if err := os.MkdirAll(destDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create target directory '%s': %w", destDir, err)
	}

	// Sanitize filename (basic example: remove path separators, limit chars)
	sanitizedFilename := filepath.Base(originalFilename)                // Remove leading paths
	sanitizedFilename = strings.ReplaceAll(sanitizedFilename, "..", "") // Prevent traversal
	// Add more sanitization as needed (e.g., allowlist characters)
	if sanitizedFilename == "" {
		sanitizedFilename = "uploaded_file"
	}

	destPath := filepath.Join(destDir, sanitizedFilename)

	// Create the destination file
	dst, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file '%s': %w", destPath, err)
	}
	defer dst.Close()

	// Copy the uploaded file data
	_, err = io.Copy(dst, file)
	if err != nil {
		// Attempt to remove partially created file on copy error
		_ = os.Remove(destPath)
		return "", fmt.Errorf("failed to copy file data to '%s': %w", destPath, err)
	}

	debug.Debug("Saved file to: %s", destPath)
	return sanitizedFilename, nil
}

// Delete removes a file from the specified directory relative to the base path.
func (r *FileRepository) Delete(targetDir, filename string) error {
	// Construct full path
	destDir := targetDir
	if !filepath.IsAbs(targetDir) {
		destDir = filepath.Join(r.basePath, targetDir)
	}
	filePath := filepath.Join(destDir, filename)

	// Ensure the path is within expected bounds (simple check)
	if !strings.HasPrefix(filePath, r.basePath) && !strings.HasPrefix(destDir, r.basePath) {
		// Prevent deleting files outside the intended base/target path if targetDir was relative
		// Note: This check is basic and might need refinement depending on how targetDir is used.
		// If targetDir can be absolute, this check might be bypassed.
		if !filepath.IsAbs(targetDir) {
			debug.Error("Attempted to delete file outside base path: %s", filePath)
			return fmt.Errorf("invalid file path for deletion")
		}
	}

	// Remove the file
	err := os.Remove(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			debug.Warning("Attempted to delete non-existent file: %s", filePath)
			return nil // Not an error if it's already gone
		}
		return fmt.Errorf("failed to delete file '%s': %w", filePath, err)
	}

	debug.Debug("Deleted file: %s", filePath)
	return nil
}

// Open returns a readable stream (os.File) for the given file path.
// The path is assumed to be the full, absolute path or relative to the OS cwd.
// TODO: Should this also join with basePath or assume full path is provided?
// Assuming full path is provided from the HashList record for now.
func (r *FileRepository) Open(filePath string) (*os.File, error) {
	// Basic check to prevent path traversal if basePath joining were used
	// cleanPath := filepath.Clean(filePath)
	// if !strings.HasPrefix(cleanPath, r.basePath) { // If joining with basePath
	// 	 return nil, fmt.Errorf("invalid file path")
	// }

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found at path '%s'", filePath)
		}
		return nil, fmt.Errorf("failed to open file '%s': %w", filePath, err)
	}
	return file, nil
}
