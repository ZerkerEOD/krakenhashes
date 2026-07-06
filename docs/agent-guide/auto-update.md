# Agent Auto-Update

Agents can keep themselves up to date automatically. Instead of manually re-downloading the binary
and replacing it on every host, an auto-updating agent downloads new versions, verifies and installs
them while idle, and rolls back on its own if something goes wrong. This page explains how it works,
how to install an auto-updating agent, and the administrator controls that govern the rollout.

## How automatic updates work

- **Idle-only:** An update is applied only when the agent is idle. If the agent is running a job, it
  finishes its current work first — a running job is never interrupted to update.
- **Verified and reversible:** Each update is **checksum verified**, the current binary is **backed
  up** first, and if the new version fails to come back online the agent **automatically rolls back**
  to the previous binary and keeps running.
- **Gradual rollout:** Updates roll out across your fleet gradually, with a configurable limit on how
  many agents update at once, so the server and network aren't overwhelmed.
- **Failures are visible:** If an update fails, the agent keeps running the previous version, and the
  failure is surfaced on the Agent Management page with a **manual retry** option.

## The launcher

Auto-updating agents run under a small companion program called the **launcher**. The launcher
supervises the agent process and performs the download, binary swap, and restart while the agent is
stopped — that brief, supervised stop is what makes a safe self-update possible (a process can't
reliably replace its own running binary).

By default the launcher installs as a **per-user service** and does **not** require root or
Administrator. It runs as the user who installed it, out of that user's own folder — just like a
normally-run agent:

| Platform | Per-user service |
|----------|------------------|
| Linux | a user **systemd** service |
| macOS | a per-user login agent *(not tested)* |
| Windows | a per-user logon task *(not tested)* |

For servers that must start the agent **before any user logs in**, install a system-wide service with
the `--system` flag, which requires `sudo` (Linux/macOS) or Administrator (Windows).

!!! note "You can still run a standalone agent"
    Using the launcher is optional. You can continue to run a [standalone agent](#standalone-agents-without-the-launcher)
    and manage updates yourself if you prefer.

## Installing an auto-updating agent

The easiest path is the **Install an Agent** panel on the Agent Management page:

1. In the Admin UI, open **Agents** and use the **Install an Agent** panel.
2. Choose your **operating system**. A dialog walks you through the rest.
3. Pick the **CPU architecture**, pick or generate a **claim code**, and confirm the **server
   address** (pre-filled from the address you're already using).
4. Copy one of the ready-made commands. Each platform offers three options:
   - **Install as a service** (uses the launcher — recommended for auto-updates)
   - **Run in a terminal**
   - **Download the binary directly**

The service option produces a launcher `install` command equivalent to:

```bash
# Per-user service (no root/Administrator needed)
krakenhashes-launcher install --host SERVER:31337 --claim YOUR_CLAIM_CODE

# System-wide service (starts before login; needs sudo/Administrator)
krakenhashes-launcher install --system --host SERVER:31337 --claim YOUR_CLAIM_CODE
```

Optional `--config-dir` and `--data-dir` flags relocate the configuration/credentials and the
binaries/wordlists/rules/hashlists directories.

## Standalone agents (without the launcher)

A standalone agent — one started directly rather than through the launcher — keeps working exactly as
before and **does not auto-update**. You update it manually by replacing its binary. This is a valid
choice if you want full control over when each host changes versions. See
[Installation](installation.md) for running the agent directly.

## Uninstalling

The launcher can cleanly remove its own service:

```bash
# Remove the service (leaves binaries, config, and data in place)
krakenhashes-launcher uninstall            # add --system if it was a system install

# Full removal: also delete binaries, configuration (including credentials), and downloaded data
krakenhashes-launcher uninstall --purge    # add --system for a system install
```

!!! warning "`--purge` deletes credentials and data"
    The `--purge` flag removes the installed binaries, the configuration (including the agent's
    credentials), and downloaded data for a complete removal. Use it only when you intend to fully
    decommission the agent on that host.

## Administrator controls

### Agent Auto-Update settings

Under **Admin → Settings**, the **Agent Auto-Update** settings govern the fleet-wide rollout:

| Setting | Purpose |
|---------|---------|
| **Enable auto-update** | Turn automatic updates on or off globally. On by default. |
| **Max agents updating at once** | The rollout limit — how many agents may update simultaneously. |
| **Health check timeout** | How long to wait for an updated agent to report healthy before treating the update as failed (and rolling back). |
| **Retry count** | How many times to retry a failed update before giving up. |

### On the Agent Management page

- The page updates **in place** rather than blanking out and reloading every 30 seconds, so your
  scroll position and any open sections are preserved. Generating a code, removing an agent, or
  clearing a stuck status updates only the affected row.
- A failed update is shown on the affected agent with a **manual retry** option.
- An agent's detail view shows its **version** relative to the server, so you can see at a glance
  when a host is out of date.

## Upgrade notes

- Upgrade the **server** to the release that includes auto-update as usual; the feature becomes
  available once the server is upgraded.
- **Existing agents keep working.** To get automatic updates going forward, reinstall the agent using
  the launcher from the **Install an Agent** dialog. Standalone agents keep working and continue to be
  updated manually.
- Auto-update is **on by default** and controlled by the admin setting. Adjust the rollout limit and
  the other options under Admin Settings to change the defaults.

## See also

- [Installation](installation.md) — installing and running agents
- [Systemd Setup](systemd-setup.md) — running agents as services
- [Configuration](configuration.md) — agent configuration options
- [Update Procedures](../deployment/updates.md) — upgrading the overall deployment
