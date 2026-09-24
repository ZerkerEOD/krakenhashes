-- The minimum useful rental was a guess, and it contradicted the rest of the
-- system.
--
-- BudgetEngine.PlanLaunch refused any TTL under a hardcoded
-- minUsefulTTL = 5 minutes, on the reasoning that "renting a GPU for 90 seconds
-- is pure waste". The number was never derived from anything. Meanwhile
-- scheduler.ReadinessBudget() says a HEALTHY agent may legitimately take 20
-- minutes to receive its first task -- the sync gate failing open, plus one
-- benchmark and one stale-window retry -- and cloud_commissioning_grace_minutes
-- gives a brand-new instance 30 minutes before the reaper will call it wedged.
--
-- So the engine would happily sell a 5-minute rental that the scheduler
-- considers still starting up and the reaper considers still commissioning. It
-- cannot reach useful work. It is guaranteed waste, which is the exact thing
-- the floor existed to prevent.
--
-- Measured on real AWS hardware: boot -> agent registered took 138 seconds,
-- BEFORE file sync and before the benchmark. Ten minutes is a normal cold
-- start.
--
-- A RATIO, NOT A DURATION. Expressing the floor as "commissioning may be at
-- most this share of the bill" keeps it correct when either side of the
-- arithmetic moves: change the sync grace or the benchmark window and the
-- minimum rental follows automatically, instead of silently drifting away from
-- a constant nobody remembers to update. With the 20-minute readiness budget,
-- 33% puts the minimum useful rental at one hour.
--
-- WHY THE DEFAULT LEANS TOWARDS LONGER RENTALS. The two failure directions are
-- not symmetric. An over-sized TTL costs budget HEADROOM while the instance
-- runs and is refunded at teardown -- SettleInstance returns
-- reserved - incurred, and accrualDelta clamps accrual to the TTL, so nothing
-- is billed past it. An under-sized TTL costs REAL MONEY: the instance dies
-- with work left, and the next rental pays the whole commissioning cost again.
-- Given the choice, reserve too much and hand it back.
--
-- Raising this buys smaller, less efficient rentals; lowering it demands
-- longer, more efficient ones. Values above 100 are clamped -- a rental cannot
-- be more than entirely commissioning.
--
-- Zero disables the ratio rung. It does NOT disable the floor: a capability
-- floor remains underneath, derived from the readiness budget plus the teardown
-- slack plus one minimum chunk, which refuses only what provably cannot be
-- given a chunk at all. That is the same arithmetic resolveChunkDuration uses
-- to decide it must skip dispatch entirely.
--
-- Seeded rather than left absent because SystemSettingsRepository.SetSetting is
-- UPDATE-only: a key that does not exist here can never be written by an admin.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'cloud_max_commissioning_pct',
    '33',
    'The largest share of a rented instance''s bill that may be spent getting it ready to work -- booting, joining the VPN, registering, downloading wordlists and benchmarking -- before the rental is judged not worth making. This is what sets the minimum useful rental length: at the default 33%, an instance that needs about 20 minutes to become useful will not be rented for less than an hour. Raise it to allow shorter, less efficient rentals; lower it to demand longer, more efficient ones. Set to 0 to allow any rental that could take at least one chunk of work.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;
