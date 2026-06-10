package public

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/gorilla/mux"
)

// AgentDownloadHandler handles public agent download requests
type AgentDownloadHandler struct {
	binaryService *services.AgentBinaryService
}

// NewAgentDownloadHandler creates a new agent download handler
func NewAgentDownloadHandler(binaryService *services.AgentBinaryService) *AgentDownloadHandler {
	return &AgentDownloadHandler{
		binaryService: binaryService,
	}
}

// DownloadAgent serves an agent binary for download
func (h *AgentDownloadHandler) DownloadAgent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	os := vars["os"]
	arch := vars["arch"]

	debug.Info("Agent download requested for %s/%s", os, arch)

	// Get binary information
	binary, err := h.binaryService.GetBinary(os, arch)
	if err != nil {
		debug.Warning("Binary not found for %s/%s: %v", os, arch, err)
		http.Error(w, "Binary not found", http.StatusNotFound)
		return
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", binary.Size))

	// Set filename for download (simplified for consistency with documentation)
	filename := "krakenhashes-agent"
	if os == "windows" {
		filename += ".exe"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// Add checksum header for verification
	w.Header().Set("X-Checksum-SHA256", binary.Checksum)

	// Serve the file
	http.ServeFile(w, r, binary.Path)

	debug.Info("Served agent binary %s/%s (size: %d bytes)", os, arch, binary.Size)
}

// GetAgentVersion returns the current agent version
func (h *AgentDownloadHandler) GetAgentVersion(w http.ResponseWriter, r *http.Request) {
	version := h.binaryService.GetVersion()

	response := map[string]string{
		"version": version,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetChecksums returns checksums for all available binaries
func (h *AgentDownloadHandler) GetChecksums(w http.ResponseWriter, r *http.Request) {
	checksums := h.binaryService.GetChecksums()

	response := map[string]interface{}{
		"version":   h.binaryService.GetVersion(),
		"checksums": checksums,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DownloadLauncher serves a launcher (auto-updating supervisor) binary.
func (h *AgentDownloadHandler) DownloadLauncher(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	os := vars["os"]
	arch := vars["arch"]

	debug.Info("Launcher download requested for %s/%s", os, arch)

	binary, err := h.binaryService.GetLauncherBinary(os, arch)
	if err != nil {
		debug.Warning("Launcher binary not found for %s/%s: %v", os, arch, err)
		http.Error(w, "Launcher binary not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", binary.Size))

	filename := "krakenhashes-launcher"
	if os == "windows" {
		filename += ".exe"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Header().Set("X-Checksum-SHA256", binary.Checksum)

	http.ServeFile(w, r, binary.Path)

	debug.Info("Served launcher binary %s/%s (size: %d bytes)", os, arch, binary.Size)
}

// GetLauncherVersion returns the current launcher version.
func (h *AgentDownloadHandler) GetLauncherVersion(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"version": h.binaryService.GetLauncherVersion(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetLauncherPlatforms returns info about all available launcher binaries.
func (h *AgentDownloadHandler) GetLauncherPlatforms(w http.ResponseWriter, r *http.Request) {
	binaries := h.binaryService.GetAllLauncherBinaries()

	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		scheme = forwardedProto
	}
	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, host)

	var platforms []Platform
	for _, binary := range binaries {
		platform := Platform{
			OS:          binary.OS,
			Arch:        binary.Arch,
			DisplayName: binary.DisplayName,
			DownloadURL: binary.DownloadURL,
			FileName:    binary.FileName,
			FileSize:    binary.Size,
			Checksum:    binary.Checksum,
		}
		if r.URL.Query().Get("absolute") == "true" {
			platform.DownloadURL = baseURL + platform.DownloadURL
		}
		platforms = append(platforms, platform)
	}

	response := map[string]interface{}{
		"version":   h.binaryService.GetLauncherVersion(),
		"platforms": platforms,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Platform represents a downloadable platform
type Platform struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	DisplayName string `json:"display_name"`
	DownloadURL string `json:"download_url"`
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	Checksum    string `json:"checksum"`
}

// GetAvailablePlatforms returns information about all available platforms
func (h *AgentDownloadHandler) GetAvailablePlatforms(w http.ResponseWriter, r *http.Request) {
	binaries := h.binaryService.GetAllBinaries()

	// Get the base URL from the request
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}

	// Check for X-Forwarded headers (when behind proxy)
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		scheme = forwardedProto
	}

	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}

	baseURL := fmt.Sprintf("%s://%s", scheme, host)

	var platforms []Platform
	for _, binary := range binaries {
		platform := Platform{
			OS:          binary.OS,
			Arch:        binary.Arch,
			DisplayName: binary.DisplayName,
			DownloadURL: binary.DownloadURL, // Relative URL
			FileName:    binary.FileName,
			FileSize:    binary.Size,
			Checksum:    binary.Checksum,
		}

		// If requested, return full URLs
		if r.URL.Query().Get("absolute") == "true" {
			platform.DownloadURL = baseURL + platform.DownloadURL
		}

		platforms = append(platforms, platform)
	}

	response := map[string]interface{}{
		"version":   h.binaryService.GetVersion(),
		"platforms": platforms,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DownloadPage returns an HTML page with download links and instructions
func (h *AgentDownloadHandler) DownloadPage(w http.ResponseWriter, r *http.Request) {
	version := h.binaryService.GetVersion()
	binaries := h.binaryService.GetAllBinaries()

	// Group binaries by OS
	grouped := make(map[string][]services.BinaryInfo)
	for _, binary := range binaries {
		grouped[binary.OS] = append(grouped[binary.OS], binary)
	}

	// Generate HTML
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>KrakenHashes Agent Downloads</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 1200px; margin: 0 auto; padding: 20px; }
        h1 { color: #333; }
        .version { color: #666; margin-bottom: 30px; }
        .platforms { display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 40px; }
        .platform { flex: 1; min-width: 300px; border: 1px solid #ddd; border-radius: 8px; padding: 20px; }
        .platform h2 { margin-top: 0; color: #444; }
        .download-link { display: inline-block; padding: 8px 16px; background: #007bff; color: white; text-decoration: none; border-radius: 4px; margin: 5px 0; }
        .download-link:hover { background: #0056b3; }
        .size { color: #666; font-size: 0.9em; }
        .instructions { background: #f5f5f5; padding: 20px; border-radius: 8px; margin-top: 30px; }
        pre { background: #333; color: #fff; padding: 15px; border-radius: 4px; overflow-x: auto; }
        code { font-family: 'Courier New', monospace; }
    </style>
</head>
<body>
    <h1>KrakenHashes Agent Downloads</h1>
    <p class="version">Version: %s</p>

    <div class="platforms">`, version)

	// Add Linux section
	if linuxBinaries, ok := grouped["linux"]; ok {
		html += `
        <div class="platform">
            <h2>🐧 Linux</h2>`
		for _, binary := range linuxBinaries {
			html += fmt.Sprintf(`
            <div>
                <strong>%s:</strong><br>
                <a href="%s" class="download-link">Download</a>
                <span class="size">(%s)</span>
            </div>`, binary.DisplayName, binary.DownloadURL, formatBytes(binary.Size))
		}
		html += `</div>`
	}

	// Add Windows section
	if windowsBinaries, ok := grouped["windows"]; ok {
		html += `
        <div class="platform">
            <h2>🪟 Windows</h2>`
		for _, binary := range windowsBinaries {
			html += fmt.Sprintf(`
            <div>
                <strong>%s:</strong><br>
                <a href="%s" class="download-link">Download</a>
                <span class="size">(%s)</span>
            </div>`, binary.DisplayName, binary.DownloadURL, formatBytes(binary.Size))
		}
		html += `</div>`
	}

	// Add macOS section
	if darwinBinaries, ok := grouped["darwin"]; ok {
		html += `
        <div class="platform">
            <h2>🍎 macOS</h2>`
		for _, binary := range darwinBinaries {
			html += fmt.Sprintf(`
            <div>
                <strong>%s:</strong><br>
                <a href="%s" class="download-link">Download</a>
                <span class="size">(%s)</span>
            </div>`, binary.DisplayName, binary.DownloadURL, formatBytes(binary.Size))
		}
		html += `</div>`
	}

	// Get base URL for examples
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host

	html += fmt.Sprintf(`
    </div>

    <div class="instructions">
        <h2>Installation Instructions</h2>

        <h3>1. Download the agent for your platform</h3>
        <p>Click the download link above or use wget/curl:</p>

        <h4>Linux/macOS (64-bit):</h4>
        <pre><code>wget %s://%s/api/public/agent/download/linux/amd64 -O krakenhashes-agent
chmod +x krakenhashes-agent</code></pre>

        <h4>Windows (PowerShell):</h4>
        <pre><code>Invoke-WebRequest -Uri %s://%s/api/public/agent/download/windows/amd64 -OutFile krakenhashes-agent.exe</code></pre>

        <h3>2. Register with claim code</h3>
        <pre><code># Linux/macOS
./krakenhashes-agent --register --claim-code YOUR_CODE --server %s://%s

# Windows
krakenhashes-agent.exe --register --claim-code YOUR_CODE --server %s://%s</code></pre>

        <h3>3. Run the agent</h3>
        <pre><code># Linux/macOS
./krakenhashes-agent

# Windows
krakenhashes-agent.exe</code></pre>
    </div>
</body>
</html>`, scheme, host, scheme, host, scheme, host, scheme, host)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// formatBytes formats bytes to human readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// getExtension returns the file extension for the given OS
func getExtension(os string) string {
	if os == "windows" {
		return ".exe"
	}
	return ""
}

// ServeChecksum serves just the checksum for a specific binary
func (h *AgentDownloadHandler) ServeChecksum(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	os := vars["os"]
	arch := vars["arch"]

	binary, err := h.binaryService.GetBinary(os, arch)
	if err != nil {
		http.Error(w, "Binary not found", http.StatusNotFound)
		return
	}

	// Return as plain text for easy scripting
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(binary.Checksum))
}