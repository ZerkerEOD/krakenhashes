-- Roll back scheduling_units. The trigger and indexes drop with the table;
-- listed explicitly for clarity and safety on partial-failure replays.

DROP TRIGGER IF EXISTS update_scheduling_units_updated_at ON scheduling_units;
DROP INDEX IF EXISTS idx_scheduling_units_dispatch;
DROP INDEX IF EXISTS idx_scheduling_units_parent;
DROP TABLE IF EXISTS scheduling_units;
