package certs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	tlspkg "github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// SANSource records where a configured list came from, so the UI can flag a
// deployment still relying on a deprecated environment variable.
type SANSource string

const (
	SourceDatabase    SANSource = "database"
	SourceEnvironment SANSource = "environment"
)

// SANSettings is the editable configuration, as stored.
type SANSettings struct {
	AdditionalIPAddresses []string  `json:"additional_ip_addresses"`
	AdditionalDNSNames    []string  `json:"additional_dns_names"`
	IPSource              SANSource `json:"ip_source"`
	DNSSource             SANSource `json:"dns_source"`
	DeprecatedEnvPresent  bool      `json:"deprecated_env_present"`
	// AllowedRanges is the fixed set of ranges a certificate name may fall in,
	// so the UI can state the policy rather than making an admin discover it by
	// having an address rejected.
	AllowedRanges []string `json:"allowed_ranges"`
}

// DriftReport says whether the live certificate matches the configuration.
type DriftReport struct {
	InSync  bool     `json:"in_sync"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// EffectiveSANs is what the certificate WOULD carry if reissued now.
//
// Shown before the admin commits, so the outcome is never a surprise -- which is
// the whole complaint about the environment variable this replaces.
type EffectiveSANs struct {
	DNSNames    []string `json:"dns_names"`
	IPAddresses []string `json:"ip_addresses"`
}

// CertificateStatus is the admin API read model.
type CertificateStatus struct {
	TLSMode         string                  `json:"tls_mode"`
	Managed         bool                    `json:"managed"`
	UnmanagedReason string                  `json:"unmanaged_reason,omitempty"`
	Settings        *SANSettings            `json:"settings,omitempty"`
	Certificate     *tlspkg.CertificateInfo `json:"certificate,omitempty"`
	CA              *tlspkg.CertificateInfo `json:"ca,omitempty"`
	Drift           *DriftReport            `json:"drift,omitempty"`
	EffectiveSANs   *EffectiveSANs          `json:"effective_sans_preview,omitempty"`
	Locked          *EffectiveSANs          `json:"locked,omitempty"`
}

// Propagation tells the UI exactly how far a reissue has actually reached.
type Propagation struct {
	// BackendHotReloaded is always true on success: the listener reads its leaf
	// through an atomic pointer, so publishing cannot fail once the files are
	// written. The field exists so the UI can state it rather than imply it.
	BackendHotReloaded bool `json:"backend_hot_reloaded"`
	// RestartRequired is false for every operation, including CA rotation.
	RestartRequired bool `json:"restart_required"`
	// ExistingConnectionsUnaffected: already-negotiated sessions keep the old
	// leaf, which is harmless -- a connected agent does not need the new name.
	ExistingConnectionsUnaffected bool          `json:"existing_connections_unaffected"`
	NginxReload                   ReloadOutcome `json:"nginx_reload"`
	// AgentsActionRequired is "none" for a leaf reissue and "refetch-ca" after
	// a CA rotation.
	AgentsActionRequired string `json:"agents_action_required"`
}

// ReissueReport is the shared response for every write operation.
type ReissueReport struct {
	Success     bool                    `json:"success"`
	Reissued    bool                    `json:"reissued"`
	Reason      string                  `json:"reason"`
	AddedSANs   []string                `json:"added_sans,omitempty"`
	RemovedSANs []string                `json:"removed_sans,omitempty"`
	Certificate *tlspkg.CertificateInfo `json:"certificate,omitempty"`
	CA          *tlspkg.CertificateInfo `json:"ca,omitempty"`
	BackupDir   string                  `json:"backup_dir,omitempty"`
	Propagation Propagation             `json:"propagation"`
	Warnings    []string                `json:"warnings,omitempty"`
}

// UpdateResult reports per-entry validation outcomes.
type UpdateResult struct {
	Accepted *SANSettings          `json:"accepted,omitempty"`
	Rejected []tlspkg.SANRejection `json:"rejected,omitempty"`
}

// ErrUnmanaged is returned by every write operation in a TLS mode that does not
// manage its own names.
type ErrUnmanaged struct{ Reason string }

func (e ErrUnmanaged) Error() string { return e.Reason }

// lockedNames are entries the administrator may not remove.
//
// nginx proxies to https://localhost:31337 in every template and the container
// health check uses loopback, so dropping these breaks the deployment in ways
// that look nothing like a certificate problem.
var lockedDNSNames = []string{"localhost"}
var lockedIPAddresses = []string{"127.0.0.1"}

// Status assembles the admin read model.
func (s *Service) Status(ctx context.Context) (*CertificateStatus, error) {
	status := &CertificateStatus{
		TLSMode: tlsMode(),
		Managed: s.Managed(),
	}

	if inspector, ok := s.provider.(tlspkg.CertificateInspector); ok {
		if info, err := inspector.DescribeServerCertificate(); err == nil {
			status.Certificate = info
		} else {
			debug.Warning("Could not describe the server certificate: %v", err)
		}
		if info, err := inspector.DescribeCACertificate(); err == nil {
			status.CA = info
		}
	}

	if !s.Managed() {
		status.UnmanagedReason = s.UnmanagedReason()
		return status, nil
	}

	ipSetting, ipSource := s.readSANWithSource(ctx, SettingAdditionalIPAddresses, config.GetAdditionalIPAddresses())
	dnsSetting, dnsSource := s.readSANWithSource(ctx, SettingAdditionalDNSNames, config.GetAdditionalDNSNames())

	status.Settings = &SANSettings{
		AdditionalIPAddresses: ipSetting,
		AdditionalDNSNames:    dnsSetting,
		IPSource:              ipSource,
		DNSSource:             dnsSource,
		DeprecatedEnvPresent:  len(config.GetAdditionalIPAddresses()) > 0 || len(config.GetAdditionalDNSNames()) > 0,
		AllowedRanges:         tlspkg.InternalRanges(),
	}

	sans, err := s.DesiredSANs(ctx)
	if err != nil {
		return nil, err
	}
	status.EffectiveSANs = &EffectiveSANs{DNSNames: sans.DNSNames, IPAddresses: sans.IPStrings()}
	status.Locked = &EffectiveSANs{DNSNames: lockedDNSNames, IPAddresses: lockedIPAddresses}

	current := s.selfSigned.ServerCertificate()
	added, removed := sans.Diff(current)
	status.Drift = &DriftReport{
		InSync:  len(added) == 0 && len(removed) == 0,
		Added:   added,
		Removed: removed,
	}

	return status, nil
}

// UpdateSANs validates and persists both lists.
//
// All-or-nothing: if any entry is rejected nothing is written, so an admin never
// ends up with a half-applied list and no clear idea which half landed.
func (s *Service) UpdateSANs(ctx context.Context, ips, dnsNames []string) (*UpdateResult, error) {
	if !s.Managed() {
		return nil, ErrUnmanaged{Reason: s.UnmanagedReason()}
	}

	acceptedIPs, rejectedIPs := tlspkg.ParseSANList(joinList(ips), tlspkg.SANKindIP)
	acceptedDNS, rejectedDNS := tlspkg.ParseSANList(joinList(dnsNames), tlspkg.SANKindDNS)

	rejected := append(rejectedIPs, rejectedDNS...)
	if len(rejected) > 0 {
		return &UpdateResult{Rejected: rejected}, nil
	}

	if err := s.settings.UpdateSetting(ctx, SettingAdditionalIPAddresses, joinList(acceptedIPs)); err != nil {
		return nil, fmt.Errorf("failed to save the IP address list: %w", err)
	}
	if err := s.settings.UpdateSetting(ctx, SettingAdditionalDNSNames, joinList(acceptedDNS)); err != nil {
		return nil, fmt.Errorf("failed to save the DNS name list: %w", err)
	}

	debug.Info("Certificate SAN configuration updated: IP=%v DNS=%v", acceptedIPs, acceptedDNS)

	return &UpdateResult{Accepted: &SANSettings{
		AdditionalIPAddresses: acceptedIPs,
		AdditionalDNSNames:    acceptedDNS,
		IPSource:              SourceDatabase,
		DNSSource:             SourceDatabase,
		AllowedRanges:         tlspkg.InternalRanges(),
	}}, nil
}

// Reissue regenerates the server leaf under the existing CA.
func (s *Service) Reissue(ctx context.Context, opts tlspkg.ReissueOptions) (*ReissueReport, error) {
	if !s.Managed() {
		return nil, ErrUnmanaged{Reason: s.UnmanagedReason()}
	}

	sans, err := s.DesiredSANs(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.selfSigned.EnsureServerCertificate(sans, opts)
	if err != nil {
		return nil, err
	}

	return s.finishReissue(ctx, result, "none"), nil
}

// RotateCA regenerates the certificate authority and every leaf beneath it.
func (s *Service) RotateCA(ctx context.Context) (*ReissueReport, error) {
	if !s.Managed() {
		return nil, ErrUnmanaged{Reason: s.UnmanagedReason()}
	}

	sans, err := s.DesiredSANs(ctx)
	if err != nil {
		return nil, err
	}

	result, err := s.selfSigned.RotateCA(sans)
	if err != nil {
		return nil, err
	}

	report := s.finishReissue(ctx, result, "refetch-ca")

	// Push the new CA to agents that are still connected. They would otherwise
	// keep a stale trust store until their next reconnect, which after a
	// rotation means a window where they cannot re-establish at all.
	//
	// Best-effort by design: an agent that does not acknowledge is an older
	// build, not a broken one -- unknown message types are ignored -- and it
	// still self-heals through the reconnect path.
	if s.notifyAgents != nil {
		results := s.notifyAgents(result.Serial, "ca_rotated", s.caFingerprint())
		failed := 0
		for _, err := range results {
			if err != nil {
				failed++
			}
		}
		debug.Info("Notified %d connected agent(s) of the CA rotation (%d could not be reached)",
			len(results)-failed, failed)
	}

	report.Warnings = append(report.Warnings,
		"Every agent must re-fetch the CA. Connected agents were notified and refresh "+
			"immediately; the rest do so automatically the next time a TLS handshake fails. "+
			"Any browser or operating system that installed the old CA must install the new "+
			"one by hand, as the CA fingerprint has changed.")
	return report, nil
}

// caFingerprint returns the SHA-256 of the current CA's DER, lowercase hex.
//
// Matches the form the agent computes from its own ca.crt, so a comparison on
// either side is a plain string equality.
func (s *Service) caFingerprint() string {
	if !s.Managed() {
		return ""
	}
	ca := s.selfSigned.CACertificate()
	if ca == nil {
		return ""
	}
	sum := sha256.Sum256(ca.Raw)
	return hex.EncodeToString(sum[:])
}

// finishReissue reloads nginx, refreshes discovery, and builds the response.
func (s *Service) finishReissue(ctx context.Context, result *tlspkg.ReissueResult, agentsAction string) *ReissueReport {
	report := &ReissueReport{
		Success:     true,
		Reissued:    result.Reissued,
		Reason:      result.Reason,
		AddedSANs:   result.AddedSANs,
		RemovedSANs: result.RemovedSANs,
		BackupDir:   result.BackupDir,
		Propagation: Propagation{
			BackendHotReloaded:            result.Reissued,
			RestartRequired:               false,
			ExistingConnectionsUnaffected: true,
			AgentsActionRequired:          agentsAction,
		},
	}

	if inspector, ok := s.provider.(tlspkg.CertificateInspector); ok {
		if info, err := inspector.DescribeServerCertificate(); err == nil {
			report.Certificate = info
		}
		if info, err := inspector.DescribeCACertificate(); err == nil {
			report.CA = info
		}
	}

	if !result.Reissued {
		return report
	}

	report.Propagation.NginxReload = ReloadNginx()
	if report.Propagation.NginxReload.Attempted && !report.Propagation.NginxReload.Succeeded {
		report.Warnings = append(report.Warnings,
			"The web UI is served separately and will present the previous certificate until "+
				"KrakenHashes is restarted. The API is already serving the new one, so agents "+
				"are unaffected.")
	}

	s.RefreshDiscoveryIgnoreSet()
	s.pruneCoveredCandidates(ctx)

	return report
}

// pruneCoveredCandidates drops suggestions for addresses the certificate now
// covers, so the discovered list does not keep offering what is already done.
func (s *Service) pruneCoveredCandidates(ctx context.Context) {
	if s.candidates == nil {
		return
	}
	cert := s.selfSigned.ServerCertificate()
	if cert == nil {
		return
	}

	covered := make([]string, 0, len(cert.DNSNames)+len(cert.IPAddresses))
	covered = append(covered, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		covered = append(covered, ip.String())
	}

	pruneCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.candidates.DeleteByAddress(pruneCtx, covered); err != nil {
		debug.Warning("Failed to prune covered SAN candidates: %v", err)
	}
}

// readSANWithSource returns a configured list and where it came from.
func (s *Service) readSANWithSource(ctx context.Context, key string, envFallback []string) ([]string, SANSource) {
	setting, err := s.settings.GetSetting(ctx, key)
	if err != nil || setting.Value == nil {
		return envFallback, SourceEnvironment
	}
	return splitList(*setting.Value), SourceDatabase
}
