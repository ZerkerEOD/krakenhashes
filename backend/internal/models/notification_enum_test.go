package models

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

/*
Every NotificationType declared in Go must exist in the database enum.

notification_type is a Postgres enum shared by notifications.notification_type,
audit_log.event_type and notification_preferences.notification_type. A constant
added in Go without a matching migration compiles, passes review, and then fails
at runtime on the first dispatch:

	pq: invalid input value for enum notification_type: "benchmark_storm"

Two constants — benchmark_storm and hashlist_malformed — shipped that way and
were undeliverable. benchmark_storm is the advisory that warns an agent is
failing benchmarks repeatedly, the exact condition that later kills a whole job
at the per-tuple hard cap. On one deployment three jobs died at 21%, 22% and 40%
while that warning could not reach anyone.

This scans the migrations for ALTER TYPE ... ADD VALUE and the CREATE TYPE, so a
new constant without a migration fails here rather than in production.
*/
func TestEveryNotificationTypeExistsInTheEnum(t *testing.T) {
	migrationsDir := filepath.Join("..", "..", "db", "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}

	// Collect every value the migrations put into the enum.
	inEnum := map[string]bool{}
	addValue := regexp.MustCompile(`ADD VALUE (?:IF NOT EXISTS )?'([a-z_]+)'`)
	createType := regexp.MustCompile(`(?is)CREATE TYPE\s+notification_type\s+AS ENUM\s*\(([^)]*)\)`)
	label := regexp.MustCompile(`'([a-z_]+)'`)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(migrationsDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		src := string(b)

		// Only count ADD VALUE lines that target notification_type.
		for _, line := range strings.Split(src, "\n") {
			if !strings.Contains(line, "notification_type") {
				continue
			}
			if m := addValue.FindStringSubmatch(line); m != nil {
				inEnum[m[1]] = true
			}
		}
		if m := createType.FindStringSubmatch(src); m != nil {
			for _, lm := range label.FindAllStringSubmatch(m[1], -1) {
				inEnum[lm[1]] = true
			}
		}
	}

	if len(inEnum) == 0 {
		t.Fatal("found no notification_type enum values in the migrations; did the scan break?")
	}

	var missing []string
	for _, nt := range AllNotificationTypes() {
		if !inEnum[string(nt)] {
			missing = append(missing, string(nt))
		}
	}

	if len(missing) > 0 {
		t.Errorf("NotificationType(s) declared in Go with no migration adding them to the "+
			"notification_type enum: %v\n\nDispatching any of these fails at runtime with "+
			"`pq: invalid input value for enum notification_type`. Add an "+
			"ALTER TYPE notification_type ADD VALUE migration.", missing)
	}
}
