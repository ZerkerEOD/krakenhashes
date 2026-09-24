package repository

import (
	"context"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests run against real Postgres because the behaviour under test IS the
// SQL: the EWMA blend lives in an ON CONFLICT clause, and the conflict target
// depends on UNIQUE NULLS NOT DISTINCT, which no in-memory fake reproduces.

func newGPUBenchmarkRepo(t *testing.T) (*CloudGPUBenchmarkRepository, *db.DB) {
	t.Helper()
	database := testutil.SetupTestDB(t)
	return NewCloudGPUBenchmarkRepository(database), database
}

// speedOf returns the single stored row for a combo, failing if the row count
// is anything but one. Most assertions here are about there being exactly one
// row, so the count check belongs in the accessor rather than every caller.
func speedOf(t *testing.T, repo *CloudGPUBenchmarkRepository, attackMode, hashType int, saltCount *int) ObservedRow {
	t.Helper()
	rows, err := repo.List(context.Background(), attackMode, hashType, saltCount)
	require.NoError(t, err)
	require.Len(t, rows, 1, "expected exactly one stored row for mode %d/type %d", attackMode, hashType)
	return rows[0]
}

/*
 * TestRecord_EWMAConvergesTowardRepeatedObservations pins the alpha SCHEDULE,
 * not just the direction of travel.
 *
 * The first two blended values are asserted exactly because they are the whole
 * argument for a sample-count-dependent alpha: sample 2 must be a plain mean of
 * the two observations (alpha 1/2), and sample 3 a plain mean of three
 * (alpha 1/3). A fixed alpha of 0.1 would put sample 2 at 1100 instead of 1500,
 * leaving the model's estimate governed by one cold first benchmark for the
 * next twenty samples.
 */
func TestRecord_EWMAConvergesTowardRepeatedObservations(t *testing.T) {
	repo, _ := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	rec := func(speed int64) {
		require.NoError(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 0, 1000, nil, speed))
	}

	// Sample 1 is stored verbatim: there is nothing to blend it with, and
	// half-crediting a brand-new model against a static prior would leave the
	// table permanently unable to reach the truth.
	rec(1000)
	row := speedOf(t, repo, 0, 1000, nil)
	assert.Equal(t, int64(1000), row.Speed)
	assert.Equal(t, 1, row.SampleCount)

	// alpha = 1/2 -> mean(1000, 2000)
	rec(2000)
	row = speedOf(t, repo, 0, 1000, nil)
	assert.Equal(t, int64(1500), row.Speed)
	assert.Equal(t, 2, row.SampleCount)

	// alpha = 1/3 -> 1500 + (2000-1500)/3
	rec(2000)
	row = speedOf(t, repo, 0, 1000, nil)
	assert.Equal(t, int64(1667), row.Speed)
	assert.Equal(t, 3, row.SampleCount)

	// Keep feeding the same observation; the estimate must walk to it and the
	// sample count must track every single call.
	for i := 0; i < 47; i++ {
		rec(2000)
	}
	row = speedOf(t, repo, 0, 1000, nil)
	assert.Equal(t, 50, row.SampleCount)
	assert.GreaterOrEqual(t, row.Speed, int64(1990),
		"EWMA should have converged onto the repeated observation")
	assert.LessOrEqual(t, row.Speed, int64(2000),
		"EWMA must approach the observation from below, never overshoot it")
}

/*
 * TestRecord_AlphaFloorKeepsTheEstimateAbleToTrackChange is the test that
 * justifies the floor rather than a pure 1/(n+1) running mean.
 *
 * A running mean has infinite memory: after fifty samples at 2000, twenty
 * samples at 500 would move it only to about 1570, and it would keep drifting
 * for hundreds more. That is the wrong answer when the change is REAL — a
 * provider swapping the physical card behind a model name, or a hashcat version
 * bump. With alpha floored at 0.1 the estimate has a ~10-sample memory and
 * follows the new level instead.
 */
func TestRecord_AlphaFloorKeepsTheEstimateAbleToTrackChange(t *testing.T) {
	repo, _ := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	rec := func(speed int64) {
		require.NoError(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 0, 1000, nil, speed))
	}

	for i := 0; i < 50; i++ {
		rec(2000)
	}
	require.GreaterOrEqual(t, speedOf(t, repo, 0, 1000, nil).Speed, int64(1990))

	for i := 0; i < 20; i++ {
		rec(500)
	}

	row := speedOf(t, repo, 0, 1000, nil)
	assert.Equal(t, 70, row.SampleCount)
	assert.Less(t, row.Speed, int64(1000),
		"a floored alpha must follow a sustained level change; a pure running mean would still read ~1570 here")
	assert.Greater(t, row.Speed, int64(500),
		"twenty samples should not have fully replaced fifty — this is a smoother, not a last-write-wins")
}

/*
 * TestRecord_NullSaltCountConflictsInsteadOfDuplicating covers the trap in this
 * schema.
 *
 * The constraint is UNIQUE NULLS NOT DISTINCT, so a NULL salt_count PARTICIPATES
 * in uniqueness and two NULLs collide. Under PostgreSQL's default UNIQUE
 * semantics they would not, and every unsalted hash type — which is most of
 * them — would accumulate one fresh row per benchmark, each stuck at
 * sample_count 1, with the read path picking an arbitrary one. That is exactly
 * the failure agent_benchmarks has to work around with UPDATE-by-id.
 */
func TestRecord_NullSaltCountConflictsInsteadOfDuplicating(t *testing.T) {
	repo, database := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Record(ctx, "aws", "rtx_4090", 1, 0, 1000, nil, 1000))
	}

	// Count straight from the table rather than through List, so a bug in
	// List's IS NOT DISTINCT FROM predicate cannot hide extra rows.
	var total int
	require.NoError(t, database.QueryRow(
		`SELECT COUNT(*) FROM cloud_gpu_benchmarks`).Scan(&total))
	assert.Equal(t, 1, total, "repeated NULL-salt records must update one row, not insert three")

	row := speedOf(t, repo, 0, 1000, nil)
	assert.Equal(t, 3, row.SampleCount)
}

/*
 * TestRecord_SaltCountIsPartOfTheKey guards against the opposite mistake:
 * collapsing salted and unsalted measurements onto one row.
 *
 * Salted throughput falls roughly inversely with salt count, so a 100-salt
 * measurement blended into the unsalted row would understate an RTX 4090 by two
 * orders of magnitude and make every offer look unaffordable.
 */
func TestRecord_SaltCountIsPartOfTheKey(t *testing.T) {
	repo, _ := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	salts := 100
	require.NoError(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 0, 1400, nil, 900000))
	require.NoError(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 0, 1400, &salts, 9000))

	unsalted := speedOf(t, repo, 0, 1400, nil)
	assert.Equal(t, int64(900000), unsalted.Speed)
	assert.Nil(t, unsalted.SaltCount)

	salted := speedOf(t, repo, 0, 1400, &salts)
	assert.Equal(t, int64(9000), salted.Speed)
	require.NotNil(t, salted.SaltCount)
	assert.Equal(t, 100, *salted.SaltCount)
}

// TestList_ScopesToTheRequestedCombo checks the read path does not leak rows
// from other attack modes or hash types into a projection.
func TestList_ScopesToTheRequestedCombo(t *testing.T) {
	repo, _ := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	require.NoError(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 0, 1000, nil, 100))
	require.NoError(t, repo.Record(ctx, "vastai", "rtx_5090", 1, 0, 1000, nil, 200))
	require.NoError(t, repo.Record(ctx, "aws", "rtx_4090", 8, 0, 1000, nil, 800))
	require.NoError(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 3, 1000, nil, 999))
	require.NoError(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 0, 22000, nil, 777))

	rows, err := repo.List(ctx, 0, 1000, nil)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	// ORDER BY provider, gpu_model, gpu_count.
	assert.Equal(t, "aws", rows[0].Provider)
	assert.Equal(t, 8, rows[0].GPUCount)
	assert.Equal(t, "vastai", rows[1].Provider)
	assert.Equal(t, "rtx_4090", rows[1].GPUModel)
	assert.Equal(t, "rtx_5090", rows[2].GPUModel)

	empty, err := repo.List(ctx, 0, 99999, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

// TestPerGPUSpeed_DividesWholeInstanceThroughput: gpu_count is part of the key,
// so an 8x row and a 1x row for the same card both exist. A consumer comparing
// them without dividing would rank the same physical GPU eight different ways.
func TestPerGPUSpeed_DividesWholeInstanceThroughput(t *testing.T) {
	assert.Equal(t, int64(1000), ObservedRow{Speed: 8000, GPUCount: 8}.PerGPUSpeed())
	assert.Equal(t, int64(1000), ObservedRow{Speed: 1000, GPUCount: 1}.PerGPUSpeed())
	// A zero count is not a divisor. Record() coerces it to 1 on write, but a
	// row written before that guard existed must not panic the ranking pass.
	assert.Equal(t, int64(1000), ObservedRow{Speed: 1000, GPUCount: 0}.PerGPUSpeed())
}

// TestRecord_RejectsUnusableSamples: a zero speed is a failed measurement, not
// a slow GPU. Folding one in drags the model toward zero, which the estimator
// reads as an infinite ETA — an offer that can never pay for itself.
func TestRecord_RejectsUnusableSamples(t *testing.T) {
	repo, database := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	assert.Error(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 0, 1000, nil, 0))
	assert.Error(t, repo.Record(ctx, "vastai", "rtx_4090", 1, 0, 1000, nil, -5))
	assert.Error(t, repo.Record(ctx, "", "rtx_4090", 1, 0, 1000, nil, 100))
	assert.Error(t, repo.Record(ctx, "vastai", "", 1, 0, 1000, nil, 100))

	var total int
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM cloud_gpu_benchmarks`).Scan(&total))
	assert.Zero(t, total)
}

/*
 * TestRecord_RefusesUnknownGPUCount.
 *
 * An earlier revision defaulted a zero count to 1. That is the most expensive
 * mistake available in this file, because the recorded speed is the whole BOX's.
 *
 * Filing an 8-GPU box under gpu_count=1 records one GPU as eight times faster
 * than it is. The ranker keys on (provider, model, count), so the next genuine
 * single-GPU offer of that model matches that row exactly and gets an 8x
 * inflated throughput -- one eighth the cost-per-work of every honest
 * candidate. It wins every ranking pass, and sample_count keeps climbing until
 * it graduates past the confidence floor and stops being tempered by the class
 * prior. Worse when the model is the reference GPU: the anchor derives from it,
 * so one mis-counted row deflates every other model at the same time.
 *
 * A refused sample costs one observation. An inflated one corrupts the ranking
 * until somebody reads an invoice.
 */
func TestRecord_RefusesUnknownGPUCount(t *testing.T) {
	repo, _ := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	err := repo.Record(ctx, "mock", "rtx_4090", 0, 0, 1000, nil, 1000)
	require.Error(t, err, "a zero gpu_count must be refused, not defaulted to 1")
	assert.Contains(t, err.Error(), "gpu_count")

	// And nothing was written.
	rows, listErr := repo.List(ctx, 0, 1000, nil)
	require.NoError(t, listErr)
	assert.Empty(t, rows, "a refused sample must not leave a row behind")
}

// cloudAgent builds the provider -> instance -> agent chain and stamps the
// instance with the GPU the provider claims to have rented. Returns the agent
// id. gpuModel is written raw, as a provider would spell it.
func cloudAgent(t *testing.T, database *db.DB, provider, gpuModel string, gpuCount int) int {
	t.Helper()

	providerID := testutil.CreateTestCloudProviderConfig(t, database, provider,
		"cfg-"+uuid.NewString()[:8], testutil.ProviderConfigOpts{Enabled: true})
	instanceID, _ := testutil.CreateTestCloudInstance(t, database, providerID, testutil.InstanceOpts{})

	// CreateTestCloudInstance leaves gpu_model/gpu_count NULL; they are what
	// this repository keys on, so set them explicitly. An empty gpuModel writes
	// NULL, which is the "provider never told us" case.
	var model interface{}
	if gpuModel != "" {
		model = gpuModel
	}
	var count interface{}
	if gpuCount > 0 {
		count = gpuCount
	}
	_, err := database.Exec(
		`UPDATE cloud_instances SET gpu_model = $2, gpu_count = $3 WHERE id = $1`,
		instanceID, model, count)
	require.NoError(t, err)

	owner := testutil.CreateTestUser(t, database, "gpu-"+uuid.NewString()[:8],
		"gpu-"+uuid.NewString()[:8]+"@test.local", testutil.DefaultTestPassword, "admin")
	return testutil.CreateTestAgent(t, database, owner.ID, &instanceID)
}

/*
 * TestResolveCloudAgent_OnPremAgentResolvesToNothing is the guard that keeps
 * on-prem hardware out of this table entirely.
 *
 * An on-prem agent has no provider and no offer behind it, so a row written
 * from one could never be found from an offer — it would just be an
 * unattributable number sitting in the ranking input. nil/nil rather than an
 * error because on-prem agents are the overwhelming majority of benchmarks and
 * the caller must not have to classify an ordinary outcome.
 */
func TestResolveCloudAgent_OnPremAgentResolvesToNothing(t *testing.T) {
	repo, database := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	owner := testutil.CreateTestUser(t, database, "onprem-"+uuid.NewString()[:8],
		"onprem-"+uuid.NewString()[:8]+"@test.local", testutil.DefaultTestPassword, "admin")
	agentID := testutil.CreateTestAgent(t, database, owner.ID, nil)

	identity, err := repo.ResolveCloudAgent(ctx, agentID)
	require.NoError(t, err)
	assert.Nil(t, identity, "an agent with a NULL cloud_instance_id must record nothing")

	// An id belonging to no agent at all takes the same path.
	identity, err = repo.ResolveCloudAgent(ctx, 999999)
	require.NoError(t, err)
	assert.Nil(t, identity)
}

// TestResolveCloudAgent_CloudAgentYieldsProviderAndModel walks the join the
// write path depends on: agents -> cloud_instances -> cloud_provider_configs.
func TestResolveCloudAgent_CloudAgentYieldsProviderAndModel(t *testing.T) {
	repo, database := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	agentID := cloudAgent(t, database, "vastai", "NVIDIA GeForce RTX 4090", 4)

	identity, err := repo.ResolveCloudAgent(ctx, agentID)
	require.NoError(t, err)
	require.NotNil(t, identity)
	assert.Equal(t, "vastai", identity.Provider)
	assert.Equal(t, 4, identity.GPUCount)
	// Returned raw. Normalisation happens in the caller, because
	// internal/services/cloud imports this package and the reverse edge would
	// be an import cycle.
	assert.Equal(t, "NVIDIA GeForce RTX 4090", identity.GPUModel)
}

// TestResolveCloudAgent_MissingModelRecordsNothing: gpu_model is the only part
// of the key with no defensible default. Filing the sample under the empty
// model would match no offer ever, so the sample is dropped instead.
func TestResolveCloudAgent_MissingModelRecordsNothing(t *testing.T) {
	repo, database := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	agentID := cloudAgent(t, database, "aws", "", 0)

	identity, err := repo.ResolveCloudAgent(ctx, agentID)
	require.NoError(t, err)
	assert.Nil(t, identity)
}

/*
 * TestResolveCloudAgent_DropsUnknownGPUCount.
 *
 * cloud_instances.gpu_count is nullable, and a provider that omits num_gpus in
 * its offer leaves it NULL. The resolver must then drop the sample, exactly as
 * it already does for an unknown gpu_model -- the comment there says a value
 * with no sane default should not be invented, and a count has even less of a
 * sane default than a model name.
 *
 * See TestRecord_RefusesUnknownGPUCount for what a defaulted count does to the
 * ranking.
 */
func TestResolveCloudAgent_DropsUnknownGPUCount(t *testing.T) {
	repo, database := newGPUBenchmarkRepo(t)
	ctx := context.Background()

	agentID := cloudAgent(t, database, "mock", "RTX 4090", 0)

	identity, err := repo.ResolveCloudAgent(ctx, agentID)
	require.NoError(t, err, "an unknown count is not an error, just an unusable sample")
	assert.Nil(t, identity, "an instance with no known GPU count must not produce an observation")
}
