# Cloud GPU Provisioning

KrakenHashes can rent ephemeral GPU instances from Vast.ai or AWS EC2, run an agent on
them, dispatch **only** the intended job's work to them, charge the spend to a per-client
budget, and guarantee teardown — including when the backend itself has died.

Three requirements shape every decision below:

1. **Cost safety.** A rented GPU costs $0.50–$22/hr and neither provider will stop it for
   you. Every failure mode must converge on "the instance dies."
2. **The server is never exposed to the internet.** Cloud agents join the operator's
   *existing* VPN. KrakenHashes does not build a VPN.
3. **Minimal data egress.** A rented box gets the files its job needs, never the org's
   whole corpus.

---

## Budget: why `$10` means `$10`

The ledger tracks one authoritative number, **committed spend**:

```
committed = SUM(reservation) + SUM(release) + SUM(reconciliation)
available = cap − committed
```

| Ledger kind | Meaning |
|---|---|
| `reservation` | `+R`, committed at launch for the instance's whole TTL |
| `release` | negative, returns the unused remainder at teardown |
| `reconciliation` | signed provider-authoritative correction (AWS Cost Explorer lags ~24h) |
| `incurred` | how much of the committed envelope has actually been consumed |

**`incurred` deliberately does not reduce availability** — the reservation covering it
already did. Subtracting both would double-count every running instance and refuse
launches while the budget was half free.

Because the money is committed *before* the instance boots, the cap cannot be exceeded by
something already running. TTL falls out of the same arithmetic:

```
ttl = min(policy.max_instance_ttl, available_budget / hourly_rate)
```

!!! note "Fixing NPK's bug"
    This is NPK's `campaign_max_price / spotPrice` idea with their instance-count bug
    fixed — theirs forgot to multiply by fleet size, so an N-node fleet silently got N×
    the intended window. Here each instance reserves its own runway.

The reservation is written inside a transaction holding `SELECT … FOR UPDATE` on the
client row. Without that lock two concurrent provisioning decisions both read the same
headroom, both decide they fit, and both launch.

### Threshold ladder

Per-client, fully configurable, defaults 80 / 95 / 99 / 100:

| Threshold | Action |
|---|---|
| `notify_pct` (nullable ⇒ never notify) | alert admins |
| `stop_provision_pct` | no new instances |
| `drain_pct` | stop dispatching new chunks, let in-flight work finish, then destroy |
| `hard_stop_pct` | destroy now, mid-chunk |

`allow_overage=false` (default) makes the cap absolute. The ladder only decides how
*gracefully* the cap is reached; reservation accounting is what prevents exceeding it.

**Drain in detail.** The instance is marked `draining`, which removes it from
`getIdleAgents` so it receives no further chunks or benchmarks. It is destroyed as soon as
nothing is in flight, or after `drain_timeout_seconds` (default 300) if work is still
running — whichever comes first. `0` disables the timeout, which does *not* mean "wait
forever": the instance is already excluded from dispatch, so its current chunk is its last,
and TTL plus idle drain still bound it. A task in `processing` counts as in flight, because
the agent is still uploading cracks it has already found. If spend falls back below
`drain_pct` — a raised cap, a new period, a released reservation — the instance resumes
rather than being thrown away.

Unlike `cloud_idle_drain_minutes`, `cloud_commissioning_grace_minutes` and
`cloud_orphan_grace_minutes`, which the reaper loads once at boot,
`drain_timeout_seconds` lives on `cloud_budget_policies` and is re-read on every
assessment — so it takes effect on the next sweep with no restart.

---

## Teardown ladder

Ranked by "the backend died — does the instance still die?"

| # | Mechanism | Survives backend loss | Latency |
|---|---|---|---|
| 1 | **In-guest absolute deadline**, armed as PID 1's *first* action | ✅ fully | ±60s |
| 2 | **Heartbeat-loss self-destruct** — agent unreachable for N min | ✅ fully | N min |
| 3 | Backend reaper reconciling by label/tag | ❌ | ≤60s |
| 4 | Startup reconciliation on backend boot | ❌ | on boot |
| 5 | Ordered SIGTERM drain | ❌ | on shutdown |
| 6 | Provider ceilings (AWS vCPU quota, Vast.ai prepaid balance) | ✅ prevents | n/a |

Tier 1 is armed **before** the container image is pulled. NPK's GPU nodes had no working
watchdog at all — their only correct `trap … EXIT; shutdown` lived on a cheap
wordlist-compression node — and that is the hole this ordering closes.

!!! danger "The self-destruct must not use the VPN"
    On Vast.ai, self-destruct calls `DELETE /api/v0/instances/$CONTAINER_ID/` using
    `$CONTAINER_API_KEY`. Routing that through the tunnel would send it down a dead link
    in exactly the scenario the heartbeat watchdog exists for, so `NO_PROXY` covers the
    provider control plane and `169.254.169.254`.

Additional guarantees:

- **Write-before-launch.** The `cloud_instances` row and its budget reservation are
  written *before* the provider is called. A crash between them leaves a row with a label
  and no provider ID, which the reaper reconciles.
- **Orphan sweep.** Anything on the provider account with no matching row is destroyed
  after a grace period. This is the only recovery for a launch whose label never landed.
- **Terminate dead-man's switch.** After K consecutive failed teardowns admins are paged
  with the raw provider ID, and the instance **keeps accruing** and keeps blocking new
  provisioning — reporting it terminated would hide real spend.

---

## Scheduler integration

### Dispatch isolation

A rented instance may only receive work from the job it was provisioned for. The predicate
is composed at the cycle level, **not** inside `CompatCache`:

`OnUnitChanged` has no production callers, `OnAgentChanged` only fires on
connect/disconnect, and `WarmAll` re-warms every 30s — so a cached `true` for an unrelated
unit could survive ~10 cycles. Combined with `IsFileMapReady` failing open, that could
hand a freshly-registered cloud agent **another client's job** on its first cycle. Wrapping
outside the cache caps staleness at one 3-second cycle. The lookup **fails closed**.

The snapshot covers *all* cloud agents, not just idle ones, because preemption asks about
the holder of a victim task — and a paid instance must never be preempted away from the
job that bought it.

### What `max_agents` means

> `max_agents` governs the **shared on-prem pool**. Cloud instances are governed by budget
> and `job_executions.cloud_max_instances`.

`max_agents` exists for fleet fairness — stopping one job monopolising shared hardware. A
rented instance is dedicated to one job and paid for by that job's client, so it competes
with nobody. Charging it against fairness would make the feature a no-op in the common
case: a job with the default `max_agents=1` and one on-prem agent has zero capacity, so
every rented instance would be compatible but never allocated.

Cloud agents are therefore excluded from **both** `MaxAgents` accounting and
`ActiveAgentCount`. Excluding one but not the other double-books the cap from opposite
directions.

### Priority

- Cloud agents are job-locked, so cross-job priority never applies to them.
- They are immune to preemption, and never preempt.
- Priority governs the **autoscaler**: a constrained budget is spent in the scheduler's own
  order, `priority DESC, created_at ASC`. One ordering across the system.

### Chunk sizing

"Take 100% if it fits" already works — `sizeChunk` clamps to the gap, so a rented instance
targeting a 60-minute chunk that finds 8 minutes of work left takes all 8. Three additions:

1. **TTL clamp (mandatory).** A chunk is never planned past an instance's death. Without
   it, an instance with 4 minutes left claims 20 minutes of keyspace, dies, and strands
   16 minutes until the sweeper evicts it.
2. **`cloud_chunk_duration_seconds`** (default 3600 vs the on-prem 1200). hashcat startup
   and kernel autotune are billed at rental rates. Deliberately *not* one-chunk-per-rental:
   that makes the overrun guard useless for stall detection (it fires at
   `chunk_duration × 1.2`) and turns a speed over-estimate into dying mid-chunk every time.

    It is a **floor, not an override**. A job asking for *longer* keeps its own value;
    only "shorter on a rented GPU" is overruled. This matters because
    `job_executions.chunk_size_seconds` is `INT DEFAULT 900` with a `NOT NULL` preset
    source, so it is never NULL — "the operator chose 1200" and "the create form
    pre-filled 1200" are indistinguishable, and both have the same right answer on rented
    hardware. Set it to `0` to disable the floor and give every job its own chunk size on
    cloud. Resolution order is system default → per-job value → cloud floor → TTL clamp,
    in `resolveChunkDuration` (`scheduler/dispatcher.go`).
3. **Endgame tapering** (fleet-wide) — see below.

### Endgame tapering

Near job end, a fast rented GPU finishes the last gap and then **bills while idle**,
waiting for a slow on-prem card to finish a chunk it started ten minutes ago. Making cloud
chunks bigger makes this worse: the cause is the *on-prem* agent's long chunk.

So once a unit's remaining work drops below `endgame_threshold_multiple ×` the chunk
duration, every agent's chunk is sized speed-proportionally:

```
share_i = remaining_base × speed_i / Σ speed_j
```

`Σ speed` includes agents **already running** a chunk on the unit, not just those being
allocated — otherwise the first tapered cycle over-allocates to whoever happens to be idle.

Shares are computed in the caller and passed through `DispatchInputs`, because
`DispatchOneChunkPerAgent` sizes each allocation in its own transaction and only ever sees
one at a time.

Gated on `scheduler_endgame_tapering_enabled` (default on — it shortens on-prem job tails
too). With it off, sizing is byte-for-byte what it was before.

---

## Job-scoped file sync

On connect, `initiateFileSync` normally asks the agent for its whole inventory and diffs it
against **every verified wordlist, rule and binary**. A fresh cloud instance owns none of
them, so the diff is 100%: the entire corpus downloads while a $2–22/hr GPU idles, plus
ingress billing.

It is also an exfiltration path — client potfiles are served as file type `wordlist`, so a
full sync ships **every client's cracked plaintexts** to a machine whose operator has root.

Cloud agents skip it entirely and receive an explicit, job-scoped download list instead.
This needs no new protocol: `FileSyncCommandPayload` already carries a file list and the
agent downloads exactly what it is given without diffing. `Size` and `MD5Hash` are
populated on every entry, because the agent's disk pre-check only fires when `Size > 0` and
it only verifies a download when `MD5Hash` is non-empty.

`agent_sync_recovery` also excludes cloud agents — otherwise it would re-trigger the
corpus-wide sync every 60s.

### Disk sizing

`disk_gb` is computed from `SUM(file_size)` plus a binary-extraction multiplier (the
archive and its extracted tree coexist), a hashlist estimate (`hashlists` stores no byte
size), loopback delta rounds, and the agent's own 512 MiB pre-flight floor.

This must be right: **Vast.ai disk is immutable after creation**, and `AGENT_DISK_FULL` is
classified *transient*, so an undersized instance would retry-loop against the same agent
until its TTL expired. Cloud agents therefore treat disk-full as **structural** — terminate,
don't retry.

---

## Benchmarks

Cloud agents run a **real** benchmark like everyone else. Synthetic
`agent_benchmarks` rows are deliberately never written, because:

1. `CountAgentsWithRecentBenchmark` counts any row with `speed > 0`, so a synthetic seed
   makes an invented number look like corroborating evidence from "another agent" — which
   feeds the quarantine decision and can **disable real on-prem GPUs**.
2. Seeded too high, the overrun guard fires, the job-scoped blocklist engages, and the
   customer's job can be failed outright.
3. Seeded too low, chunks fall under the 30s / 5% floors where *both* EMA self-heal paths
   return early, and the instance grinds micro-chunks for its whole TTL.

`cloud_gpu_benchmarks` exists purely for **pre-launch estimation** — "how fast is an RTX
4090 on `-m 1000`, so what will this cost" — fed by observed cloud speeds and read only by
the estimator.

---

## Provisioning rules: soft vs hard placement

`decideProvisioningAction` is a pure function in the style of `decideBudgetAction` — table
testable, no I/O, and its refusal strings are surfaced verbatim to the operator. Where the
budget engine answers *how much*, this answers *when anything at all*.

Rules are evaluated **cheapest first**, so a job that fails the priority floor never costs an
estimator call or a spend query. That ordering is for the caller's benefit and cannot be
exploited by calling with a half-filled input: `ProvisioningInput` is a plain value and its
unset fields are not neutral — `SpendReadable: false` refuses, and `ProjectionAvailable: false`
silently skips two rules.

Placement splits on **soft vs hard**, not cheap vs expensive:

| Rule | Placed in | Why there |
|---|---|---|
| Priority floor | `CloudEligibleJobs` (Go filter) | Means "not worth spending on automatically". Per-client merged rules cannot be a WHERE clause |
| Skip if finishing soon | `CloudEligibleJobs` (Go filter) | Needs the estimator; projections are pessimistic for salted types, so overriding is legitimate |
| Minimum starvation | Autoscaler | The age lives in `StarvationSnapshot`; the threshold is carried on `EligibleJob` so the comparison stays where the observation is |
| Max spend per job | `ProvisionForJob` — **hard** | An admin-bypassable spend cap is not a spend cap |
| Provisioning window | `ProvisionForJob` — **hard** | Usually encodes an external constraint, not an operator preference |
| Peer-host opt-in | Provider filter — **hard** | Data-exposure consent; without it the job sees no peer offers but may still rent secure capacity |

### Three traps worth naming

**`TimeToFinish == 0` means no throughput, not "instant".** That is exactly the starving job
this feature exists to rent for, so reading the zero as "finishing now" would refuse to
provision at the precise moment it is needed, silently, on every pass. The rule fires only on
`ProjectionAvailable && HaveThroughput && TimeToFinish > 0 && TimeToFinish <= window`; all four
conditions are load-bearing and each has its own regression row.

**Per-job spend joins through the instance**, not `cloud_spend_ledger.job_execution_id`. Only
`reservation` rows carry that column, so filtering on it sums gross reservations with no
releases netted out and over-reports every job whose instance terminated early.

**Starvation is asked per job, not per unit.** An increment job with more units than agents
leaves siblings unallocated on every cycle including the ones where it is cracking at full
throughput, so a per-unit reading would make its starvation age grow without bound and the
minimum-starvation rail would never reset for any job large enough to matter.

---

## Estimation and the coverage bar

Projections are computed in **base** keyspace, never effective: for salted types
`effective_keyspace` shrinks as salts crack, so an effective-based projection drifts as the
job progresses. The projection is therefore pessimistic for salted types, which is the safe
direction for a spending decision — and the UI says so.

**Coverage %** (NPK's idea, the most useful single number) is what percentage of the
remaining work the budget can pay to complete. Below 100 means the money runs out first.
KrakenHashes surfaces the number and requires explicit confirmation below 100 rather than
blocking, since partial progress is still progress.

`time_to_finish_seconds` is emitted through a custom marshaller. A Go `time.Duration`
serialises as **nanoseconds**, so shipping it straight out under a field named `_seconds`
would hand the UI a number 10⁹ too large and turn every ETA into nonsense.

The projection resolves the paying client from the job's own hashlist when the caller does
not name one. Without that fallback the available budget would be zero, and the coverage
bar would read 0% — indistinguishable from a genuine shortfall, but caused by a missing
argument.

---

## Admin API

Under `/api/admin/cloud/...`, registered on the admin subrouter (so `middleware.AdminOnly`
already applies) from `main.go` rather than `SetupAdminRoutes`, because the handler needs
repositories and an estimator that only exist later in startup. `routes.AdminRouter` is
published for exactly this, mirroring `routes.JobIntegrationManager`.

The registration happens **before** the HTTP servers start: attaching routes to a
`mux.Router` that is already serving is a data race. Moving the whole cloud block ahead of
the servers also fixes an ordering bug — `SetCloudAgentLocks` must be wired before the
scheduler's first cycle, or the compat wrapper has no lock snapshot and a rented agent
could be offered another client's job.

| Method | Path | Purpose |
|---|---|---|
| GET | `/providers` | List configs (secrets redacted; reports whether the encryption key is ephemeral) |
| POST | `/providers` | Create |
| PUT | `/providers/{id}` | Update; blank secret fields keep the stored value |
| DELETE | `/providers/{id}` | Delete; 409 while any instance references it |
| POST | `/providers/{id}/preflight` | Provider self-check; always 200 — a failing preflight is a successful diagnosis |
| POST | `/providers/{id}/acknowledge` | Record the third-party data-exposure acknowledgement |
| GET | `/instances` | Live fleet |
| DELETE | `/instances/{id}` | Manual destroy; **502 means the provider refused and it is still billing** |
| GET | `/clients` | Every client that is cloud-enabled or funded |
| GET/PUT | `/clients/{id}/settings` | Budget, TTL ceiling, provider allowlist |
| GET | `/clients/{id}/budget` | Live spend plus the action the ladder implies |
| PUT/DELETE | `/clients/{id}/policy` | Per-client threshold override |
| POST | `/clients/{id}/acknowledge` | Per-client provider acknowledgement |
| GET/PUT | `/policy` | System-default threshold ladder |
| GET/PUT | `/rules` | System-default provisioning rules, **unmerged** |
| GET/PUT/DELETE | `/clients/{id}/rules` | Per-client override; GET returns the **merged** view plus the raw override and the default |
| GET | `/jobs/{id}/projection` | Coverage bar inputs |

The two rules read routes are **deliberately asymmetric**, and conflating them is the one
mistake here that quietly changes policy for every client. `/rules` returns the system default
unmerged, because an admin editing the defaults has to see what the defaults themselves say.
`/clients/{id}/rules` returns the merged view, because that is what the client is actually
subject to — and the repository stamps the requested client's identity onto the result, so a
load-edit-save round trip on a per-client screen can never target the system-default row.

The client id always comes from the **URL**, never the body: a body-supplied `client_id` would
let a request against one client's route write another's override, or with a null, the system
default for everyone.

Enabling a provider is refused unless it has credentials, a VPN provider, a VPN credential
and a `backend_vpn_host`. An instance that cannot join the VPN can never reach the backend:
it would boot, fail to connect, and bill until its watchdog fired. Peer providers
(`vastai`, `runpod_community`) additionally require the third-party acknowledgement to
already be on file — routed through `CloudProvider.RequiresThirdPartyAck()`, which is the
single place that trust-tier question is answered.

Acknowledgements and credential changes are attributed to the authenticated caller, never
to anything in the request body — `provider_ack` is written only through its own endpoint,
never through the settings update.

---

## Provider notes

### Vast.ai

| Property | Consequence |
|---|---|
| **No idempotency token** on instance create | The label *is* the idempotency key: list-by-label → adopt → else create. A retried PUT would create a second paid contract. |
| **No TTL, no auto-destroy** | The in-guest watchdog and the reaper are the *only* teardown. |
| Storage bills from contract creation and through `stopped` | Never stop, always `DELETE`. |
| `exited` / `unknown` / `offline` never recover | Destroy immediately; polling them burns money. |
| Rate limits are min-intervals with **no `Retry-After`** | Backoff is ours; use the list endpoint, not per-instance polling. |
| Instance ID is `new_contract`, not `id` | Mishandling silently breaks teardown. |
| `env` must be a JSON **object** on create | The OpenAPI schema says string; sending a string silently drops every variable. |
| **Unprivileged containers** — no `/dev/net/tun`, no `NET_ADMIN` | Userspace VPN only. OpenVPN is impossible. |
| Hosts are individually-owned machines whose operators have root | Requires explicit per-client acknowledgement. |

### RunPod

Two provider kinds over one adapter, differing in the v2 API's `cloud: SECURE | COMMUNITY`
field and in their consent chain. The API is weaker than both incumbents in ways that shape
the design:

- **No idempotency key, no server-side filter, no pagination.** Ownership is a client-side
  anchored regex against the `kh-<uuid[:18]>` label every instance already carries, so a
  dedicated account is a requirement rather than advice.
- **No TTL field.** Teardown rests on the in-guest deadline and the backend reaper; there is
  no provider-enforced ceiling to fall back on.
- **No balance endpoint**, so pre-flight cannot verify funding — a `402` at create time is the
  only signal.
- **Billing buckets are one hour minimum**, so cost-so-far is unknown for most pods and
  accrual stays wall-clock.
- **A stopped pod still bills**, disk at roughly double. The adapter always terminates, never
  stops, and never attaches network volumes — those outlive the pod and would retain cracked
  plaintexts after termination.

`api.runpod.io` is kept off-tunnel via `KH_NO_PROXY_EXTRA` for the same reason as
`console.vast.ai`: a self-destruct path that needs the tunnel whose loss it is reacting to
cannot work.

### AWS

Two cost-bounding ideas that sound right are **not available**:

- Spot `ValidUntil` is supported only for *persistent* requests; we use one-time.
- `BlockDurationMinutes` (spot blocks) is **deprecated**.

What actually bounds an instance is `InstanceInitiatedShutdownBehavior=terminate` plus the
in-guest deadline.

Other essentials:

- **GPU quotas default to 0** on every account (`L-DB2E81BA` on-demand, `L-3819A6DF` spot).
  Queried at provision time, not configuration time — NPK snapshots quotas at deploy and
  their users' approved increases stay invisible until reinstall.
- `GetServiceQuota` returns `NoSuchResourceException` for an untouched quota, meaning
  "still at the AWS default", so it falls back to `GetAWSDefaultServiceQuota`.
- `DryRun` validates IAM and parameters but **not quota or capacity**.
- `DeleteOnTermination=true` on every block device; an untagged surviving EBS volume is the
  classic orphan.
- `ClientToken` is deterministic per instance — a random token per retry would let an
  SDK-level retry after a network timeout launch a second paid GPU.
- AMI resolved via SSM public parameter, never a name glob (NPK issue #112).

---

## Related

- [Cloud Providers setup](../../admin-guide/system-setup/cloud-providers.md)
- [Cloud agent deployment](../../agent-guide/cloud-deployment.md)
- [Scheduler v2 overview](scheduler-v2-overview.md)
