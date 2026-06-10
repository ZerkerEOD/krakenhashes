# Changelog

## 2.2.0 - Agent Auto-Update

This release adds automatic updates for agents, a redesigned agent install experience, and a smoother Agent Management page. Highlights are below.

### Automatic agent updates

- Agents can now update themselves to the latest version automatically. You no longer have to manually re-download and replace the binary on every host.
- Updates are applied only when an agent is idle, so a running job is never interrupted. A busy agent waits until its current work finishes before it updates.
- Each update is checksum verified, backs up the current binary first, and rolls back on its own if the new version fails to come online.
- Updates roll out gradually across your fleet, with a configurable limit on how many agents update at once, so the server and network are not overwhelmed.
- If an update fails, the agent keeps running the previous version and the failure is surfaced in the UI with a manual retry option.

### New launcher

- Auto-updating agents now run under a small companion program called the launcher. The launcher supervises the agent and performs the download, swap, and restart while the agent is stopped, which is what makes safe self-updates possible.
- The launcher installs as a per-user service by default and does not require root or Administrator. It runs as the user who installed it, from that user's own folder (Like a normal agent).
  - Linux: a user systemd service
  - macOS: a per-user login agent (not tested)
  - Windows: a per-user logon task (not tested)
- A system wide service that starts before login is still available for servers by using the `--system` flag, which requires sudo or Administrator.
- You can still run a standalone agent without the launcher if you prefer to manage updates yourself.

### Redesigned agent install

- The Agent Management page now has a single "Install an Agent" panel. Pick your operating system and a dialog walks you through the rest.
- The dialog lets you choose the CPU architecture, pick or generate a claim code, and confirm the server address, which is filled in automatically from the address you are already using.
- For each platform you get ready to copy commands for three options: install as a service, run in a terminal, or download the binary directly.
- The older download section with stacked tabs and per architecture rows has been removed.

### Agent Management page

- The page now updates its data in place. It no longer blanks out and reloads the entire view every 30 seconds, so your scroll position and any open sections are preserved.
- Generating a code, removing an agent, or clearing a stuck status now updates only the affected item instead of refreshing the whole page.

### Admin controls

- New Agent Auto-Update settings in Admin Settings: turn auto-update on or off, set how many agents may update at the same time, set the health check timeout, and set how many times to retry a failed update before giving up.

### Uninstalling

- The launcher can cleanly remove its own service with an uninstall command.
- An optional purge flag also deletes the installed binaries, configuration (including credentials), and downloaded data for a complete removal.

### Upgrade notes

- Upgrade your server to this release as usual. Auto-update becomes available once the server is upgraded.
- Existing agents keep working. To get automatic updates going forward, reinstall the agent using the launcher from the new install dialog. Standalone agents keep working and continue to be updated manually.
- Auto-update is on by default and is controlled by the new admin setting. Adjust the rollout limit and the other options under Admin Settings if you want to change the defaults.
