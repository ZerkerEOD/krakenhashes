-- Admin provisioning rules: WHEN the system may spend, as distinct from
-- cloud_budget_policies' HOW MUCH.
--
-- The autoscaler previously rented after ONE 60-second tick of starvation, for
-- any job at any priority, at any hour, with no per-job ceiling. These are the
-- rails an operator needs before leaving it unattended.
--
-- WHY A NEW TABLE RATHER THAN EXTENDING cloud_budget_policies
--
-- That table is a spend-threshold LADDER whose CHECK constraint encodes
-- notify <= stop_provision <= drain <= hard_stop. These five rules are mutually
-- independent and would bolt unrelated clauses onto that constraint. It is also
-- consulted by PlanLaunch on the money path, while these are consulted earlier
-- and by a different set of callers.
--
-- OVERRIDE SEMANTICS DIFFER FROM cloud_budget_policies, DELIBERATELY
--
-- GetPolicy picks exactly one whole row (ORDER BY client_id NULLS LAST LIMIT 1)
-- and never merges, so a client override supplies every field. That is right for
-- a budget ladder: its fields are coupled by a CHECK and setting one means
-- setting all of them.
--
-- It is wrong for independent safety rails. Non-propagation is the dangerous
-- direction here -- an admin tightens the system default, and the clients that
-- most needed tightening are exactly the ones that opted out of it. So these
-- rows are merged PER FIELD in Go:
--
--   NULL on the SYSTEM DEFAULT row: the rule is not configured; it constrains
--                                   nothing.
--   NULL on an OVERRIDE row:        inherit whatever the default says.
--
-- Every rule therefore needs an in-band OFF value (0, or start = end for the
-- window) so a client can opt out of an inherited rule without a second boolean
-- column per rule, and so NULL never has to carry two meanings on one row.

CREATE TABLE IF NOT EXISTS cloud_provisioning_rules (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID UNIQUE REFERENCES clients(id) ON DELETE CASCADE,

    -- 0 = no floor. An ABSOLUTE priority, not a percentage of max_job_priority:
    -- job_executions.priority is absolute everywhere else in the system, and a
    -- percentage would silently re-qualify a different set of jobs whenever an
    -- admin moved the ceiling.
    min_job_priority INT
        CHECK (min_job_priority IS NULL OR min_job_priority >= 0),

    -- 0 = rent on the first starving tick, which is the shipped behaviour.
    min_starvation_seconds INT
        CHECK (min_starvation_seconds IS NULL OR min_starvation_seconds >= 0),

    -- 0 = never skip. A rented GPU takes minutes to boot and sync its files, so
    -- renting for a job that finishes inside that window is pure waste.
    skip_if_finishing_within_seconds INT
        CHECK (skip_if_finishing_within_seconds IS NULL OR skip_if_finishing_within_seconds >= 0),

    -- 0 = no per-job cap. Committed cloud spend attributable to one job
    -- execution over its WHOLE LIFE -- deliberately NOT month-windowed like
    -- cloud_spend_ledger's budget window, because a per-job cap that resets at a
    -- month boundary is not a per-job cap.
    max_spend_per_job_cents BIGINT
        CHECK (max_spend_per_job_cents IS NULL OR max_spend_per_job_cents >= 0),

    -- Wall-clock window in which provisioning may START.
    --   start = end  means ALWAYS (the in-band off value).
    --   end < start  WRAPS MIDNIGHT, which is the common answer to "only rent
    --                when the office is closed".
    -- Governs starts only: an instance is never torn down because the window
    -- closed. TTL and idle drain own teardown, and killing a mid-chunk instance
    -- would waste everything paid for it so far.
    provisioning_window_start TIME,
    provisioning_window_end   TIME,
    -- IANA name, evaluated in this zone so a UTC server can still express local
    -- business hours.
    provisioning_window_tz TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Both ends or neither. A half-configured window has no sane reading.
    CONSTRAINT cloud_provisioning_rules_window_pairing CHECK (
        (provisioning_window_start IS NULL) = (provisioning_window_end IS NULL)
    )
);

COMMENT ON TABLE cloud_provisioning_rules IS
    'When provisioning may happen. The row with client_id IS NULL is the system default; client rows override it PER FIELD (NULL = inherit). Distinct from cloud_budget_policies, which governs how much may be spent.';

-- client_id IS NULL is not covered by the UNIQUE above (NULLs are distinct), so
-- the single-system-default guarantee needs its own partial index -- and the
-- upsert needs it as an ON CONFLICT target.
CREATE UNIQUE INDEX IF NOT EXISTS idx_cloud_provisioning_rules_system_default
    ON cloud_provisioning_rules ((client_id IS NULL)) WHERE client_id IS NULL;

-- System default.
--
--   min_job_priority = 0            a floor is precisely the setting that
--                                   silently disables everything, so it must be
--                                   opt-in.
--   min_starvation_seconds = 180    three ticks at the default 60s autoscaler
--                                   interval. THIS CHANGES SHIPPED BEHAVIOUR:
--                                   today one starving tick is enough, so a
--                                   transient gap between chunks can cost a
--                                   full instance launch.
--   skip_if_finishing_within = 900  a rented GPU needs roughly 5-10 minutes to
--                                   boot and sync before it does any work.
--   max_spend_per_job_cents = 0     the client budget and the global monthly cap
--                                   already bound spend; a non-zero default here
--                                   would surprise.
--   window NULL                     no time restriction.
INSERT INTO cloud_provisioning_rules (
    client_id, min_job_priority, min_starvation_seconds,
    skip_if_finishing_within_seconds, max_spend_per_job_cents,
    provisioning_window_start, provisioning_window_end, provisioning_window_tz
) VALUES (NULL, 0, 180, 900, 0, NULL, NULL, 'UTC')
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Peer-host opt-in
-- ---------------------------------------------------------------------------
--
-- Structurally the same decision as cloud_burst_enabled -- "nothing bursts to
-- paid capacity by accident" -- extended one step: nothing lands on a machine
-- whose owner has root over the container by accident either.
--
-- Applies to the peer tiers only (vastai, runpod_community). AWS and RunPod
-- Secure are the operator's own account and RunPod's own SOC 2 datacentres
-- respectively, so neither is third-party hardware in the sense that matters.

ALTER TABLE job_executions
    ADD COLUMN IF NOT EXISTS cloud_allow_community_hosts BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE preset_jobs
    ADD COLUMN IF NOT EXISTS cloud_allow_community_hosts BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN job_executions.cloud_allow_community_hosts IS
    'Opt-in to peer-operated hosts (Vast.ai, RunPod Community) for this job. Without it the job simply does not see peer offers and may still rent secure capacity.';
