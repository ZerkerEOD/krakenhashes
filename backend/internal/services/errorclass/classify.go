// Package errorclass classifies hashcat/agent failure messages into handling
// categories so the scheduler can react correctly: fail the job vs. fail over
// to another agent vs. retry. It is the single source of truth shared by the
// benchmark-failure and task-failure attribution paths.
//
// Classification is driven by (a) typed error codes the agent emits (preferred,
// unambiguous) and (b) substring matching on the raw hashcat/agent message
// (fallback for agents that don't yet emit a code, and for surfacing detail).
// Keep the typed-code constants in sync with the agent
// (agent/internal/jobs/hashcat_executor.go) and the websocket service.
package errorclass

import "strings"

// Category is the handling bucket a failure falls into.
type Category string

const (
	// CategoryHashlistFatal: hashcat rejected the hashlist for the configured
	// hash type (every line is wrong). No agent can run it — fail the job and
	// every sibling job on the same hashlist; recovery is to change the type.
	CategoryHashlistFatal Category = "hashlist_fatal"

	// CategoryJobConfig: the job's own configuration is invalid (bad mask,
	// charset, rule syntax, increment range). No agent can run it as configured
	// — fail this job with an actionable reason, but DON'T blame the agent.
	CategoryJobConfig Category = "job_config"

	// CategoryAgentPersistent: an agent-local fault that won't fix itself by
	// retrying here — missing/incompatible GPU driver or runtime, no device,
	// self-test/autotune failure. Fail over to other agents; surface on the
	// agent and (with corroboration) quarantine it. Other agents likely succeed.
	CategoryAgentPersistent Category = "agent_persistent"

	// CategoryAgentTransient: a transient/recoverable agent-local condition —
	// out of memory, GPU watchdog/thermal, disk full, network drop, cold-cache
	// benchmark timeout. Retry (same or other agent) before any cooldown; never
	// quarantine a healthy agent on a blip.
	CategoryAgentTransient Category = "agent_transient"

	// CategoryUnknown: unrecognized. Treated conservatively like a transient
	// fault (capped retries) so a novel error neither disables a good agent nor
	// loops forever (the per-tuple hard cap still applies).
	CategoryUnknown Category = "unknown"
)

// IsTransient reports whether the category should be retried rather than
// treated as agent-specific evidence (transient + unknown).
func (c Category) IsTransient() bool {
	return c == CategoryAgentTransient || c == CategoryUnknown
}

// hashlistFatalMarkers indicate the hashlist content is wrong for the chosen
// hash mode — hashcat rejects every line. Mirrors the agent's detection and the
// hashcat tokenizer error strings.
var hashlistFatalMarkers = []string{
	"benchmark_no_hashes_loaded",
	"hashlist_rejected",
	"no hashes loaded",
	"token length exception",
	"separator unmatched",
	"salt-value exception",
	"salt-length exception",
	"signature unmatched",
	"hash-encoding exception",
}

// jobConfigMarkers indicate the job's attack configuration is invalid.
var jobConfigMarkers = []string{
	"invalid mask",
	"invalid charset",
	"invalid rule",
	"syntax error in rule",
	"skipping invalid or unsupported rule",
	"mask length is too",
	"increment is not allowed",
}

// agentPersistentMarkers indicate an agent-local hardware/driver fault that
// retrying on the same agent won't fix.
var agentPersistentMarkers = []string{
	"benchmark_zero_speed",
	"agent_no_device",
	"agent_driver",
	"no devices found",
	"no devices left",
	"cl_device_not_found",
	"clgetdeviceids",
	"clgetplatformids",
	"cuinit",
	"no cuda-capable device",
	"no opencl",
	"compatible platform found",
	"self-test failed",
	"selftest failed",
	"autotune",
}

// agentTransientMarkers indicate a recoverable, often momentary condition.
var agentTransientMarkers = []string{
	"benchmark_timeout",
	"agent_oom",
	"agent_disk_full",
	"gpu_watchdog",
	"out of memory",
	"cl_out_of_resources",
	"cl_mem_object_allocation_failure",
	"cudaerrormemoryallocation",
	"no space left on device",
	"enospc",
	"watchdog",
	"temperature limit",
	"file already closed",
	"connection reset",
	"broken pipe",
	"context deadline exceeded",
	"i/o timeout",
}

// Classify maps a raw agent/hashcat error message to a handling category.
// Matching is case-insensitive substring; most-severe/most-specific buckets are
// checked first (hashlist > job-config > agent-persistent > agent-transient) so
// an unambiguous "no hashes loaded" is never mistaken for a transient blip.
// An empty message classifies as Unknown.
func Classify(message string) Category {
	if strings.TrimSpace(message) == "" {
		return CategoryUnknown
	}
	m := strings.ToLower(message)
	if containsAny(m, hashlistFatalMarkers) {
		return CategoryHashlistFatal
	}
	if containsAny(m, jobConfigMarkers) {
		return CategoryJobConfig
	}
	if containsAny(m, agentPersistentMarkers) {
		return CategoryAgentPersistent
	}
	if containsAny(m, agentTransientMarkers) {
		return CategoryAgentTransient
	}
	return CategoryUnknown
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
