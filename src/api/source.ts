// SPDX-License-Identifier: AGPL-3.0-only
import { LiveDataSource } from './client';
import { ApiError } from './transport';
import { MockDataSource } from '../mock/source';

export type ServiceState = 'active' | 'inactive' | 'failed';
export type ServiceAction = 'start' | 'stop' | 'restart' | 'enable' | 'disable';

export interface ServiceUnit {
  name: string;
  description: string;
  state: ServiceState;
  enabled: boolean;
  pid: number | null;
  memoryMb: number;
  since: string;
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
}

export interface JournalQuery {
  unit?: string;
  priority?: LogPriority;
  limit?: number;
  before?: string;
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
}

export type Unsubscribe = () => void;

export interface DataSource {
  readonly kind: 'mock' | 'live';
  readonly capabilities: SourceCapabilities;

  getIdentity(): Promise<SystemIdentity>;
  getOverview(): Promise<SystemOverview>;
  uptimeSeconds(): number;
  sampleLoad(): LoadSample;
  subscribeMetrics(onSample: (sample: LoadSample) => void, intervalMs?: number): Unsubscribe;

  listServices(): Promise<ServiceUnit[]>;
  subscribeServices(onChange: (units: ServiceUnit[]) => void): Unsubscribe;
  runServiceAction(name: string, action: ServiceAction): Promise<ServiceUnit>;

  queryJournal(query?: JournalQuery): Promise<JournalPage>;
  streamJournal(onEntry: (entry: LogLine) => void, onError?: (err: Error) => void): Unsubscribe;
  listJournalUnits(): Promise<string[]>;

  homePath(): string[];
  listDir(path: string[]): Promise<FsEntry[]>;
  readFile(path: string[]): Promise<FileRead>;
  writeFile(path: string[], contentBase64: string, expectedRevision: string | null): Promise<FileWrite>;
  deleteFile(path: string[]): Promise<void>;

  openTerminal(opts: TerminalOpenOptions, handlers: TerminalHandlers): TerminalSession;
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
