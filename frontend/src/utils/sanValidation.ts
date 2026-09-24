import { SanKind } from '../types/serverCertificate';

/**
 * Client-side mirror of the server's certificate-name policy.
 *
 * This is a convenience only: it gives immediate feedback as an admin types
 * rather than after a round trip. The SERVER re-validates every entry and is the
 * authority — do not remove the server-side check on the strength of this file
 * existing.
 */

export type SanRejectionReason =
  | 'invalid_format'
  | 'public_ip'
  | 'duplicate'
  | 'bind_address'
  | 'multicast'
  | 'wildcard_dns'
  | 'has_port'
  | 'is_ip'
  | 'too_long';

export type SanValidation = { ok: true; value: string } | { ok: false; reason: SanRejectionReason };

/** IPv4 ranges a KrakenHashes certificate may name, as [network, maskBits]. */
const PRIVATE_V4_RANGES: Array<[string, number]> = [
  ['10.0.0.0', 8], // RFC 1918
  ['172.16.0.0', 12], // RFC 1918
  ['192.168.0.0', 16], // RFC 1918
  ['100.64.0.0', 10], // RFC 6598 CGNAT — Tailscale, NetBird
  ['127.0.0.0', 8], // loopback
  ['169.254.0.0', 16], // link-local
];

const ipv4ToInt = (ip: string): number | null => {
  const parts = ip.split('.');
  if (parts.length !== 4) return null;
  let value = 0;
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part)) return null;
    const octet = Number(part);
    if (octet > 255) return null;
    value = value * 256 + octet;
  }
  return value;
};

const v4InRange = (value: number, network: string, bits: number): boolean => {
  const networkValue = ipv4ToInt(network);
  if (networkValue === null) return false;
  // Shift arithmetic in JS is 32-bit signed; >>> 0 keeps the mask unsigned.
  const mask = bits === 0 ? 0 : (~0 << (32 - bits)) >>> 0;
  return ((value & mask) >>> 0) === ((networkValue & mask) >>> 0);
};

const isPrivateV4 = (ip: string): boolean => {
  const value = ipv4ToInt(ip);
  if (value === null) return false;
  return PRIVATE_V4_RANGES.some(([network, bits]) => v4InRange(value, network, bits));
};

const isPrivateV6 = (ip: string): boolean => {
  const lower = ip.toLowerCase();
  if (lower === '::1') return true;
  // fd00::/8 unique local, fe80::/10 link-local.
  if (/^fd[0-9a-f]{2}:/.test(lower)) return true;
  if (/^fe[89ab][0-9a-f]:/.test(lower)) return true;
  return false;
};

const looksLikeIPv6 = (value: string): boolean => value.includes(':');

/** True when the address falls inside the allowed private/VPN ranges. */
export function isPrivateAddress(ip: string): boolean {
  const value = ip.trim().replace(/^\[|\]$/g, '');
  // Normalise an IPv4-mapped IPv6 form before testing, or ::ffff:8.8.8.8 would
  // be checked against the IPv6 rules and slip through as "not matched".
  const mapped = value.toLowerCase().match(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/);
  if (mapped) return isPrivateV4(mapped[1]);
  if (looksLikeIPv6(value)) return isPrivateV6(value);
  return isPrivateV4(value);
}

/** True when the string parses as any IP address at all. */
export function looksLikeIP(value: string): boolean {
  const trimmed = value.trim().replace(/^\[|\]$/g, '');
  if (looksLikeIPv6(trimmed)) return /^[0-9a-fA-F:.]+$/.test(trimmed);
  return ipv4ToInt(trimmed) !== null;
}

export function validateIpSan(raw: string, existing: string[]): SanValidation {
  const value = raw.trim().replace(/^\[|\]$/g, '');
  if (!value) return { ok: false, reason: 'invalid_format' };
  if (!looksLikeIP(value)) return { ok: false, reason: 'invalid_format' };

  if (value === '0.0.0.0' || value === '::') return { ok: false, reason: 'bind_address' };
  if (/^(22[4-9]|23\d)\./.test(value) || value.toLowerCase().startsWith('ff')) {
    return { ok: false, reason: 'multicast' };
  }
  if (!isPrivateAddress(value)) return { ok: false, reason: 'public_ip' };
  if (existing.some((e) => e.toLowerCase() === value.toLowerCase())) {
    return { ok: false, reason: 'duplicate' };
  }

  return { ok: true, value };
}

export function validateDnsSan(raw: string, existing: string[]): SanValidation {
  const value = raw.trim().toLowerCase().replace(/\.$/, '');
  if (!value) return { ok: false, reason: 'invalid_format' };
  if (value.length > 253) return { ok: false, reason: 'too_long' };
  if (value.includes('*')) return { ok: false, reason: 'wildcard_dns' };
  if (value.includes('://') || /[/\\ ]/.test(value)) return { ok: false, reason: 'invalid_format' };
  if (value.includes(':')) return { ok: false, reason: 'has_port' };
  if (looksLikeIP(value)) return { ok: false, reason: 'is_ip' };

  const labels = value.split('.');
  for (const label of labels) {
    if (!label || label.length > 63) return { ok: false, reason: 'invalid_format' };
    if (label.startsWith('-') || label.endsWith('-')) return { ok: false, reason: 'invalid_format' };
    if (!/^[a-z0-9-]+$/.test(label)) return { ok: false, reason: 'invalid_format' };
  }

  if (existing.some((e) => e.toLowerCase() === value)) return { ok: false, reason: 'duplicate' };

  return { ok: true, value };
}

export function validateSan(kind: SanKind, raw: string, existing: string[]): SanValidation {
  return kind === 'ip' ? validateIpSan(raw, existing) : validateDnsSan(raw, existing);
}
