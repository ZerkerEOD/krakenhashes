# Cloud Agent Deployment

Cloud agents are ephemeral: rented, used for one job, and destroyed. They are provisioned
by the backend rather than installed by hand — this page explains what the image does, so
you can reason about it and debug it.

For provider credentials and budgets, see
[Cloud Providers](../admin-guide/system-setup/cloud-providers.md).

## One image, both providers

CI publishes `krakenhashes-agent-cloud`. On Vast.ai it *is* the instance; on AWS the
user-data script `docker run`s it on a stock Deep Learning Base AMI, which already ships
Docker and the NVIDIA container toolkit.

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
    On Vast.ai the container calls `DELETE /api/v0/instances/$CONTAINER_ID/` with
    `$CONTAINER_API_KEY` (a credential scoped to destroying only itself). That call is
    excluded from the proxy via `NO_PROXY`, because the tunnel being dead is exactly why
    the heartbeat timer is firing. On AWS it calls `poweroff --force --force`, which
    terminates because the instance is launched with
    `InstanceInitiatedShutdownBehavior=terminate`.

### 2. Join the VPN in userspace mode

Vast.ai containers are unprivileged: no `/dev/net/tun`, no `NET_ADMIN`, and the create API
**silently discards** `--cap-add`/`--device`. Only clients that run a userspace network
stack and expose a local SOCKS5 proxy work there:

| `KH_VPN_PROVIDER` | Mechanism |
|---|---|
| `tailscale` | `tailscaled --tun=userspace-networking --socks5-server=…` (gVisor netstack) |
| `netbird` | `NB_USE_NETSTACK_MODE=true` (gVisor netstack) |
| `wireguard` | `wireproxy` — userspace WireGuard exposing SOCKS5 |

`wireguard-go` and `boringtun` do **not** qualify: they still create a TUN device.

If the VPN fails to come up, the container self-destructs rather than sitting there
billing. The VPN is load-bearing for confidentiality here, not just reachability — the
agent's CA fetch and certificate renewal use plain HTTP.

### 3. Point the agent at the tunnel

```bash
export HTTPS_PROXY="socks5://127.0.0.1:1055"
export NO_PROXY="127.0.0.1,localhost,<provider control plane>"
```

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
| `KH_DEADLINE_EPOCH` | Absolute kill time |
| `KH_HEARTBEAT_LOSS_TIMEOUT` | Seconds of backend silence before self-destruct |
| `KH_READY_DEADLINE_EPOCH` | Absolute instant by which the agent must have registered once; unset or `0` disables the rail |

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
creation**. An undersized instance reports `AGENT_DISK_FULL`, which is normally classified
*transient* and retried — so for cloud agents it is treated as **structural**: the instance
is terminated rather than retried into a loop that burns its whole TTL.

## Debugging a rental

1. **Instance stuck `loading` / `pending`** — usually a slow image pull. Check
   `inet_down` on the offer; the ready-deadline reaper destroys it eventually.
2. **Agent never registers** — VPN failed, or the claim code expired. The entrypoint logs
   `[kh-cloud …]` lines before the agent starts.
3. **WebSocket connects but nothing dispatches** — check the agent is job-locked to the
   right job, and that its benchmark completed.
4. **Instance destroyed sooner than expected** — check the budget threshold ladder and the
   `terminate_attempts` / `termination_reason` on the instance row.

Vast.ai states `exited`, `unknown` and `offline` **never recover**. KrakenHashes destroys
them immediately rather than polling, because polling them is just spending money.
