# Network Share Storage

## Overview

KrakenHashes can keep the master copy of its **wordlists and rules** on a network share (NFS or SMB/CIFS) instead of local disk, and can assign each agent a **storage tier** that decides whether it caches every list, downloads lists per job, or reads them straight off a mounted share. The goals are:

1. **Smaller server disks.** A server with a 40 GB wordlist corpus no longer needs that corpus on local SSD — it reads it from the share.
2. **Thin agents.** An on-prem agent on the same share can run jobs without ever downloading a wordlist.
3. **No disruption to the existing path.** The default is unchanged: local storage, and agents that download and cache everything.

This document describes how the pieces fit together. For the operator-facing walkthrough see [Storage Architecture](../../admin-guide/resource-management/storage.md#network-share-storage); for the agent-side knobs see [Agent Configuration](../../agent-guide/configuration.md#storage-tier-network-share-feature).

## Two storage backends

Whether the server serves wordlists/rules from local disk or the share is a single piece of **database state** — `network_shares.storage_backend`, one of `local` or `share` (`models.StorageBackendLocal` / `models.StorageBackendShare`). It is **not** an environment variable, so a restart simply reads the current backend; there are no transitional env flags to set or unset. The value is flipped only by a completed migration (see [Migration lifecycle](#migration-lifecycle)).

Only `wordlists/` and `rules/` are ever share-backed. These always stay local regardless of backend:

- hashlists, binaries, charsets, and uploads;
- the global potfile master (it is mutable, so it is always served over HTTP via the existing `?bytes=N` size-snapshot mechanism — never read directly off the share);
- client wordlists (`wordlists/clients/…`) and association wordlists (`wordlists/association/…`), which are per-task and download over HTTP regardless of tier.

### The path resolver

All path resolution goes through the process-global `internal/storagepaths` package, so a runtime backend flip is picked up everywhere without re-plumbing call sites:

| Function | Purpose |
|----------|---------|
| `Initialize(dataDir, shareDir, initialBackend)` | Called once at startup (after config, then again after migrations with the DB backend). |
| `WordlistsRoot()` / `RulesRoot()` | The active root for wordlists/rules — the share path when the backend is `share`, else local. |
| `LocalWordlistsDir()` / `LocalRulesDir()` | Always-local roots, used for client/association wordlists that never move. |
| `Backend()` / `SetBackend(backend)` | Read / atomically flip the active backend (the migration calls `SetBackend`). |
| `ShareDir()` / `ShareConfigured()` | The configured `KH_SHARE_DIR` mount path, and whether one is set. |
| `ShareHealthy()` | `true` unless the backend is `share` and the mount fails to stat — used to treat a share outage as transient rather than as missing files. |

Serving (`routes/filesync.go`), the wordlist/rule managers, the directory monitor, the potfile service, and the job/preset services all resolve through these helpers.

## Server side

### Mounting the share

The **server consumes** the share; it never mounts or manages it. You mount the share on the host (or inside the container) via `docker-compose` and point the backend at that path:

- `KH_SHARE_DIR` — the in-container path the backend reads (`config.Config.ShareDir`). Empty ⇒ no share, local-only.
- `KH_SHARE_DIR_HOST` — the host path (or an already-mounted network path) bind-mounted to `KH_SHARE_DIR` in compose.

KrakenHashes stores **no share credentials**: the server's live in your compose/mount config, and each agent's are entered by its operator at mount time. The backend only ever reads/writes files under the resolved roots.

### Admin API

The Storage panel (**Admin → System Settings → Storage**) is backed by `handlers/admin/networkshare`, wired in `routes/admin.go` before the generic `/settings/{key}` route:

| Method & path | Handler | Purpose |
|---------------|---------|---------|
| `GET /api/admin/settings/network-share` | `GetConfig` | Current config + `share_dir`, health, backend, migration state. |
| `PUT /api/admin/settings/network-share` | `UpdateConfig` | Save the (non-secret) share coordinates + enable flag. |
| `POST /api/admin/settings/network-share/validate` | `Validate` | Stat the mount, confirm readable `wordlists/`+`rules/`, write/throughput probe, free space. |
| `POST /api/admin/settings/network-share/migrate` | `StartMigration` | Begin a `to_share` / `to_local` migration. |
| `GET /api/admin/settings/network-share/migration` | `MigrationStatus` | Live migration progress snapshot. |

## Migration lifecycle

Switching backends is done by an in-process, admin-triggered migration — no restart, no manual file copying. It is driven by `models.MigrationState` and coordinated by two pieces:

- **`internal/migrationgate`** — a dependency-free global gate the rest of the app consults: `Phase()`, `DispatchPaused()` (draining/migrating/validating), `WritesFrozen()` (migrating/validating), `MaintenanceActive()`, and a live `Progress` snapshot (`Get`/`Set`). The scheduler's cycle returns early when `DispatchPaused()`; `AddWordlist`/`AddRule` reject when `WritesFrozen()`.
- **`services.MigrationEngine`** (`networkshare_migration.go`) — `Start(direction)` validates preconditions (share enabled + mounted + not already on the target) and runs the phases in a background goroutine.

| State (`migration_state`) | What happens | Availability |
|---------------------------|--------------|--------------|
| `draining` | Wait for in-flight tasks to finish and agents to reconnect. Floor = `drainReconnectFloor()` (12 min default, override `KH_MIGRATION_DRAIN_FLOOR_SECONDS`, sized to the agent reconnect back-off), refined by the longest-running task's ETA. | Operational; no new dispatch. |
| `migrating` | Writes frozen. The worker enumerates `wordlists/`+`rules/` (excluding `clients/` and `association/`) and copies to the target via a temp file + atomic rename, skipping any file already present and byte-identical by MD5 (resumable/idempotent). | Reads continue; uploads + dispatch paused. |
| `validating` | Every migrated file is re-verified by MD5 against the source. | Still locked. |
| `completed` | `storagepaths.SetBackend(target)` flips the backend; writes, uploads and dispatch resume. | Normal, serving from the new backend. |
| `failed` | **Backend left unchanged**; gates released so the system resumes on the original storage. | Normal (original backend). |

The same engine runs in reverse (`to_local`) for rollback or decommission. Source files are **never deleted** after a successful migration — the operator reclaims that space manually once satisfied. Because a failure leaves the backend untouched, an interrupted or failed migration is always safe.

!!! note "Maintenance page"
    The current build gates reads/writes/dispatch as above and surfaces progress in the Storage tab; an app-wide full-screen maintenance landing page was intentionally not added (it risked locking admins out).

## Per-agent storage tiers

Each agent carries `storage_tier` (`agents.storage_tier`, default `full_cache`) and `network_share_mount_path`. The three tiers (`models.StorageTier*`):

| Tier | Agent behavior |
|------|----------------|
| `full_cache` | Download over HTTP and keep every list. Pre-feature behavior. |
| `on_demand` | Download per task over HTTP, then LRU-evict wordlist/rule files via `evictOnDemandCache` once free space drops below `KH_AGENT_ONDEMAND_TARGET_FREE_GB` (default 20 GiB). Files in use by a running task, client/potfile/ephemeral files, are never evicted. |
| `network_direct` | Read immutable wordlists/rules directly off the mounted share — no download. **On-prem only.** |

### Agent runtime

The `JobManager` holds the active tier/mount (`SetStorageConfig`, `StorageTier()`, `NetworkShareMountPath()`), and `immutableFileBase()` returns the share mount for `network_direct` and the local data dir otherwise. For `network_direct`, `ensureWordlists`/`ensureRules` become verify-only via `verifyImmutableFiles` (stat under the mount, no download; `wordlists/clients/` always skipped). The executor joins wordlist/rule paths against `immutableFileBase`, while hashlist/charset/potfile/client paths stay on the local data dir.

A file missing on the mount makes the agent return an error tagged with `jobs.ShareNotReadyMarker` (`"network share not ready"`), which the connection layer turns into a `task_assignment_rejected` so the scheduler re-dispatches elsewhere — it never fails the job.

### Mount detection & guards

- `IsNetworkFS(path)` detects a network filesystem per OS: `mountfs_linux.go` (statfs magics NFS/SMB/CIFS/SMB2), `mountfs_darwin.go` (fstypename), `mountfs_other.go` (Windows/other → undetermined). Native Windows `DRIVE_REMOTE` detection is not yet implemented, so the guard does not block on Windows.
- The agent **refuses to start if its own binary is on a network mount** — only the data path may be a network share.
- `network_direct` agents report mount liveness as `share_ready` on their `agent_status`.

## Configuration delivery (seed-only)

Two mechanisms set an agent's tier, in this order of authority:

1. **Seed at registration (WS9).** `--storage-tier` / `--network-share-mount-path` (or `KH_STORAGE_TIER` / `KH_NETWORK_SHARE_MOUNT_PATH`) set the agent's initial tier/mount, reported in the registration request and recorded as the agent's starting value. Precedence on the agent is flag > env > default; an invalid tier, or `network_direct` with no mount path, clamps to `full_cache` (both on the agent and on the server) so a misconfigured agent still registers.
2. **Admin UI, authoritative thereafter.** The tier shown in the Storage panel / agent detail page is pushed to the agent via the `config_update` message (`handlers/websocket/handler.go` → `storage_tier` + `network_share_mount_path` from the DB row), which fires on **every** connect. So after registration the server/DB wins and re-asserts its value on each reconnect.

There is no provenance column and no agent-side lock: the seed is a convenience for provisioning, and the UI remains the source of truth.

## Scheduling interaction

- **Dispatch readiness (WS1).** A preparing task (`assigned`, no first progress) is measured against `task_startup_grace_seconds` (default 600) instead of the 120 s heartbeat, so a long multi-GB file pull isn't evicted before it starts. The agent emits `task_loading` pings during the prep chain to refresh activity.
- **Share-ready gating.** The scheduler's `WSSender.IsShareReady(agentID)` excludes an agent that has **explicitly** reported `share_ready=false` (its mount is down) — fail-closed for share-dependent agents, fail-open otherwise. `getIdleAgents` applies this.
- **Locality tiebreak (WS7).** `WSSender.AgentHeldFiles(agentID)` feeds `localityScore(agent, unit)` in the allocator: among equally-eligible agents, prefer one that already holds the unit's files (or is `network_direct`, which reads them off the share). It is a pure tiebreak — it never changes priority, `max_agents`, or overflow fairness, and never starves a unit.

## Latency & high-latency shares

`network_direct` targets a **low-latency, LAN-adjacent share**. A remote share (another building/state/country) should use the download tiers (`full_cache`/`on_demand`) over the HTTP relay instead — the same stance as cloud agents — because the relay retries and tolerates a slow link, whereas a kernel mount does not and every job reads the whole list off it before hashcat starts. The system still hardens `network_direct` so a slow-but-working share degrades rather than fails:

- **No blocking mount touch (C1).** Every latency-sensitive mount stat is bounded. The agent's `shareReady()` reads an `atomic.Bool` updated by a background `monitorShareHealth` poller that does a goroutine+`select`-bounded stat on its own ticker — it never runs a bare `os.Stat` on the `writePump`/heartbeat path, so a hung mount can't freeze the connection. A timeout → `share_ready=false` (fail-closed, see Scheduling interaction). Server side, `storagepaths.ShareHealthy()` and the serve-stat are bounded the same way (`statDirWithTimeout`); a timeout reads as transient/unhealthy, never as "file deleted".
- **Benchmark window scales with the tier (C2).** `scheduler/speedtest.go` `ResolveSpeedTestParameters(..., storageTier)` multiplies the benchmark `TestDuration`/`TimeoutDuration` by `networkDirectSpeedTestMultiplier` (×4) for `network_direct`, on top of the compression choice, so a large uncompressed list read off the mount doesn't hit `ErrBenchmarkTimeout`. The dispatch path reads `storage_tier` alongside `extra_parameters` and passes it through.
- **Startup grace scales with the tier (C2).** The sweeper's `assigned` (pre-first-progress) grace is multiplied ×4 for `network_direct` agents (a `CASE WHEN a.storage_tier = 'network_direct'` on the `task_startup_grace_seconds` interval), so the cold dictionary read off the mount isn't evicted mid-read.
- **Timeouts don't count (C2).** `AttributeBenchmarkFailure` treats a `network_direct` benchmark timeout (`errorclass.IsBenchmarkTimeout`) as a non-counting transient — like `CategoryAgentNotReady`, it returns before the per-tuple hard cap and combo blocklist and adds only a short cooldown. A slow share costs time, never a failed job or a blocklisted agent. Genuine faults (bad hashlist, dead GPU, OOM) on a `network_direct` agent still flow through the normal counting path.
- **Fail-fast mount options (C3).** The generated mount command defaults to `soft` (and, for NFS, `timeo=150`/`retrans=2`) unless the operator set a hardness/timeout option, so a dropped share returns `EIO` quickly instead of hanging on the kernel `hard` default — which is what makes the fail-closed health check effective.
- **Cancelable migration (C3).** `MigrationEngine` runs on a cancelable context (`Cancel()`, wired to `DELETE /api/admin/settings/network-share/migration`); drain/copy/validate observe it so an admin can abort a migration wedged on a slow share. The failure persist uses a fresh context so the `failed` state is still recorded after a cancel.

## Cloud agents use the relay

Cloud GPU instances run their VPN in userspace / SOCKS-only mode (no kernel interface, no mount capability), so they **cannot** perform a CIFS/NFS mount and cannot use `network_direct`. They consume wordlists/rules over the **HTTP relay** (`full_cache`/`on_demand`) across the VPN — the server reads the share and serves the bytes. `network_direct` is therefore an on-prem-only tier, and KrakenHashes never auto-mounts or stores share credentials on cloud.

## Security

- KrakenHashes stores no share secrets in the primary flow — server credentials are in compose, agent credentials are operator-entered at mount time.
- Agents mount the share **read-only**; the server is the only writer.
- Client data and hashlists never live on the share (root/volume separation), and the binary-on-mount refusal plus `filepath.IsLocal`-guarded path joins prevent a mount from backing the executable or escaping the data dir.

## Code reference

- Resolver: `backend/internal/storagepaths/storagepaths.go`
- Migration: `backend/internal/services/networkshare_migration.go`, `backend/internal/migrationgate/`
- Share config: `backend/internal/models/network_share.go`, `backend/internal/repository/network_share_repository.go`, `backend/internal/handlers/admin/networkshare/`, `backend/internal/routes/admin.go`
- Per-agent columns: `backend/db/migrations/20260929120000_add_agent_storage_tier.up.sql`, `backend/internal/models/agent.go`
- Config push + readiness: `backend/internal/handlers/websocket/handler.go`, `backend/internal/services/websocket/service.go`
- Scheduling: `backend/internal/services/scheduler/{cycle.go,allocator.go,sweeper.go,speedtest.go,benchmark.go}`
- Latency hardening: `backend/internal/services/errorclass/classify.go` (`IsBenchmarkTimeout`), `backend/internal/services/job_scheduling_benchmark_planning.go` (`AttributeBenchmarkFailure` network_direct exemption)
- Agent runtime: `agent/internal/jobs/jobs.go`, `agent/internal/jobs/hashcat_executor.go`, `agent/internal/jobs/mountfs_*.go`, `agent/cmd/agent/main.go`, `agent/internal/agent/registration.go`

## Related

- [Storage Architecture](../../admin-guide/resource-management/storage.md) — operator walkthrough
- [Agent Configuration](../../agent-guide/configuration.md#storage-tier-network-share-feature) — tier flags/env
- [Agent Management](../../admin-guide/operations/agents.md#storage-tier) — per-agent setting
- [Environment Variables](../environment.md#network-share-storage) — all related env vars
- [Scheduler v2 Overview](scheduler-v2-overview.md) — dispatch and allocation
