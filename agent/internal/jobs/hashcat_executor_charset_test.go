package jobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// indexOf returns the position of want in args, or -1 if absent.
func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// newCharsetTestExecutor builds an executor over a temp data dir with the
// hashlist file the command builder stats (os.Stat at hashcat_executor.go:1020).
// IsKeyspaceSplit is left false by callers so the builder never shells out to
// the --keyspace probe — this stays a pure arg-construction test.
func newCharsetTestExecutor(t *testing.T) (*HashcatExecutor, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "hashlists"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "hashlists", "test.hash"),
		[]byte("5d41402abc4b2a76b9719d911017c592\n"),
		0o644,
	))
	return NewHashcatExecutor(dir), dir
}

// TestBuildHashcatCommand_CustomCharsets is the agent-side regression guard for
// GH #132: a mask (-a 3) attack that references ?1..?4 must emit the matching
// -1..-4 charset definitions, or hashcat aborts "Custom-charset 1 is undefined".
// The backend fix forwards the charset fields; this locks the agent command it
// is supposed to produce once it receives them. Covers both the real-run and
// the benchmark/RunSpeedTest paths (isBenchmark toggles the same builder).
func TestBuildHashcatCommand_CustomCharsets(t *testing.T) {
	baseAssignment := func() *JobTaskAssignment {
		return &JobTaskAssignment{
			TaskID:          "charset-test",
			JobExecutionID:  "job-132",
			HashlistID:      1,
			HashlistPath:    "hashlists/test.hash",
			AttackMode:      int(AttackModeBruteForce),
			HashType:        0,
			Mask:            "?1?1?1?1",
			BinaryPath:      "/bin/true", // never executed by the builder
			ReportInterval:  5,
			IsKeyspaceSplit: false,
		}
	}

	for _, isBenchmark := range []bool{false, true} {
		label := "run"
		if isBenchmark {
			label = "benchmark"
		}

		t.Run("inline charset emits -1 before mask ("+label+")", func(t *testing.T) {
			e, _ := newCharsetTestExecutor(t)
			a := baseAssignment()
			a.CustomCharsets = map[string]string{"1": "?l?d"}

			cmd, _, _, _, _, err := e.buildHashcatCommandWithOptions(a, isBenchmark)
			require.NoError(t, err)

			dashOne := indexOf(cmd.Args, "-1")
			require.NotEqual(t, -1, dashOne, "expected -1 flag in args: %v", cmd.Args)
			require.Equal(t, "?l?d", cmd.Args[dashOne+1], "expected -1 to be followed by its definition")

			mask := indexOf(cmd.Args, "?1?1?1?1")
			require.NotEqual(t, -1, mask, "expected the mask in args: %v", cmd.Args)
			require.Less(t, dashOne, mask, "charset definition must precede the mask")
		})

		t.Run("multiple inline slots ("+label+")", func(t *testing.T) {
			e, _ := newCharsetTestExecutor(t)
			a := baseAssignment()
			a.Mask = "?1?2?1?2"
			a.CustomCharsets = map[string]string{"1": "?l?d", "2": "?u?s"}

			cmd, _, _, _, _, err := e.buildHashcatCommandWithOptions(a, isBenchmark)
			require.NoError(t, err)

			d1 := indexOf(cmd.Args, "-1")
			require.NotEqual(t, -1, d1, "args: %v", cmd.Args)
			require.Equal(t, "?l?d", cmd.Args[d1+1])
			d2 := indexOf(cmd.Args, "-2")
			require.NotEqual(t, -1, d2, "args: %v", cmd.Args)
			require.Equal(t, "?u?s", cmd.Args[d2+1])
		})

		t.Run("file charset uses local charsets path ("+label+")", func(t *testing.T) {
			e, dir := newCharsetTestExecutor(t)
			a := baseAssignment()
			a.CharsetFiles = map[string]CharsetFileInfo{
				"1": {Name: "special.hcchr", MD5Hash: "abc", ByteCount: 10},
			}

			cmd, _, _, _, _, err := e.buildHashcatCommandWithOptions(a, isBenchmark)
			require.NoError(t, err)

			d1 := indexOf(cmd.Args, "-1")
			require.NotEqual(t, -1, d1, "args: %v", cmd.Args)
			require.Equal(t, filepath.Join(dir, "charsets", "special.hcchr"), cmd.Args[d1+1])
		})

		t.Run("hex charset injects --hex-charset ("+label+")", func(t *testing.T) {
			e, _ := newCharsetTestExecutor(t)
			a := baseAssignment()
			a.CustomCharsets = map[string]string{"1": "414243"}
			a.HexCharset = true

			cmd, _, _, _, _, err := e.buildHashcatCommandWithOptions(a, isBenchmark)
			require.NoError(t, err)

			require.NotEqual(t, -1, indexOf(cmd.Args, "--hex-charset"), "args: %v", cmd.Args)
			require.NotEqual(t, -1, indexOf(cmd.Args, "-1"), "args: %v", cmd.Args)
		})

		t.Run("no charset means no -1 flag ("+label+")", func(t *testing.T) {
			e, _ := newCharsetTestExecutor(t)
			a := baseAssignment()
			a.Mask = "?l?l?l?l" // built-in charset, no custom slots

			cmd, _, _, _, _, err := e.buildHashcatCommandWithOptions(a, isBenchmark)
			require.NoError(t, err)

			require.Equal(t, -1, indexOf(cmd.Args, "-1"), "unexpected -1 flag: %v", cmd.Args)
			require.Equal(t, -1, indexOf(cmd.Args, "--hex-charset"), "unexpected --hex-charset: %v", cmd.Args)
		})
	}
}
