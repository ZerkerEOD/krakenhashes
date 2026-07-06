# Database migrations

Applied automatically on backend startup by `RunMigrations`
(`backend/internal/database/database.go`) via
[golang-migrate](https://github.com/golang-migrate/migrate). Each migration is a pair:
`<version>_<description>.up.sql` and `<version>_<description>.down.sql`.

## Naming: use a UTC timestamp for NEW migrations

Historically migrations used a zero-padded sequential counter (`000001` … `000165`). That
collides when two feature branches each grab "the next number" — both then claim the same
version, and the DB can end up ahead of whichever branch you build next (which used to brick
local startup).

**Going forward, name new migrations with a UTC timestamp instead of the next integer:**

```
YYYYMMDDHHMMSS_short_description.up.sql
YYYYMMDDHHMMSS_short_description.down.sql
# e.g. 20260706143000_add_widget_flag.up.sql
```

A timestamp is always larger than the old 6-digit versions, so it sorts after them, and two
branches created minutes apart never collide. Once you switch to timestamps, don't go back to
sequential numbers — a new `000166` would sort *before* the timestamped migrations.

Keep migrations additive and idempotent where you can (`ADD COLUMN IF NOT EXISTS`,
`ALTER TYPE ... ADD VALUE IF NOT EXISTS`). Postgres enum values can't be removed, so their
`.down.sql` is a no-op with an explanatory comment.

## Dev tolerance for a "database ahead" state

golang-migrate is strictly linear and refuses to start when the database records a version
that has no migration file in the running build — which happens in development when you switch
to a branch that lacks a migration another branch already applied to the shared dev database.
To keep that from bricking local startup, `RunMigrations` tolerates this case in **dev only**:

- Enabled implicitly when `DEBUG=true` (the dev default), or explicitly with
  `KH_MIGRATE_ALLOW_AHEAD=true`.
- It logs a warning and continues **without** applying migrations.
- Production (`DEBUG=false`, flag unset) keeps this fatal — a DB ahead of the deployed code is
  a real problem there and should stop startup.

To instead realign a dev database's marker with the current branch, set
`schema_migrations.version` to the branch's highest migration number and `dirty=false`, e.g.:

```sql
UPDATE schema_migrations SET version = 164, dirty = false;
```
