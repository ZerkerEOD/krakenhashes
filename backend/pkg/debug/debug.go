package debug

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarning
	LevelError
)

var (
	// mu protects isEnabled and currentLevel from concurrent access
	mu sync.RWMutex
	// isEnabled controls whether debug messages are output (use IsDebugEnabled() to read)
	isEnabled bool
	// currentLevel is the minimum level of messages to output (use GetLogLevel() to read)
	currentLevel LogLevel
	logger       *log.Logger
	levelNames   = map[LogLevel]string{
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
	// Initialize logger with timestamp and caller info
	logger = log.New(os.Stdout, "", 0)

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

	// Only log initialization if debugging is enabled
	if enabled {
		Info("Debug logging initialized - Enabled: %v, Level: %s", enabled, levelNames[level])
	}
}

// IsDebugEnabled returns whether debug logging is enabled (thread-safe)
func IsDebugEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return isEnabled
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

// SetEnabled enables or disables debug logging at runtime (thread-safe)
func SetEnabled(enabled bool) {
	mu.Lock()
	defer mu.Unlock()
	isEnabled = enabled
}

// SetLogLevel sets the minimum log level at runtime (thread-safe)
func SetLogLevel(level LogLevel) {
	mu.Lock()
	defer mu.Unlock()
	currentLevel = level
}

// ParseLevel converts a string to LogLevel
func ParseLevel(levelStr string) (LogLevel, bool) {
	level, exists := levelMap[strings.ToUpper(levelStr)]
	return level, exists
}

// Log prints a structured log message if debugging is enabled
func Log(message string, fields map[string]interface{}) {
	if fields == nil {
		LogWithLevel(LevelInfo, "%s", message)
	} else {
		var fieldStrs []string
		for k, v := range fields {
			fieldStrs = append(fieldStrs, fmt.Sprintf("%s=%v", k, v))
		}
		LogWithLevel(LevelInfo, "%s [%s]", message, strings.Join(fieldStrs, ", "))
	}
}

func LogWithLevel(level LogLevel, format string, v ...interface{}) {
	// Check if debugging is enabled and if the message level is high enough
	mu.RLock()
	enabled := isEnabled
	minLevel := currentLevel
	mu.RUnlock()

	if !enabled || level < minLevel {
		return
	}

	// Get caller information
	pc, file, line, _ := runtime.Caller(2)
	funcName := runtime.FuncForPC(pc).Name()

	// Format the message
	message := fmt.Sprintf(format, v...)
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")

	logger.Printf("[%s] [%s] [%s:%d] [%s] %s\n",
		levelNames[level],
		timestamp,
		file,
		line,
		funcName,
		message,
	)
}

// Debug logs a debug level message
func Debug(format string, v ...interface{}) {
	LogWithLevel(LevelDebug, format, v...)
}

// Info logs an info level message
func Info(format string, v ...interface{}) {
	LogWithLevel(LevelInfo, format, v...)
}

// Warning logs a warning level message
func Warning(format string, v ...interface{}) {
	LogWithLevel(LevelWarning, format, v...)
}

// Error logs an error level message
func Error(format string, v ...interface{}) {
	LogWithLevel(LevelError, format, v...)
}

// Reinitialize updates the debug settings based on current environment variables
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

	// Only log reinitialization if debugging is enabled
	if enabled {
		Info("Debug logging reinitialized - Enabled: %v, Level: %s", enabled, levelNames[level])
	}
}

// sensitiveHeaders maps header names to their redaction field names
var sensitiveHeaders = map[string]string{
	"X-Api-Key":     "api_key",
	"Authorization": "authorization",
	"Cookie":        "cookie",
}

// SanitizeHeaders returns a string representation of headers with sensitive values redacted
// Sensitive headers (X-Api-Key, Authorization, Cookie) are replaced with [REDACTED:field:len=N]
func SanitizeHeaders(headers http.Header) string {
	sanitized := make(http.Header)
	for key, values := range headers {
		if fieldName, isSensitive := sensitiveHeaders[key]; isSensitive {
			totalLen := 0
			for _, v := range values {
				totalLen += len(v)
			}
			sanitized[key] = []string{fmt.Sprintf("[REDACTED:%s:len=%d]", fieldName, totalLen)}
		} else {
			sanitized[key] = values
		}
	}
	return fmt.Sprintf("%v", sanitized)
}

// homePathPattern matches /home/username or /Users/username patterns
var homePathPattern = regexp.MustCompile(`(/home/|/Users/)([^/\s"'\]]+)`)

// jwtPattern matches JWT tokens (base64url header.payload.signature starting with eyJhbGci)
var jwtPattern = regexp.MustCompile(`eyJhbGci[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`)

// SanitizeLogContent sanitizes sensitive content in log files
// - Replaces /home/username/ or /Users/username/ with /home/[USER]/ or /Users/[USER]/
// - Replaces JWT tokens with [REDACTED:jwt_token]
func SanitizeLogContent(content string) string {
	// Sanitize user home paths
	content = homePathPattern.ReplaceAllString(content, "$1[USER]")

	// Sanitize JWT tokens
	content = jwtPattern.ReplaceAllString(content, "[REDACTED:jwt_token]")

	return content
}
