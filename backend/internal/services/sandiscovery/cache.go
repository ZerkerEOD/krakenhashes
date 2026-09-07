// Package sandiscovery records addresses that reach this server but are not
// covered by its TLS certificate, so an administrator can see what to add.
//
// Nothing here ever changes a certificate. A recorded address is a suggestion
// that an administrator reviews and applies explicitly. That boundary is the
// security property of the whole package: the Host header is chosen by the
// caller, and an agent API key is transmitted in cleartext on the plain-HTTP
// port, so neither may be allowed to influence what a certificate asserts.
package sandiscovery

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	tlspkg "github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// Source identifies how an address was observed. Ordered loosely by how much
// weight the UI gives it.
type Source string

const (
	// SourceAgentTLSFailure is an authenticated agent reporting that it could
	// not verify this server's certificate for the address it dialled. The
	// strongest signal available: something concrete is broken right now.
	SourceAgentTLSFailure Source = "agent_tls_failure"
	// SourceHostHeader is the Host header of an inbound request. Useful but
	// caller-controlled, and the UI says so.
	SourceHostHeader Source = "host_header"
	// SourceTLSSNI is the server name from a TLS ClientHello. Absent for
	// bare-IP clients, which never send SNI.
	SourceTLSSNI Source = "tls_sni"
	// SourceManual is an address typed in by an administrator.
	SourceManual Source = "manual"
)

// flushInterval is how often aggregated observations are written to the database.
const flushInterval = 30 * time.Second

// hotEntryLimit bounds how many distinct addresses are buffered between flushes,
// so a burst of spoofed Host headers cannot grow memory without limit. Existing
// entries keep counting past the cap; only new addresses are dropped.
const hotEntryLimit = 512

// Observation is one sighting, before aggregation.
type Observation struct {
	Address   string
	Source    Source
	Port      int
	AgentID   int // 0 when unknown
	AgentName string
	UserAgent string
}

type entry struct {
	address   string
	kind      string
	source    Source
	hits      int64
	lastSeen  time.Time
	agentID   int
	agentName string
	userAgent string
	port      int
}

// Cache aggregates observations in memory and flushes them periodically, so the
// request path never performs a database write.
type Cache struct {
	repo *repository.TLSSANCandidateRepository

	mu  sync.Mutex
	hot map[string]*entry

	// ignore holds the addresses already covered by the certificate plus the
	// structurally uninteresting ones. Read on every observation and swapped
	// wholesale after a reissue, so the overwhelmingly common case -- a request
	// arriving on an address the certificate already names -- costs one atomic
	// load and one map lookup, with no mutex and no allocation.
	ignore atomic.Pointer[map[string]struct{}]
}

func NewCache(repo *repository.TLSSANCandidateRepository) *Cache {
	c := &Cache{repo: repo, hot: make(map[string]*entry)}
	empty := make(map[string]struct{})
	c.ignore.Store(&empty)
	return c
}

// RefreshIgnoreSet republishes the set of addresses that should never be
// recorded. Called at startup and after every certificate reissue.
func (c *Cache) RefreshIgnoreSet(dnsNames []string, ipAddresses []string) {
	set := make(map[string]struct{}, len(dnsNames)+len(ipAddresses)+8)

	// Never actionable regardless of what the certificate covers.
	for _, v := range []string{"localhost", "127.0.0.1", "::1", "0.0.0.0", "::"} {
		set[v] = struct{}{}
	}
	for _, n := range dnsNames {
		set[strings.ToLower(n)] = struct{}{}
	}
	for _, ip := range ipAddresses {
		set[ip] = struct{}{}
	}

	// The container's own addresses. Under bridge networking nginx proxies to
	// the backend over the 172.x bridge address, which would otherwise be
	// recorded constantly and is unreachable from any agent.
	for _, ip := range localAddresses() {
		set[ip] = struct{}{}
	}

	c.ignore.Store(&set)
}

// Observe records a sighting. Never blocks on I/O and never returns an error:
// discovery is best-effort telemetry and must not be able to fail a request.
func (c *Cache) Observe(o Observation) {
	address, kind, ok := normalize(o.Address)
	if !ok {
		return
	}

	if ignore := c.ignore.Load(); ignore != nil {
		if _, skip := (*ignore)[address]; skip {
			return
		}
	}

	key := kind + "|" + address + "|" + string(o.Source)

	c.mu.Lock()
	defer c.mu.Unlock()

	if e, exists := c.hot[key]; exists {
		e.hits++
		e.lastSeen = time.Now()
		if o.AgentID != 0 {
			e.agentID, e.agentName = o.AgentID, o.AgentName
		}
		if o.UserAgent != "" {
			e.userAgent = o.UserAgent
		}
		if o.Port != 0 {
			e.port = o.Port
		}
		return
	}

	if len(c.hot) >= hotEntryLimit {
		// Dropped silently: this is the spoofed-Host-header burst case, and
		// logging per drop would just move the amplification into the log.
		return
	}

	c.hot[key] = &entry{
		address: address, kind: kind, source: o.Source,
		hits: 1, lastSeen: time.Now(),
		agentID: o.AgentID, agentName: o.AgentName,
		userAgent: o.UserAgent, port: o.Port,
	}
}

// Flush writes the buffered observations and prunes old rows.
func (c *Cache) Flush(ctx context.Context) error {
	c.mu.Lock()
	buffered := c.hot
	c.hot = make(map[string]*entry)
	c.mu.Unlock()

	if len(buffered) == 0 {
		return c.repo.Prune(ctx)
	}

	observations := make([]repository.TLSSANCandidateObservation, 0, len(buffered))
	for _, e := range buffered {
		obs := repository.TLSSANCandidateObservation{
			Address:       e.address,
			Kind:          e.kind,
			Source:        string(e.source),
			Hits:          e.hits,
			LastSeenAt:    e.lastSeen,
			LastAgentName: e.agentName,
			LastUserAgent: e.userAgent,
			LastPort:      e.port,
		}
		if e.agentID != 0 {
			id := e.agentID
			obs.LastAgentID = &id
		}
		observations = append(observations, obs)
	}

	if err := c.repo.Upsert(ctx, observations); err != nil {
		return err
	}
	return c.repo.Prune(ctx)
}

// Run flushes on a ticker until ctx is cancelled.
func (c *Cache) Run(ctx context.Context) {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush so observations recorded just before shutdown are not
			// lost -- a restart is exactly when an admin is investigating this.
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := c.Flush(flushCtx); err != nil {
				debug.Warning("Final SAN candidate flush failed: %v", err)
			}
			cancel()
			return
		case <-ticker.C:
			if err := c.Flush(ctx); err != nil {
				debug.Warning("Failed to flush SAN candidates: %v", err)
			}
		}
	}
}

// normalize validates and canonicalises an observed value, returning false for
// anything that must never be recorded.
//
// The private-address policy is applied here as well as at the API boundary: a
// public address should never even be *suggested*, so it is dropped at ingest
// rather than shown and then refused.
func normalize(raw string) (address, kind string, ok bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", false
	}

	// Strip a port if one came along with a Host header.
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")

	if net.ParseIP(value) != nil {
		ip, err := tlspkg.ValidateInternalIP(value)
		if err != nil {
			return "", "", false
		}
		return ip.String(), "ip", true
	}

	name, err := tlspkg.ValidateDNSName(value)
	if err != nil {
		return "", "", false
	}
	return name, "dns", true
}

// localAddresses returns this process's own interface addresses.
//
// Used only to suppress self-observations. It is deliberately NOT a discovery
// source: on a Docker bridge network these are the 172.x container addresses, and
// the host's LAN or Tailscale address -- the one agents actually dial -- never
// appears here at all.
func localAddresses() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			out = append(out, ipnet.IP.String())
		}
	}
	return out
}
