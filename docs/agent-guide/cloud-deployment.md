# Cloud Agent Deployment

Cloud agents are ephemeral: rented, used for one job, and destroyed. They are provisioned
by the backend rather than installed by hand — this page explains what the image does, so
you can reason about it and debug it.

For provider credentials and budgets, see
[Cloud Providers](../admin-guide/system-setup/cloud-providers.md); for the VPN every rented
agent has to join, see [Cloud Agent VPN](../admin-guide/system-setup/cloud-vpn.md).

## One image, every provider

CI publishes `krakenhashes-agent-cloud`. How it is run differs:

| Provider | How the image runs |
|---|---|
| **Vast.ai** | The container **is** the instance |
| **RunPod** | The container **is** the pod — same shape as Vast.ai |
| **AWS** | A VM. The user-data script `docker run`s the image on a stock Deep Learning Base AMI, which already ships Docker and the NVIDIA container toolkit |

That split is the one structural difference between providers, and it drives everything
below: on the two container providers the self-destruct must call the provider's own API,
while on AWS it can ask the host to power off.

A single artifact means no per-region public AMIs to copy and re-publish every release — a
real consideration for a community project.

Hashcat is **not** bundled, matching the existing agent images: it syncs from the backend,
so the image stays small and the hashcat version stays under your control.

## What the entrypoint does, in order

### 1. Arm the self-destruct — before anything else

Three independent timers, armed as PID 1's first action:

| Timer | Reset? | Covers |
|---|---|---|
| `KH_DEADLINE_EPOCH` (absolute) | never | the hard cap on what this instance can cost |
| `KH_HEARTBEAT_LOSS_TIMEOUT` | on every successful backend contact | the control plane dying an hour into a six-hour rental |
| `KH_READY_DEADLINE_EPOCH` (absolute) | never | an agent that has **never** reached the backend |

The third exists because the heartbeat timer cannot cover the startup window. It measures
time since the *last* contact, and an agent that never registered has no last contact and
writes no heartbeat file — so that check is skipped entirely and only the TTL remains. A
rented instance whose VPN came up but which could not reach the backend therefore billed
its entire lease. It is absolute rather than a duration for the same reason the kill
deadline is: the container restarts under `--restart=unless-stopped`, and any clock kept
inside it restarts too, so a crash-looping agent would never accumulate the window.

It mirrors the instance row's `ready_deadline_at`, which the backend reaper enforces from
the other side. Both are needed: the reaper cannot act when the backend is down, and "the
backend is unreachable from the instance" is precisely the case this bounds.

This runs **before** the VPN starts and before the image is pulled. A VPN that never
connects, or an agent that never starts, must still result in a destroyed instance — and
only a timer armed before those steps can guarantee that.

!!! danger "Self-destruct never uses the VPN"
    Every provider's teardown call is excluded from the proxy, because the tunnel being dead
    is exactly why the timer is firing. Belt and braces: the `curl`s also pass
    `--noproxy '*'`, so teardown survives even a mis-built `KH_NO_PROXY_EXTRA`.

    | Provider | What the container does |
    |---|---|
    | **Vast.ai** | `DELETE /api/v0/instances/$CONTAINER_ID/` with `$CONTAINER_API_KEY` — a credential scoped to destroying only itself |
    | **RunPod Secure** | `DELETE https://rest.runpod.io/v1/pods/$RUNPOD_POD_ID` with `KH_RUNPOD_API_KEY`, **only if both are present** |
    | **RunPod Community** | Nothing. The key is never injected — see below |
    | **AWS** | Writes `0` to the bind-mounted host deadline file; the host watchdog powers off within 30s, and the instance terminates because it was launched with `InstanceInitiatedShutdownBehavior=terminate` |

    `RUNPOD_POD_ID` is injected by RunPod itself and cannot be passed at create time — it does
    not exist until the create response comes back.

!!! warning "RunPod Community has no in-guest teardown at all"
    RunPod issues no per-pod scoped credential, so the only key that can delete a pod is
    **account-scoped** — it can delete every other pod on the account. On Community the host
    operator has root over the container and would read it out of the environment, so the
    backend never injects it and this branch is simply skipped. Teardown there is the backend
    reaper alone.

    Without a provider branch a pod falls through to the last resort, which in an
    unprivileged container is `kill -9 1` — that kills the **container** while the pod stays
    allocated and keeps billing. The container cannot power itself off: it has no
    `CAP_SYS_BOOT`, and `--restart=unless-stopped` undoes killing PID 1. That is why AWS gets
    a bind-mounted deadline file rather than a `poweroff`.

### 2. Join the VPN in userspace mode

Vast.ai and RunPod containers are unprivileged: no `/dev/net/tun`, no `NET_ADMIN`, and
Vast's create API **silently discards** `--cap-add`/`--device`. Only clients that run a
userspace network stack and expose a local SOCKS5 proxy work there:

| `KH_VPN_PROVIDER` | Mechanism |
|---|---|
| `tailscale` | `tailscaled --tun=userspace-networking --socks5-server=…` (gVisor netstack) |
| `netbird` | `NB_USE_NETSTACK_MODE=true` (gVisor netstack) |
| `wireguard` | `wireproxy` — userspace WireGuard exposing SOCKS5 |

`wireguard-go` and `boringtun` do **not** qualify: they still create a TUN device. **OpenVPN
cannot work here at all** for the same reason — it needs a tun device and `CAP_NET_ADMIN`.
See [Cloud Agent VPN](../admin-guide/system-setup/cloud-vpn.md#supported-vpns) for the full
rationale and for how to obtain a credential for each supported client.

Readiness is judged on the **local SOCKS5 listener accepting a connection**, not on the VPN
client's own status output. That is what the agent actually depends on: a client that started
but never opened its listener is indistinguishable from one that never started, and both have
to count as failure.

If the VPN fails to come up, the container self-destructs rather than sitting there
billing. The VPN is load-bearing for confidentiality here, not just reachability — the
agent's CA fetch and certificate renewal use plain HTTP.

### 3. Point the agent at the tunnel

```bash
export HTTPS_PROXY="socks5://127.0.0.1:1055"
export HTTP_PROXY="socks5://127.0.0.1:1055"
export NO_PROXY="127.0.0.1,localhost,${KH_NO_PROXY_EXTRA}"
```

`KH_NO_PROXY_EXTRA` is set by the backend, not baked into the image, because its contents are
provider-specific — the agent must reach **its own** provider's control plane outside the
tunnel in order to self-destruct when the tunnel is down:

| Provider | `KH_NO_PROXY_EXTRA` | Why |
|---|---|---|
| `vastai` | `console.vast.ai` | The self-destruct `DELETE` |
| `runpod`, `runpod_community` | `api.runpod.io,rest.runpod.io` | `rest.runpod.io` is the REST v1 endpoint teardown actually calls; `api.runpod.io` is GraphQL. Both are listed so a future endpoint change cannot silently break teardown |
| `aws` | `169.254.169.254` | IMDS. Proxying it breaks instance identity lookups and the shutdown path |

!!! warning "`HTTPS_PROXY`, not `ALL_PROXY`"
    Go's `net/http` does **not** read `ALL_PROXY`. A `wss://` URL is rewritten to `https`
    before the proxy function is consulted, so `HTTPS_PROXY` is the variable that governs
    the WebSocket. Go treats `socks5` as `socks5h`, sending the **hostname** to the proxy,
    which is what lets the VPN resolve your backend's private name.

## Environment contract

| Variable | Purpose |
|---|---|
| `KH_HOST` | Backend address **on the VPN** |
| `KH_CLAIM_CODE` | One-time, expiring registration voucher |
| `KH_EPHEMERAL` | `true` — config from environment, never write `.env` |
| `KH_VPN_PROVIDER` | `tailscale` \| `netbird` \| `wireguard` |
| `KH_VPN_AUTH_KEY` | Per-instance ephemeral credential |
| `KH_VPN_LOGIN_SERVER` | Self-hosted control plane URL (optional) |
| `KH_VPN_TAG` | Tag/group to place the node in |
| `KH_VPN_CONFIG` | The **WireGuard** peer config. Routed here rather than to `KH_VPN_AUTH_KEY`, which is a different variable — sending it to the wrong one leaves the tunnel unconfigured and the container self-destructs every time |
| `KH_DEADLINE_EPOCH` | Absolute kill time |
| `KH_HEARTBEAT_LOSS_TIMEOUT` | Seconds of backend silence before self-destruct |
| `KH_HEARTBEAT_FILE` | Where the agent stamps its last successful backend contact. Written **only** by the agent on real contact — never seeded at boot, or a crash-looping container would reset its own deadline |
| `KH_READY_DEADLINE_EPOCH` | Absolute instant by which the agent must have registered once; unset or `0` disables the rail |
| `KH_NO_PROXY_EXTRA` | Provider control-plane hosts to keep off the tunnel — see above |
| `KH_HOST_DEADLINE_FILE` | **AWS only.** The bind-mounted host deadline file; writing `0` asks the host to power off. Absent where there is no host to reach |
| `KH_WATCHDOG_INTERVAL` | How often the in-container watchdog re-checks the three timers |
| `KH_RUNPOD_API_KEY` | **RunPod Secure only, and only when opted in.** Account-scoped; never injected on Community |

### Why ephemeral mode exists

Normally the agent reads `.env` and *not* the environment, so an agent co-located with the
backend cannot inherit the backend's `KH_CONFIG_DIR`/`KH_DATA_DIR`. Cloud agents invert
that: there is no persistent filesystem to carry a `.env`, and writing `KH_CLAIM_CODE` to
disk would hand a live registration credential to whoever operates the rented machine.

See [Configuration](configuration.md#environment-variables-env-file).

## File sync is job-scoped

A cloud agent never runs the full-corpus sync. It receives an explicit download list
containing only what its job needs — wordlists, rules, charsets, the hashlist, the binary —
pre-warmed on connect so the download overlaps with startup instead of stalling a GPU that
is already billing.

Without this, a fresh instance would download the org's entire corpus (150GB+ is common)
at rental rates plus ingress charges — and, because client potfiles are served as
`wordlist` type, would receive **every client's cracked plaintexts**.

## Disk is sized once and cannot grow

`disk_gb` is computed from the job's file set. On **Vast.ai this is immutable after
creation**; on **RunPod** `containerDiskInGb` is set once at create and KrakenHashes never
resizes a pod, so it behaves the same way. An undersized instance reports `AGENT_DISK_FULL`,
which is normally classified *transient* and retried — so for cloud agents it is treated as
**structural**: the instance is terminated rather than retried into a loop that burns its
whole TTL.

## Debugging a rental

1. **Instance stuck `loading` / `pending`** — usually a slow image pull. Check
   `inet_down` on the offer; the ready-deadline reaper destroys it eventually.
2. **Agent never registers** — VPN failed, or the claim code expired. The entrypoint logs
   `[kh-cloud …]` lines before the agent starts.
3. **WebSocket connects but nothing dispatches** — check the agent is job-locked to the
   right job, and that its benchmark completed.
4. **Instance destroyed sooner than expected** — check the budget threshold ladder and the
   `terminate_attempts` / `termination_reason` on the instance row.
5. **A RunPod pod is still billing after the agent died** — expected on **Community**, where
   there is no in-guest rail and the backend reaper is the only teardown. If the backend was
   down, destroy it by hand from the Cloud Fleet page. On **Secure**, check that
   `allow_in_guest_self_destruct` is set and a key was supplied; without both, the pod also
   falls back to reaper-only.
6. **Two pods for one launch** — RunPod has no idempotency key on create, so a lost response
   can leave a pod whose id was never seen. The duplicate shows up in the fleet inventory and
   is reaped as an orphan within a sweep.
7. **None of the above, and it was RunPod or Vast.ai** — those adapters are
   [Experimental](../admin-guide/system-setup/cloud-providers.md#maturity-which-providers-have-actually-been-paid-for)
   and have never been driven end to end with real money, so an unexplained rental there is as
   likely to be our bug as your configuration. Please report it — bug report on GitHub,
   diagnostics by Discord DM:
   [Reporting a problem](../admin-guide/system-setup/cloud-providers.md#reporting-a-problem-with-an-experimental-provider).
   Say what the **provider's own console** showed; that is the fact the backend cannot see.

Vast.ai states `exited`, `unknown` and `offline` **never recover**. KrakenHashes destroys
them immediately rather than polling, because polling them is just spending money.

## Related

- [Cloud GPU Providers](../admin-guide/system-setup/cloud-providers.md) — budgets, trust tiers, provisioning rules
- [Cloud Agent VPN](../admin-guide/system-setup/cloud-vpn.md) — credentials, and why OpenVPN is excluded
- [Cloud GPU Provisioning](../reference/architecture/cloud-provisioning.md) — the teardown ladder and budget accounting
- [Configuration](configuration.md) — ephemeral mode and the full agent environment
