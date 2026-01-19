package debug

import (
	"bytes"
	"log"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// saveAndRestoreState is a helper to save and restore debug state for testing
func saveAndRestoreState(t *testing.T) func() {
	t.Helper()
	originalDebugEnv := os.Getenv("DEBUG")
	originalLogLevelEnv := os.Getenv("LOG_LEVEL")

	mu.Lock()
	originalEnabled := isEnabled
	originalLevel := currentLevel
	mu.Unlock()

	return func() {
		os.Setenv("DEBUG", originalDebugEnv)
		os.Setenv("LOG_LEVEL", originalLogLevelEnv)
		mu.Lock()
		isEnabled = originalEnabled
		currentLevel = originalLevel
		mu.Unlock()
	}
}

func TestLogLevel(t *testing.T) {
	// Test log level constants
	assert.Equal(t, LogLevel(0), LevelDebug)
	assert.Equal(t, LogLevel(1), LevelInfo)
	assert.Equal(t, LogLevel(2), LevelWarning)
	assert.Equal(t, LogLevel(3), LevelError)

	// Test level names
	assert.Equal(t, "DEBUG", levelNames[LevelDebug])
	assert.Equal(t, "INFO", levelNames[LevelInfo])
	assert.Equal(t, "WARNING", levelNames[LevelWarning])
	assert.Equal(t, "ERROR", levelNames[LevelError])
}

func TestInit(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	tests := []struct {
		name          string
		debugEnv      string
		logLevelEnv   string
		expectEnabled bool
		expectLevel   LogLevel
	}{
		{
			name:          "debug disabled by default",
			debugEnv:      "",
			logLevelEnv:   "",
			expectEnabled: false,
			expectLevel:   LevelInfo,
		},
		{
			name:          "debug enabled with true",
			debugEnv:      "true",
			logLevelEnv:   "",
			expectEnabled: true,
			expectLevel:   LevelInfo,
		},
		{
			name:          "debug enabled with 1",
			debugEnv:      "1",
			logLevelEnv:   "",
			expectEnabled: true,
			expectLevel:   LevelInfo,
		},
		{
			name:          "debug level set to DEBUG",
			debugEnv:      "true",
			logLevelEnv:   "DEBUG",
			expectEnabled: true,
			expectLevel:   LevelDebug,
		},
		{
			name:          "debug level set to WARNING",
			debugEnv:      "true",
			logLevelEnv:   "WARNING",
			expectEnabled: true,
			expectLevel:   LevelWarning,
		},
		{
			name:          "debug level case insensitive",
			debugEnv:      "true",
			logLevelEnv:   "error",
			expectEnabled: true,
			expectLevel:   LevelError,
		},
		{
			name:          "invalid log level defaults to INFO",
			debugEnv:      "true",
			logLevelEnv:   "INVALID",
			expectEnabled: true,
			expectLevel:   LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("DEBUG", tt.debugEnv)
			os.Setenv("LOG_LEVEL", tt.logLevelEnv)

			// Reinitialize to pick up new env vars
			Reinitialize()

			assert.Equal(t, tt.expectEnabled, IsDebugEnabled())
			assert.Equal(t, tt.expectLevel, GetLogLevel())
		})
	}
}

func TestLog(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	// Save and replace the stdout logger to capture output
	mu.Lock()
	originalLogger := stdoutLogger
	mu.Unlock()
	defer func() {
		mu.Lock()
		stdoutLogger = originalLogger
		mu.Unlock()
	}()

	tests := []struct {
		name           string
		enabled        bool
		currentLevel   LogLevel
		logLevel       LogLevel
		format         string
		args           []interface{}
		expectOutput   bool
		expectContains []string
	}{
		{
			name:         "debug disabled - no output",
			enabled:      false,
			currentLevel: LevelInfo,
			logLevel:     LevelInfo,
			format:       "test message",
			expectOutput: false,
		},
		{
			name:         "level too low - no output",
			enabled:      true,
			currentLevel: LevelWarning,
			logLevel:     LevelInfo,
			format:       "test message",
			expectOutput: false,
		},
		{
			name:         "info message output",
			enabled:      true,
			currentLevel: LevelInfo,
			logLevel:     LevelInfo,
			format:       "test message %s",
			args:         []interface{}{"with args"},
			expectOutput: true,
			expectContains: []string{
				"[INFO]",
				"test message with args",
			},
		},
		{
			name:         "error message output",
			enabled:      true,
			currentLevel: LevelDebug,
			logLevel:     LevelError,
			format:       "error occurred: %v",
			args:         []interface{}{"test error"},
			expectOutput: true,
			expectContains: []string{
				"[ERROR]",
				"error occurred: test error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			mu.Lock()
			stdoutLogger = log.New(&buf, "", 0)
			isEnabled = tt.enabled
			currentLevel = tt.currentLevel
			mu.Unlock()

			Log(tt.logLevel, tt.format, tt.args...)

			output := buf.String()
			if tt.expectOutput {
				assert.NotEmpty(t, output)
				for _, expected := range tt.expectContains {
					assert.Contains(t, output, expected)
				}
			} else {
				assert.Empty(t, output)
			}
		})
	}
}

func TestLogFunctions(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	// Create a buffer to capture log output
	var buf bytes.Buffer
	mu.Lock()
	originalLogger := stdoutLogger
	stdoutLogger = log.New(&buf, "", 0)
	isEnabled = true
	currentLevel = LevelDebug
	mu.Unlock()
	defer func() {
		mu.Lock()
		stdoutLogger = originalLogger
		mu.Unlock()
	}()

	// Test Debug
	buf.Reset()
	Debug("debug message %d", 123)
	output := buf.String()
	assert.Contains(t, output, "[DEBUG]")
	assert.Contains(t, output, "debug message 123")

	// Test Info
	buf.Reset()
	Info("info message %s", "test")
	output = buf.String()
	assert.Contains(t, output, "[INFO]")
	assert.Contains(t, output, "info message test")

	// Test Warning
	buf.Reset()
	Warning("warning message %v", true)
	output = buf.String()
	assert.Contains(t, output, "[WARNING]")
	assert.Contains(t, output, "warning message true")

	// Test Error
	buf.Reset()
	Error("error message: %s", "failed")
	output = buf.String()
	assert.Contains(t, output, "[ERROR]")
	assert.Contains(t, output, "error message: failed")
}

func TestLogLevelFiltering(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	// Create a buffer to capture log output
	var buf bytes.Buffer
	mu.Lock()
	originalLogger := stdoutLogger
	stdoutLogger = log.New(&buf, "", 0)
	isEnabled = true
	mu.Unlock()
	defer func() {
		mu.Lock()
		stdoutLogger = originalLogger
		mu.Unlock()
	}()

	tests := []struct {
		name         string
		currentLevel LogLevel
		messages     []struct {
			fn     func(string, ...interface{})
			msg    string
			expect bool
		}
	}{
		{
			name:         "INFO level filters DEBUG",
			currentLevel: LevelInfo,
			messages: []struct {
				fn     func(string, ...interface{})
				msg    string
				expect bool
			}{
				{Debug, "debug msg", false},
				{Info, "info msg", true},
				{Warning, "warning msg", true},
				{Error, "error msg", true},
			},
		},
		{
			name:         "WARNING level filters INFO and DEBUG",
			currentLevel: LevelWarning,
			messages: []struct {
				fn     func(string, ...interface{})
				msg    string
				expect bool
			}{
				{Debug, "debug msg", false},
				{Info, "info msg", false},
				{Warning, "warning msg", true},
				{Error, "error msg", true},
			},
		},
		{
			name:         "ERROR level only shows errors",
			currentLevel: LevelError,
			messages: []struct {
				fn     func(string, ...interface{})
				msg    string
				expect bool
			}{
				{Debug, "debug msg", false},
				{Info, "info msg", false},
				{Warning, "warning msg", false},
				{Error, "error msg", true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetLevel(tt.currentLevel)

			for _, msg := range tt.messages {
				buf.Reset()
				msg.fn(msg.msg)
				output := buf.String()

				if msg.expect {
					assert.NotEmpty(t, output, "Expected output for: %s", msg.msg)
					assert.Contains(t, output, msg.msg)
				} else {
					assert.Empty(t, output, "Expected no output for: %s", msg.msg)
				}
			}
		})
	}
}

func TestReinitialize(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	// Set initial state
	os.Setenv("DEBUG", "false")
	os.Setenv("LOG_LEVEL", "INFO")
	Reinitialize()

	assert.False(t, IsDebugEnabled())
	assert.Equal(t, LevelInfo, GetLogLevel())

	// Change environment and reinitialize
	os.Setenv("DEBUG", "true")
	os.Setenv("LOG_LEVEL", "ERROR")
	Reinitialize()

	assert.True(t, IsDebugEnabled())
	assert.Equal(t, LevelError, GetLogLevel())
}

func TestLogOutput(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	// Create a buffer to capture log output
	var buf bytes.Buffer
	mu.Lock()
	originalLogger := stdoutLogger
	stdoutLogger = log.New(&buf, "", 0)
	isEnabled = true
	currentLevel = LevelDebug
	mu.Unlock()
	defer func() {
		mu.Lock()
		stdoutLogger = originalLogger
		mu.Unlock()
	}()

	// Test log output format
	Info("test message")
	output := buf.String()

	// Should contain all expected parts
	assert.Contains(t, output, "[INFO]")
	assert.Contains(t, output, "test message")
	assert.Regexp(t, `\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}\]`, output) // Timestamp
	assert.Regexp(t, `\[\S+:\d+\]`, output)                                     // File:line
}

func TestConcurrentLogging(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	// Create a buffer to capture log output (with its own mutex for thread safety)
	var buf bytes.Buffer
	var bufMu sync.Mutex
	safeLogger := log.New(&buf, "", 0)

	mu.Lock()
	originalLogger := stdoutLogger
	stdoutLogger = safeLogger
	isEnabled = true
	currentLevel = LevelDebug
	mu.Unlock()
	defer func() {
		mu.Lock()
		stdoutLogger = originalLogger
		mu.Unlock()
	}()

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			Debug("concurrent debug %d", id)
			Info("concurrent info %d", id)
			Warning("concurrent warning %d", id)
			Error("concurrent error %d", id)
		}(i)
	}

	// Wait for all goroutines
	wg.Wait()

	bufMu.Lock()
	output := buf.String()
	bufMu.Unlock()

	// Should have output from all goroutines
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Equal(t, 40, len(lines)) // 4 messages per goroutine * 10 goroutines
}

func TestSetEnabled(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	SetEnabled(true)
	assert.True(t, IsDebugEnabled())

	SetEnabled(false)
	assert.False(t, IsDebugEnabled())
}

func TestSetLevel(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	SetLevel(LevelDebug)
	assert.Equal(t, LevelDebug, GetLogLevel())

	SetLevel(LevelError)
	assert.Equal(t, LevelError, GetLogLevel())
}

func TestGetLogLevelName(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	SetLevel(LevelDebug)
	assert.Equal(t, "DEBUG", GetLogLevelName())

	SetLevel(LevelInfo)
	assert.Equal(t, "INFO", GetLogLevelName())

	SetLevel(LevelWarning)
	assert.Equal(t, "WARNING", GetLogLevelName())

	SetLevel(LevelError)
	assert.Equal(t, "ERROR", GetLogLevelName())
}

func TestGetStatus(t *testing.T) {
	restore := saveAndRestoreState(t)
	defer restore()

	SetEnabled(true)
	SetLevel(LevelWarning)

	status := GetStatus()
	assert.True(t, status.Enabled)
	assert.Equal(t, "WARNING", status.Level)
	assert.GreaterOrEqual(t, status.BufferCapacity, 0)
}

// Benchmark tests
func BenchmarkLog(b *testing.B) {
	// Save original values
	mu.Lock()
	originalEnabled := isEnabled
	originalLevel := currentLevel
	originalLogger := stdoutLogger
	stdoutLogger = log.New(bytes.NewBuffer(nil), "", 0)
	isEnabled = true
	currentLevel = LevelInfo
	mu.Unlock()
	defer func() {
		mu.Lock()
		isEnabled = originalEnabled
		currentLevel = originalLevel
		stdoutLogger = originalLogger
		mu.Unlock()
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("benchmark message %d", i)
	}
}

func BenchmarkLogDisabled(b *testing.B) {
	// Save original values
	mu.Lock()
	originalEnabled := isEnabled
	isEnabled = false
	mu.Unlock()
	defer func() {
		mu.Lock()
		isEnabled = originalEnabled
		mu.Unlock()
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("benchmark message %d", i)
	}
}

func BenchmarkLogFiltered(b *testing.B) {
	// Save original values
	mu.Lock()
	originalEnabled := isEnabled
	originalLevel := currentLevel
	isEnabled = true
	currentLevel = LevelError // Filter out INFO messages
	mu.Unlock()
	defer func() {
		mu.Lock()
		isEnabled = originalEnabled
		currentLevel = originalLevel
		mu.Unlock()
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("benchmark message %d", i)
	}
}
