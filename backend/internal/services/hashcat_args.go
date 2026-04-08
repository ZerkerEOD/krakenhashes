package services

import (
	"fmt"
	"strings"
)

// maxAdditionalArgsLength is the maximum allowed length for additional_args
const maxAdditionalArgsLength = 500

// blockedFlags contains hashcat flags that must not be user-specified.
// Organized by category for maintainability.
var blockedFlags = map[string]string{
	// System-controlled: set by job config
	"-m":                "hash type is set by job configuration",
	"--hash-type":       "hash type is set by job configuration",
	"-a":                "attack mode is set by job configuration",
	"--attack-mode":     "attack mode is set by job configuration",
	"-r":                "rules are set by job configuration",
	"--rules-file":      "rules are set by job configuration",
	"-1":                "custom charsets are set by job configuration",
	"--custom-charset1": "custom charsets are set by job configuration",
	"-2":                "custom charsets are set by job configuration",
	"--custom-charset2": "custom charsets are set by job configuration",
	"-3":                "custom charsets are set by job configuration",
	"--custom-charset3": "custom charsets are set by job configuration",
	"-4":                "custom charsets are set by job configuration",
	"--custom-charset4": "custom charsets are set by job configuration",
	"-5":                "custom charsets are set by job configuration",
	"--custom-charset5": "custom charsets are set by job configuration",
	"-6":                "custom charsets are set by job configuration",
	"--custom-charset6": "custom charsets are set by job configuration",
	"-7":                "custom charsets are set by job configuration",
	"--custom-charset7": "custom charsets are set by job configuration",
	"-8":                "custom charsets are set by job configuration",
	"--custom-charset8": "custom charsets are set by job configuration",
	"-i":                "increment mode is set by job configuration",
	"--increment":       "increment mode is set by job configuration",
	"-ii":               "increment mode is set by job configuration",
	"--increment-inverse": "increment mode is set by job configuration",
	"--increment-min":   "increment settings are set by job configuration",
	"--increment-max":   "increment settings are set by job configuration",

	// System-controlled: set by chunking system
	"-s":                "skip is controlled by the chunking system",
	"--skip":            "skip is controlled by the chunking system",
	"-l":                "limit is controlled by the chunking system",
	"--limit":           "limit is controlled by the chunking system",
	"--keyspace":        "keyspace is used internally",
	"--total-candidates": "total-candidates is used internally",

	// System-controlled: set by agent device management
	"-d":                "backend devices are managed by agent configuration",
	"--backend-devices": "backend devices are managed by agent configuration",
	"-D":                "device types are managed by agent configuration",
	"--opencl-device-types": "device types are managed by agent configuration",

	// System-controlled: set by agent output/session management
	"-o":              "outfile is controlled by the agent",
	"--outfile":       "outfile is controlled by the agent",
	"--outfile-format": "outfile format is controlled by the agent",
	"--outfile-json":  "outfile format is controlled by the agent",
	"--potfile-disable": "potfile is controlled by the agent",
	"--potfile-path":   "potfile path is controlled by the agent",
	"--restore-disable": "restore is controlled by the agent",
	"--restore-file-path": "restore file path is controlled by the agent",
	"--restore":       "restore is controlled by the agent",
	"--session":       "session is controlled by the agent",
	"--status":        "status output is controlled by the agent",
	"--status-json":   "status JSON output is controlled by the agent",
	"--status-timer":  "status timer is controlled by the agent",
	"--quiet":         "quiet mode is controlled by the agent",

	// File path injection risk
	"--debug-file":             "debug file path is not allowed for security reasons",
	"--induction-dir":          "induction directory is not allowed for security reasons",
	"--outfile-check-dir":      "outfile check directory is not allowed for security reasons",
	"--markov-hcstat2":         "custom hcstat2 files are not allowed for security reasons",
	"--keyboard-layout-mapping": "keyboard layout files are not allowed for security reasons",
	"--truecrypt-keyfiles":     "keyfile paths are not allowed for security reasons",
	"--veracrypt-keyfiles":     "keyfile paths are not allowed for security reasons",

	// Would break agent behavior (changes hashcat mode)
	"-V":               "version flag would prevent cracking",
	"--version":        "version flag would prevent cracking",
	"-h":               "help flag would prevent cracking",
	"--help":           "help flag would prevent cracking",
	"-b":               "benchmark mode would prevent cracking",
	"--benchmark":      "benchmark mode would prevent cracking",
	"--benchmark-all":  "benchmark mode would prevent cracking",
	"--benchmark-min":  "benchmark mode would prevent cracking",
	"--benchmark-max":  "benchmark mode would prevent cracking",
	"--speed-only":     "speed-only mode would prevent cracking",
	"--progress-only":  "progress-only mode would prevent cracking",
	"--show":           "show mode would prevent cracking",
	"--left":           "left mode would prevent cracking",
	"-H":               "hash-info mode would prevent cracking",
	"--hash-info":      "hash-info mode would prevent cracking",
	"--example-hashes": "example-hashes mode would prevent cracking",
	"--identify":       "identify mode would prevent cracking",
	"--stdout":         "stdout mode would prevent cracking",
	"--brain-server":   "brain server mode would break agent communication",
}

// shellMetacharacters contains characters that could be used for shell injection
var shellMetacharacters = []string{";", "|", "&", "`", "$", "(", ")", "{", "}", "<", ">", "\\", "\n", "\r"}

// ValidateAdditionalArgs validates user-provided additional hashcat arguments.
// Returns an error if the args contain blocked flags, shell metacharacters, or exceed length limits.
func ValidateAdditionalArgs(args string) error {
	if args == "" {
		return nil
	}

	// Check length
	if len(args) > maxAdditionalArgsLength {
		return fmt.Errorf("additional arguments exceed maximum length of %d characters", maxAdditionalArgsLength)
	}

	// Check for shell metacharacters
	for _, char := range shellMetacharacters {
		if strings.Contains(args, char) {
			return fmt.Errorf("additional arguments contain disallowed character: %q", char)
		}
	}

	// Parse and check each flag
	tokens := strings.Fields(args)
	for _, token := range tokens {
		// Extract the flag name (handle --flag=value syntax)
		flagName := token
		if idx := strings.Index(token, "="); idx > 0 && strings.HasPrefix(token, "-") {
			flagName = token[:idx]
		}

		// Check against blocklist
		if reason, blocked := blockedFlags[flagName]; blocked {
			return fmt.Errorf("flag %q is not allowed: %s", flagName, reason)
		}
	}

	return nil
}
