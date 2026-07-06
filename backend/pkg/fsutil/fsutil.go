package fsutil

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CountLinesInFile counts the number of lines in a file
func CountLinesInFile(filePath string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	// Get file info for size
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}

	// For very large files (over 1GB), use a more efficient counting method
	if fileInfo.Size() > 1024*1024*1024 {
		// Use a buffered reader with a large buffer size for better performance
		const bufferSize = 16 * 1024 * 1024
		reader := bufio.NewReaderSize(file, bufferSize)

		var count int64
		var buf [4096]byte

		for {
			c, err := reader.Read(buf[:])
			if err != nil {
				if err == io.EOF {
					break
				}
				return 0, err
			}

			// Count newlines in the buffer
			for i := 0; i < c; i++ {
				if buf[i] == '\n' {
					count++
				}
			}
		}

		// Add 1 if the file doesn't end with a newline
		if count > 0 {
			lastByte := make([]byte, 1)
			if _, err := file.ReadAt(lastByte, fileInfo.Size()-1); err == nil {
				if lastByte[0] != '\n' {
					count++
				}
			}
		}

		return count, nil
	}

	// For regular files, use scanner with increased buffer size
	// Create a scanner with a large buffer to handle long lines
	const maxScanTokenSize = 1024 * 1024 // 1MB buffer
	scanner := bufio.NewScanner(file)
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	var count int64
	for scanner.Scan() {
		count++
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return count, nil
}

// EnsureDirectoryExists creates a directory if it doesn't exist
func EnsureDirectoryExists(dirPath string) error {
	return os.MkdirAll(dirPath, 0755)
}

// FileExists checks if a file exists
func FileExists(filePath string) bool {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// DirectoryExists checks if a directory exists
func DirectoryExists(dirPath string) bool {
	info, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}

// WalkDirectory walks a directory and calls the callback for each file
func WalkDirectory(dirPath string, callback func(path string, info os.FileInfo) error) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return callback(path, info)
		}
		return nil
	})
}

// GetFileSize returns the size of a file in bytes
func GetFileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// NormalizeRuleFile strips duplicate empty/whitespace-only lines from a hashcat rule file.
// hashcat treats empty lines as passthrough rules (:). Multiple empty lines produce
// redundant duplicate work. This keeps at most one empty line (the first encountered).
// Returns true if the file was modified, false if no changes were needed.
func NormalizeRuleFile(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to open rule file: %w", err)
	}

	var lines []string
	seenEmpty := false
	changed := false

	scanner := bufio.NewScanner(file)
	// Use large buffer for rule files with long lines
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			if seenEmpty {
				// Skip duplicate empty line
				changed = true
				continue
			}
			seenEmpty = true
		}

		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		file.Close()
		return false, fmt.Errorf("failed to read rule file: %w", err)
	}
	file.Close()

	if !changed {
		return false, nil
	}

	// Write back the normalized content
	outFile, err := os.Create(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to write normalized rule file: %w", err)
	}
	defer outFile.Close()

	writer := bufio.NewWriter(outFile)
	for _, line := range lines {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return false, fmt.Errorf("failed to write line: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return false, fmt.Errorf("failed to flush writer: %w", err)
	}

	return true, nil
}

// CountHashcatRules counts the number of rules in a hashcat rule file.
// It counts all lines except those starting with # (comments).
// Empty lines are counted as they are valid passthrough rules in hashcat.
func CountHashcatRules(filePath string) (int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var count int64
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "#") {
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return count, nil
}

// SanitizeFilename sanitizes a filename for safe storage
// It replaces spaces and path separators with hyphens and converts to lowercase
func SanitizeFilename(filename string) string {
	// Replace problematic characters with hyphens
	sanitized := strings.ReplaceAll(filename, " ", "-")
	sanitized = strings.ReplaceAll(sanitized, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")

	// Convert to lowercase for consistency
	sanitized = strings.ToLower(sanitized)

	return sanitized
}

// ExtractBaseNameWithoutExt extracts the base filename without extension(s)
// It handles multi-part extensions like .v2.dive.rule by removing only the final extension
func ExtractBaseNameWithoutExt(filename string) string {
	base := filepath.Base(filename)

	// Remove only the last extension (e.g., .rule, .txt)
	// This preserves multi-part names like name.v2.dive
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)
	
	// If the result is empty (hidden files like .gitignore), return the original base
	if nameWithoutExt == "" {
		return base
	}
	
	return nameWithoutExt
}
