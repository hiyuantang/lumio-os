// SPDX-License-Identifier: AGPL-3.0-only
import { base64ToText, textToBase64 } from '../api/encoding';
import type {
  DataSource,
  FileRead,
  FileWrite,
  FsEntry,
  JournalPage,
  JournalQuery,
  LoadSample,
  LogLine,
  PrivilegedFileWrite,
  ServiceAction,
  ServiceDetail,
  ServiceUnit,
  SessionUser,
  SourceCapabilities,
  SystemIdentity,
  SystemOverview,
  TerminalHandlers,
  TerminalOpenOptions,
  TerminalSession,
  Unsubscribe,
  UpdatePlan,
  UpdateProgress,
} from '../api/source';
import { ApiError } from '../api/transport';
import {
  deleteEntry,
  entryRevision,
  getEntry,
  homePath as mockHomePath,
  listDir as mockListDir,
  writeEntry,
} from './filesystem';
import { LOG_UNITS, makeLogLine, seedLogLines } from './journal';
import {
  getOverview,
  listServices,
  getServiceDetail,
  runServiceAction,
  sampleLoad,
  subscribeServices,
  uptimeSeconds,
} from './system';
import { MockTerminalSession } from './terminal';
import { readSystemFile, writePrivilegedFile } from './privileged-files';
import {
  applyUpdatePlan,
  calculateUpdatePlan,
  refreshUpdates,
  subscribeUpdateProgress,
} from './updates';

const TICK_MS = 2000;

export class MockDataSource implements DataSource {
  readonly kind = 'mock' as const;
  readonly capabilities: SourceCapabilities = {
    isLive: false,
    canServiceActions: true,
    canTerminal: true,
    canWriteFiles: true,
    canManageUpdates: true,
  };

  async login(username: string): Promise<SessionUser> {
    return { name: username, home: '/home/user' };
  }

  async logout(): Promise<void> {}

  async getSession(): Promise<SessionUser | null> {
    return null;
  }

  async reauth(): Promise<void> {}

  onSessionExpired(): Unsubscribe {
    return () => {};
  }

  async getIdentity(): Promise<SystemIdentity> {
    const overview = getOverview();
    return {
      hostname: overview.hostname,
      os: overview.os,
      kernel: overview.kernel,
      architecture: 'x86_64',
      bootId: 'mock',
      serverTime: new Date().toISOString(),
    };
  }

  async getOverview(): Promise<SystemOverview> {
    return getOverview();
  }

  uptimeSeconds(): number {
    return uptimeSeconds();
  }

  sampleLoad(): LoadSample {
    return sampleLoad();
  }

  subscribeMetrics(onSample: (sample: LoadSample) => void, intervalMs = TICK_MS): Unsubscribe {
    const id = window.setInterval(() => onSample(sampleLoad()), intervalMs);
    return () => window.clearInterval(id);
  }

  async listServices(): Promise<ServiceUnit[]> {
    return listServices();
  }

  async getServiceDetail(name: string): Promise<ServiceDetail> {
    return getServiceDetail(name);
  }

  subscribeServices(onChange: (units: ServiceUnit[]) => void): Unsubscribe {
    return subscribeServices(() => onChange(listServices()));
  }

  runServiceAction(name: string, action: ServiceAction): Promise<ServiceUnit> {
    return runServiceAction(name, action);
  }

  async queryJournal(query: JournalQuery = {}): Promise<JournalPage> {
    let entries = seedLogLines(query.limit ?? 40);
    if (query.unit) entries = entries.filter((line) => line.unit === query.unit);
    if (query.priority) entries = entries.filter((line) => line.priority === query.priority);
    if (query.since) {
      const since = Date.parse(query.since);
      if (Number.isFinite(since)) entries = entries.filter((line) => line.timestamp >= since);
    }
    if (query.boot === 'previous') {
      entries = entries.map((line) => ({ ...line, bootId: 'mock-previous-boot', fields: { ...line.fields, _BOOT_ID: 'mock-previous-boot' } }));
    }
    return { entries, nextCursor: null };
  }

  streamJournal(onEntry: (entry: LogLine) => void): Unsubscribe {
    const id = window.setInterval(() => onEntry(makeLogLine()), TICK_MS);
    return () => window.clearInterval(id);
  }

  async listJournalUnits(): Promise<string[]> {
    return [...LOG_UNITS];
  }

  homePath(): string[] {
    return mockHomePath();
  }

  async listDir(path: string[]): Promise<FsEntry[]> {
    return mockListDir(path);
  }

  async readFile(path: string[]): Promise<FileRead> {
    const entry = getEntry(path);
    if (!entry || entry.kind !== 'file') {
      throw new ApiError('not_found', 'No such file.');
    }
    return {
      content: entry.content ?? null,
      contentBase64: entry.content != null ? textToBase64(entry.content) : null,
      revision: entryRevision(path),
      truncated: false,
      sizeBytes: entry.size,
    };
  }

  readSystemFile(path: string): Promise<FileRead> {
    return readSystemFile(path);
  }

  async writeFile(path: string[], contentBase64: string, expectedRevision: string | null): Promise<FileWrite> {
    return writeEntry(path, base64ToText(contentBase64), expectedRevision);
  }

  writePrivilegedFile(
    path: string,
    contentBase64: string,
    expectedRevision: string,
    restartUnit?: string,
  ): Promise<PrivilegedFileWrite> {
    return writePrivilegedFile(path, contentBase64, expectedRevision, restartUnit);
  }

  async deleteFile(path: string[]): Promise<void> {
    deleteEntry(path);
  }

  refreshUpdates(): Promise<string> {
    return refreshUpdates();
  }

  calculateUpdatePlan(): Promise<UpdatePlan> {
    return calculateUpdatePlan();
  }

  applyUpdatePlan(planId: string): Promise<string> {
    return applyUpdatePlan(planId);
  }

  subscribeUpdateProgress(requestId: string, onProgress: (progress: UpdateProgress) => void): Unsubscribe {
    return subscribeUpdateProgress(requestId, onProgress);
  }

  openTerminal(opts: TerminalOpenOptions, handlers: TerminalHandlers): TerminalSession {
    return new MockTerminalSession(opts, handlers);
  }
}
