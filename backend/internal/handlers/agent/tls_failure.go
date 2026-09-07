package agent

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/sandiscovery"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// TLSFailureRequest is what an agent reports when it cannot verify this server's
// certificate.
type TLSFailureRequest struct {
	AttemptedHost string   `json:"attempted_host"`
	AttemptedPort int      `json:"attempted_port"`
	FailureKind   string   `json:"failure_kind"`
	CertSANs      []string `json:"cert_sans,omitempty"`
	CertSubject   string   `json:"cert_subject,omitempty"`
	Error         string   `json:"error,omitempty"`
	AgentVersion  string   `json:"agent_version,omitempty"`
}

// TLSFailureResponse acknowledges the report. It deliberately carries no
// credentials and nothing an unauthenticated caller could not already infer.
type TLSFailureResponse struct {
	Recorded         bool   `json:"recorded"`
	CandidateAddress string `json:"candidate_address,omitempty"`
}

// tlsFailureCooldown is the minimum gap between accepted reports from one agent.
//
// In practice this is nearly unreachable, because the agent already reports at
// most once per distinct failure rather than once per retry. It exists so a
// misbehaving or downgraded agent cannot turn a reconnect loop into a write loop.
const tlsFailureCooldown = 5 * time.Minute

// unauthenticatedBurst bounds reports from callers that have not yet presented a
// valid key, so a flood is rejected before it costs a database lookup.
const unauthenticatedBurst = 30

// TLSFailureHandler records agent-reported TLS verification failures.
//
// SECURITY: this endpoint is served over PLAIN HTTP on the agent bootstrap port,
// because an agent that cannot verify the server certificate has no working TLS
// channel to report over -- that is the entire point of it. Two rules keep the
// exposure from widening:
//
//  1. It never mints or returns a credential of any kind.
//  2. It never causes a certificate to be issued. A valid API key lets an agent
//     SUGGEST a name; an administrator must review and apply it. Compromising one
//     agent key therefore buys an attacker a single row in a review list, not a
//     certificate for a name of their choosing.
//
// The pre-existing /api/agent/renew-certificates endpoint on this same port
// already accepts a cleartext API key and returns a client certificate together
// with its private key, so this is strictly less sensitive than what is there
// today -- but the port must still be reachable only by agents.
type TLSFailureHandler struct {
	agentRepo *repository.AgentRepository
	cache     *sandiscovery.Cache

	mu           sync.Mutex
	lastAccepted map[int]time.Time
	anonWindow   time.Time
	anonCount    int
}

func NewTLSFailureHandler(agentRepo *repository.AgentRepository, cache *sandiscovery.Cache) *TLSFailureHandler {
	return &TLSFailureHandler{
		agentRepo:    agentRepo,
		cache:        cache,
		lastAccepted: make(map[int]time.Time),
	}
}

// HandleTLSFailure records a report from an agent that could not verify this
// server's certificate.
func (h *TLSFailureHandler) HandleTLSFailure(w http.ResponseWriter, r *http.Request) {
	if !h.allowUnauthenticated() {
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}

	apiKey := r.Header.Get("X-API-Key")
	agentIDStr := r.Header.Get("X-Agent-ID")
	if apiKey == "" || agentIDStr == "" {
		// Uniform 401 for every authentication failure below. Distinguishing
		// "no such agent" from "wrong key" would turn this into an agent-ID
		// oracle for an unauthenticated caller.
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	agentID, err := strconv.Atoi(agentIDStr)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	agent, err := h.agentRepo.GetByID(r.Context(), agentID)
	if err != nil {
		debug.Debug("TLS failure report for unknown agent %d", agentID)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if subtle.ConstantTimeCompare([]byte(agent.APIKey.String), []byte(apiKey)) != 1 {
		debug.Warning("TLS failure report with an invalid API key for agent %d", agentID)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req TLSFailureRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if retryAfter, ok := h.rateLimited(agent.ID); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		http.Error(w, "Report already received recently", http.StatusTooManyRequests)
		return
	}

	host := hostOnly(req.AttemptedHost)
	if host == "" {
		http.Error(w, "attempted_host is required", http.StatusBadRequest)
		return
	}

	debug.Warning("Agent %d (%s) cannot verify this server's certificate for %s: %s [%s]",
		agent.ID, agent.Name, req.AttemptedHost, req.Error, req.FailureKind)
	debug.Warning("Add %s in Admin -> Settings -> Server Certificate and reissue, or the agent cannot connect.", host)

	// A public address is accepted with 200 and then dropped by the cache's
	// normalisation. Returning 400 here would let a caller map the private-range
	// allowlist by probing.
	h.cache.Observe(sandiscovery.Observation{
		Address:   host,
		Source:    sandiscovery.SourceAgentTLSFailure,
		Port:      req.AttemptedPort,
		AgentID:   agent.ID,
		AgentName: agent.Name,
		UserAgent: fmt.Sprintf("krakenhashes-agent/%s", req.AgentVersion),
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(TLSFailureResponse{
		Recorded:         true,
		CandidateAddress: host,
	}); err != nil {
		debug.Error("Failed to encode TLS failure response: %v", err)
	}
}

// rateLimited reports whether this agent may record another report now.
func (h *TLSFailureHandler) rateLimited(agentID int) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	if last, seen := h.lastAccepted[agentID]; seen {
		if elapsed := now.Sub(last); elapsed < tlsFailureCooldown {
			return tlsFailureCooldown - elapsed, false
		}
	}

	// Bounded without a background goroutine: prune on write once the map grows
	// past a size no real deployment reaches.
	if len(h.lastAccepted) > 1000 {
		for id, t := range h.lastAccepted {
			if now.Sub(t) > time.Hour {
				delete(h.lastAccepted, id)
			}
		}
	}

	h.lastAccepted[agentID] = now
	return 0, true
}

// allowUnauthenticated applies a coarse per-minute cap before any authentication
// work is done.
func (h *TLSFailureHandler) allowUnauthenticated() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	if now.Sub(h.anonWindow) > time.Minute {
		h.anonWindow = now
		h.anonCount = 0
	}
	h.anonCount++
	return h.anonCount <= unauthenticatedBurst
}

// hostOnly strips a port and IPv6 brackets from a reported address.
func hostOnly(value string) string {
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return value
}
