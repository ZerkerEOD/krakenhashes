/**
 * Agent version helpers.
 *
 * Agents report a build-time version string that is either a tagged release
 * ("2.1.0", "v2.1.0") or a dev build ("2.1.0-9df8480-dev"). The cluster's
 * expected version (from versions.json via /api/public/agent/version) is always
 * a clean release ("2.1.0"). These helpers display both correctly and compare
 * them on the release portion only — matching the backend's suffix-stripping
 * stale check — so a dev build of the current release reads as up-to-date.
 */

export interface ParsedVersion {
  /** Release portion, e.g. "2.1.0" (no leading v, no -commit-dev suffix). */
  release: string;
  /** Commit hash for a dev build, else undefined. */
  commit?: string;
  /** True when the version carries any pre-release / dev suffix. */
  isDev: boolean;
  /** The original raw string. */
  raw: string;
}

export function parseAgentVersion(raw: string | undefined | null): ParsedVersion {
  const value = (raw || '').trim();
  const noV = value.replace(/^v/i, '');
  const dash = noV.indexOf('-');
  if (dash === -1) {
    return { release: noV, isDev: false, raw: value };
  }
  const rest = noV.slice(dash + 1); // e.g. "9df8480-dev" or "9df8480"
  return {
    release: noV.slice(0, dash),
    commit: rest.split('-')[0] || undefined,
    isDev: rest.length > 0,
    raw: value,
  };
}

/** Display label: "v2.1.0" for releases, "2.1.0 (dev)" for dev builds. */
export function formatAgentVersion(raw: string | undefined | null): string {
  const p = parseAgentVersion(raw);
  if (!p.release) return 'Unknown';
  return p.isDev ? `${p.release} (dev)` : `v${p.release}`;
}

export type VersionStatus = 'up-to-date' | 'update-available' | 'unknown';

/**
 * Compare an agent's reported version to the expected cluster version on the
 * release portion only (so dev builds of the current release are up-to-date).
 */
export function agentVersionStatus(
  agentVersion: string | undefined | null,
  expectedVersion: string | undefined | null,
): VersionStatus {
  const a = parseAgentVersion(agentVersion).release;
  const e = parseAgentVersion(expectedVersion).release;
  if (!a || !e) return 'unknown';
  return compareReleases(a, e) < 0 ? 'update-available' : 'up-to-date';
}

function compareReleases(a: string, b: string): number {
  const pa = a.split('.').map((n) => parseInt(n, 10) || 0);
  const pb = b.split('.').map((n) => parseInt(n, 10) || 0);
  for (let i = 0; i < 3; i++) {
    const x = pa[i] || 0;
    const y = pb[i] || 0;
    if (x !== y) return x < y ? -1 : 1;
  }
  return 0;
}
