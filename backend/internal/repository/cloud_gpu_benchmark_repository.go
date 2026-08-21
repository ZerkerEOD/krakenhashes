package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
)

/*
 * CloudGPUBenchmarkRepository owns cloud_gpu_benchmarks: observed hashcat
 * speeds keyed by GPU MODEL rather than by agent.
 *
 * WHY A SEPARATE TABLE FROM agent_benchmarks
 *
 * agent_benchmarks answers "how fast is agent 7", which is only askable about
 * hardware that already exists. Renting decisions have to be made about
 * hardware that does not exist yet: the question is "how fast is an RTX 4090 on
 * -m 1000", so the key must be the model, not an agent id.
 *
 * The migration is explicit that this must never be folded back into
 * agent_benchmarks: "a synthetic row there would make
 * CountAgentsWithRecentBenchmark treat an invented number as corroborating
 * evidence and could quarantine real on-prem hardware."
 *
 * WHY THE ORDER IS RIGHT EVEN WHEN THIS TABLE IS EMPTY
 *
 * Ranking offers by cost per unit of WORK needs only RELATIVE throughput — the
 * absolute anchor is a constant common to every offer and cancels out of the
 * comparison. cloud.DefaultGPUClasses supplies that relative prior, and every
 * row here replaces a slice of the prior with a measurement. A cold table
 * still ranks correctly; a warm one ranks correctly AND estimates cost.
 *
 * NORMALISATION IS THE CALLER'S JOB
 *
 * gpu_model is stored as a normalised key (cloud.NormalizeGPUModel), but this
 * package cannot call that function: internal/services/cloud imports
 * internal/repository, so the reverse edge is an import cycle. Both the write
 * path and the read path must therefore normalise before they get here. A
 * caller that normalises differently produces a table that silently never
 * matches anything — cost-per-work ranking degrades to the static prior
 * forever with nothing in the logs to say so.
 */
type CloudGPUBenchmarkRepository struct {
	db *db.DB
}

// NewCloudGPUBenchmarkRepository creates a new cloud GPU benchmark repository.
func NewCloudGPUBenchmarkRepository(database *db.DB) *CloudGPUBenchmarkRepository {
	return &CloudGPUBenchmarkRepository{db: database}
}

/*
 * ewmaAlphaFloor bounds the smoothing factor from below, which is what turns a
 * plain running mean into an average with finite memory.
 *
 * The effective weight of a new sample is
 *
 *     alpha(n) = max(1/(n+1), ewmaAlphaFloor)
 *
 * where n is the number of samples already folded into the row. With the floor
 * at 0.1 that is an exact running mean for the first ten samples (alpha = 1/2,
 * 1/3, ... 1/10) and then a true exponential average with a memory of roughly
 * the last ten observations.
 *
 * WHY A FIXED ALPHA IS WRONG AT BOTH ENDS
 *
 * A fixed SMALL alpha (say 0.1) is wrong at sample 1. The row's starting value
 * is a single observation on a machine we have never seen, and 0.1 says to
 * ignore 90% of the second measurement — so one unlucky first benchmark (a
 * cold card, a throttled Vast.ai host, a noisy neighbour) governs the model's
 * estimate for the next twenty samples. A cold table is exactly when the
 * estimator is least trustworthy and most needs to move.
 *
 * A fixed LARGE alpha (say 0.5) is wrong at sample 50. By then the estimate has
 * genuine information in it, and weighting every new transient at half means
 * the number never settles: offer ranking flaps between providers from one
 * provisioning pass to the next, and the operator sees the same job quoted two
 * different prices a minute apart.
 *
 * The 1/(n+1) schedule is the minimum-variance estimator while the quantity
 * looks stationary, and the floor is what stops it from becoming an
 * infinite-memory average that can never track a real change — a provider
 * quietly swapping the physical card behind a model name, or a hashcat version
 * bump that moves throughput for one hash mode. Without the floor, alpha at
 * sample 500 is 0.002 and the table is frozen against reality.
 *
 * There is deliberately no outlier ratio check like the one
 * JobExecutionService.recordObservedSpeed applies to agent_benchmarks. That one
 * needs it: a poisoned agent_benchmarks row directly sizes the next chunk, so a
 * 10x-wrong speed hands an agent a task it cannot finish inside the chunk
 * timeout. A poisoned row here only mis-ranks an offer, and the next few real
 * samples pull it back. Adding the check would also cost the atomicity below —
 * it needs the current value, which means a read before the write.
 */
const ewmaAlphaFloor = 0.1

/*
 * ObservedRow is one measured (provider, model, count, combo) speed.
 *
 * GPUModel is the normalised key, not the provider's spelling. SaltCount is a
 * pointer because NULL (unsalted) is a distinct key from any integer, not a
 * missing value.
 */
type ObservedRow struct {
	Provider    string
	GPUModel    string
	GPUCount    int
	AttackMode  int
	HashType    int
	SaltCount   *int
	Speed       int64
	SampleCount int
	UpdatedAt   time.Time
}

/*
 * PerGPUSpeed is Speed divided across the instance's GPUs.
 *
 * Speed is whole-instance throughput, because that is what an agent reports and
 * what an instance is billed for. gpu_count is part of the key, so a 4x4090 row
 * and a 1x4090 row both exist and differ by roughly 4x. Comparing them without
 * dividing ranks the same physical card four different ways depending on how
 * many of them the offer happened to bundle.
 */
func (o ObservedRow) PerGPUSpeed() int64 {
	if o.GPUCount <= 1 {
		return o.Speed
	}
	return o.Speed / int64(o.GPUCount)
}

/*
 * CloudAgentIdentity is what a cloud agent's observed speed gets keyed by.
 *
 * GPUModel is the provider's raw spelling; the caller normalises it. See the
 * package comment above for why normalisation cannot happen in here.
 */
type CloudAgentIdentity struct {
	Provider string
	GPUModel string
	GPUCount int
}

/*
 * ResolveCloudAgent maps an agent id onto the rented hardware behind it.
 *
 * A nil identity with a nil error means "not a cloud agent, record nothing" —
 * the same convention as websocket.CloudFileSetResolver.ResolveForAgent. On-prem
 * agents are the overwhelming majority of calls on the benchmark path, so this
 * has to be an ordinary answer rather than an error the caller must classify.
 *
 * gpu_count comes from cloud_instances, which is what the OFFER promised, not
 * what the agent enumerated at runtime. That is deliberate: this table is read
 * to rank offers, and an offer is described by its provider's own numbers. A
 * row keyed on a runtime device count would not be findable from an offer.
 */
func (r *CloudGPUBenchmarkRepository) ResolveCloudAgent(ctx context.Context, agentID int) (*CloudAgentIdentity, error) {
	var provider string
	var gpuModel sql.NullString
	var gpuCount sql.NullInt32

	err := r.db.QueryRowContext(ctx, `
		SELECT pc.provider, ci.gpu_model, ci.gpu_count
		FROM agents a
		JOIN cloud_instances ci ON ci.id = a.cloud_instance_id
		JOIN cloud_provider_configs pc ON pc.id = ci.provider_config_id
		WHERE a.id = $1`, agentID).Scan(&provider, &gpuModel, &gpuCount)
	if err == sql.ErrNoRows {
		// Either the agent is on-prem (cloud_instance_id IS NULL, so the join
		// drops it) or the instance row was deleted. Both mean the same thing
		// here: there is no rented hardware to attribute this speed to.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve cloud agent %d: %w", agentID, err)
	}

	// gpu_model is nullable and is the only part of the key with no sane
	// default. Without it the row would be filed under the empty model and
	// would match no offer ever, so drop the sample instead of inventing one.
	if !gpuModel.Valid || gpuModel.String == "" {
		return nil, nil
	}

	/*
	 * gpu_count gets the SAME treatment as gpu_model: an unknown count drops
	 * the sample rather than defaulting to 1.
	 *
	 * Defaulting looks harmless and is the most expensive mistake available in
	 * this file. The stored speed is the whole box's, so filing an 8-GPU box
	 * under gpu_count=1 records one GPU as eight times faster than it is. The
	 * ranker keys on (provider, model, count), so the next genuine single-GPU
	 * offer of that model matches that row exactly and gets an 8x inflated
	 * throughput — one eighth the cost-per-work of every honest candidate. It
	 * wins every ranking pass, and sample_count keeps climbing until it
	 * graduates past the confidence floor and stops being tempered by the class
	 * prior at all.
	 *
	 * Worse when the model happens to be the reference GPU: the anchor is
	 * derived from it, so a single mis-counted row deflates EVERY other model's
	 * relative value at the same time.
	 *
	 * A dropped sample costs one observation. An inflated one corrupts the
	 * ranking until somebody notices the invoice.
	 */
	if !gpuCount.Valid || gpuCount.Int32 <= 0 {
		return nil, nil
	}

	return &CloudAgentIdentity{
		Provider: provider,
		GPUModel: gpuModel.String,
		GPUCount: int(gpuCount.Int32),
	}, nil
}

/*
 * Record folds one observed speed into the EWMA for its key, inserting the row
 * on first sight.
 *
 * The blend runs inside the ON CONFLICT clause rather than as a read, compute,
 * write from Go. Two cloud agents of the same model benchmarking the same combo
 * at the same moment is the NORMAL case — a burst provisions several identical
 * instances at once — and a read-modify-write would have both read the same
 * sample_count, both compute the same alpha, and the loser's sample would
 * vanish. The single statement makes each sample count exactly once.
 *
 * gpuModel MUST already be normalised (cloud.NormalizeGPUModel).
 */
func (r *CloudGPUBenchmarkRepository) Record(
	ctx context.Context,
	provider, gpuModel string,
	gpuCount, attackMode, hashType int,
	saltCount *int,
	speed int64,
) error {
	if provider == "" || gpuModel == "" {
		return fmt.Errorf("cloud gpu benchmark requires provider and gpu_model (got %q/%q)", provider, gpuModel)
	}
	// A zero or negative speed is not a slow GPU, it is a failed measurement,
	// and folding it in drags the model's estimate toward zero — which the
	// estimator reads as an infinite ETA and an offer that can never pay for
	// itself. Callers already gate on this; refuse it here too rather than
	// trust every future caller to remember.
	if speed <= 0 {
		return fmt.Errorf("cloud gpu benchmark speed must be positive (got %d)", speed)
	}
	// Same reasoning as ResolveCloudAgent: a defaulted count files a whole
	// box's speed against one GPU and inflates that model's throughput by the
	// real count. Refuse rather than invent.
	if gpuCount <= 0 {
		return fmt.Errorf("cloud gpu benchmark requires a known gpu_count (got %d); "+
			"defaulting it would record the whole box's speed against a single GPU", gpuCount)
	}

	/*
	 * The conflict target names all six key columns because the constraint is
	 * UNIQUE NULLS NOT DISTINCT: a NULL salt_count PARTICIPATES in uniqueness
	 * and Postgres treats two NULLs as equal here. Naming fewer columns would
	 * not match the constraint at all and the statement would fail outright.
	 *
	 * The update is written in delta form — old + alpha*(observed - old) —
	 * rather than alpha*observed + (1-alpha)*old so the alpha expression
	 * appears once. The two are algebraically identical; the second spelling
	 * invites the two copies to drift apart in a later edit.
	 */
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO cloud_gpu_benchmarks (
			provider, gpu_model, gpu_count, attack_mode, hash_type,
			salt_count, speed, sample_count, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 1, NOW())
		ON CONFLICT (provider, gpu_model, gpu_count, attack_mode, hash_type, salt_count)
		DO UPDATE SET
			speed = ROUND(
				cloud_gpu_benchmarks.speed
				+ GREATEST(1.0 / (cloud_gpu_benchmarks.sample_count + 1), $8::numeric)
				  * (EXCLUDED.speed - cloud_gpu_benchmarks.speed)
			)::BIGINT,
			sample_count = cloud_gpu_benchmarks.sample_count + 1,
			updated_at = NOW()`,
		provider, gpuModel, gpuCount, attackMode, hashType, saltCount, speed, ewmaAlphaFloor,
	)
	if err != nil {
		return fmt.Errorf("failed to record cloud gpu benchmark (%s/%s x%d, mode %d, type %d): %w",
			provider, gpuModel, gpuCount, attackMode, hashType, err)
	}
	return nil
}

/*
 * List returns every observed row for one attack mode / hash type / salt count.
 *
 * salt_count is matched with IS NOT DISTINCT FROM so the read keys the row the
 * same way the UNIQUE NULLS NOT DISTINCT constraint does. Matching with `=`
 * would silently return nothing for every unsalted hash type, which is most of
 * them, and the caller would fall back to the static prior forever.
 *
 * The match is exact rather than nearest-salt-count on purpose: salted
 * throughput is roughly inversely proportional to salt count, so a 10-salt
 * measurement is not an approximation of a 10,000-salt job, it is wrong by
 * three orders of magnitude. Better to have no observation and use the prior.
 */
func (r *CloudGPUBenchmarkRepository) List(ctx context.Context, attackMode, hashType int, saltCount *int) ([]ObservedRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT provider, gpu_model, gpu_count, attack_mode, hash_type,
		       salt_count, speed, sample_count, updated_at
		FROM cloud_gpu_benchmarks
		WHERE attack_mode = $1
		  AND hash_type = $2
		  AND salt_count IS NOT DISTINCT FROM $3
		ORDER BY provider, gpu_model, gpu_count`,
		attackMode, hashType, saltCount)
	if err != nil {
		return nil, fmt.Errorf("failed to list cloud gpu benchmarks (mode %d, type %d): %w", attackMode, hashType, err)
	}
	defer rows.Close()

	var out []ObservedRow
	for rows.Next() {
		var row ObservedRow
		var salt sql.NullInt32
		if err := rows.Scan(
			&row.Provider, &row.GPUModel, &row.GPUCount, &row.AttackMode,
			&row.HashType, &salt, &row.Speed, &row.SampleCount, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan cloud gpu benchmark row: %w", err)
		}
		if salt.Valid {
			v := int(salt.Int32)
			row.SaltCount = &v
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate cloud gpu benchmarks: %w", err)
	}
	return out, nil
}
