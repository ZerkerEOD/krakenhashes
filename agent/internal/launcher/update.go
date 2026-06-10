package launcher

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/updateipc"
)

// applyUpdateSwap downloads, verifies, backs up, and swaps in the new agent
// binary described by instr. It returns (true, targetVersion) only when the
// swap succeeded; otherwise it clears the instruction and returns (false, "")
// so the launcher keeps running the existing binary (the backend's
// health-timeout + retry policy will drive any re-attempt).
func (s *Supervisor) applyUpdateSwap(instr updateipc.UpdateInstruction) (bool, string) {
	instr.Attempts++
	if instr.Attempts > maxLauncherAttempts {
		s.logf("update to %s exceeded %d attempts; giving up (keeping current binary)", instr.TargetVersion, maxLauncherAttempts)
		_ = updateipc.ClearInstruction(s.cfg.ConfigDir)
		return false, ""
	}
	// Persist the incremented attempt count so a crash mid-swap is counted.
	if err := updateipc.WriteInstruction(s.cfg.ConfigDir, instr); err != nil {
		s.logf("failed to persist update attempt count: %v", err)
	}

	url := resolveDownloadURL(instr)
	if url == "" {
		s.logf("update for %s has no resolvable download URL; aborting", instr.TargetVersion)
		_ = updateipc.ClearInstruction(s.cfg.ConfigDir)
		return false, ""
	}
	s.logf("downloading agent %s from %s", instr.TargetVersion, url)

	if err := s.download(url, s.newPath()); err != nil {
		s.logf("download failed: %v", err)
		_ = os.Remove(s.newPath())
		_ = updateipc.ClearInstruction(s.cfg.ConfigDir)
		return false, ""
	}
	if err := verifyChecksum(s.newPath(), instr.SHA256); err != nil {
		s.logf("checksum verification failed: %v", err)
		_ = os.Remove(s.newPath())
		_ = updateipc.ClearInstruction(s.cfg.ConfigDir)
		return false, ""
	}
	if !isWindows() {
		if err := os.Chmod(s.newPath(), 0o755); err != nil {
			s.logf("failed to chmod new binary: %v", err)
		}
	}

	// Back up the current binary, then atomically swap in the new one.
	if _, err := os.Stat(s.cfg.AgentBinary); err == nil {
		if err := copyFile(s.cfg.AgentBinary, s.backupPath()); err != nil {
			s.logf("failed to back up current agent binary: %v", err)
			_ = os.Remove(s.newPath())
			_ = updateipc.ClearInstruction(s.cfg.ConfigDir)
			return false, ""
		}
	}
	if err := replaceFile(s.cfg.AgentBinary, s.newPath()); err != nil {
		s.logf("failed to swap in new agent binary: %v", err)
		_ = updateipc.ClearInstruction(s.cfg.ConfigDir)
		return false, ""
	}

	_ = updateipc.ClearInstruction(s.cfg.ConfigDir)
	s.logf("agent binary updated to %s", instr.TargetVersion)
	return true, instr.TargetVersion
}

// bootstrapAgent fetches the agent binary on first run when it's missing. The
// download is integrity-protected by TLS (no separate checksum is available
// pre-install); for self-signed servers the config-dir ca.crt is trusted if
// present.
func (s *Supervisor) bootstrapAgent() error {
	if _, err := os.Stat(s.cfg.AgentBinary); err == nil {
		return nil
	}
	if s.cfg.BootstrapBaseURL == "" {
		return fmt.Errorf("agent binary %q missing and no server configured to fetch it", s.cfg.AgentBinary)
	}
	url := fmt.Sprintf("%s/api/public/agent/download/%s/%s",
		strings.TrimRight(s.cfg.BootstrapBaseURL, "/"), runtime.GOOS, runtime.GOARCH)
	s.logf("agent binary missing; bootstrapping from %s", url)

	if err := s.download(url, s.newPath()); err != nil {
		_ = os.Remove(s.newPath())
		return err
	}
	if !isWindows() {
		_ = os.Chmod(s.newPath(), 0o755)
	}
	if err := replaceFile(s.cfg.AgentBinary, s.newPath()); err != nil {
		return fmt.Errorf("install bootstrapped agent: %w", err)
	}
	s.logf("bootstrapped agent binary")
	return nil
}

// resolveDownloadURL builds an absolute download URL from the instruction.
// If Instr.DownloadURL is an absolute http(s) URL it is returned unchanged.
// Otherwise, when Instr.DownloadURL is empty the function constructs the path
// "/api/public/agent/download/{OS}/{Arch}" and joins it to Instr.ServerBaseURL.
// Returns an empty string when there is not enough information to produce a URL.
func resolveDownloadURL(instr updateipc.UpdateInstruction) string {
	path := instr.DownloadURL
	if path == "" {
		if instr.OS == "" || instr.Arch == "" {
			return ""
		}
		path = fmt.Sprintf("/api/public/agent/download/%s/%s", instr.OS, instr.Arch)
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	base := strings.TrimRight(instr.ServerBaseURL, "/")
	if base == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// download fetches url to dst. It verifies normally first (system roots + the
// config-dir ca.crt if present); if that fails with a TLS certificate error it
// retries once insecurely. This is safe for both callers: the first-run
// bootstrap is pre-registration TOFU with no checksum (mirroring the documented
// `curl -k` install), and update downloads are integrity-pinned by the SHA-256
// from the update instruction — delivered over the mTLS WebSocket — so an
// untrusted transport still can't substitute a binary. The retry is NOT gated on
// the absence of ca.crt: a stale/mismatched config-dir ca.crt would otherwise
// both fail verification AND suppress the retry, permanently breaking bootstrap.
func (s *Supervisor) download(url, dst string) error {
	err := s.downloadOnce(url, dst, false)
	if err != nil && isCertError(err) {
		s.logf("warning: server TLS certificate not trusted; retrying %s insecurely (self-signed)", url)
		err = s.downloadOnce(url, dst, true)
	}
	return err
}

func (s *Supervisor) downloadOnce(url, dst string, insecure bool) error {
	client := &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: s.tlsClientConfig(insecure),
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create staging file: %w", err)
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("download body: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close staging file: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("finalize staging file: %w", err)
	}
	return nil
}

// tlsClientConfig trusts the config-dir ca.crt (self-signed KrakenHashes CA) if
// present, falling back to the system roots. When insecure is true (self-signed
// first-run bootstrap only — see download), verification is skipped.
func (s *Supervisor) tlsClientConfig(insecure bool) *tls.Config {
	if insecure {
		// #nosec G402 -- intentional: first-run bootstrap of a self-signed
		// server before any CA is available, mirroring `curl -k`.
		return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}
	}
	caPath := filepath.Join(s.cfg.ConfigDir, "ca.crt")
	pem, err := os.ReadFile(caPath)
	if err != nil {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(pem)
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
}

// isCertError reports whether err looks like a TLS certificate verification
// isCertError reports whether err likely indicates a TLS or certificate validation error.
// It returns false for nil and otherwise inspects the error text for the substrings
// "x509", "certificate", or "tls:".
func isCertError(err error) bool {
	if err == nil {
		return false
	}
	e := err.Error()
	return strings.Contains(e, "x509") || strings.Contains(e, "certificate") || strings.Contains(e, "tls:")
}

// verifyChecksum computes the SHA-256 hash of the file at path and compares it case-insensitively to want.
// It returns nil when the computed hash matches want, or an error if want is empty, the file cannot be read, or the hashes do not match.
func verifyChecksum(path, want string) error {
	if want == "" {
		return fmt.Errorf("no expected checksum provided")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(want)) {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, want)
	}
	return nil
}

// replaceFile moves src onto dst atomically where possible. os.Rename overwrites
// on Unix; on Windows (and when rename onto an existing file fails) it removes
// replaceFile replaces the file at dst with src, attempting an atomic rename and retrying after removing dst if necessary.
// If the initial rename fails, it removes dst (unless it does not exist) and retries the rename.
// Returns any error encountered while installing src to dst.
func replaceFile(dst, src string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
}

// copyFile copies the file at src to dst, preserving the source file mode (including the executable bit) when possible.
// It writes the contents to a temporary file alongside dst and then atomically replaces dst with the temporary file.
// The temporary file is removed if an error occurs during copy or close.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	mode := os.FileMode(0o755)
	if fi, err := in.Stat(); err == nil {
		mode = fi.Mode()
	}

	tmp := dst + ".copytmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return replaceFile(dst, tmp)
}

// isWindows reports whether the current operating system is Windows.
func isWindows() bool { return runtime.GOOS == "windows" }

// normalizeVersion trims surrounding whitespace and removes a single leading 'v' from the version string.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}
