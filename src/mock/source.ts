// SPDX-License-Identifier: AGPL-3.0-only
import type {
  DataSource,
  FileRead,
  FsEntry,
  JournalPage,
  JournalQuery,
  LoadSample,
  LogLine,
  ServiceAction,
  ServiceUnit,
  SourceCapabilities,
  SystemIdentity,
  SystemOverview,
  Unsubscribe,
} from '../api/source';
import { getEntry, homePath as mockHomePath, listDir as mockListDir } from './filesystem';
import { LOG_UNITS, makeLogLine, seedLogLines } from './journal';
import {
  getOverview,
  listServices,
  runServiceAction,
  sampleLoad,
  subscribeServices,
  uptimeSeconds,
} from './system';

const TICK_MS = 2000;

export class MockDataSource implements DataSource {
  readonly kind = 'mock' as const;
  readonly capabilities: SourceCapabilities = {
    isLive: false,
    canServiceActions: true,
    canTerminal: true,
    canWriteFiles: false,
  };

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
    return {
      content: entry?.kind === 'file' ? (entry.content ?? null) : null,
      revision: null,
      truncated: false,
      sizeBytes: entry?.size ?? 0,
    };
  }
}
