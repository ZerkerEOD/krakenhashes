/**
 * One canonical status → tone map per entity, mirrored from the backend enums
 * in `backend/internal/models`. Every page renders status through
 * `StatusChip` or `getStatusTone`, so a colour decision is made exactly once.
 */
import type { StatusTone } from '../../styles/palette';

export type StatusEntity =
  | 'job'
  | 'task'
  | 'agent'
  | 'hashlist'
  | 'verification'
  | 'cloud'
  | 'loopback'
  | 'audit'
  | 'diagnostic'
  | 'sync'
  | 'generic';

const job: Record<string, StatusTone> = {
  preparing: 'idle',
  pending: 'queued',
  running: 'running',
  paused: 'warning',
  processing: 'info',
  completed: 'success',
  failed: 'error',
  cancelled: 'idle',
  archived: 'idle',
};

const task: Record<string, StatusTone> = {
  pending: 'queued',
  assigned: 'queued',
  reconnect_pending: 'warning',
  running: 'running',
  processing: 'info',
  processing_error: 'warning',
  completed: 'success',
  failed: 'error',
  cancelled: 'idle',
};

const agent: Record<string, StatusTone> = {
  pending: 'queued',
  active: 'success',
  inactive: 'idle',
  error: 'error',
  disabled: 'idle',
  updating: 'info',
  // legacy aliases that still appear in a few payloads
  online: 'success',
  offline: 'idle',
};

const hashlist: Record<string, StatusTone> = {
  uploading: 'info',
  processing: 'running',
  ready: 'success',
  error: 'error',
  deleting: 'warning',
  ready_with_errors: 'warning',
  awaiting_validation_decision: 'warning',
  cancelled: 'idle',
};

const verification: Record<string, StatusTone> = {
  pending: 'queued',
  verified: 'success',
  failed: 'error',
  deleted: 'idle',
};

const cloud: Record<string, StatusTone> = {
  requested: 'queued',
  launching: 'info',
  provisioning: 'info',
  syncing: 'info',
  running: 'running',
  draining: 'warning',
  terminating: 'warning',
  terminated: 'idle',
  failed: 'error',
};

const loopback: Record<string, StatusTone> = {
  waiting: 'queued',
  active: 'running',
  completed: 'success',
  failed: 'error',
  cancelled: 'idle',
};

const audit: Record<string, StatusTone> = {
  info: 'info',
  warning: 'warning',
  critical: 'error',
};

const diagnostic: Record<string, StatusTone> = {
  info: 'info',
  warning: 'warning',
  error: 'error',
};

const sync: Record<string, StatusTone> = {
  pending: 'queued',
  in_progress: 'running',
  completed: 'success',
  failed: 'error',
};

const generic: Record<string, StatusTone> = {
  success: 'success',
  ok: 'success',
  healthy: 'success',
  enabled: 'success',
  active: 'success',
  verified: 'success',
  warning: 'warning',
  degraded: 'warning',
  error: 'error',
  failed: 'error',
  critical: 'error',
  info: 'info',
  running: 'running',
  pending: 'queued',
  queued: 'queued',
  disabled: 'idle',
  inactive: 'idle',
  unknown: 'idle',
};

export const STATUS_MAPS: Record<StatusEntity, Record<string, StatusTone>> = {
  job,
  task,
  agent,
  hashlist,
  verification,
  cloud,
  loopback,
  audit,
  diagnostic,
  sync,
  generic,
};

/** Statuses that mean "work is actively happening" and may pulse. */
export const ACTIVE_STATUSES: Partial<Record<StatusEntity, string[]>> = {
  job: ['running', 'processing'],
  task: ['running', 'processing'],
  hashlist: ['uploading', 'processing', 'deleting'],
  cloud: ['launching', 'provisioning', 'syncing', 'draining', 'terminating'],
  loopback: ['active'],
  sync: ['in_progress'],
  agent: ['updating'],
};

export const getStatusTone = (entity: StatusEntity, status: string | null | undefined): StatusTone => {
  if (!status) return 'default';
  const key = String(status).toLowerCase();
  return STATUS_MAPS[entity]?.[key] ?? generic[key] ?? 'default';
};

export const isActiveStatus = (entity: StatusEntity, status: string | null | undefined): boolean =>
  Boolean(status && ACTIVE_STATUSES[entity]?.includes(String(status).toLowerCase()));

/** `ready_with_errors` → "Ready with errors" (fallback label when no translation exists). */
export const humanizeStatus = (status: string): string => {
  const s = status.replace(/[_-]+/g, ' ').trim();
  return s.charAt(0).toUpperCase() + s.slice(1);
};
