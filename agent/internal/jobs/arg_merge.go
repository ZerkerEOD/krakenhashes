package jobs

import (
	"strings"
)

// shortToLong maps common hashcat short flags to their long-form equivalents.
// This enables deduplication when the same flag appears in different forms.
// Only commonly-used flags are included; unknown flags pass through without dedup.
var shortToLong = map[string]string{
	"-w": "--workload-profile",
	"-O": "--optimized-kernel-enable",
	"-M": "--multiply-accel-disable",
	"-T": "--kernel-threads",
	"-t": "--markov-threshold",
	"-n": "--kernel-accel",
	"-u": "--kernel-loops",
	"-S": "--slow-candidates",
	"-c": "--segment-size",
	"-p": "--separator",
}

// booleanFlags are hashcat flags that take no value (boolean switches).
// This is used during parsing to know not to consume the next token as a value.
var booleanFlags = map[string]bool{
	"--force":                      true,
	"--hex-charset":                true,
	"--hex-salt":                   true,
	"--hex-wordlist":               true,
	"--keep-guessing":              true,
	"--self-test-disable":          true,
	"--loopback":                   true,
	"--slow-candidates":            true,
	"--deprecated-check-disable":   true,
	"--markov-disable":             true,
	"--markov-classic":             true,
	"--markov-inverse":             true,
	"--hwmon-disable":              true,
	"--logfile-disable":            true,
	"--wordlist-autohex-disable":   true,
	"--outfile-autohex-disable":    true,
	"--optimized-kernel-enable":    true,
	"--multiply-accel-disable":     true,
	"--username":                   true,
	"--dynamic-x":                  true,
	"--remove":                     true,
	"--backend-ignore-cuda":        true,
	"--backend-ignore-hip":         true,
	"--backend-ignore-metal":       true,
	"--backend-ignore-opencl":      true,
	"--color-cracked":              true,
	"-O":                           true,
	"-M":                           true,
	"-S":                           true,
}

// parsedArg represents a parsed hashcat argument with its canonical key and optional value.
type parsedArg struct {
	canonical string // Normalized long-form key (e.g., "--workload-profile")
	original  string // Original flag as written (e.g., "-w")
	value     string // Value if any (e.g., "4"), empty for boolean flags
	hasValue  bool   // Whether this arg has a value
	usesEqual bool   // Whether the original used --flag=value syntax
}

// canonicalize normalizes a flag to its long form if a mapping exists.
func canonicalize(flag string) string {
	if long, ok := shortToLong[flag]; ok {
		return long
	}
	return flag
}

// isBooleanFlag checks if a flag is a known boolean flag (no value).
func isBooleanFlag(flag string) bool {
	// Check the flag directly
	if booleanFlags[flag] {
		return true
	}
	// Also check the canonical form
	canonical := canonicalize(flag)
	return booleanFlags[canonical]
}

// parseArgs splits an argument string into structured parsedArg entries.
func parseArgs(args string) []parsedArg {
	if args == "" {
		return nil
	}

	tokens := strings.Fields(args)
	var result []parsedArg

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]

		if !strings.HasPrefix(token, "-") {
			// Not a flag — treat as a standalone value (shouldn't happen in practice
			// for extra params, but preserve it)
			result = append(result, parsedArg{
				canonical: token,
				original:  token,
			})
			continue
		}

		// Check for --flag=value syntax
		if strings.Contains(token, "=") && strings.HasPrefix(token, "--") {
			parts := strings.SplitN(token, "=", 2)
			flag := parts[0]
			value := parts[1]
			result = append(result, parsedArg{
				canonical: canonicalize(flag),
				original:  flag,
				value:     value,
				hasValue:  true,
				usesEqual: true,
			})
			continue
		}

		// Boolean flag (no value)
		if isBooleanFlag(token) {
			result = append(result, parsedArg{
				canonical: canonicalize(token),
				original:  token,
			})
			continue
		}

		// Flag with space-separated value: -w 4 or --workload-profile 4
		if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
			result = append(result, parsedArg{
				canonical: canonicalize(token),
				original:  token,
				value:     tokens[i+1],
				hasValue:  true,
			})
			i++ // Skip the value token
			continue
		}

		// Unknown flag without a value — could be an unknown boolean flag
		result = append(result, parsedArg{
			canonical: canonicalize(token),
			original:  token,
		})
	}

	return result
}

// MergeHashcatArgs merges job-level args with agent-level args.
// Agent args take priority over job args for duplicate flags.
// Returns the merged, deduplicated argument string.
//
// Priority: agentArgs > jobArgs
// Both short form (-w 4) and long form (--workload-profile=4) are handled.
func MergeHashcatArgs(jobArgs, agentArgs string) string {
	if jobArgs == "" && agentArgs == "" {
		return ""
	}
	if jobArgs == "" {
		return agentArgs
	}
	if agentArgs == "" {
		return jobArgs
	}

	jobParsed := parseArgs(jobArgs)
	agentParsed := parseArgs(agentArgs)

	// Build ordered result: start with job args, then override with agent args
	// Use a map to track which canonical keys are present
	seen := make(map[string]int) // canonical key -> index in result
	var result []parsedArg

	// Add all job args first
	for _, arg := range jobParsed {
		idx := len(result)
		seen[arg.canonical] = idx
		result = append(result, arg)
	}

	// Overlay agent args — agent wins on conflicts
	for _, arg := range agentParsed {
		if idx, exists := seen[arg.canonical]; exists {
			// Replace the job arg with the agent arg (agent wins)
			result[idx] = arg
		} else {
			// New arg from agent, append
			idx := len(result)
			seen[arg.canonical] = idx
			result = append(result, arg)
		}
	}

	// Reconstruct the argument string, preserving original syntax
	var parts []string
	for _, arg := range result {
		if arg.hasValue {
			if arg.usesEqual {
				parts = append(parts, arg.original+"="+arg.value)
			} else {
				parts = append(parts, arg.original, arg.value)
			}
		} else {
			parts = append(parts, arg.original)
		}
	}

	return strings.Join(parts, " ")
}
