// SPDX-License-Identifier: AGPL-3.0-only
import type {
  WireFileEntry,
  WireFileRead,
  WireFilesList,
  WireIdentity,
  WireJournalEntry,
  WireJournalPage,
  WireMetricsSample,
  WireOverview,
  WireServicesEvent,
  WireServicesList,
  WireServiceUnit,
} from './protocol';
import type {
  DataSource,
  FileRead,
  FsEntry,
  JournalPage,
  JournalQuery,
  LoadSample,
  LogLine,
  LogPriority,
  ServiceAction,
  ServiceUnit,
  SourceCapabilities,
  SystemIdentity,
  SystemOverview,
  Unsubscribe,
} from './source';
import { apiGet, apiPost } from './transport';
import { LumioSocket } from './ws';

const MB = 1024 * 1024;
const GB = 1024 * 1024 * 1024;

const PRIORITY_MAP: Record<string, LogPriority> = {
  emerg: 'err',
  alert: 'err',
  crit: 'err',
  err: 'err',
  error: 'err',
  warning: 'warning',
  warn: 'warning',
  notice: 'info',
  info: 'info',
  debug: 'debug',
};

const PRIORITY_CODE: Record<LogPriority, 3 | 4 | 6 | 7> = {
  err: 3,
  warning: 4,
  info: 6,
  debug: 7,
};

let nextLogId = 1;

function mapServiceUnit(unit: WireServiceUnit): ServiceUnit {
  return {
    name: unit.name,
    description: unit.description ?? '',
    state: unit.activeState === 'active' ? 'active' : unit.activeState === 'failed' ? 'failed' : 'inactive',
    enabled: unit.enabledState === 'enabled',
    pid: null,
    memoryMb: 0,
    since: '—',
  };
}

function mapMetricsSample(sample: WireMetricsSample): LoadSample {
  const rx = sample.network.reduce((sum, iface) => sum + iface.rxBytesPerSec, 0);
  const tx = sample.network.reduce((sum, iface) => sum + iface.txBytesPerSec, 0);
  return {
    cpuPercent: Math.round(sample.cpu.usagePercent),
    netDownKbps: Math.round(rx / 1024),
    netUpKbps: Math.round(tx / 1024),
  };
}

function mapJournalEntry(entry: WireJournalEntry): LogLine {
  const priority = PRIORITY_MAP[entry.priority] ?? 'info';
  const pid = Number(entry.fields?._PID);
  return {
    id: nextLogId++,
    timestamp: Date.parse(entry.ts) || Date.now(),
    priority,
    priorityCode: PRIORITY_CODE[priority],
    unit: entry.unit,
    message: entry.message,
    pid: Number.isFinite(pid) ? pid : 0,
    hostname: entry.fields?._HOSTNAME ?? '',
  };
}

function mapFileEntry(entry: WireFileEntry): FsEntry {
  return {
    name: entry.name,
    kind: entry.type === 'directory' ? 'dir' : 'file',
    size: entry.sizeBytes,
    modified: formatModified(entry.modifiedAt),
  };
}

function formatModified(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${months[date.getMonth()]} ${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function sortEntries(a: FsEntry, b: FsEntry): number {
  if (a.kind !== b.kind) return a.kind === 'dir' ? -1 : 1;
  return a.name.localeCompare(b.name);
}

function decodeBase64(b64: string): string {
  const binary = atob(b64);
  const bytes = Uint8Array.from(binary, (ch) => ch.charCodeAt(0));
  return new TextDecoder('utf-8').decode(bytes);
}

function toAbsolutePath(path: string[]): string {
  return `/${['home', ...path].join('/')}`;
}

export class LiveDataSource implements DataSource {
  readonly kind = 'live' as const;
  readonly capabilities: SourceCapabilities = {
    isLive: true,
    canServiceActions: false,
    canTerminal: false,
    canWriteFiles: false,
  };

  private socket = new LumioSocket();
  private identity: SystemIdentity | null = null;
  private bootedAt = Date.now();
  private lastMetrics: WireMetricsSample | null = null;
  private cpuHistory: number[] = [];
  private units = new Map<string, WireServiceUnit>();

  async getIdentity(): Promise<SystemIdentity> {
    if (!this.identity) {
      const data = await apiGet<WireIdentity>('/system/identity');
      this.identity = {
        hostname: data.hostname,
        os: data.os.prettyName,
        kernel: data.os.kernel,
        architecture: data.architecture,
        bootId: data.bootId,
        serverTime: data.serverTime,
      };
    }
    return this.identity;
  }

  async getOverview(): Promise<SystemOverview> {
    const [identity, overview, metrics] = await Promise.all([
      this.getIdentity(),
      apiGet<WireOverview>('/system/overview'),
      apiGet<WireMetricsSample>('/system/metrics'),
    ]);
    this.noteMetrics(metrics);
    this.bootedAt = Date.now() - overview.uptimeSeconds * 1000;
    const root = metrics.disks.find((disk) => disk.mount === '/') ?? metrics.disks[0];
    const alerts: SystemOverview['alerts'] = [];
    if (overview.failedUnits > 0) {
      alerts.push({
        id: 'failed-units',
        level: 'critical',
        text: `${overview.failedUnits} unit${overview.failedUnits === 1 ? '' : 's'} failed`,
      });
    }
    if (overview.securityUpdatesPending > 0) {
      alerts.push({
        id: 'security-updates',
        level: 'warning',
        text: `${overview.securityUpdatesPending} security update${overview.securityUpdatesPending === 1 ? ' is' : 's are'} ready to install`,
      });
    }
    if (overview.rebootRequired) {
      alerts.push({ id: 'reboot-required', level: 'info', text: 'A reboot is required to finish installing updates' });
    }
    return {
      hostname: identity.hostname,
      os: identity.os,
      kernel: identity.kernel,
      bootedAt: this.bootedAt,
      cpuPercent: Math.round(metrics.cpu.usagePercent),
      memoryUsedMb: Math.round(overview.memoryUsedBytes / MB),
      memoryTotalMb: Math.round(overview.memoryTotalBytes / MB),
      storageUsedGb: root ? Math.round(root.usedBytes / GB) : 0,
      storageTotalGb: root ? Math.round(root.totalBytes / GB) : 0,
      pendingUpdates: overview.updatesPending,
      securityUpdates: overview.securityUpdatesPending,
      alerts,
      cpuHistory: [...this.cpuHistory],
    };
  }

  uptimeSeconds(): number {
    return Math.max(0, Math.floor((Date.now() - this.bootedAt) / 1000));
  }

  sampleLoad(): LoadSample {
    return this.lastMetrics
      ? mapMetricsSample(this.lastMetrics)
      : { cpuPercent: 0, netDownKbps: 0, netUpKbps: 0 };
  }

  subscribeMetrics(onSample: (sample: LoadSample) => void, intervalMs = 2000): Unsubscribe {
    return this.socket.subscribe({
      capability: 'system.metrics',
      params: () => ({ intervalMs }),
      onEvent: (data) => {
        const sample = data as WireMetricsSample;
        this.noteMetrics(sample);
        onSample(mapMetricsSample(sample));
      },
    });
  }

  private noteMetrics(sample: WireMetricsSample) {
    this.lastMetrics = sample;
    this.cpuHistory.push(Math.round(sample.cpu.usagePercent));
    if (this.cpuHistory.length > 24) this.cpuHistory.shift();
  }

  async listServices(): Promise<ServiceUnit[]> {
    const data = await apiGet<WireServicesList>('/services');
    this.units.clear();
    for (const unit of data.units) this.units.set(unit.name, unit);
    return data.units.map(mapServiceUnit);
  }

  subscribeServices(onChange: (units: ServiceUnit[]) => void): Unsubscribe {
    return this.socket.subscribe({
      capability: 'services.subscribe',
      params: () => ({}),
      onEvent: (data) => {
        const event = data as WireServicesEvent;
        if (event.kind === 'snapshot') {
          this.units.clear();
          for (const unit of event.units) this.units.set(unit.name, unit);
        } else {
          const fallback: WireServiceUnit = this.units.get(event.unit.name) ?? {
            name: event.unit.name,
            description: '',
            loadState: 'loaded',
            activeState: 'inactive',
            subState: 'dead',
            enabledState: 'disabled',
          };
          this.units.set(event.unit.name, { ...fallback, ...event.unit });
        }
        onChange([...this.units.values()].map(mapServiceUnit).sort((a, b) => a.name.localeCompare(b.name)));
      },
    });
  }

  async runServiceAction(name: string, action: ServiceAction): Promise<ServiceUnit> {
    const data = await apiPost<WireServiceUnit>('/services/action', {
      requestId: crypto.randomUUID(),
      action: `services.${action}`,
      arguments: { unit: name },
    });
    return mapServiceUnit(data);
  }

  async queryJournal(query: JournalQuery = {}): Promise<JournalPage> {
    const data = await apiGet<WireJournalPage>('/journal', {
      unit: query.unit,
      priority: query.priority,
      limit: query.limit,
      before: query.before,
    });
    return { entries: data.entries.map(mapJournalEntry), nextCursor: data.nextCursor };
  }

  streamJournal(onEntry: (entry: LogLine) => void, onError?: (err: Error) => void): Unsubscribe {
    let lastCursor: string | null = null;
    const seen = new Set<string>();
    const remember = (cursor: string) => {
      seen.add(cursor);
      if (seen.size > 500) {
        const oldest = seen.values().next().value;
        if (oldest !== undefined) seen.delete(oldest);
      }
    };
    return this.socket.subscribe({
      capability: 'journal.stream',
      params: () => ({ after: lastCursor }),
      onEvent: (data) => {
        const entry = data as WireJournalEntry;
        if (entry.cursor) {
          if (seen.has(entry.cursor)) return;
          remember(entry.cursor);
          lastCursor = entry.cursor;
        }
        onEntry(mapJournalEntry(entry));
      },
      onError: (err) => onError?.(err),
    });
  }

  async listJournalUnits(): Promise<string[]> {
    const data = await apiGet<WireJournalPage>('/journal', { limit: 200 });
    return [...new Set(data.entries.map((entry) => entry.unit))].sort();
  }

  homePath(): string[] {
    return ['user'];
  }

  async listDir(path: string[]): Promise<FsEntry[]> {
    const data = await apiGet<WireFilesList>('/files/list', { path: toAbsolutePath(path) });
    return data.entries.map(mapFileEntry).sort(sortEntries);
  }

  async readFile(path: string[]): Promise<FileRead> {
    const data = await apiGet<WireFileRead>('/files/read', { path: toAbsolutePath(path) });
    const content = data.encoding === 'utf-8' || data.encoding === 'ascii' ? decodeBase64(data.content) : null;
    return { content, revision: data.revision, truncated: data.truncated, sizeBytes: data.sizeBytes };
  }
}
