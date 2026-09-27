package wordlist_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/google/uuid"
)

// seedWordlist inserts a wordlist row with the given status/missing_since and
// returns its id.
func seedWordlist(t *testing.T, database interface {
	QueryRow(string, ...interface{}) *sql.Row
}, owner uuid.UUID, fileName, status string, missing bool) int {
	t.Helper()
	var id int
	err := database.QueryRow(`
		INSERT INTO wordlists (name, description, wordlist_type, format, file_name, md5_hash,
			file_size, word_count, created_by, verification_status, missing_since)
		VALUES ($1, '', 'general', 'plaintext', $2, $3, 1, 0, $4, $5, CASE WHEN $6 THEN NOW() ELSE NULL END)
		RETURNING id`,
		"wl-"+uuid.NewString()[:8], fileName, uuid.NewString()[:32], owner, status, missing,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed wordlist: %v", err)
	}
	return id
}

func wlStatus(t *testing.T, s *wordlist.Store, id int) (string, bool) {
	t.Helper()
	all, err := s.ListWordlists(context.Background(), nil)
	if err != nil {
		t.Fatalf("list wordlists: %v", err)
	}
	for _, w := range all {
		if w.ID == id {
			return w.VerificationStatus, w.MissingSince != nil
		}
	}
	t.Fatalf("wordlist %d not found", id)
	return "", false
}

func TestWordlistMissingReconcileStore(t *testing.T) {
	database := testutil.SetupTestDB(t)
	user := testutil.CreateTestUser(t, database, "wlmiss", "wlmiss@example.com", testutil.DefaultTestPassword, "user")
	store := wordlist.NewStore(database.DB)
	ctx := context.Background()

	// A verified wordlist whose file has gone.
	gone := seedWordlist(t, database, user.ID, "general/gone.txt", models.VerificationStatusVerified, false)
	// A wordlist that failed for another reason (e.g. filter generation): failed, no missing_since.
	contentFail := seedWordlist(t, database, user.ID, "general/badfilter.txt", models.VerificationStatusFailed, false)

	// Mark missing: verified -> failed + missing_since.
	if changed, err := store.MarkWordlistMissing(ctx, gone); err != nil || !changed {
		t.Fatalf("MarkWordlistMissing = %v, %v; want true, nil", changed, err)
	}
	if s, miss := wlStatus(t, store, gone); s != models.VerificationStatusFailed || !miss {
		t.Fatalf("after mark: status=%q missing=%v; want failed, true", s, miss)
	}

	// Idempotent: already flagged, no second stamp.
	if changed, err := store.MarkWordlistMissing(ctx, gone); err != nil || changed {
		t.Fatalf("second MarkWordlistMissing = %v, %v; want false, nil", changed, err)
	}

	// A content failure must never be treated as missing, so restore leaves it.
	if changed, err := store.RestoreWordlistOnDisk(ctx, contentFail); err != nil || changed {
		t.Fatalf("RestoreWordlistOnDisk(content-fail) = %v, %v; want false, nil", changed, err)
	}
	if s, miss := wlStatus(t, store, contentFail); s != models.VerificationStatusFailed || miss {
		t.Fatalf("content-fail changed: status=%q missing=%v; want failed, false", s, miss)
	}

	// Restore the genuinely-missing one once its file is back.
	if changed, err := store.RestoreWordlistOnDisk(ctx, gone); err != nil || !changed {
		t.Fatalf("RestoreWordlistOnDisk(gone) = %v, %v; want true, nil", changed, err)
	}
	if s, miss := wlStatus(t, store, gone); s != models.VerificationStatusVerified || miss {
		t.Fatalf("after restore: status=%q missing=%v; want verified, false", s, miss)
	}

	// MarkWordlistMissing must not touch a content failure (it isn't 'verified').
	if changed, err := store.MarkWordlistMissing(ctx, contentFail); err != nil || changed {
		t.Fatalf("MarkWordlistMissing(content-fail) = %v, %v; want false, nil", changed, err)
	}
}

func seedRule(t *testing.T, database interface {
	QueryRow(string, ...interface{}) *sql.Row
}, owner uuid.UUID, fileName, status string, missing bool) int {
	t.Helper()
	var id int
	err := database.QueryRow(`
		INSERT INTO rules (name, description, rule_type, file_name, md5_hash, file_size, rule_count,
			created_by, verification_status, missing_since)
		VALUES ($1, '', 'hashcat', $2, $3, 1, 0, $4, $5, CASE WHEN $6 THEN NOW() ELSE NULL END)
		RETURNING id`,
		"rl-"+uuid.NewString()[:8], fileName, uuid.NewString()[:32], owner, status, missing,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	return id
}

func ruleStatus(t *testing.T, s *rule.Store, id int) (string, bool) {
	t.Helper()
	all, err := s.ListRules(context.Background(), nil)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	for _, r := range all {
		if r.ID == id {
			return r.VerificationStatus, r.MissingSince != nil
		}
	}
	t.Fatalf("rule %d not found", id)
	return "", false
}

func TestRuleMissingReconcileStore(t *testing.T) {
	database := testutil.SetupTestDB(t)
	user := testutil.CreateTestUser(t, database, "rlmiss", "rlmiss@example.com", testutil.DefaultTestPassword, "user")
	store := rule.NewStore(database.DB)
	ctx := context.Background()

	gone := seedRule(t, database, user.ID, "hashcat/gone.rule", models.VerificationStatusVerified, false)
	contentFail := seedRule(t, database, user.ID, "hashcat/bad.rule", models.VerificationStatusFailed, false)

	if changed, err := store.MarkRuleMissing(ctx, gone); err != nil || !changed {
		t.Fatalf("MarkRuleMissing = %v, %v; want true, nil", changed, err)
	}
	if s, miss := ruleStatus(t, store, gone); s != models.VerificationStatusFailed || !miss {
		t.Fatalf("after mark: status=%q missing=%v; want failed, true", s, miss)
	}
	if changed, err := store.MarkRuleMissing(ctx, gone); err != nil || changed {
		t.Fatalf("second MarkRuleMissing = %v, %v; want false, nil", changed, err)
	}
	if changed, err := store.RestoreRuleOnDisk(ctx, contentFail); err != nil || changed {
		t.Fatalf("RestoreRuleOnDisk(content-fail) = %v, %v; want false, nil", changed, err)
	}
	if changed, err := store.RestoreRuleOnDisk(ctx, gone); err != nil || !changed {
		t.Fatalf("RestoreRuleOnDisk(gone) = %v, %v; want true, nil", changed, err)
	}
	if s, miss := ruleStatus(t, store, gone); s != models.VerificationStatusVerified || miss {
		t.Fatalf("after restore: status=%q missing=%v; want verified, false", s, miss)
	}
}
