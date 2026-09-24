# Cloud GPU Provisioning

KrakenHashes can rent ephemeral GPU instances from AWS EC2, RunPod or Vast.ai, run an agent
on them, dispatch **only** the intended job's work to them, charge the spend to a per-client
budget, and guarantee teardown — including when the backend itself has died.

Three requirements shape every decision below:

1. **Cost safety.** A rented GPU costs $0.50–$22/hr and **no** provider will stop it for
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
something already running. TTL falls out of the same arithmetic, bounded above by what can
be afforded and below by what is worth renting at all:

```
upper  = min(policy.max_instance_ttl, available_budget / hourly_rate)
target = commissioning + estimated_time_to_finish + drain_tail   (when throughput is known)
floor  = commissioning / cloud_max_commissioning_pct
ttl    = clamp(target, floor, upper)

drain_tail = cloud_teardown_slack_seconds + cloud_crack_drain_grace_minutes
```

**Why there is a tail.** Two waits meet at the end of a rental and they were not the same
length. `resolveChunkDuration` refuses to plan a chunk past `remaining TTL - teardown slack`,
so the last chunk ends about 120 seconds before the deadline — and that was all the upload
time a TTL-bound instance had. But the reaper will hold an instance for `CrackDrainGrace`
(10 minutes) while its agent is still sending cracks, because a large flush genuinely takes
minutes. The reaper's patience is worth nothing once the machine is gone: `ttl_epoch` is
armed **in-guest** and the watchdog powers off regardless. An instance sized to finish its
work exactly at its TTL therefore got 120 seconds to upload and was killed mid-flush if it
needed more — losing cracks on the *ordinary successful path*. Sizing the tail to the grace
the reaper already honours makes the two agree, and costs nothing real because the tail is
only ever reserved.

!!! note "Fixing NPK's bug"
    This is NPK's `campaign_max_price / spotPrice` idea with their instance-count bug
    fixed — theirs forgot to multiply by fleet size, so an N-node fleet silently got N×
    the intended window. Here each instance reserves its own runway.

**Why there is a floor.** Commissioning — boot, VPN join, registration, file sync,
benchmark — is paid before the instance can consume any keyspace, and
`scheduler.ReadinessBudget()` puts that at 20 minutes for a healthy agent. Measured cold
start on AWS was 138 seconds to *register alone*. The floor was previously a hardcoded 5
minutes, which is shorter than the instance needs to finish starting up, so the engine
could sell a rental that was guaranteed waste. It is now derived:
`cloud_max_commissioning_pct` (default 33) caps commissioning's share of the bill, putting
the minimum useful rental at about an hour. Set it to `0` to leave only the capability
floor — commissioning plus teardown slack plus one minimum chunk — below which
`resolveChunkDuration` refuses to size a chunk at all, so the instance could never be given
work.

**Why the target is sized to the job.** Reserving the whole ceiling for a job with twenty
minutes of work left commits money that a second instance could have used. Nothing is
ultimately *billed* for it — when the job completes, the job-finished rung destroys the
instance within one reaper sweep, and `SettleInstance` refunds reserved minus incurred —
but the reservation blocks other provisioning while it is held. Sizing the reservation to
the work is what lets one budget buy several instances in parallel rather than one at a
time.

Measured on the mock provider: a 60-minute rental that finished its work after 6 minutes
was released at 6 minutes with `reservation +100`, `incurred 9`, `release −91`. That is
the property the whole scheme depends on — an over-long TTL is refunded, an over-short one
is re-paid — so it is worth re-checking whenever teardown changes.

The projection comes from the same `Estimator` the finishing-soon rule uses, asked "how
long *with this offer added*" — `RankedOffer.AbsoluteSpeed`, the ranker's relative
throughput put back on an absolute scale by its calibration anchor. That hypothetical is
what makes the sizing work on the **first** rental: a starving cloud-only job has no agent
on it by definition, so without it the projection would always be unknown at the moment it
matters. When no anchor exists — a deployment that has never benchmarked this GPU for this
work — `AbsoluteSpeed` is zero, the projection reports itself unknown, and the TTL falls
back to the full ceiling. Zero there means *unknown*, never *instant*.

**Sizing happens once, at launch.** `ttl_epoch` is armed inside the guest and the host
watchdog poweroffs regardless of what the database later says, so whatever is computed here
must be something the machine can honour. There is no TTL extension —
`CloudInstanceRepository.ExtendTTL` exists but has no callers, and extending the row would
not move the in-guest deadline anyway.

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

### Unsent cracks bound teardown too

A rented instance is not destroyed while its agent still owns a task that is *actively being
written to* — capped by **`cloud_crack_drain_grace_minutes`** (default 10). This guards the
**job-finished** and **idle-drain** rungs.

The case it exists for is the ordinary successful one. Hashcat reports "all hashes cracked"
*while still running*; the backend moves the task to `processing` and completes the **job**
off that signal, deliberately leaving the task mid-handshake so it can finish uploading. The
reaper then saw a finished job and destroyed the instance with no grace at all — taking the
disk holding cracks the agent had not yet sent. That loss is permanent: the outfile dies with
the machine, and recovery books the searched range as *covered*, so no other agent re-runs it.
The job reads `completed` and looks perfect.

The grace is **quiet time, not total wait** — measured from the task's last write, so a
working agent is never destroyed and a wedged one is not waited on forever. `0` disables it
and restores immediate teardown; it is the only cloud grace whose zero value can lose data
rather than merely waste money.

**TTL expiry and the budget hard stop stay unconditional.** The TTL epoch is armed inside the
guest too and will `poweroff` regardless, so a backend-side grace there would be a promise the
guest does not honour.

Unlike `cloud_idle_drain_minutes`, `cloud_commissioning_grace_minutes`,
`cloud_crack_drain_grace_minutes` and `cloud_orphan_grace_minutes`, which the reaper loads
once at boot, `drain_timeout_seconds` lives on `cloud_budget_policies` and is re-read on every
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
| 6 | Provider ceilings (AWS vCPU quota, RunPod credit balance, Vast.ai prepaid balance) | ✅ prevents | n/a |

!!! warning "Tiers 1 and 2 are not available on every provider"
    They rely on the guest being able to destroy itself, which needs a credential it can
    safely hold. **RunPod Community has neither** — no per-pod scoped credential exists, and
    an account-scoped one would be readable by the host operator — so the in-guest deadline
    still kills hashcat and ends the data exposure but **cannot stop the pod billing**. On
    that tier the ladder effectively starts at tier 3, and the refusal to hold the key is
    enforced in the adapter's constructor rather than at the injection site, so no later
    change to the injection path can reintroduce it.

Tier 1 is armed **before** the container image is pulled. NPK's GPU nodes had no working
watchdog at all — their only correct `trap … EXIT; shutdown` lived on a cheap
wordlist-compression node — and that is the hole this ordering closes.

!!! danger "The self-destruct must not use the VPN"
    On Vast.ai, self-destruct calls `DELETE /api/v0/instances/$CONTAINER_ID/` using
    `$CONTAINER_API_KEY`; on RunPod Secure it calls `DELETE rest.runpod.io/v1/pods/$ID`.
    Routing either through the tunnel would send it down a dead link in exactly the scenario
    the heartbeat watchdog exists for, so `NO_PROXY` covers each provider's control plane and
    `169.254.169.254`. The calls additionally pass `--noproxy '*'`, so a mis-built
    `NO_PROXY` cannot break teardown either.

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
| GET | `/providers/{id}/capacity` | Capacity explorer. Read-only and spends nothing; **502** when the provider is reachable but the probe failed, **501** for a provider that has no placement axis |
| POST | `/providers/{id}/acknowledge` | Record the third-party data-exposure acknowledgement |
| GET | `/instances` | Live fleet |
| DELETE | `/instances/{id}` | Manual destroy; **502 means the provider refused and it is still billing** |
| GET | `/clients` | Every client that is cloud-enabled or funded |
| GET/PUT | `/clients/defaults` | Deployment-wide client defaults applied to clients with no explicit settings |
| GET/PUT | `/clients/{id}/settings` | Budget, TTL ceiling, provider allowlist |
| GET | `/clients/{id}/budget` | Live spend plus the action the ladder implies |
| PUT/DELETE | `/clients/{id}/policy` | Per-client threshold override |
| POST | `/clients/{id}/acknowledge` | Per-client provider acknowledgement |
| GET/PUT | `/policy` | System-default threshold ladder |
| GET/PUT | `/rules` | System-default provisioning rules, **unmerged** |
| GET/PUT/DELETE | `/clients/{id}/rules` | Per-client override; GET returns the **merged** view plus the raw override and the default |
| GET | `/jobs/{id}/projection` | Coverage bar inputs |
| POST | `/jobs/{jobId}/provision` | Operator-initiated launch. Names the specific precondition that failed, including the two that are otherwise invisible: no client to bill, and cloud burst never enabled |

`GET /providers/{id}/capacity` splits its failure modes deliberately. Unlike preflight, a
failure here is **not** a diagnosis — without the placement list there is no screen to render
— so a probe error is a real 502 rather than a 200 carrying a report. A provider that chooses
placement itself returns **501**, which today means only `mock`.

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

## Provider maturity is derived, never stored

Every provider kind declares a maturity — `stable` or `experimental` — from a single map in
the model layer. It is **computed on marshal and never persisted**, which is the whole point:
a stored column can drift out of step with reality, and an admin who would rather not see the
warning could edit it into a lie. A derived value cannot be either.

| Kind | Maturity | UI |
|---|---|---|
| `aws`, `mock` | `stable` | green **Tested** chip |
| `vastai`, `runpod`, `runpod_community` | `experimental` | amber **Experimental** chip |

The wire value is `experimental` rather than `beta` deliberately: "beta" is a claim about
completeness, and the claim being made here is about **evidence**. The adapters are finished;
what is missing is a paid run that somebody watched.

**It is not a measure of how much code exists.** Vast.ai is fully implemented and has never
been driven end to end with a funded account; AWS has been taken through boot, commissioning,
cracking, clean release and a settled refund. An implemented adapter and a proven one look
identical from the outside and cost very differently when they are wrong.

`MaturityUnknown` is never a valid answer for a shipped provider — a test asserts every kind
declares one, so adding a provider without classifying it fails the build rather than
defaulting to reassuring.

Maturity is a **separate axis from trust**, and the two only partly overlap: RunPod Secure is
experimental but first-party, Vast.ai is both experimental and third-party, `mock` is neither.
The UI renders them as two distinct chips for that reason — collapsing them would hide that a
SOC 2 provider is the unproven one.

Both maturity states are rendered in the provider settings table, not just the unproven one.
"Proven" inferred from the absence of a warning is not a claim anyone reads, and the table is
where the providers are compared side by side. The Cloud Fleet page stays warning-only: a
Tested chip on every AWS instance row is noise on a page that is watched continuously.

---

## Capacity exploration is a cross-provider interface

`ExploreCapacity` is deliberately **not** part of the `Provider` interface; callers type-assert
for it. That keeps a provider with no placement axis from having to implement a stub that lies,
and it is why the HTTP layer can answer 501 honestly.

All three real providers implement it. Only `mock` does not.

| Provider | Placement axis | Hardware axis | Selection mode |
|---|---|---|---|
| AWS | Availability zone | Instance type | `matrix` — hardware chosen **per** placement |
| RunPod | Data centre | GPU type | `axes` — two independent lists, combined |
| Vast.ai | Country | GPU model | `axes` |

The report carries its own **trust metadata** rather than leaving the UI to guess, because the
signals genuinely differ in kind:

| Trust | Meaning | Example |
|---|---|---|
| `definitive` | A "no" here can never launch | AWS `DescribeInstanceTypeOfferings` |
| `measured` | Real, observed data, but not a guarantee | live prices, rentable counts |
| `advisory` | A hint worth ordering by and nothing more | AWS spot placement score |

That grading is what stops a spot placement score being presented as availability. The score
is used to order candidates and never to remove one — a live account scored every zone 1/10
minutes before a launch in one of them succeeded on the first attempt.

AWS is the only provider with a `definitive` signal, and the only one that has a configured
rate to show beside the live price. RunPod and Vast.ai report availability counts instead,
which AWS does not expose at all.

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

Two provider kinds over one adapter (`runpod.go` for the lifecycle, `runpod_offers.go` for
GraphQL and offers), differing in the REST v1 `cloudType: SECURE | COMMUNITY` field and in
their consent chain. Every tier-dependent value derives from a single `secure()` predicate
off `cfg.Provider`, so the two facts cannot disagree.

Container-based like Vast.ai rather than VM-based like AWS, so `vastai.go` is the closer
sibling: image plus env, label-based ownership, userspace VPN. `vpn.go` needs no changes —
RunPod pods are unprivileged containers exactly like Vast's, and the entrypoint's
`--tun=userspace-networking` / `NB_USE_NETSTACK_MODE` / wireproxy stack already avoids a tun
device.

The API is weaker than both incumbents in ways that shape the design:

- **No idempotency key on `POST /pods`, and no server-enforced unique name.** A retried
  create yields two pods with the same label. Mitigated by adopting an existing pod by label
  before creating, then reconciling by name on an *ambiguous* failure — a transport error or
  5xx, where the request may have been accepted. Only a genuine capacity refusal becomes
  `ErrOfferUnavailable`, because that sentinel **releases the budget reservation** and
  applying it to a timeout would free the budget for a pod that is quietly running.
- **`GET /pods` does filter** (`name`, `gpuTypeId`, `desiredStatus`, `dataCenterId`), but no
  filter helps *ownership*: labels are unique per instance, so the useful query is "all of
  them" plus an anchored client-side regex against `kh-<uuid[:18]>`. Two pods sharing a label
  is the expected double-launch signature, and the loser is re-keyed under a synthetic
  `label#dup:<id>` so the orphan machinery reaps it rather than a map write silently dropping
  it. **Pagination is undocumented and is the highest-consequence unknown in the adapter**,
  since `ListOwned` is the backstop every "the reaper will adopt it later" argument depends
  on; a suspiciously round page size is logged.
- **`desiredStatus` is a DESIRED state, not an observation**, and it is the only status the
  list endpoint returns. It over-claims `RUNNING` for a pod still pulling its image. Harmless
  because readiness is gated on agent registration and `ready_deadline_at`, never on provider
  status — but `Status` reports the raw value so an operator is not told "running" while the
  console disagrees. A non-404 error is **never** mapped to `ObservedGone`: that is terminal
  and would finalize the row for a pod still billing.
- **No TTL field.** Teardown rests on the in-guest deadline and the backend reaper.
- **No `createdAt`** on the pod object; age is inferred from `lastStartedAt`.
- **Billing buckets are one hour minimum**, so `CostSoFar` returns `(0, false, nil)` always.
  `costPerHr × elapsed` is available and deliberately unused: it *is* the caller's own
  wall-clock estimate, so returning it as authoritative would launder an estimate into a fact.
- **A stopped pod still bills**, disk at roughly double — which is why `EXITED` maps to
  terminal. The adapter always terminates, never stops, and never attaches network volumes.

Availability data is the **best of the three providers**: `gpuTypes.lowestPrice` returns
`stockStatus`, `rentedCount` and `totalCount`, filterable by `dataCenterId` and
`secureCloud`. Real inventory with a denominator, where AWS has only an advisory score.
Placement pins the *opposite* way from AWS: empty `data_center_ids` lets RunPod's scheduler
try everywhere and is the widest search.

Both `api.runpod.io` (GraphQL) and `rest.runpod.io` (REST v1) are kept off-tunnel via
`KH_NO_PROXY_EXTRA`, for the same reason as `console.vast.ai`: a self-destruct path that
needs the tunnel whose loss it is reacting to cannot work. Only the REST host is used by the
guest, but listing both means the teardown path cannot be silently broken by the adapter
switching endpoints.

**Teardown is asymmetric between the tiers, and this is the sharpest constraint on the
provider.** RunPod issues no per-pod scoped credential — Vast's `CONTAINER_API_KEY` has no
equivalent — so the only key that can delete a pod is account-scoped. On Secure that key may
be injected as `KH_RUNPOD_API_KEY` when the operator opts in, giving the same three rails as
Vast. On **Community it is never injected**, enforced in the constructor rather than at the
injection site so no later code path can bypass it: a Community host operator has root over
the container and would read it out of the environment, and that key can create and delete
every other pod on the account. The in-guest deadline still kills hashcat and ends the data
exposure, but on Community **only the reaper can stop the billing**.

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

### Multiple providers at once

Every enabled and permitted provider config is searched on each provision
(`rankedCandidates`), ranked **per provider** — `rankOffers` is single-provider because the
observation ladder is provider-scoped — and then merged into **one global cost-per-work
order**. The launch walk spans providers. Two configs of the same kind are legal; only
`name` is unique.

Failure is isolated at both ends. A provider whose credentials will not decrypt is skipped
during the search, and a *provider-local* launch failure — currently VPN credential decrypt
and mint, both provably before any row, voucher, reservation or provider call — carries
`errProviderLocal` so the walk continues onto another provider's candidates. The bar for
that sentinel is high and ambiguity disqualifies: anything at or after the instance row may
have left something billing, so it still aborts the walk and leaves the row for the reaper.

Two known limitations:

- **The allowlist is keyed by provider *kind*, not config id**, so "AWS-prod" and "AWS-dev"
  cannot be permitted separately, and same-kind configs share one benchmark observation pool.
- **`max_concurrent_instances` is per config row**, so two configs on one provider account
  permit twice the intended cap.

### Placement: offers are (type, zone) pairs

`SearchOffers` emits **one offer per instance type per configured zone**, each carrying its
own `PlacementRef` (the subnet), and `buildRunInput` prefers the offer's subnet over the
provider's global `subnet_id`. That pairing is the whole point:

> EC2 spot capacity belongs to the **(instance type, availability zone) pair**, not to the
> type and not to the region.

Before this, `AWSSettings.SubnetID` was a single string, so a search over *N* instance types
produced *N* candidates that all resolved to the same physical inventory. `attemptLaunch`
walks the ranked list on `ErrOfferUnavailable` and logs "trying the next candidate" for each
— which reads like working fallback and is not, because the second refusal was implied by
the first. Observed: fifteen consecutive `InsufficientInstanceCapacity` refusals (each
correctly costing $0), then a success two cycles after widening to three types across two
zones.

The number that matters is `len(zones) × len(types)`. The zone picker in the admin UI shows
it live and warns at 1.

Ordering: when spot is enabled and more than one zone is configured, `SearchOffers` calls
`GetSpotPlacementScores` and maps the 1–10 result onto `Offer.Availability`, which the
ranker uses as a **tie-break after cost, confidence and hourly rate**. Same type in three
zones ties on all three, so without it the launch order is alphabetical — `us-east-2a`
first, every time, including when 2a is the empty one.

The score never *filters*. It read 1/10 in every zone on a live account minutes before a
first-try launch succeeded in one of them, and `availabilityFromScore(0)` returns
`AvailabilityUnknown` rather than `AvailabilityNone` precisely so that a failed call or a
missing `ec2:GetSpotPlacementScores` permission costs ordering instead of deleting every
candidate.

Backward compatibility: `placements()` never returns empty. With no `zones`, it yields a
single placement carrying the legacy `subnet_id` — and if that is empty too, one with no
subnet at all, which is exactly the pre-zones behaviour of letting EC2 choose.

---

## Related

- [Cloud Providers setup](../../admin-guide/system-setup/cloud-providers.md)
- [AWS](../../admin-guide/system-setup/cloud-aws.md) · [RunPod](../../admin-guide/system-setup/cloud-runpod.md) · [Vast.ai](../../admin-guide/system-setup/cloud-vastai.md)
- [Cloud agent VPN](../../admin-guide/system-setup/cloud-vpn.md)
- [Cloud agent deployment](../../agent-guide/cloud-deployment.md)
- [Scheduler v2 overview](scheduler-v2-overview.md)
- [Job priority](../../admin-guide/advanced/job-priority.md) — the scale the minimum-priority rule is absolute against
