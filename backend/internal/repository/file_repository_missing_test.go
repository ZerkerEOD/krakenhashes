package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
)

// seedRule inserts a rules row and returns its id.
func seedRule(t *testing.T, repo *FileRepository, owner uuid.UUID, fileName, status string) int {
	t.Helper()
	var id int
	err := repo.db.QueryRowContext(context.Background(), `
		INSERT INTO rules (name, rule_type, file_name, md5_hash, file_size, created_by, verification_status)
		VALUES ($1, 'hashcat', $2, $3, 1, $4, $5)
		RETURNING id`,
		"test-"+uuid.NewString()[:8], fileName, uuid.NewString()[:32], owner, status,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed rule %q: %v", fileName, err)
	}
	t.Cleanup(func() {
		_, _ = repo.db.ExecContext(context.Background(), `DELETE FROM rules WHERE id = $1`, id)
	})
	return id
}

func ruleStatus(t *testing.T, repo *FileRepository, id int) string {
	t.Helper()
	var status string
	if err := repo.db.QueryRowContext(context.Background(),
		`SELECT verification_status FROM rules WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}

// A 'verified' row whose file is gone is exactly the state that hid the root
// cause of an agent outage for nine days: the admin UI showed the rule as
// healthy while every agent sync 404ed on it.
func TestMarkMissingOnDisk(t *testing.T) {
	database := testutil.SetupTestDB(t)
	ownerUser := testutil.CreateTestUser(t, database,
		"filerepo-"+uuid.NewString()[:8], "filerepo-"+uuid.NewString()[:8]+"@test.local",
		testutil.DefaultTestPassword, "user")
	owner := ownerUser.ID

	base := t.TempDir()
	repo := NewFileRepository(database, base)
	rulesRoot := filepath.Join(base, "rules")
	ctx := context.Background()

	t.Run("verified row with a missing file becomes failed", func(t *testing.T) {
		id := seedRule(t, repo, owner, "hashcat/missing.rule", "verified")

		if err := repo.MarkMissingOnDisk(ctx, "rule", filepath.Join(rulesRoot, "hashcat", "missing.rule")); err != nil {
			t.Fatalf("MarkMissingOnDisk: %v", err)
		}
		if got := ruleStatus(t, repo, id); got != "failed" {
			t.Errorf("verification_status = %q, want \"failed\"", got)
		}
	})

	t.Run("a bare file_name is still matched", func(t *testing.T) {
		id := seedRule(t, repo, owner, "bare.rule", "verified")

		if err := repo.MarkMissingOnDisk(ctx, "rule", filepath.Join(rulesRoot, "hashcat", "bare.rule")); err != nil {
			t.Fatalf("MarkMissingOnDisk: %v", err)
		}
		if got := ruleStatus(t, repo, id); got != "failed" {
			t.Errorf("verification_status = %q, want \"failed\"", got)
		}
	})

	// Only 'verified' rows move. A row already marked failed, or still pending
	// its first verification, must not be churned on every 404 an agent makes.
	t.Run("non-verified rows are left alone", func(t *testing.T) {
		for _, status := range []string{"pending", "failed"} {
			id := seedRule(t, repo, owner, "hashcat/"+status+".rule", status)
			if err := repo.MarkMissingOnDisk(ctx, "rule", filepath.Join(rulesRoot, "hashcat", status+".rule")); err != nil {
				t.Fatalf("MarkMissingOnDisk: %v", err)
			}
			if got := ruleStatus(t, repo, id); got != status {
				t.Errorf("status %q became %q, want unchanged", status, got)
			}
		}
	})

	t.Run("an unrelated row is untouched", func(t *testing.T) {
		keep := seedRule(t, repo, owner, "hashcat/present.rule", "verified")

		if err := repo.MarkMissingOnDisk(ctx, "rule", filepath.Join(rulesRoot, "hashcat", "other.rule")); err != nil {
			t.Fatalf("MarkMissingOnDisk: %v", err)
		}
		if got := ruleStatus(t, repo, keep); got != "verified" {
			t.Errorf("unrelated row became %q, want \"verified\"", got)
		}
	})

	// Paths outside the resource root are not rows we own; guards against a
	// traversal-shaped path flipping something unrelated.
	t.Run("a path outside the resource root is a no-op", func(t *testing.T) {
		id := seedRule(t, repo, owner, "hashcat/outside.rule", "verified")

		if err := repo.MarkMissingOnDisk(ctx, "rule", "/etc/passwd"); err != nil {
			t.Fatalf("MarkMissingOnDisk: %v", err)
		}
		if got := ruleStatus(t, repo, id); got != "verified" {
			t.Errorf("row became %q on an out-of-root path, want \"verified\"", got)
		}
	})

	// Binaries and charsets have different verification lifecycles, so they are
	// deliberately not touched.
	t.Run("unhandled file types are a no-op", func(t *testing.T) {
		id := seedRule(t, repo, owner, "hashcat/untouched.rule", "verified")

		for _, ft := range []string{"binary", "charset", "hashlist", ""} {
			if err := repo.MarkMissingOnDisk(ctx, ft, filepath.Join(rulesRoot, "hashcat", "untouched.rule")); err != nil {
				t.Fatalf("MarkMissingOnDisk(%q): %v", ft, err)
			}
		}
		if got := ruleStatus(t, repo, id); got != "verified" {
			t.Errorf("row became %q via an unhandled file type, want \"verified\"", got)
		}
	})
}
