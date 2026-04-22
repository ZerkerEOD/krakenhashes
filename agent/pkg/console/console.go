package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	// Global writer for console output
	writer io.Writer = os.Stdout

	// Mutex for thread-safe console output
	mu sync.Mutex

	// Track if we're in a progress display mode
	inProgress bool

	// ANSI color codes
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"

	// ANSI cursor control
	clearLine = "\r\033[K"

	// Check if colors are supported
	colorsSupported = isTerminal()
)

// isTerminal checks if stdout is a terminal
func isTerminal() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

// SetWriter sets the output writer (useful for testing)
func SetWriter(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	writer = w
}

// color returns the colored string if colors are supported
func color(text, colorCode string) string {
	if !colorsSupported {
		return text
	}
	return colorCode + text + colorReset
}

// Print outputs a message to the console
func Print(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()

	if inProgress {
		fmt.Fprint(writer, clearLine)
	}

	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(writer, msg)
	inProgress = false
}

// Info outputs an info message with optional color
func Info(format string, args ...interface{}) {
	Print("["+color("INFO", colorBlue)+"] "+format, args...)
}

// Success outputs a success message in green
func Success(format string, args ...interface{}) {
	Print("["+color("OK", colorGreen)+"] "+format, args...)
}

// Warning outputs a warning message in yellow
func Warning(format string, args ...interface{}) {
	Print("["+color("WARN", colorYellow)+"] "+format, args...)
}

// Error outputs an error message in red
func Error(format string, args ...interface{}) {
	Print("["+color("ERROR", colorRed)+"] "+format, args...)
}

// Status outputs a status message in cyan
func Status(format string, args ...interface{}) {
	Print("["+color("*", colorCyan)+"] "+format, args...)
}

// Progress outputs a progress update that overwrites the current line
func Progress(format string, args ...interface{}) {
	mu.Lock()
	defer mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	if colorsSupported {
		fmt.Fprint(writer, clearLine+msg)
	} else {
		// For non-terminals, print with newline
		fmt.Fprintln(writer, msg)
	}
	inProgress = true
}

// ProgressBar generates a progress bar string
func ProgressBar(current, total int64, width int) string {
	if total == 0 {
		return strings.Repeat("─", width)
	}

	percent := float64(current) * 100.0 / float64(total)
	filled := int(float64(width) * float64(current) / float64(total))

	if filled > width {
		filled = width
	}

	bar := "["
	bar += strings.Repeat("=", filled)
	if filled < width {
		bar += ">"
		bar += strings.Repeat(" ", width-filled-1)
	}
	bar += "]"

	return fmt.Sprintf("%s %6.2f%%", bar, percent)
}

// FormatBytes formats bytes into human-readable format
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// FormatSpeed formats hash rate into human-readable format
func FormatSpeed(hashesPerSecond int64) string {
	const (
		KH = 1000
		MH = KH * 1000
		GH = MH * 1000
		TH = GH * 1000
	)

	switch {
	case hashesPerSecond >= TH:
		return fmt.Sprintf("%.2f TH/s", float64(hashesPerSecond)/TH)
	case hashesPerSecond >= GH:
		return fmt.Sprintf("%.2f GH/s", float64(hashesPerSecond)/GH)
	case hashesPerSecond >= MH:
		return fmt.Sprintf("%.2f MH/s", float64(hashesPerSecond)/MH)
	case hashesPerSecond >= KH:
		return fmt.Sprintf("%.2f KH/s", float64(hashesPerSecond)/KH)
	default:
		return fmt.Sprintf("%d H/s", hashesPerSecond)
	}
}

// FormatDuration formats a duration into human-readable format
func FormatDuration(seconds int) string {
	if seconds < 0 {
		return "calculating..."
	}

	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, secs)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, secs)
	}
	return fmt.Sprintf("%ds", secs)
}

// DownloadProgress represents download progress information
type DownloadProgress struct {
	FileName      string
	BytesReceived int64
	TotalBytes    int64
	BytesPerSec   int64
}

// FormatDownloadProgress formats download progress for display
func FormatDownloadProgress(p DownloadProgress) string {
	bar := ProgressBar(p.BytesReceived, p.TotalBytes, 30)

	received := FormatBytes(p.BytesReceived)
	total := FormatBytes(p.TotalBytes)
	speed := FormatBytes(p.BytesPerSec) + "/s"

	// Calculate ETA
	eta := ""
	if p.BytesPerSec > 0 && p.TotalBytes > p.BytesReceived {
		remaining := p.TotalBytes - p.BytesReceived
		seconds := int(remaining / p.BytesPerSec)
		eta = " | ETA: " + FormatDuration(seconds)
	}

	return fmt.Sprintf("%s | %s | %s/%s | %s%s",
		bar, p.FileName, received, total, speed, eta)
}

// MultiProgress manages multiple concurrent progress displays
type MultiProgress struct {
	mu        sync.Mutex
	downloads map[string]*DownloadProgress
	lastDraw  time.Time
	active    bool
}

// NewMultiProgress creates a new multi-progress manager
func NewMultiProgress() *MultiProgress {
	return &MultiProgress{
		downloads: make(map[string]*DownloadProgress),
	}
}

// Update updates or adds a download progress
func (mp *MultiProgress) Update(id string, progress DownloadProgress) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.downloads[id] = &progress
	mp.active = true

	// Throttle updates to avoid overwhelming the terminal
	if time.Since(mp.lastDraw) > 100*time.Millisecond {
		mp.draw()
		mp.lastDraw = time.Now()
	}
}

// Remove removes a download from tracking
func (mp *MultiProgress) Remove(id string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	delete(mp.downloads, id)
	if len(mp.downloads) == 0 {
		mp.active = false
		// Clear the progress display
		if colorsSupported {
			fmt.Fprint(writer, clearLine)
		}
	} else {
		mp.draw()
	}
}

// draw renders all active downloads
func (mp *MultiProgress) draw() {
	if !mp.active || len(mp.downloads) == 0 {
		return
	}

	// For terminal output, we'll show a summary of all downloads
	if colorsSupported {
		// Count and summarize
		totalFiles := len(mp.downloads)
		var totalReceived, totalSize int64
		var totalSpeed int64

		for _, p := range mp.downloads {
			totalReceived += p.BytesReceived
			totalSize += p.TotalBytes
			totalSpeed += p.BytesPerSec
		}

		// Create summary line
		summary := fmt.Sprintf("Downloading %d files | %s/%s | %s/s",
			totalFiles,
			FormatBytes(totalReceived),
			FormatBytes(totalSize),
			FormatBytes(totalSpeed))

		fmt.Fprint(writer, clearLine+summary)
		inProgress = true
	}
}

// Clear clears all progress displays
func (mp *MultiProgress) Clear() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.downloads = make(map[string]*DownloadProgress)
	mp.active = false

	if colorsSupported {
		fmt.Fprint(writer, clearLine)
	}
	inProgress = false
}

// TaskProgress represents task execution progress
type TaskProgress struct {
	TaskID            string
	ProgressPercent   float64
	HashRate          int64
	TimeRemaining     int
	Status            string
	KeyspaceProcessed int64
	TotalKeyspace     int64
	CrackedCount      int
}

// FormatTaskProgress formats task progress for display
func FormatTaskProgress(p TaskProgress) string {
	bar := ProgressBar(int64(p.ProgressPercent), 100, 30)
	speed := FormatSpeed(p.HashRate)
	eta := FormatDuration(p.TimeRemaining)

	// Format keyspace as "current/total"
	keyspace := fmt.Sprintf("%d/%d", p.KeyspaceProcessed, p.TotalKeyspace)

	return fmt.Sprintf("%s %6.2f%% | %s | %s | ETA: %s | Found: %d",
		bar, p.ProgressPercent, keyspace, speed, eta, p.CrackedCount)
}