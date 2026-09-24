// Package certs owns the database-backed view of the server certificate's
// subject alternative names, and the operations that act on it.
//
// It is the only place that knows about both system_settings and the TLS
// provider. The provider deliberately has no database dependency: it must be
// able to generate a first certificate at startup, long before a database
// connection exists.
package certs

import (
	"context"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// Setting keys. Both are seeded by 20260907120000_add_certificate_san_settings.
const (
	SettingAdditionalIPAddresses = "tls_additional_ip_addresses"
	SettingAdditionalDNSNames    = "tls_additional_dns_names"
)

// Legacy environment variables these settings replace. Still read as the
// bootstrap source for the very first certificate, and imported once into the
// database, but never authoritative after that.
const (
	envAdditionalIPAddresses = "KH_ADDITIONAL_IP_ADDRESSES"
	envAdditionalDNSNames    = "KH_ADDITIONAL_DNS_NAMES"
)

// ImportEnvSANsIfUnset copies the legacy environment variables into
// system_settings the first time the backend boots after this feature lands, and
// never again.
//
// A NULL row means "never configured". Once the row holds any string -- including
// the empty string, which is how an administrator clears the list -- the database
// is authoritative and the environment variable is dead.
//
// Returns the number of keys imported.
func ImportEnvSANsIfUnset(ctx context.Context, repo *repository.SystemSettingsRepository) int {
	imported := 0

	pairs := []struct {
		key    string
		envVar string
		value  []string
	}{
		{SettingAdditionalIPAddresses, envAdditionalIPAddresses, config.GetAdditionalIPAddresses()},
		{SettingAdditionalDNSNames, envAdditionalDNSNames, config.GetAdditionalDNSNames()},
	}

	for _, p := range pairs {
		setting, err := repo.GetSetting(ctx, p.key)
		if err != nil {
			debug.Warning("Could not read %s; leaving it alone: %v", p.key, err)
			continue
		}

		if setting.Value != nil {
			// Already configured through the UI. Warn if the environment
			// variable still disagrees -- this is the feedback loop whose
			// absence let a stale variable look effective for so long.
			envValue := strings.Join(p.value, ",")
			if envValue != "" && envValue != *setting.Value {
				debug.Warning("%s is set to %q but is NO LONGER READ. Certificate names are managed in "+
					"Admin -> Settings -> Server Certificate (%s = %q). Remove the environment variable.",
					p.envVar, envValue, p.key, *setting.Value)
			}
			continue
		}

		value := strings.Join(p.value, ",")
		if err := repo.UpdateSetting(ctx, p.key, value); err != nil {
			debug.Warning("Failed to import %s into %s: %v", p.envVar, p.key, err)
			continue
		}

		if value != "" {
			debug.Info("Imported %s=%q into the %s setting. The environment variable is no longer read.",
				p.envVar, value, p.key)
		}
		imported++
	}

	return imported
}

// splitList parses a stored comma-separated setting value.
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// joinList renders a list back into the stored comma-separated form.
func joinList(values []string) string {
	return strings.Join(values, ",")
}

// readSANSetting returns the configured list for one key.
//
// A NULL row falls back to the environment variable: on the very first boot the
// import above has already run, but if it failed for any reason the environment
// remains a working source rather than silently yielding an empty list.
func readSANSetting(ctx context.Context, repo *repository.SystemSettingsRepository, key string, envFallback []string) []string {
	setting, err := repo.GetSetting(ctx, key)
	if err != nil {
		debug.Warning("Could not read %s, falling back to the environment: %v", key, err)
		return envFallback
	}
	if setting.Value == nil {
		return envFallback
	}

	var out []string
	for _, part := range strings.Split(*setting.Value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
