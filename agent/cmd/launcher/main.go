// Command krakenhashes-launcher is the stable supervisor for the KrakenHashes
// agent. It spawns the agent as a child, forwards its environment/args/working
// directory so registration keeps working, and performs the agent binary
// auto-update (download -> verify -> backup -> swap -> restart) while the agent
// is stopped. The launcher is the binary installed as a service or run in a
// terminal; the agent never replaces itself.
//
// Usage:
//
//	krakenhashes-launcher [run] [--agent-binary PATH] [--health-timeout SECS] [agent flags...]
//	krakenhashes-launcher install   [--system] [--host HOST] [--claim CODE] [--config-dir DIR] [--data-dir DIR]
//	krakenhashes-launcher uninstall [--system] [--purge] [--config-dir DIR] [--data-dir DIR]
//	krakenhashes-launcher version
//
// install/uninstall default to a per-user service (no root): systemd --user on
// Linux, a per-user LaunchAgent on macOS, a logon Scheduled Task on Windows.
// --system installs/removes a root/system service instead (sudo / Administrator).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/launcher"
)

// Version is injected at build time via -ldflags.
var Version = "dev"

// main parses command-line arguments to select a subcommand and dispatches to the appropriate handler.
// It supports the subcommands `run` (default), `install`, `uninstall`, and `version`/`--version`/`-v`.
// `version` prints the launcher version and exits. `install` and `uninstall` invoke doInstall/doUninstall
// and will log a fatal error and exit on failure. The default `run` path calls doRun with a logger and
// the remaining arguments.
func main() {
	logger := log.New(os.Stderr, "[launcher] ", log.LstdFlags)

	args := os.Args[1:]
	sub := "run"
	if len(args) > 0 {
		switch args[0] {
		case "run", "install", "uninstall", "version", "--version", "-v":
			sub = args[0]
			args = args[1:]
		}
	}

	switch sub {
	case "version", "--version", "-v":
		fmt.Printf("krakenhashes-launcher %s\n", Version)
		return
	case "install":
		if err := doInstall(args); err != nil {
			logger.Fatalf("install failed: %v", err)
		}
		return
	case "uninstall":
		if err := doUninstall(args); err != nil {
			logger.Fatalf("uninstall failed: %v", err)
		}
		return
	default: // run
		doRun(logger, args)
	}
}

// launcherFlags are consumed by the launcher; everything else is forwarded to
// the agent verbatim.
type launcherFlags struct {
	agentBinary   string
	healthTimeout time.Duration
}

// parseLauncherFlags parses command-line tokens and extracts launcher-specific options,
// returning the populated launcherFlags and the remaining arguments to pass through to the agent.
//
// Recognized flags (left-to-right):
// - --agent-binary <path>, -agent-binary <path>, --agent-binary=<path>
//   -> sets agentBinary.
// - --health-timeout <seconds>, -health-timeout <seconds>, --health-timeout=<seconds>
//   -> parses seconds as an integer and sets healthTimeout accordingly; leaves the default if parsing fails.
// Any tokens not matching the above forms are appended, in order, to the returned passthrough slice.
func parseLauncherFlags(args []string) (launcherFlags, []string) {
	var lf launcherFlags
	var passthrough []string

	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--agent-binary" || a == "-agent-binary":
			if i+1 < len(args) {
				lf.agentBinary = args[i+1]
				i += 2
				continue
			}
		case strings.HasPrefix(a, "--agent-binary="):
			lf.agentBinary = strings.TrimPrefix(a, "--agent-binary=")
			i++
			continue
		case a == "--health-timeout" || a == "-health-timeout":
			if i+1 < len(args) {
				if secs, err := strconv.Atoi(args[i+1]); err == nil {
					lf.healthTimeout = time.Duration(secs) * time.Second
				}
				i += 2
				continue
			}
		case strings.HasPrefix(a, "--health-timeout="):
			if secs, err := strconv.Atoi(strings.TrimPrefix(a, "--health-timeout=")); err == nil {
				lf.healthTimeout = time.Duration(secs) * time.Second
			}
			i++
			continue
		}
		passthrough = append(passthrough, a)
		i++
	}
	return lf, passthrough
}

// doRun parses launcher-specific flags from args, resolves the agent and
// directory paths, constructs a launcher.Config, starts the launcher
// supervisor, and blocks until the service shuts down or an unrecoverable
// error occurs.
//
// It logs fatal and exits on unrecoverable startup or runtime errors. The
// provided logger is used for all startup and shutdown messages; args are the
// command-line tokens forwarded to the agent.
func doRun(logger *log.Logger, args []string) {
	lf, agentArgs := parseLauncherFlags(args)

	exe, err := os.Executable()
	if err != nil {
		logger.Fatalf("cannot resolve launcher path: %v", err)
	}
	exeDir := filepath.Dir(exe)

	configDir := resolveDir(agentArgs, "config-dir", "KH_CONFIG_DIR", filepath.Join(exeDir, "config"))
	dataDir := resolveDir(agentArgs, "data-dir", "KH_DATA_DIR", filepath.Join(exeDir, "data"))

	agentBinary := lf.agentBinary
	if agentBinary == "" {
		agentBinary = defaultAgentBinaryPath(exeDir)
	}

	// Export the resolved dirs so the agent child uses identical paths
	// (registration credentials + the update/ready files must agree).
	env := append(os.Environ(),
		"KH_CONFIG_DIR="+configDir,
		"KH_DATA_DIR="+dataDir,
	)

	cfg := launcher.Config{
		AgentBinary:      agentBinary,
		ConfigDir:        configDir,
		WorkDir:          exeDir,
		AgentArgs:        agentArgs,
		Env:              env,
		HealthTimeout:    lf.healthTimeout,
		BootstrapBaseURL: deriveBaseURL(agentArgs),
		Logger:           logger,
	}

	sup := launcher.New(cfg)
	mode := launcher.DetectRunMode()
	logger.Printf("krakenhashes-launcher %s starting (mode=%s, agent=%s, config=%s)", Version, mode, agentBinary, configDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Foreground/systemd/launchd: cancel on SIGINT/SIGTERM. Under the Windows
	// SCM the dispatcher handles Stop, so signal handling is a harmless no-op.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Println("received stop signal; shutting down")
		cancel()
	}()

	if err := launcher.RunService(ctx, sup); err != nil {
		logger.Fatalf("launcher exited with error: %v", err)
	}
}

// doInstall parses install-related flags from args, builds a launcher.InstallOptions
// (setting LauncherPath, Host, System, ClaimCode, ConfigDir, DataDir and any extra
// args), applies defaults for config/data to "<exeDir>/config" and "<exeDir>/data",
// and invokes launcher.Install returning any resulting error.
//
// Recognized flags:
//   --system / -system         (no value) mark system-wide installation
//   --host / -host <host>      host for the agent/service
//   --claim / -claim <code>    claim code to provision the agent
//   --config-dir / -config-dir <path>  custom configuration directory
//   --data-dir / -data-dir <path>      custom data directory
//
// Any unknown tokens are forwarded to the agent as ExtraArgs.
func doInstall(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve launcher path: %w", err)
	}
	exeDir := filepath.Dir(exe)

	opts := launcher.InstallOptions{
		LauncherPath: exe,
		Host:         "",
	}
	// Parse install flags; unknown flags pass through to the agent.
	i := 0
	for i < len(args) {
		a := args[i]
		val := func() string {
			if i+1 < len(args) {
				v := args[i+1]
				i += 2
				return v
			}
			i++
			return ""
		}
		switch {
		case a == "--system" || a == "-system":
			opts.System = true
			i++
		case a == "--host" || a == "-host":
			opts.Host = val()
		case a == "--claim" || a == "-claim":
			opts.ClaimCode = val()
		case a == "--config-dir" || a == "-config-dir":
			opts.ConfigDir = val()
		case a == "--data-dir" || a == "-data-dir":
			opts.DataDir = val()
		default:
			opts.ExtraArgs = append(opts.ExtraArgs, a)
			i++
		}
	}
	if opts.ConfigDir == "" {
		opts.ConfigDir = filepath.Join(exeDir, "config")
	}
	if opts.DataDir == "" {
		opts.DataDir = filepath.Join(exeDir, "data")
	}
	return launcher.Install(opts)
}

// doUninstall removes the launcher service. With --purge it also deletes the
// installed binaries and the config/data directories (default to the launcher's
// sibling config/ and data/, or the dirs given via --config-dir / --data-dir,
// doUninstall parses uninstall subcommand arguments, constructs a launcher.UninstallOptions value, and invokes launcher.Uninstall with the resolved options.
// 
// Supported flags in args are:
//   --system / -system       : mark uninstall as system-wide
//   --purge  / -purge        : remove service, binaries, config and data (including agent credentials)
//   --config-dir / -config-dir <path> : path to configuration directory
//   --data-dir   / -data-dir   <path> : path to data directory
//
// When --purge is specified and config/data paths are not provided, defaults are set to "<exeDir>/config" and "<exeDir>/data" where exeDir is the directory containing the launcher executable. The function returns any error produced by launcher.Uninstall.
func doUninstall(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve launcher path: %w", err)
	}
	exeDir := filepath.Dir(exe)

	opts := launcher.UninstallOptions{LauncherPath: exe}
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--system" || a == "-system":
			opts.System = true
			i++
		case a == "--purge" || a == "-purge":
			opts.Purge = true
			i++
		case a == "--config-dir" || a == "-config-dir":
			if i+1 < len(args) {
				opts.ConfigDir = args[i+1]
				i += 2
			} else {
				i++
			}
		case a == "--data-dir" || a == "-data-dir":
			if i+1 < len(args) {
				opts.DataDir = args[i+1]
				i += 2
			} else {
				i++
			}
		default:
			i++
		}
	}

	if opts.Purge {
		if opts.ConfigDir == "" {
			opts.ConfigDir = filepath.Join(exeDir, "config")
		}
		if opts.DataDir == "" {
			opts.DataDir = filepath.Join(exeDir, "data")
		}
		opts.AgentBinary = defaultAgentBinaryPath(exeDir)
		fmt.Println("--purge: removing service, binaries, config and data (including agent credentials)")
	}
	return launcher.Uninstall(opts)
}

// defaultAgentBinaryPath returns the expected path of the krakenhashes-agent binary located in exeDir.
// On Windows the filename includes the ".exe" extension.
func defaultAgentBinaryPath(exeDir string) string {
	name := "krakenhashes-agent"
	if os.PathSeparator == '\\' { // windows
		name += ".exe"
	}
	return filepath.Join(exeDir, name)
}

// resolveDir resolves a directory from (in order) the --flag in agentArgs, the
// resolveDir returns the absolute path selected from, in order: the flag value for flagName in agentArgs, the environment variable envName, or the provided default def.
func resolveDir(agentArgs []string, flagName, envName, def string) string {
	if v := peekFlag(agentArgs, flagName); v != "" {
		return abs(v)
	}
	if v := os.Getenv(envName); v != "" {
		return abs(v)
	}
	return abs(def)
}

// abs returns the absolute form of p; if the absolute path cannot be determined it returns p unchanged.
func abs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// peekFlag searches args for a flag named `name` in the forms `--name value`, `-name value`,
// `--name=value`, or `-name=value` and returns the associated value. If the flag is not present
// or no value is available, it returns the empty string.
func peekFlag(args []string, name string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--"+name || a == "-"+name {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(a, "--"+name+"=") {
			return strings.TrimPrefix(a, "--"+name+"=")
		}
		if strings.HasPrefix(a, "-"+name+"=") {
			return strings.TrimPrefix(a, "-"+name+"=")
		}
	}
	return ""
}

// deriveBaseURL builds the server base URL (scheme://host[:port]) for first-run
// deriveBaseURL determines the bootstrap base URL (scheme and host) for the agent
// from provided agent arguments or environment variables.
//
// It selects the host in this order: the `--host`/`-host` flag from agentArgs, then
// the `KH_HOST` environment variable. If `KH_PORT` is set and the chosen host is
// non-empty and has no existing port, `:<KH_PORT>` is appended. TLS is enabled
// when the `--tls` flag is present with a value other than `"false"` or `"0"`,
// otherwise when the `USE_TLS` environment variable equals `"true"` (case-insensitive)
// or `"1"`. The returned string is `<scheme>://<host>` where `scheme` is
// `"https"` when TLS is enabled and `"http"` otherwise. If no host can be
// determined, an empty string is returned.
func deriveBaseURL(agentArgs []string) string {
	host := peekFlag(agentArgs, "host")
	if host == "" {
		host = os.Getenv("KH_HOST")
		if port := os.Getenv("KH_PORT"); port != "" && host != "" && !strings.Contains(host, ":") {
			host = host + ":" + port
		}
	}
	if host == "" {
		return ""
	}
	useTLS := true
	if v := peekFlag(agentArgs, "tls"); v != "" {
		useTLS = v != "false" && v != "0"
	} else if v := os.Getenv("USE_TLS"); v != "" {
		useTLS = strings.EqualFold(v, "true") || v == "1"
	}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	return scheme + "://" + host
}
