package scheduler

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeBinaryResolver satisfies BinaryResolver without touching the version
// subsystem: it just hands back a fixed id. buildBenchmarkRequest's follow-up
// "SELECT file_name, md5 FROM binary_versions" is non-fatal when the row is
// absent, so no binary_versions fixture is needed.
type fakeBinaryResolver struct{}

func (fakeBinaryResolver) DetermineBinaryForTask(ctx context.Context, agentID int, jobExecutionID uuid.UUID) (int64, error) {
	return 1, nil
}

// TestBuildBenchmarkRequest_ForwardsCustomCharsets is the backend-side
// regression guard for GH #132. It exercises the real fix: buildBenchmarkRequest
// (and, by the same shared enrichment pattern, cycle.sendAssignment) must read
// job_executions.custom_charsets / custom_charset_files / hex_charset and put
// them on the outgoing payload. Before the fix these came off an always-empty
// BuildTaskAssignment result, so a mask (-a 3) benchmark shipped ?1..?4 with no
// -1 definition and hashcat aborted in RunSpeedTest.
//
// DB-backed: skips cleanly when no Postgres is reachable (see
// requireSchedulerTestDB). buildBenchmarkRequest reads the unit struct in
// memory and only hits the DB via unit.ParentJobID, so no scheduling_units row
// is required — just the job_execution chain.
func TestBuildBenchmarkRequest_ForwardsCustomCharsets(t *testing.T) {
	database := requireSchedulerTestDB(t)
	ctx := context.Background()

	jobID := createTestJobExecution(t, database)

	// Turn the default -a 0 job into an -a 3 mask job that references custom
	// charset slot 1 (inline ?l?d) and slot 2 (a .hcchr file), with the hex flag.
	_, err := database.ExecContext(ctx, `
		UPDATE job_executions
		   SET attack_mode          = 3,
		       mask                 = $2,
		       custom_charsets      = $3::jsonb,
		       custom_charset_files = $4::jsonb,
		       hex_charset          = true
		 WHERE id = $1
	`, jobID,
		"?1?2?1?2",
		`{"1":"?l?d"}`,
		`{"2":{"id":"c0ffee00-0000-0000-0000-000000000002","file_path":"charsets/special.hcchr","md5":"abc123","byte_count":10}}`,
	)
	require.NoError(t, err)

	mask := "?1?2?1?2"
	unit := &models.SchedulingUnit{
		ParentJobID: jobID,
		AttackMode:  AttackModeMask, // 3
		MaskString:  &mask,
	}

	gap := BenchmarkGap{
		AgentID:    987654, // no agent row — lookups are non-fatal
		UnitID:     uuid.New(),
		AttackMode: AttackModeMask,
		HashType:   0,
	}

	req, err := buildBenchmarkRequest(ctx, database, fakeBinaryResolver{}, unit, gap)
	require.NoError(t, err)
	require.NotNil(t, req)

	require.Equal(t, mask, req.Mask)
	require.Equal(t, map[string]string{"1": "?l?d"}, req.CustomCharsets,
		"inline charset slot 1 must reach the benchmark payload")
	require.True(t, req.HexCharset, "hex_charset must be forwarded")

	cf, ok := req.CharsetFiles["2"]
	require.True(t, ok, "file charset slot 2 must reach the benchmark payload: %+v", req.CharsetFiles)
	require.Equal(t, "special.hcchr", cf.Name, "file charset name must be the basename")
	require.Equal(t, "abc123", cf.MD5Hash)
	require.Equal(t, 10, cf.ByteCount)
}
