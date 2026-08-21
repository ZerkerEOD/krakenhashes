package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

/*
 * CloudProvisioningRulesRepository owns cloud_provisioning_rules: WHEN paid
 * capacity may be rented, as distinct from CloudBudgetRepository's HOW MUCH.
 *
 * WHY THIS DELIBERATELY DOES NOT COPY GetPolicy
 *
 * CloudBudgetRepository.GetPolicy resolves a client with
 * ORDER BY client_id NULLS LAST LIMIT 1: one whole row wins and nothing is
 * merged. That is correct there. The budget ladder's fields are coupled by a
 * CHECK (notify <= stop_provision <= drain <= hard_stop), so an operator who
 * sets a client's thresholds means all of them, and a half-inherited ladder
 * would not even satisfy the constraint.
 *
 * It is wrong here. These rules are mutually independent safety rails, and the
 * dangerous direction is non-propagation: an admin tightens
 * min_starvation_seconds on the system default, and the clients that already
 * had an override — which is to say the clients someone had a specific reason
 * to single out — never see it. The tightening reaches everyone except the
 * accounts it was aimed at, and the invoice arrives from exactly those.
 *
 * So GetRules reads BOTH rows and merges them PER FIELD through
 * models.MergeProvisioningRules:
 *
 *   NULL on the SYSTEM DEFAULT row: rule not configured; constrains nothing.
 *   NULL on an OVERRIDE row:        inherit whatever the default says.
 *
 * Every rule has an in-band OFF value (0, or start == end for the window), so a
 * client opts out of an inherited rule by writing that value rather than by
 * NULLing the field. That is what lets NULL carry exactly one meaning per row
 * kind, and it is why every rule field on the model is a pointer: an
 * implementation that reads zero as "unset" turns a deliberate opt-out back
 * into the inherited value with nothing in the logs to say so.
 */
type CloudProvisioningRulesRepository struct {
	db *db.DB
}

// NewCloudProvisioningRulesRepository creates a new cloud provisioning rules
// repository.
func NewCloudProvisioningRulesRepository(database *db.DB) *CloudProvisioningRulesRepository {
	return &CloudProvisioningRulesRepository{db: database}
}

/*
 * provisioningWindowFormat is the canonical string form of a window bound:
 * 24-hour, zero-padded, seconds always present. "09:00:00" — never "9:00",
 * never "9:00 AM".
 *
 * THE READ MUST GO THROUGH to_char, NOT A BARE COLUMN
 *
 * lib/pq decodes a TIME column into a time.Time (encode.go maps oid.T_time
 * through mustParse("15:04:05", ...)), and database/sql's convertAssign then
 * renders a time.Time into a *string as RFC3339Nano. Selecting
 * provisioning_window_start directly into a string therefore yields
 * "0000-01-01T09:00:00Z". That value writes back to Postgres without complaint
 * and only fails much later, when whatever evaluates the window cannot parse it
 * and quietly concludes the window is shut — an autoscaler that has silently
 * stopped provisioning, with a stored value that looks fine in the table.
 * Asking Postgres for to_char(col, 'HH24:MI:SS') hands back TEXT and removes
 * the driver conversion from the path entirely.
 *
 * to_char is also preferred over col::text because ::text preserves fractional
 * seconds: '09:00:00.75'::time::text is "09:00:00.75", which would not compare
 * equal to the "09:00:00" that was written.
 */
const provisioningWindowFormat = "15:04:05"

/*
 * provisioningWindowInputFormats are accepted on WRITE and normalised to
 * provisioningWindowFormat before they reach the database, so the stored value
 * and the read format can never disagree.
 *
 * A UI time picker emits "22:30". Storing that verbatim and reading back
 * "22:30:00" makes every save-then-compare look like a pending change, and any
 * caller that diffs the two to decide whether an update is needed writes
 * forever.
 */
var provisioningWindowInputFormats = []string{"15:04:05", "15:04"}

/*
 * provisioningRulesColumns is shared by every read so the SELECT list and
 * scanProvisioningRules cannot drift apart. Every rule column is nullable and
 * NULL is load-bearing on both row kinds, so the scan order silently shifting
 * by one would not fail — it would swap two rules' values.
 */
const provisioningRulesColumns = `
		id, client_id, min_job_priority, min_starvation_seconds,
		skip_if_finishing_within_seconds, max_spend_per_job_cents,
		to_char(provisioning_window_start, 'HH24:MI:SS'),
		to_char(provisioning_window_end, 'HH24:MI:SS'),
		provisioning_window_tz, created_at, updated_at`

// scanProvisioningRules reads one row into the model. Each rule lands in a
// Null* first and only becomes a non-nil pointer when the column actually held
// a value, because "not configured" and "set to zero" are different answers.
func scanProvisioningRules(scanner rowScanner) (*models.CloudProvisioningRules, error) {
	rules := &models.CloudProvisioningRules{}

	var clientID uuid.NullUUID
	var minPriority, minStarvation, skipIfFinishing sql.NullInt32
	var maxSpendPerJob sql.NullInt64
	var windowStart, windowEnd, windowTZ sql.NullString

	err := scanner.Scan(
		&rules.ID, &clientID, &minPriority, &minStarvation,
		&skipIfFinishing, &maxSpendPerJob, &windowStart, &windowEnd,
		&windowTZ, &rules.CreatedAt, &rules.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if clientID.Valid {
		rules.ClientID = &clientID.UUID
	}
	if minPriority.Valid {
		v := int(minPriority.Int32)
		rules.MinJobPriority = &v
	}
	if minStarvation.Valid {
		v := int(minStarvation.Int32)
		rules.MinStarvationSeconds = &v
	}
	if skipIfFinishing.Valid {
		v := int(skipIfFinishing.Int32)
		rules.SkipIfFinishingWithinSeconds = &v
	}
	if maxSpendPerJob.Valid {
		v := maxSpendPerJob.Int64
		rules.MaxSpendPerJobCents = &v
	}
	if windowStart.Valid {
		v := windowStart.String
		rules.ProvisioningWindowStart = &v
	}
	if windowEnd.Valid {
		v := windowEnd.String
		rules.ProvisioningWindowEnd = &v
	}
	if windowTZ.Valid {
		v := windowTZ.String
		rules.ProvisioningWindowTZ = &v
	}

	return rules, nil
}

/*
 * GetRules returns the rules in force for one client: the system default with
 * the client's override layered on top, field by field.
 *
 * Both rows come back from ONE statement rather than two reads. Two round-trips
 * outside a transaction can straddle an admin's UpsertRules and merge a
 * pre-update default under a post-update override, yielding a combination that
 * was never configured and that cannot be reproduced from the table afterwards
 * — the worst possible shape for a rule that decides whether to spend money.
 *
 * A clientID of uuid.Nil resolves to the bare system default: clients.id
 * defaults to gen_random_uuid so no client can hold the nil UUID, and the
 * default row is not matched by `client_id = $1` in any case.
 */
func (r *CloudProvisioningRulesRepository) GetRules(ctx context.Context, clientID uuid.UUID) (*models.CloudProvisioningRules, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+provisioningRulesColumns+`
		FROM cloud_provisioning_rules
		WHERE client_id IS NULL OR client_id = $1`, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud provisioning rules: %w", err)
	}
	defer rows.Close()

	var systemDefault, override *models.CloudProvisioningRules
	for rows.Next() {
		row, err := scanProvisioningRules(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cloud provisioning rules: %w", err)
		}
		if row.ClientID == nil {
			systemDefault = row
			continue
		}
		override = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read cloud provisioning rules: %w", err)
	}

	// Same failure GetPolicy names, for the same reason: the migration seeds
	// this row, so its absence means the migration did not run or something
	// truncated it. Substituting an empty default would be worse than failing —
	// under these semantics every nil field means "constrains nothing", so the
	// autoscaler would be handed permission to rent for any job, at any
	// priority, at any hour, with no per-job ceiling.
	if systemDefault == nil {
		return nil, fmt.Errorf("no cloud provisioning rules found (the system default row is missing)")
	}

	merged := models.MergeProvisioningRules(systemDefault, override)
	if merged == nil {
		return nil, fmt.Errorf("failed to merge cloud provisioning rules for client %s", clientID)
	}

	/*
	 * Stamp the identity of the CLIENT ASKED ABOUT, so this view can never be
	 * mistaken for the system-default row.
	 *
	 * With no override the merge is a copy of the default — including
	 * ClientID == nil. That is a live footgun on the obvious admin flow:
	 * GetRules(clientX) to populate a per-client form, admin edits a field,
	 * UpsertRules(result). UpsertRules picks its ON CONFLICT target off exactly
	 * this nil, so it would write the SYSTEM DEFAULT ROW — silently changing
	 * the policy for every client while the screen that did it says "saved" and
	 * names one.
	 *
	 * Overwriting ClientID makes the wrong outcome unreachable rather than
	 * merely unlikely: a round trip now materialises the inherited values into
	 * clientX's own override, which is what the screen claimed to be doing. The
	 * stored-row ID goes with it — an ID belonging to the default row invites
	 * an ID-keyed UPDATE of the wrong row for the same reason.
	 *
	 * uuid.Nil is left alone: no client can hold it (clients.id defaults to
	 * gen_random_uuid), so that call is an explicit request for the bare
	 * default and stamping it would only forge an FK violation.
	 */
	if clientID != uuid.Nil {
		id := clientID
		merged.ClientID = &id
		if override == nil {
			merged.ID = uuid.Nil
		}
	}

	return merged, nil
}

/*
 * GetSystemDefault returns the system default row UNMERGED.
 *
 * This is the admin-editing view. An admin changing the defaults has to see
 * what the default itself says; feeding them a merged result and saving it back
 * would bake one client's override into the defaults for everyone.
 *
 * Every provisioning decision must use GetRules instead — this row alone is not
 * what any particular client is subject to.
 */
func (r *CloudProvisioningRulesRepository) GetSystemDefault(ctx context.Context) (*models.CloudProvisioningRules, error) {
	rules, err := scanProvisioningRules(r.db.QueryRowContext(ctx, `
		SELECT `+provisioningRulesColumns+`
		FROM cloud_provisioning_rules
		WHERE client_id IS NULL`))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no cloud provisioning rules found (the system default row is missing)")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get default cloud provisioning rules: %w", err)
	}
	return rules, nil
}

/*
 * GetClientOverride returns a client's own row UNMERGED, or (nil, nil) when the
 * client has no override at all.
 *
 * This is the only way to see which fields a client has ACTUALLY SET. GetRules
 * merges, so a value inherited from the default and a value the client set to
 * the same number are indistinguishable in its result — and they behave
 * differently the moment an admin edits the default, since only the inherited
 * one follows. An editing UI that cannot tell them apart will silently convert
 * inheritance into a pinned copy the first time anyone saves the form.
 *
 * A nil result is "no override", not an error: inheriting everything is the
 * normal state for most clients.
 */
func (r *CloudProvisioningRulesRepository) GetClientOverride(ctx context.Context, clientID uuid.UUID) (*models.CloudProvisioningRules, error) {
	if clientID == uuid.Nil {
		// uuid.Nil names the system default in this table's other methods, and
		// no client can hold it. Returning "no override" is the honest answer
		// and keeps this from becoming a second path to the default row.
		return nil, nil
	}
	rules, err := scanProvisioningRules(r.db.QueryRowContext(ctx, `
		SELECT `+provisioningRulesColumns+`
		FROM cloud_provisioning_rules
		WHERE client_id = $1`, clientID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud provisioning rules override for client %s: %w", clientID, err)
	}
	return rules, nil
}

/*
 * UpsertRules writes a client's override, or the system default when
 * rules.ClientID is nil. On success rules.ID, CreatedAt and UpdatedAt carry the
 * stored row, so a caller can tell an update from an insert by whether the ID
 * changed.
 *
 * TWO CONFLICT TARGETS, THE SAME TRICK AS UpsertPolicy
 *
 * The system default row has client_id NULL, and the UNIQUE constraint on
 * client_id does not cover it — NULLs are distinct, so every insert of a NULL
 * client_id is a fresh, non-conflicting row. ON CONFLICT (client_id) therefore
 * cannot target it: the statement never takes the DO UPDATE branch, proceeds as
 * a plain INSERT, and trips idx_cloud_provisioning_rules_system_default
 * instead, so the admin's edit fails outright. Were that partial index ever
 * dropped, the same statement would silently create a SECOND system default and
 * GetRules would then merge under whichever of the two it happened to scan
 * last, making the edit appear to apply or not for reasons invisible from the
 * table. The partial index is the only reachable conflict target for that row.
 *
 * THIS REPLACES THE WHOLE ROW
 *
 * Every column is written from the struct, NULLs included. That is required:
 * a DO UPDATE SET that skipped nil fields would let an admin change an
 * override's value but never hand the field back to the default, so the first
 * override of a rule would be permanent.
 *
 * The consequence for callers is that the struct passed here must be an
 * override, never a merged result. Round-tripping GetRules output back through
 * UpsertRules materialises every inherited default into the client's own row
 * and permanently disconnects that client from future tightening — the exact
 * failure this table's per-field merge exists to prevent. Edit against
 * GetSystemDefault for the defaults, and against a raw override read for a
 * client.
 *
 * Validation runs first for the same reason as UpsertPolicy's: the CHECK
 * constraints enforce the same things, but a raw constraint-violation string is
 * not something to put in front of an admin.
 */
func (r *CloudProvisioningRulesRepository) UpsertRules(ctx context.Context, rules *models.CloudProvisioningRules) error {
	if rules == nil {
		return fmt.Errorf("cloud provisioning rules must not be nil")
	}

	if rules.MinJobPriority != nil && *rules.MinJobPriority < 0 {
		return fmt.Errorf("minimum job priority must not be negative, got %d", *rules.MinJobPriority)
	}
	if rules.MinStarvationSeconds != nil && *rules.MinStarvationSeconds < 0 {
		return fmt.Errorf("minimum starvation seconds must not be negative, got %d", *rules.MinStarvationSeconds)
	}
	if rules.SkipIfFinishingWithinSeconds != nil && *rules.SkipIfFinishingWithinSeconds < 0 {
		return fmt.Errorf("skip-if-finishing-within seconds must not be negative, got %d", *rules.SkipIfFinishingWithinSeconds)
	}
	if rules.MaxSpendPerJobCents != nil && *rules.MaxSpendPerJobCents < 0 {
		return fmt.Errorf("maximum spend per job must not be negative, got %d", *rules.MaxSpendPerJobCents)
	}

	// Both ends or neither, matching cloud_provisioning_rules_window_pairing. A
	// lone start is an unfinished sentence and inventing the missing end would
	// invent a policy nobody chose. MergeProvisioningRules cannot express a
	// half-window either — it layers the window as a unit precisely so a
	// default's start never pairs with an override's end.
	if (rules.ProvisioningWindowStart == nil) != (rules.ProvisioningWindowEnd == nil) {
		return fmt.Errorf("provisioning window needs both a start and an end, or neither")
	}

	var windowStart, windowEnd sql.NullString
	if rules.ProvisioningWindowStart != nil {
		start, err := normalizeProvisioningWindowBound("provisioning window start", *rules.ProvisioningWindowStart)
		if err != nil {
			return err
		}
		end, err := normalizeProvisioningWindowBound("provisioning window end", *rules.ProvisioningWindowEnd)
		if err != nil {
			return err
		}
		windowStart = sql.NullString{String: start, Valid: true}
		windowEnd = sql.NullString{String: end, Valid: true}
	}

	var windowTZ sql.NullString
	if rules.ProvisioningWindowTZ != nil {
		// An empty string is stored as NULL rather than as itself. A non-nil
		// pointer to "" would be a third state the merge cannot see: it reads
		// as "configured" and so blocks inheritance, while meaning nothing at
		// evaluation time.
		if tz := strings.TrimSpace(*rules.ProvisioningWindowTZ); tz != "" {
			// Rejected at write time rather than at evaluation time: a zone that
			// does not load is otherwise discovered by the autoscaler, which runs
			// unattended and — for anyone using a window at all — usually at
			// night. The runtime image installs tzdata, so real IANA names load.
			if _, err := time.LoadLocation(tz); err != nil {
				return fmt.Errorf("provisioning window timezone %q is not a known IANA name: %w", tz, err)
			}
			windowTZ = sql.NullString{String: tz, Valid: true}
		}
	}

	conflictTarget := "(client_id)"
	if rules.ClientID == nil {
		conflictTarget = "((client_id IS NULL)) WHERE client_id IS NULL"
	}

	err := r.db.QueryRowContext(ctx, `
		INSERT INTO cloud_provisioning_rules (
			client_id, min_job_priority, min_starvation_seconds,
			skip_if_finishing_within_seconds, max_spend_per_job_cents,
			provisioning_window_start, provisioning_window_end, provisioning_window_tz
		) VALUES ($1,$2,$3,$4,$5,$6::time,$7::time,$8)
		ON CONFLICT `+conflictTarget+` DO UPDATE SET
			min_job_priority                 = EXCLUDED.min_job_priority,
			min_starvation_seconds           = EXCLUDED.min_starvation_seconds,
			skip_if_finishing_within_seconds = EXCLUDED.skip_if_finishing_within_seconds,
			max_spend_per_job_cents          = EXCLUDED.max_spend_per_job_cents,
			provisioning_window_start        = EXCLUDED.provisioning_window_start,
			provisioning_window_end          = EXCLUDED.provisioning_window_end,
			provisioning_window_tz           = EXCLUDED.provisioning_window_tz,
			updated_at                       = NOW()
		RETURNING id, created_at, updated_at`,
		rules.ClientID, rules.MinJobPriority, rules.MinStarvationSeconds,
		rules.SkipIfFinishingWithinSeconds, rules.MaxSpendPerJobCents,
		windowStart, windowEnd, windowTZ,
	).Scan(&rules.ID, &rules.CreatedAt, &rules.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to save cloud provisioning rules: %w", err)
	}
	return nil
}

/*
 * DeleteClientRules removes a client's override so the client falls back to the
 * system default. Deleting an override that is already gone is not an error:
 * the caller's intent — "this client uses the defaults" — is already true.
 *
 * Deleting the system default is refused. GetRules has no further fallback, so
 * it would start failing for EVERY client rather than just this one, and the
 * autoscaler would lose the ability to answer "may I provision?" at all.
 *
 * uuid.Nil is the only way this signature can name that row, and passing it
 * would otherwise be worse than an error: `client_id = $1` never matches a NULL
 * client_id, so an admin trying to reset the defaults would get silent success
 * and an unchanged table. Use UpsertRules with a nil ClientID to change them.
 */
func (r *CloudProvisioningRulesRepository) DeleteClientRules(ctx context.Context, clientID uuid.UUID) error {
	if clientID == uuid.Nil {
		return fmt.Errorf("refusing to delete the system default cloud provisioning rules: every client resolves through them, use UpsertRules with a nil client to change the defaults")
	}

	_, err := r.db.ExecContext(ctx,
		`DELETE FROM cloud_provisioning_rules WHERE client_id = $1`, clientID)
	if err != nil {
		return fmt.Errorf("failed to delete cloud provisioning rules override: %w", err)
	}
	return nil
}

/*
 * normalizeProvisioningWindowBound converts an accepted input form to
 * provisioningWindowFormat, so what is stored always matches what the read
 * returns.
 *
 * "24:00" is rejected even though Postgres accepts '24:00:00'::time. The
 * canonical layout cannot represent it, so it would come back out of to_char as
 * "24:00:00" and fail to re-parse in every consumer that uses the same layout.
 * Midnight is "00:00", and an always-open window is start == end — the in-band
 * OFF value the migration specifies.
 */
func normalizeProvisioningWindowBound(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	for _, layout := range provisioningWindowInputFormats {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.Format(provisioningWindowFormat), nil
		}
	}
	return "", fmt.Errorf("%s must be a 24-hour time such as 09:00 or 09:00:00, got %q", field, value)
}
