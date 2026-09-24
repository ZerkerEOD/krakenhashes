package diagnostic

import (
	"strings"
	"testing"
)

/*
 * The comment on DiagnosticTables has always claimed "the privacy guarantee is
 * enforced by the dump unit tests". Until the cloud tables were added there were
 * no tests in this package at all, so the guarantee was a comment and nothing
 * else. These are that enforcement.
 *
 * A diagnostics bundle is downloaded by an admin and then handed to someone
 * else — that is its entire purpose. Every table added here widens what leaves
 * the deployment, and the cloud tables were the first to carry stored
 * credentials.
 */

func sensitive(table, column string) bool {
	for _, c := range SensitiveColumns[table] {
		if c == column {
			return true
		}
	}
	return false
}

func inBundle(table string) bool {
	for _, t := range DiagnosticTables {
		if t == table {
			return true
		}
	}
	return false
}

/*
 * TestSecretBearingColumnsAreRedacted is the half that protects the operator.
 *
 * Keyed by column rather than by table so that adding a table with a column
 * named like a credential is caught even if nobody thought about it here.
 */
func TestSecretBearingColumnsAreRedacted(t *testing.T) {
	mustRedact := map[string][]string{
		"cloud_provider_configs": {
			"credentials_encrypted",
			"vpn_credential_encrypted",
			"backend_vpn_host",
			"vpn_tag_or_group",
			"settings",
			"name",
		},
		"cloud_instances": {
			"client_name_snapshot",
			"vpn_credential_ref",
			"provider_raw",
		},
	}

	for table, cols := range mustRedact {
		if !inBundle(table) {
			continue // not exported at all; nothing can leak
		}
		for _, col := range cols {
			if !sensitive(table, col) {
				t.Errorf("%s.%s is exported in the diagnostics bundle but NOT redacted.\n"+
					"A bundle is downloaded to be handed to someone else. If this column "+
					"genuinely carries nothing sensitive, delete it from this test and say "+
					"why in the comment on SensitiveColumns — do not just drop it.", table, col)
			}
		}
	}
}

/*
 * TestDiagnosticProseIsNotRedacted is the half that protects the bundle's
 * usefulness, and it is the one likely to be "fixed" by mistake.
 *
 * sanitizeValue destroys a value rather than masking part of it, so adding one
 * of these to SensitiveColumns silently converts an actionable cloud bug report
 * into "something went wrong". Each of these is prose the BACKEND wrote about
 * its own behaviour: termination reasons, provider refusals, spend notes. They
 * name AWS regions, GPU models and request IDs — never client or hash material.
 */
func TestDiagnosticProseIsNotRedacted(t *testing.T) {
	mustStayReadable := map[string][]string{
		"cloud_instances":        {"label", "state", "termination_reason", "last_terminate_error"},
		"cloud_spend_ledger":     {"note", "kind"},
		"scheduling_diagnostics": {"reason_code", "detail", "severity"},
	}

	for table, cols := range mustStayReadable {
		for _, col := range cols {
			if sensitive(table, col) {
				t.Errorf("%s.%s is redacted, which guts the cloud diagnostics bundle.\n"+
					"This column is backend-authored prose about the backend's own "+
					"behaviour — it is the answer an operator is being asked to send. "+
					"If it has started carrying client data, fix the writer, not this list.",
					table, col)
			}
		}
	}
}

/*
 * TestEveryRedactedTableIsActuallyExported stops SensitiveColumns growing
 * entries for tables nobody dumps. A stale entry reads as protection that is
 * not doing anything, and the next person adds the table believing it is
 * already covered.
 *
 * `users`, `clients` and `teams` are the deliberate exception: they are not in
 * DiagnosticTables, and their entries exist so that a future addition inherits
 * the redaction rather than shipping the names first and learning later.
 */
func TestEveryRedactedTableIsActuallyExported(t *testing.T) {
	staleAllowed := map[string]bool{"users": true, "clients": true, "teams": true}

	for table := range SensitiveColumns {
		if !inBundle(table) && !staleAllowed[table] {
			t.Errorf("SensitiveColumns has an entry for %q, which is not in "+
				"DiagnosticTables. Either export it or drop the entry; a rule that "+
				"applies to nothing looks like protection and is not.", table)
		}
	}
}

/*
 * TestSanitizeValueDestroysTheValue pins the thing both lists above depend on.
 * If redaction ever became partial masking, "it is in SensitiveColumns" would
 * stop meaning "it cannot leak", and every judgement call in this file would
 * silently change meaning.
 */
func TestSanitizeValueDestroysTheValue(t *testing.T) {
	secret := "AKIAIOSFODNN7EXAMPLE/wJalrXUtnFEMI"

	got, ok := sanitizeValue(secret, "credentials_encrypted").(string)
	if !ok {
		t.Fatalf("sanitizeValue returned %T, want string", got)
	}
	if strings.Contains(got, "AKIA") || strings.Contains(got, "wJalr") {
		t.Errorf("sanitizeValue leaked part of the input: %q", got)
	}
	if !strings.HasPrefix(got, "[REDACTED:") {
		t.Errorf("sanitizeValue = %q, want a [REDACTED:...] placeholder", got)
	}

	// A nil stays nil rather than becoming the string "[REDACTED:...]", so an
	// absent credential is not reported as a present-but-hidden one.
	if v := sanitizeValue(nil, "credentials_encrypted"); v != nil {
		t.Errorf("sanitizeValue(nil) = %v, want nil", v)
	}

	// Non-string columns (jsonb arrives as []byte, but a driver may hand back
	// something else entirely) must not fall through unredacted.
	if v := sanitizeValue(map[string]string{"ip": "3.14.15.92"}, "provider_raw"); v == nil {
		t.Error("sanitizeValue(non-string) returned nil, want a redacted placeholder")
	} else if s, _ := v.(string); !strings.HasPrefix(s, "[REDACTED:") {
		t.Errorf("sanitizeValue(non-string) = %v, want a [REDACTED:...] placeholder", v)
	}
}
