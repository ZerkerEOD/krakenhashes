package debug

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/logbuffer"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarning
	LevelError
)

const (
	// DefaultLogBufferSize is the default number of entries in the ring buffer
	DefaultLogBufferSize = 1000
	// LogFileName is the name of the log file when file logging is enabled
	LogFileName = "agent.log"
)

var (
	// mu protects all mutable state from concurrent access
	mu sync.RWMutex

	// isEnabled controls whether debug messages are output
	isEnabled bool
	// currentLevel is the minimum level of messages to output
	currentLevel LogLevel

	// File logging state
	fileLoggingEnabled bool
	logFile            *os.File
	logFilePath        string

	// Multi-writer for stdout + file
	stdoutLogger *log.Logger
	fileLogger   *log.Logger
	multiLogger  *log.Logger

	// Ring buffer for in-memory log collection
	logBuffer *logbuffer.RingBuffer

	// Path sanitization: base path to strip from logged paths
	basePathPrefix string
	basePathMu     sync.RWMutex

	levelNames = map[LogLevel]string{
		LevelDebug:   "DEBUG",
		LevelInfo:    "INFO",
		LevelWarning: "WARNING",
		LevelError:   "ERROR",
	}
	levelMap = map[string]LogLevel{
		"DEBUG":   LevelDebug,
		"INFO":    LevelInfo,
		"WARNING": LevelWarning,
		"ERROR":   LevelError,
	}
)

func init() {
	// Initialize stdout logger
	stdoutLogger = log.New(os.Stdout, "", 0)

	// Initialize ring buffer
	bufferSize := DefaultLogBufferSize
	if sizeStr := os.Getenv("LOG_BUFFER_SIZE"); sizeStr != "" {
		if size, err := strconv.Atoi(sizeStr); err == nil && size > 0 {
			bufferSize = size
		}
	}
	logBuffer = logbuffer.New(bufferSize)

	// Check DEBUG environment variable
	debugEnv := os.Getenv("DEBUG")
	enabled := debugEnv == "true" || debugEnv == "1"

	// Set log level from environment variable
	levelEnv := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	level := LevelInfo // Default to INFO if not specified
	if l, exists := levelMap[levelEnv]; exists {
		level = l
	}

	// Set initial values with mutex protection
	mu.Lock()
	isEnabled = enabled
	currentLevel = level
	mu.Unlock()

	// Auto-enable file logging if DEBUG is enabled and LOG_DIR is set
	if enabled {
		if logDir := os.Getenv("LOG_DIR"); logDir != "" {
			// Try to enable file logging, but don't fail if it doesn't work
			_ = EnableFileLogging(logDir)
		}
	}

	// Only log initialization if debugging is enabled
	if enabled {
		Info("Debug logging initialized - Enabled: %v, Level: %s, FileLogging: %v", enabled, levelNames[level], fileLoggingEnabled)
	}
}

// IsDebugEnabled returns whether debug logging is enabled (thread-safe)
func IsDebugEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return isEnabled
}

// IsFileLoggingEnabled returns whether file logging is enabled (thread-safe)
func IsFileLoggingEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return fileLoggingEnabled
}

// GetLogFilePath returns the path to the log file if file logging is enabled
func GetLogFilePath() string {
	mu.RLock()
	defer mu.RUnlock()
	return logFilePath
}

// GetLogLevel returns the current log level (thread-safe)
func GetLogLevel() LogLevel {
	mu.RLock()
	defer mu.RUnlock()
	return currentLevel
}

// GetLogLevelName returns the name of the current log level (thread-safe)
func GetLogLevelName() string {
	mu.RLock()
	defer mu.RUnlock()
	return levelNames[currentLevel]
}

// EnableFileLogging enables writing logs to a file in the specified directory
// The log file will be created at logsDir/agent.log
func EnableFileLogging(logsDir string) error {
	mu.Lock()
	defer mu.Unlock()

	// Already enabled to the same directory
	if fileLoggingEnabled && logFilePath == filepath.Join(logsDir, LogFileName) {
		return nil
	}

	// Close existing file if open
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}

	// Create logs directory if it doesn't exist
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Open log file for append
	path := filepath.Join(logsDir, LogFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logFile = f
	logFilePath = path
	fileLoggingEnabled = true

	// Create file logger
	fileLogger = log.New(f, "", 0)

	// Create multi-writer for both stdout and file
	multiWriter := io.MultiWriter(os.Stdout, f)
	multiLogger = log.New(multiWriter, "", 0)

	return nil
}

// DisableFileLogging disables file logging and closes the log file
func DisableFileLogging() error {
	mu.Lock()
	defer mu.Unlock()

	if !fileLoggingEnabled {
		return nil
	}

	fileLoggingEnabled = false
	logFilePath = ""

	if logFile != nil {
		err := logFile.Close()
		logFile = nil
		fileLogger = nil
		multiLogger = nil
		return err
	}

	return nil
}

// GetBufferedLogs returns all log entries since the specified time
func GetBufferedLogs(since time.Time) []logbuffer.LogEntry {
	return logBuffer.GetSince(since)
}

// GetAllBufferedLogs returns all log entries in the buffer
func GetAllBufferedLogs() []logbuffer.LogEntry {
	return logBuffer.GetAll()
}

// ClearLogBuffer clears the in-memory log buffer
func ClearLogBuffer() {
	logBuffer.Clear()
}

// GetBufferCount returns the number of entries in the log buffer
func GetBufferCount() int {
	return logBuffer.Count()
}

// Log prints a debug message with the specified level if debugging is enabled
func Log(level LogLevel, format string, v ...interface{}) {
	// Check if debugging is enabled and if the message level is high enough
	mu.RLock()
	enabled := isEnabled
	minLevel := currentLevel
	fileEnabled := fileLoggingEnabled
	mu.RUnlock()

	if !enabled || level < minLevel {
		return
	}

	// Get caller information (skip 2 levels: Log -> Debug/Info/etc -> actual caller)
	pc, file, line, _ := runtime.Caller(2)
	funcName := runtime.FuncForPC(pc).Name()

	// Format the message and sanitize paths
	message := fmt.Sprintf(format, v...)
	message = SanitizeMessage(message)
	timestamp := time.Now()
	timestampStr := timestamp.Format("2006-01-02 15:04:05.000")

	// Add to ring buffer
	logBuffer.Add(logbuffer.LogEntry{
		Timestamp: timestamp,
		Level:     levelNames[level],
		Message:   message,
		File:      file,
		Line:      line,
		Function:  funcName,
	})

	// Format the log line
	logLine := fmt.Sprintf("[%s] [%s] [%s:%d] [%s] %s\n",
		levelNames[level],
		timestampStr,
		file,
		line,
		funcName,
		message,
	)

	// Write to appropriate logger(s)
	mu.RLock()
	if fileEnabled && multiLogger != nil {
		multiLogger.Print(logLine)
	} else {
		stdoutLogger.Print(logLine)
	}
	mu.RUnlock()
}

// Debug logs a debug level message
func Debug(format string, v ...interface{}) {
	Log(LevelDebug, format, v...)
}

// Info logs an info level message
func Info(format string, v ...interface{}) {
	Log(LevelInfo, format, v...)
}

// Warning logs a warning level message
func Warning(format string, v ...interface{}) {
	Log(LevelWarning, format, v...)
}

// Error logs an error level message
func Error(format string, v ...interface{}) {
	Log(LevelError, format, v...)
}

// Reinitialize reinitializes the debug package with current environment variables
func Reinitialize() {
	// Check DEBUG environment variable
	debugEnv := os.Getenv("DEBUG")
	enabled := debugEnv == "true" || debugEnv == "1"

	// Set log level from environment variable
	levelEnv := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	level := LevelInfo // Default to INFO if not specified
	if l, exists := levelMap[levelEnv]; exists {
		level = l
	}

	// Update values with mutex protection
	mu.Lock()
	isEnabled = enabled
	currentLevel = level
	mu.Unlock()

	// Handle file logging based on DEBUG state
	if enabled {
		if logDir := os.Getenv("LOG_DIR"); logDir != "" {
			_ = EnableFileLogging(logDir)
		}
	} else {
		_ = DisableFileLogging()
	}

	// Only log reinitialization if debugging is enabled
	if enabled {
		Info("Debug logging reinitialized - Enabled: %v, Level: %s, FileLogging: %v", enabled, levelNames[level], fileLoggingEnabled)
	}
}

// SetEnabled directly sets the debug enabled state (used for runtime toggling)
func SetEnabled(enabled bool) {
	mu.Lock()
	isEnabled = enabled
	mu.Unlock()
}

// SetLevel directly sets the log level (used for runtime changes)
func SetLevel(level LogLevel) {
	mu.Lock()
	currentLevel = level
	mu.Unlock()
}

// GetDebugStatus returns a summary of the current debug configuration
type DebugStatus struct {
	Enabled            bool   `json:"enabled"`
	Level              string `json:"level"`
	FileLoggingEnabled bool   `json:"file_logging_enabled"`
	LogFilePath        string `json:"log_file_path,omitempty"`
	BufferCount        int    `json:"buffer_count"`
	BufferCapacity     int    `json:"buffer_capacity"`
}

// GetStatus returns the current debug status
func GetStatus() DebugStatus {
	mu.RLock()
	defer mu.RUnlock()

	return DebugStatus{
		Enabled:            isEnabled,
		Level:              levelNames[currentLevel],
		FileLoggingEnabled: fileLoggingEnabled,
		LogFilePath:        logFilePath,
		BufferCount:        logBuffer.Count(),
		BufferCapacity:     logBuffer.Capacity(),
	}
}

// SetBasePath sets the base path that should be stripped from logged paths
// to convert absolute paths to relative paths for privacy.
// e.g., SetBasePath("/home/user/agent") means "/home/user/agent/data/foo.txt"
// will be logged as "data/foo.txt"
func SetBasePath(path string) {
	basePathMu.Lock()
	defer basePathMu.Unlock()
	// Ensure path ends with separator for clean replacement
	if path != "" && !strings.HasSuffix(path, string(os.PathSeparator)) {
		path += string(os.PathSeparator)
	}
	basePathPrefix = path
}

// GetBasePath returns the currently configured base path for sanitization
func GetBasePath() string {
	basePathMu.RLock()
	defer basePathMu.RUnlock()
	return basePathPrefix
}

// SanitizeMessage converts absolute paths to relative paths by removing the base path prefix
func SanitizeMessage(msg string) string {
	basePathMu.RLock()
	prefix := basePathPrefix
	basePathMu.RUnlock()

	if prefix == "" {
		return msg
	}

	// First: replace paths WITH trailing separator (files/subdirs inside the dir)
	// e.g., "/home/user/agent/data/foo.txt" -> "data/foo.txt"
	msg = strings.ReplaceAll(msg, prefix, "")

	// Second: replace paths WITHOUT trailing separator (the dir itself)
	// e.g., "/home/user/agent" -> "."
	pathWithoutSlash := strings.TrimSuffix(prefix, string(os.PathSeparator))
	if pathWithoutSlash != "" {
		msg = strings.ReplaceAll(msg, pathWithoutSlash, ".")
	}

	return msg
}
