// SPDX-License-Identifier: AGPL-3.0-only
import { LiveDataSource } from './client';
import { ApiError } from './transport';
import { MockDataSource } from '../mock/source';

export type ServiceState = 'active' | 'inactive' | 'failed';
export type ServiceAction = 'start' | 'stop' | 'restart' | 'reload' | 'enable' | 'disable';
export type PowerAction = 'reboot' | 'poweroff';

export interface PowerSchedule {
  action: `system.${PowerAction}`;
  scheduledAt: string;
}

export interface NetworkRoute {
  to: string;
  via: string;
  metric?: number;
}

export interface NetworkNameservers {
  addresses?: string[];
  search?: string[];
}

export interface EthernetConfig {
  dhcp4: boolean;
  dhcp6: boolean;
  addresses?: string[];
  nameservers?: NetworkNameservers;
  routes?: NetworkRoute[];
  optional?: boolean;
}

export interface NetworkConfig {
  version: 2;
  ethernets: Record<string, EthernetConfig>;
}

export interface NetworkInterface {
  name: string;
  hardwareAddress?: string;
  addresses: string[];
  up: boolean;
  loopback: boolean;
}

export interface NetworkSnapshot {
  revision: string;
  interfaces: NetworkInterface[];
}

export interface PendingNetworkChange {
  token: string;
  previousRevision: string;
  expiresAt: string;
  confirmTimeoutSec: number;
}

export interface NetworkConfirmation {
  token: string;
  confirmed: true;
}

export interface ServiceUnit {
  name: string;
  description: string;
  state: ServiceState;
  enabled: boolean;
  pid: number | null;
  memoryMb: number;
  since: string;
}

export interface ServiceDependency {
  name: string;
  relation: 'requires' | 'wants';
}

export interface ServiceUnitFile {
  path: string;
  content: string | null;
  override: boolean;
  error: string | null;
}

export interface ServiceDetail {
  name: string;
  documentation: string[];
  dependencies: ServiceDependency[];
  files: ServiceUnitFile[];
}

export interface SystemAlert {
  id: string;
  level: 'info' | 'warning' | 'critical';
  text: string;
}

export interface SystemOverview {
  hostname: string;
  os: string;
  kernel: string;
  bootedAt: number;
  cpuPercent: number;
  memoryUsedMb: number;
  memoryTotalMb: number;
  storageUsedGb: number;
  storageTotalGb: number;
  pendingUpdates: number;
  securityUpdates: number;
  alerts: SystemAlert[];
  cpuHistory: number[];
}

export interface SystemIdentity {
  hostname: string;
  os: string;
  kernel: string;
  architecture: string;
  bootId: string;
  serverTime: string;
}

export interface LoadSample {
  cpuPercent: number;
  netDownKbps: number;
  netUpKbps: number;
}

export type LogPriority = 'err' | 'warning' | 'info' | 'debug';

export interface LogLine {
  id: number;
  timestamp: number;
  priority: LogPriority;
  priorityCode: 3 | 4 | 6 | 7;
  unit: string;
  message: string;
  pid: number;
  hostname: string;
  bootId: string;
  fields: Record<string, string>;
}

export type JournalBoot = 'current' | 'previous';

export interface JournalQuery {
  unit?: string;
  priority?: LogPriority;
  since?: string;
  boot?: JournalBoot;
  limit?: number;
  after?: string;
}

export interface JournalPage {
  entries: LogLine[];
  nextCursor: string | null;
}

export interface FsEntry {
  name: string;
  kind: 'dir' | 'file';
  size: number;
  modified: string;
  content?: string;
  children?: FsEntry[];
}

export interface FileRead {
  content: string | null;
  contentBase64: string | null;
  revision: string | null;
  truncated: boolean;
  sizeBytes: number;
}

export interface FileWrite {
  revision: string;
  sizeBytes: number;
}

export interface PrivilegedFileWrite extends FileWrite {
  rollbackRef: string;
  validation: { kind: string; checked: boolean };
  restart: { success: boolean; error?: string } | null;
}

export interface UpdatePackage {
  name: string;
  fromVersion: string;
  toVersion: string;
  security: boolean;
  downloadBytes: number;
  installedDeltaBytes: number;
}

export interface UpdatePlan {
  id: string;
  createdAt: string;
  expiresAt: string;
  packages: UpdatePackage[];
  securityCount: number;
  downloadBytes: number;
  installedDeltaBytes: number;
  rebootRequired: boolean;
}

export interface UpdateProgress {
  requestId: string;
  planId: string;
  phase: string;
  percent: number;
  message: string;
  done: boolean;
  success: boolean;
  error?: string;
  updatedAt: string;
}

export interface TerminalOpenOptions {
  cols: number;
  rows: number;
  user?: string;
}

export interface TerminalHandlers {
  onData(data: string): void;
  onExit(code: number): void;
  onError?(err: Error): void;
  onReset?(): void;
}

export interface TerminalSession {
  write(data: string): void;
  resize(cols: number, rows: number): void;
  close(): void;
}

export interface SourceCapabilities {
  isLive: boolean;
  canServiceActions: boolean;
  canTerminal: boolean;
  canWriteFiles: boolean;
  canManageUpdates: boolean;
  canPowerControl: boolean;
  canConfigureNetwork: boolean;
}

export interface SessionUser {
  name: string;
  uid?: number;
  gid?: number;
  home?: string;
}

export type Unsubscribe = () => void;

export interface DataSource {
  readonly kind: 'mock' | 'live';
  readonly capabilities: SourceCapabilities;

  login(username: string, password: string): Promise<SessionUser>;
  logout(): Promise<void>;
  getSession(): Promise<SessionUser | null>;
  reauth(password: string): Promise<void>;
  onSessionExpired(listener: () => void): Unsubscribe;

  getIdentity(): Promise<SystemIdentity>;
  getOverview(): Promise<SystemOverview>;
  uptimeSeconds(): number;
  sampleLoad(): LoadSample;
  subscribeMetrics(onSample: (sample: LoadSample) => void, intervalMs?: number): Unsubscribe;
  runPowerAction(action: PowerAction): Promise<PowerSchedule>;
  getNetworkSnapshot(): Promise<NetworkSnapshot>;
  applyNetworkConfig(config: NetworkConfig, expectedRevision: string, confirmTimeoutSec?: number): Promise<PendingNetworkChange>;
  confirmNetworkConfig(token: string): Promise<NetworkConfirmation>;

  listServices(): Promise<ServiceUnit[]>;
  getServiceDetail(name: string): Promise<ServiceDetail>;
  subscribeServices(onChange: (units: ServiceUnit[]) => void): Unsubscribe;
  runServiceAction(name: string, action: ServiceAction, expectedActiveState?: string): Promise<ServiceUnit>;

  queryJournal(query?: JournalQuery): Promise<JournalPage>;
  streamJournal(onEntry: (entry: LogLine) => void, onError?: (err: Error) => void): Unsubscribe;
  listJournalUnits(): Promise<string[]>;

  homePath(): string[];
  listDir(path: string[]): Promise<FsEntry[]>;
  readFile(path: string[]): Promise<FileRead>;
  readSystemFile(path: string): Promise<FileRead>;
  writeFile(path: string[], contentBase64: string, expectedRevision: string | null): Promise<FileWrite>;
  writePrivilegedFile(path: string, contentBase64: string, expectedRevision: string, restartUnit?: string): Promise<PrivilegedFileWrite>;
  deleteFile(path: string[]): Promise<void>;

  refreshUpdates(): Promise<string>;
  calculateUpdatePlan(): Promise<UpdatePlan>;
  applyUpdatePlan(planId: string): Promise<string>;
  subscribeUpdateProgress(requestId: string, onProgress: (progress: UpdateProgress) => void, onError?: (err: Error) => void): Unsubscribe;

  openTerminal(opts: TerminalOpenOptions, handlers: TerminalHandlers): TerminalSession;
}

export function isReauthRequired(err: unknown): boolean {
  return err instanceof ApiError && err.code === 'forbidden' && err.details.reauthRequired === true;
}

export function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'unauthorized':
        return 'Your session has expired. Please log in again.';
      case 'forbidden':
        return 'This action is not permitted.';
      case 'not_found':
        return 'This item no longer exists.';
      case 'conflict':
      case 'stale_revision':
        return 'The system changed while you worked. Refresh and try again.';
      case 'busy':
        return 'The server is busy. Try again in a moment.';
      case 'unavailable':
        return 'The server is unreachable right now.';
      default:
        return err.message || 'Something went wrong.';
    }
  }
  return err instanceof Error ? err.message : 'Something went wrong.';
}

function liveModeEnabled(): boolean {
  const flag = import.meta.env.VITE_LUMIO_LIVE as string | undefined;
  if (flag === '1' || flag === 'true') return true;
  if (flag === '0' || flag === 'false') return false;
  return import.meta.env.PROD;
}

const dataSource: DataSource = liveModeEnabled() ? new LiveDataSource() : new MockDataSource();

export function getDataSource(): DataSource {
  return dataSource;
}
