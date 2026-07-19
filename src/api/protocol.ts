// SPDX-License-Identifier: AGPL-3.0-only

export type ProtocolErrorCode =
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'stale_revision'
  | 'validation_failed'
  | 'busy'
  | 'unavailable'
  | 'internal';

export interface WireError {
  code: ProtocolErrorCode;
  message: string;
  details: Record<string, unknown>;
}

export interface SuccessEnvelope<T> {
  ok: true;
  data: T;
}

export interface ErrorEnvelope {
  ok: false;
  error: WireError;
}

export interface WireIdentity {
  hostname: string;
  os: {
    id: string;
    versionId: string;
    prettyName: string;
    kernel: string;
  };
  architecture: string;
  bootId: string;
  serverTime: string;
  user?: {
    name: string;
    uid: string;
    gid: string;
    home: string;
  };
}

export interface WireOverview {
  uptimeSeconds: number;
  cpuUsagePercent: number;
  memoryUsedBytes: number;
  memoryTotalBytes: number;
  updatesPending: number;
  securityUpdatesPending: number;
  failedUnits: number;
  rebootRequired: boolean;
}

export interface WireMetricsSample {
  ts: number;
  cpu: {
    usagePercent: number;
    load1: number;
    load5: number;
    load15: number;
    cores: number;
  };
  memory: {
    totalBytes: number;
    usedBytes: number;
    availableBytes: number;
  };
  disks: { mount: string; totalBytes: number; usedBytes: number }[];
  network: { interface: string; rxBytesPerSec: number; txBytesPerSec: number }[];
}

export interface WireServiceUnit {
  name: string;
  description: string;
  loadState: string;
  activeState: string;
  subState: string;
  enabledState: string;
}

export interface WireServicesList {
  units: WireServiceUnit[];
}

export interface WireServiceActionResult {
  unit: {
    name: string;
    activeState: string;
    subState: string;
    enabledState: string;
  };
}

export interface WireSessionUser {
  name: string;
  uid: number;
  gid: number;
  home: string;
}

export interface WireAuthLogin {
  user: WireSessionUser;
  csrf?: string;
}

export interface WireAuthSession {
  user: WireSessionUser;
}

export interface WireReauth {
  reauthenticatedUntil: number;
}

export type WireServicesEvent =
  | { kind: 'snapshot'; units: WireServiceUnit[] }
  | { kind: 'changed'; unit: Partial<WireServiceUnit> & { name: string } };

export interface WireJournalEntry {
  cursor: string;
  ts: string;
  priority: string;
  unit: string;
  message: string;
  fields: Record<string, string>;
}

export interface WireJournalPage {
  entries: WireJournalEntry[];
  nextCursor: string | null;
}

export interface WireFileEntry {
  name: string;
  type: string;
  sizeBytes: number;
  mode: string;
  modifiedAt: string;
  symlinkTarget: string | null;
}

export interface WireFilesList {
  path: string;
  entries: WireFileEntry[];
}

export interface WireFileRead {
  path: string;
  sizeBytes: number;
  revision: string;
  encoding: string;
  content: string;
  truncated: boolean;
}

export interface WireFileWrite {
  path: string;
  revision: string;
  sizeBytes: number;
}

export type ServerFrame =
  | { type: 'hello'; protocol: number; serverVersion: string }
  | { type: 'ping'; ts: number }
  | { type: 'subscribed'; channel: number; data?: unknown }
  | { type: 'event'; channel: number; seq: number; data: unknown }
  | { type: 'error'; channel: number; error: WireError }
  | { type: 'closed'; channel: number; error: WireError | null };
