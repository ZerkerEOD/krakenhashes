package certs

import (
	"context"
	"crypto/x509"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/sandiscovery"
	tlspkg "github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/env"
)

// Service reconciles the certificate on disk with the SAN list an administrator
// has configured.
type Service struct {
	provider  tlspkg.Provider
	appConfig *config.Config
	settings  *repository.SystemSettingsRepository

	// selfSigned is non-nil only in self-signed mode. The provided and certbot
	// providers take their names from an operator-supplied certificate or from
	// the ACME account, so there is nothing here to manage.
	selfSigned *tlspkg.SelfSignedProvider

	// discovery is optional; when set, its ignore set is refreshed after every
	// reissue so newly covered addresses stop being suggested.
	discovery  *sandiscovery.Cache
	candidates *repository.TLSSANCandidateRepository

	// notifyAgents pushes a cert_refresh to every connected agent. Optional and
	// injected as a function so this package does not depend on the websocket
	// handler, which already depends on a great deal else.
	notifyAgents func(requestID, reason, caFingerprint string) map[int]error
}

// AttachAgentNotifier wires the cert_refresh broadcast in.
func (s *Service) AttachAgentNotifier(fn func(requestID, reason, caFingerprint string) map[int]error) {
	s.notifyAgents = fn
}

// AttachDiscovery wires the candidate-discovery cache in.
//
// Separate from New because the cache needs a repository, which needs a database
// connection that does not exist when the TLS provider is first built.
func (s *Service) AttachDiscovery(cache *sandiscovery.Cache, candidates *repository.TLSSANCandidateRepository) {
	s.discovery = cache
	s.candidates = candidates
}

// Candidates exposes the candidate repository to the admin handler, which reads
// and dismisses rows directly.
func (s *Service) Candidates() *repository.TLSSANCandidateRepository {
	return s.candidates
}

// RefreshDiscoveryIgnoreSet republishes the addresses the certificate already
// covers, so requests arriving on them are not recorded as candidates.
func (s *Service) RefreshDiscoveryIgnoreSet() {
	if s.discovery == nil || !s.Managed() {
		return
	}
	cert := s.selfSigned.ServerCertificate()
	if cert == nil {
		return
	}
	ips := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}
	s.discovery.RefreshIgnoreSet(cert.DNSNames, ips)
}

// ObserveSNI records a server name seen in a TLS ClientHello.
//
// A cheap secondary signal with two hard limits that must not be "improved" away:
//
//  1. Clients dialling a bare IP send NO SNI at all -- Go strips IP literals from
//     the extension -- so ServerName is empty for exactly the case this feature
//     exists to catch. This can never be the primary source.
//  2. Do NOT substitute ClientHelloInfo.Conn.LocalAddr(). Under Docker bridge
//     networking that is the container's 172.x address, never the address the
//     client dialled, and feeding it in would fill the candidate list with an
//     address unreachable from every agent.
//
// It remains correct and useful for DNS-named clients and for bare-metal or
// host-network deployments.
func (s *Service) ObserveSNI(serverName string) {
	if s.discovery == nil || serverName == "" {
		return
	}
	s.discovery.Observe(sandiscovery.Observation{
		Address: serverName,
		Source:  sandiscovery.SourceTLSSNI,
	})
}

// New builds the service. The provider is type-asserted once here rather than at
// every call site.
func New(provider tlspkg.Provider, settings *repository.SystemSettingsRepository, appConfig *config.Config) *Service {
	s := &Service{provider: provider, settings: settings, appConfig: appConfig}
	if ss, ok := provider.(*tlspkg.SelfSignedProvider); ok {
		s.selfSigned = ss
	}
	return s
}

// Managed reports whether the SAN list is under KrakenHashes' control.
func (s *Service) Managed() bool { return s.selfSigned != nil }

// UnmanagedReason explains, in terms an administrator can act on, why the SAN
// list cannot be edited in the current TLS mode.
//
// The API returns this rather than hiding the panel: the entire class of bug this
// feature addresses is "an admin configured something and nothing happened, with
// no feedback", and a silently absent control reproduces exactly that.
func (s *Service) UnmanagedReason() string {
	if s.Managed() {
		return ""
	}
	switch tlsMode() {
	case string(tlspkg.ModeProvided):
		return "KH_TLS_MODE=provided: the certificate's subject alternative names are fixed by the " +
			"certificate you supplied. Reissue it outside KrakenHashes, replace the files, and restart the backend."
	case string(tlspkg.ModeCertbot):
		return "KH_TLS_MODE=certbot: subject alternative names come from KH_CERTBOT_DOMAIN and are issued " +
			"by your ACME provider. Add names there and certbot will reissue."
	default:
		return "The active TLS provider does not manage its own subject alternative names."
	}
}

// DesiredSANs resolves the full SAN set the server certificate should carry.
//
// The database is authoritative; the legacy environment variables are consulted
// only for a setting that has never been written.
func (s *Service) DesiredSANs(ctx context.Context) (tlspkg.SANSet, error) {
	if !s.Managed() {
		return tlspkg.SANSet{}, fmt.Errorf("certificate names are not managed in this TLS mode")
	}

	ips := readSANSetting(ctx, s.settings, SettingAdditionalIPAddresses, config.GetAdditionalIPAddresses())
	dns := readSANSetting(ctx, s.settings, SettingAdditionalDNSNames, config.GetAdditionalDNSNames())

	return tlspkg.BuildServerSANs(s.selfSigned.ProviderConfig(), tlspkg.DesiredSANs{
		IPAddresses: ips,
		DNSNames:    dns,
	}), nil
}

// EnsureAtStartup imports the legacy environment variables once, then reissues
// the server certificate if it no longer covers the configured addresses or is
// close to expiry.
//
// This is the piece that fixes the original bug without anyone clicking
// anything: before it existed, editing the SAN configuration had no effect until
// the certificates were deleted -- which also destroyed the CA and every enrolled
// agent's trust.
//
// Deliberately never fatal. A deployment that cannot reissue must still boot on
// its existing certificate, so that an administrator can log in and fix whatever
// is wrong. Failing startup here would turn a certificate problem into a total
// outage.
func (s *Service) EnsureAtStartup(ctx context.Context) {
	if !s.Managed() {
		debug.Info("TLS mode %q manages its own certificate names; skipping SAN reconciliation", tlsMode())
		return
	}

	ImportEnvSANsIfUnset(ctx, s.settings)

	sans, err := s.DesiredSANs(ctx)
	if err != nil {
		debug.Error("Could not determine the desired certificate names: %v", err)
		return
	}

	current := s.selfSigned.ServerCertificate()
	added, removed := sans.Diff(current)
	if len(added) > 0 || len(removed) > 0 {
		debug.Warning("Server certificate SAN drift detected. Adding %v; removing %v.", added, removed)
	}

	result, err := s.selfSigned.EnsureServerCertificate(sans, tlspkg.ReissueOptions{})
	if err != nil {
		debug.Error("Failed to reissue the server certificate: %v", err)
		debug.Error("The previous certificate is still in use. Agents connecting to an address it does "+
			"not cover will keep failing. Current coverage: %s", describeSANs(current))
		return
	}

	if !result.Reissued {
		debug.Info("Server certificate is up to date: %s", sans.String())
		return
	}

	// nginx terminates :443 for the web UI from the same file and, unlike the Go
	// listener, has no hot-swap callback. Without this the browser keeps seeing
	// the old certificate while agents on :31337 already see the new one.
	outcome := ReloadNginx()
	switch {
	case !outcome.Attempted:
		// Not in Docker: there is no nginx to reload.
	case outcome.Succeeded:
		debug.Info("nginx reloaded with the reissued certificate")
	default:
		debug.Warning("nginx is still serving the previous certificate on port 443: %s", outcome.Detail)
		debug.Warning("Restart KrakenHashes to complete the update. The API on port %d is already "+
			"serving the new certificate, so agents are unaffected.", s.appConfig.HTTPSPort)
	}

	s.RefreshDiscoveryIgnoreSet()
}

// tlsMode reports the configured TLS mode, read the same way
// tls.LoadProviderConfig reads it.
func tlsMode() string {
	return env.GetOrDefault("KH_TLS_MODE", string(tlspkg.ModeSelfSigned))
}

// describeSANs renders a certificate's coverage for an error message, so a failed
// reissue still tells the administrator what the server is actually serving.
func describeSANs(cert *x509.Certificate) string {
	if cert == nil {
		return "none (no certificate loaded)"
	}
	ips := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}
	return fmt.Sprintf("DNS=%v IP=%v", cert.DNSNames, ips)
}
