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

## ⚠️ Before merging: your migration must sort ABOVE master's newest

Timestamps fix *collisions* but they make *interleaving* more likely, because merge order has
nothing to do with authoring order. This is the failure that matters, and it is silent:

golang-migrate stores **one scalar** `schema_migrations.version` and walks strictly forward.
A migration numbered **below** a database's current version is **never applied and never
reported** — `Up()` returns `ErrNoChange` and the backend logs "Database schema is already up
to date."

So if you branch in August, master ships a migration while you work, and you merge afterwards,
your migration is numbered below master's and:

- it installs perfectly on a **fresh** database (everything sorts in order), and
- it is **silently skipped** on every database that already tracks master's newer version.

Whatever it created simply does not exist. The first later migration that references it fails,
leaving the database **dirty** and the backend refusing to start
(`RunMigrations` returns an error, `cmd/server/main.go` exits 1).

**This happened.** The cloud-provisioning branch was authored 13–21 August while master shipped
`20260815160100`; five of its seven migrations sorted below that and were renumbered to
`20260822090000`–`20260822090600` before merging.

**Rule: before you merge, check that every migration you added sorts above the highest
migration on master. If not, rename it — and its `.down.sql` — preserving the relative order of
your own migrations, since they may depend on each other.** CI enforces this
(`.github/workflows/test.yml`, "Migrations must sort above master's newest"); the up/down job
cannot, because it runs against a fresh database where out-of-order is impossible by
construction.

Renaming has one consequence worth knowing: any database that already applied the old numbers
now records a version with no file, which is the "database ahead" state below. Realign it or
recreate it.

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
