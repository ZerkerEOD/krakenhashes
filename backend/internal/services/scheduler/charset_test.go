package scheduler

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
)

// TestConvertCharsetFiles guards the DB->wire mapping for file-based custom
// charsets. A regression here is how GH #132 shipped: a mask (-a 3) attack
// referencing ?1..?4 went out with no charset definition and hashcat aborted
// with "Custom-charset 1 is undefined".
func TestConvertCharsetFiles(t *testing.T) {
	t.Run("nil map returns nil", func(t *testing.T) {
		if got := convertCharsetFiles(nil); got != nil {
			t.Fatalf("convertCharsetFiles(nil) = %v, want nil", got)
		}
	})

	t.Run("empty map returns nil", func(t *testing.T) {
		if got := convertCharsetFiles(models.CustomCharsetFiles{}); got != nil {
			t.Fatalf("convertCharsetFiles(empty) = %v, want nil", got)
		}
	})

	t.Run("maps ref to wire info with basename", func(t *testing.T) {
		files := models.CustomCharsetFiles{
			"1": {
				ID:        "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				FilePath:  "charsets/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.hcchr",
				MD5:       "d41d8cd98f00b204e9800998ecf8427e",
				ByteCount: 62,
			},
			"3": {
				FilePath:  "charsets/special.hcchr",
				MD5:       "0cc175b9c0f1b6a831c399e269772661",
				ByteCount: 10,
			},
		}

		got := convertCharsetFiles(files)
		if len(got) != 2 {
			t.Fatalf("got %d slots, want 2: %+v", len(got), got)
		}

		slot1, ok := got["1"]
		if !ok {
			t.Fatalf("slot 1 missing from %+v", got)
		}
		// Name must be the basename only — the agent joins it onto its own
		// data/charsets dir, so a relative path prefix would double up.
		if slot1.Name != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.hcchr" {
			t.Errorf("slot 1 Name = %q, want the basename", slot1.Name)
		}
		if slot1.MD5Hash != "d41d8cd98f00b204e9800998ecf8427e" {
			t.Errorf("slot 1 MD5Hash = %q", slot1.MD5Hash)
		}
		if slot1.ByteCount != 62 {
			t.Errorf("slot 1 ByteCount = %d, want 62", slot1.ByteCount)
		}
		// Size is not stored on CharsetFileRef; the agent verifies by MD5.
		if slot1.Size != 0 {
			t.Errorf("slot 1 Size = %d, want 0", slot1.Size)
		}

		if got["3"].Name != "special.hcchr" {
			t.Errorf("slot 3 Name = %q, want special.hcchr", got["3"].Name)
		}
	})
}
