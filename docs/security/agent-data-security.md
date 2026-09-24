# Agent Data Security

This document describes how KrakenHashes protects sensitive data on agent machines, including the trust model for multi-team environments and file-level security measures.

## Sensitive Data on Agents

During job execution, agents download several types of sensitive files:

| File Type | Contains | Retention |
|-----------|----------|-----------|
| Hashlists | Password hashes to crack | Deleted after task completion |
| Client potfiles | Previously cracked passwords | Deleted after task completion |
| Client wordlists | Client-specific word lists | Deleted after task completion |
| Rule chunks | Hashcat rule segments | Deleted after task completion |
| Shared wordlists | Global wordlists | Retained (not client-specific) |
| Shared rules | Global rule files | Retained (not client-specific) |

Hashcat requires input files on disk — it cannot read hashlists from stdin. Files on disk are unavoidable during execution.

## Trust Model

The trust model controls **which agents receive which teams' jobs**. This is the primary defense against cross-team data exposure.

### Agent Ownership

Agents inherit ownership from the voucher used to register them:

- **System agents**: Registered with a voucher created as "System Agent" by an admin. These agents belong to the **Default Team**. Owner is the system user (`00000000-0000-0000-0000-000000000000`). Teams must trust the Default Team to use system agents.
- **User-owned agents**: Registered with a standard voucher. These agents serve only the teams their owner belongs to.
- **Ownerless agents**: Agents without an owner (legacy/edge case) also belong to the Default Team.

### The Default Team

The Default Team (`00000000-0000-0000-0000-000000000001`) serves as:
- Home for **system agents** — admin-managed shared agents
- Safety net for **orphaned clients** (auto-assigned when teams enabled, or when a team is deleted)
- Fallback for **legacy hashlists** without a client association

The Default Team cannot be deleted. It is protected at both the database and application level.

### Trust Relationships

Trust relationships are **directional** and managed per-team:

- If **Team Audit** trusts **Default Team**, then system agents can run Audit's jobs
- If **Team Audit** trusts **Team IT**, then IT's agents can run Audit's jobs
- This does **not** mean Audit's agents can run IT's jobs (IT must separately trust Audit)
- By default, no trust relationships exist — teams only use their own agents

### Scheduling Logic

When the scheduler assigns agents to jobs, all agents (including system) go through the same logic:

1. **Direct team match** → Agent's team(s) overlap with job's team(s) → Allowed
2. **Trust match** → Job's team trusts one of the agent's teams → Allowed
3. **No match** → Agent is excluded from this job

System agents have `[Default Team]` as their team. They access a team's jobs only if that team trusts Default Team (or if the job itself belongs to Default Team).

### Default Posture

When teams are first enabled:
- No trust relationships exist (trust nobody)
- System agents belong to Default Team — they only serve Default Team's jobs until teams trust Default
- User-owned agents serve only their owner's teams
- Team admins configure trust as needed (e.g., trust Default Team to use shared system agents)

## File-Level Protections

### Immediate Post-Task Cleanup

After every task completes (success or failure), the agent immediately deletes:
- The hashlist file
- Original hashlist file (for hashcat mode 9)
- Client potfile
- All client wordlists
- Rule chunk files
- Empty client directories

This minimizes the window during which sensitive data exists on disk.

### File Permissions

All directories created for job data use restrictive permissions:
- **Directories**: `0700` (owner read/write/execute only)
- **Downloaded files**: `0600` (owner read/write only)

This prevents other users on the same machine from reading sensitive files.

### Periodic Cleanup

A background cleanup process runs periodically to catch any files missed by immediate cleanup:
- Client wordlist directories older than 24 hours are removed
- Hashlist files older than 24 hours are removed
- Empty client directories are pruned

### Re-download Policy

Client wordlists are always re-downloaded for each task rather than cached. This ensures:
- No stale data persists between tasks
- Files are cleaned up reliably after each task
- No cross-task data leakage

## Deployment Recommendations

### Dedicated Service Account

Run the agent as a dedicated, unprivileged user account to isolate it from other services:

**Linux (systemd)**:
```bash
# Create dedicated user
sudo useradd -r -s /usr/sbin/nologin -d /opt/krakenhashes-agent kragent

# Set up directories
sudo mkdir -p /opt/krakenhashes-agent
sudo chown kragent:kragent /opt/krakenhashes-agent

# Create systemd service
sudo cat > /etc/systemd/system/krakenhashes-agent.service << 'EOF'
[Unit]
Description=KrakenHashes Agent
After=network.target

[Service]
Type=simple
User=kragent
Group=kragent
WorkingDirectory=/opt/krakenhashes-agent
ExecStart=/opt/krakenhashes-agent/agent
Restart=on-failure
RestartSec=10

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/krakenhashes-agent
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now krakenhashes-agent
```

**macOS (launchd)**:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.krakenhashes.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/opt/krakenhashes-agent/agent</string>
    </array>
    <key>WorkingDirectory</key>
    <string>/opt/krakenhashes-agent</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>UserName</key>
    <string>kragent</string>
</dict>
</plist>
```

### Docker Deployment

The Docker agent image already runs as a non-root user, providing isolation by default. GPU passthrough is required for hashcat execution:

```bash
docker run -d \
  --gpus all \
  --name krakenhashes-agent \
  -e KH_HOST=your-server:31337 \
  -e KH_CLAIM_CODE=YOUR_VOUCHER \
  krakenhashes/agent:latest
```

### Network Segmentation

For maximum isolation:
- Place agents on a separate network segment from the backend
- Only allow outbound connections from agents to the backend API/WebSocket port
- Block agent-to-agent communication if agents serve different teams

## Rented (cloud) agents

A cloud agent is an ephemeral GPU rented for one job and destroyed afterwards. Everything
above still applies, plus three things that do not arise with hardware you own.

### The VPN is mandatory

Cloud agents never reach the backend over the open internet. They join the operator's existing
VPN — Tailscale, NetBird or WireGuard, always in userspace mode — and provisioning **fails
closed** without a credential, at two independent points. There is no direct-connection mode
and no bypass, including for the mock provider. See
[Cloud Agent VPN](../admin-guide/system-setup/cloud-vpn.md).

The rented container is third-party hardware, so keeping it on the overlay is what stops a
machine someone else operates from talking to a public endpoint. It is also load-bearing for
confidentiality rather than just reachability: the agent's CA fetch and certificate renewal
use plain HTTP inside the tunnel.

### On peer-operated tiers, the host has root over your container

**Vast.ai** and **RunPod Community** are individually- or peer-owned machines. The owner of
the machine your job lands on has root over the container, and can read the hashes, wordlists,
potfiles and cracked plaintexts on it. **The data is not encrypted at rest on the host.**

The protection there is a terms-of-service clause, not an isolation boundary. Do not use those
tiers for production or client engagement data.

Because it is a policy boundary rather than a technical one, it is gated by consent rather
than by a control — **three** separate opt-ins, each attributed in the audit log:

1. A provider-level acknowledgement before the config can be enabled.
2. A per-client acknowledgement before that client may allowlist it.
3. A **per-job** opt-in before any job lands on one.

Without the per-job flag a job simply does not see peer offers; it can still rent from the
secure providers in the client's allowlist, so leaving it off degrades rather than blocks.

AWS and RunPod Secure carry none of this: they are your own account or a single-tenant SOC 2
datacenter.

### Credentials are never written to disk on the instance

Cloud agents run in **ephemeral mode** — configuration comes from the environment and no
`.env` is ever written. Writing the claim code to disk would hand a live registration
credential to whoever operates the rented machine.

File sync is **job-scoped** for the same reason: a rented instance receives an explicit
download list containing only what its job needs, never the org's whole corpus. Without that
it would also receive every client's cracked plaintexts, since client potfiles are served as
wordlists.

One credential is a deliberate exception worth understanding. **RunPod Secure** can optionally
be given an API key so the pod can delete itself when it loses contact — and RunPod issues no
per-pod scoped credential, so that key is **account-scoped**. Supply a narrower
`self_destruct_api_key` rather than reusing the provisioning key. On **RunPod Community** the
key is never injected at all, which is exactly why teardown there is reaper-only.

## Security Limitations

- **No secure erasure**: Files are deleted with standard `os.Remove()`, not secure wiping. Data may be recoverable from disk with forensic tools until overwritten. For environments requiring secure erasure, use full-disk encryption on agent machines.
- **GPU memory**: Hashcat loads data into GPU memory during execution. GPU memory is not explicitly cleared after tasks.
- **Hashcat temp files**: Hashcat may create its own temporary files during execution that are outside the agent's cleanup scope.
- **Shared wordlists/rules**: Global (non-client) wordlists and rules are retained on disk and shared across all jobs. These are not considered client-sensitive data.
- **Trust is advisory**: The trust model controls job scheduling, not file access. An agent with filesystem access could theoretically access any file in its data directory regardless of team boundaries.
- **Peer-operated cloud hosts are outside every protection above**: on Vast.ai and RunPod Community the host operator has root over the container, so file permissions, post-task cleanup and periodic cleanup are all defences the host can simply ignore. Treat anything placed there as disclosed.
