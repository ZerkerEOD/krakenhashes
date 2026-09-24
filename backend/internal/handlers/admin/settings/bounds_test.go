package settings

import (
	"strings"
	"testing"
)

func TestValidateSettingValue(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
		// substring the error must mention, so a passing test proves the message
		// is actually useful to whoever hit it
		wantMsg string
	}{
		// The case this whole change exists for: 1200 in a field bounded at 60.
		{name: "keyspace timeout 20 hours is refused", key: "keyspace_calculation_timeout_minutes", value: "1200", wantErr: true, wantMsg: "between 1 and 60"},
		{name: "keyspace timeout default is accepted", key: "keyspace_calculation_timeout_minutes", value: "4"},
		{name: "keyspace timeout at max is accepted", key: "keyspace_calculation_timeout_minutes", value: "60"},
		{name: "keyspace timeout one past max is refused", key: "keyspace_calculation_timeout_minutes", value: "61", wantErr: true},
		{name: "keyspace timeout at min is accepted", key: "keyspace_calculation_timeout_minutes", value: "1"},
		{name: "keyspace timeout zero is refused", key: "keyspace_calculation_timeout_minutes", value: "0", wantErr: true},
		{name: "keyspace timeout negative is refused", key: "keyspace_calculation_timeout_minutes", value: "-5", wantErr: true},

		// default_chunk_duration is entered in minutes but stored in seconds, so
		// the bound here must be the stored one. 1200 is legal for this key and
		// illegal for the one above -- that collision is what caused the bug.
		{name: "chunk duration 1200s is accepted", key: "default_chunk_duration", value: "1200"},
		{name: "chunk duration below stored min is refused", key: "default_chunk_duration", value: "59", wantErr: true},
		{name: "chunk duration at stored max is accepted", key: "default_chunk_duration", value: "86400"},
		{name: "chunk duration past stored max is refused", key: "default_chunk_duration", value: "86401", wantErr: true},

		// A bound whose min is 0 must not be confused with "no bound".
		{name: "network grace zero is accepted", key: "network_grace_seconds", value: "0"},
		{name: "network grace past max is refused", key: "network_grace_seconds", value: "601", wantErr: true},

		// Non-numeric input on a registered key.
		{name: "non-numeric is refused", key: "keyspace_calculation_timeout_minutes", value: "abc", wantErr: true, wantMsg: "whole number"},
		{name: "empty is refused", key: "keyspace_calculation_timeout_minutes", value: "", wantErr: true, wantMsg: "whole number"},
		{name: "float is refused", key: "keyspace_calculation_timeout_minutes", value: "4.5", wantErr: true, wantMsg: "whole number"},
		{name: "surrounding whitespace is tolerated", key: "keyspace_calculation_timeout_minutes", value: "  4  "},

		// Keys owned by a dedicated endpoint must be bounded here too, or the
		// generic key/value route is a way around that endpoint's validation.
		{name: "agent update concurrency past its cap is refused", key: "agent_update_max_concurrent", value: "9999", wantErr: true, wantMsg: "between 1 and 10"},
		{name: "agent update concurrency in range is accepted", key: "agent_update_max_concurrent", value: "4"},
		{name: "agent health timeout below its floor is refused", key: "agent_update_health_timeout_seconds", value: "59", wantErr: true},
		{name: "agent health timeout in range is accepted", key: "agent_update_health_timeout_seconds", value: "300"},
		{name: "download concurrency past its cap is refused", key: "agent_max_concurrent_downloads", value: "11", wantErr: true},
		{name: "download concurrency in range is accepted", key: "agent_max_concurrent_downloads", value: "3"},

		// 0 means "keep forever" for retention, so it must not be rejected.
		{name: "retention zero means keep forever", key: "metrics_retention_realtime_days", value: "0"},
		{name: "retention negative is refused", key: "metrics_retention_realtime_days", value: "-1", wantErr: true},

		// Unregistered keys must pass through untouched, so adding coverage stays
		// incremental and booleans/strings are never rejected as "not a number".
		{name: "unregistered key passes", key: "potfile_enabled", value: "true"},
		{name: "unregistered key passes arbitrary text", key: "agent_overflow_allocation_mode", value: "round_robin"},
		{name: "unknown key passes", key: "some_setting_added_later", value: "whatever"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSettingValue(tt.key, tt.value)

			if tt.wantErr && err == nil {
				t.Fatalf("ValidateSettingValue(%q, %q) = nil, want an error", tt.key, tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateSettingValue(%q, %q) = %v, want nil", tt.key, tt.value, err)
			}
			if tt.wantMsg != "" && err != nil && !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not mention %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

// The frontend and this map declare the same bounds in two places. Nothing can
// diff them automatically across languages, so at minimum assert the registry
// is self-consistent -- an inverted pair would reject every possible value and
// silently make a setting unwritable.
func TestNumericSettingBoundsAreSane(t *testing.T) {
	if len(numericSettingBounds) == 0 {
		t.Fatal("numericSettingBounds is empty")
	}

	for key, bound := range numericSettingBounds {
		if bound.min > bound.max {
			t.Errorf("%s: min %d exceeds max %d — no value can satisfy this", key, bound.min, bound.max)
		}
		if bound.min < 0 {
			t.Errorf("%s: min %d is negative; no current setting is meaningfully negative", key, bound.min)
		}
	}
}
