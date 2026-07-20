// SPDX-License-Identifier: AGPL-3.0-only
import { base64ToText, textToBase64 } from './encoding';
import type {
  WireAuthLogin,
  WireAuthSession,
  WireFileEntry,
  WireFileRead,
  WireFilesList,
  WireFileWrite,
  WireIdentity,
  WireJournalEntry,
  WireJournalPage,
  WireMetricsSample,
  WireOverview,
  WirePrivilegedFileWrite,
  WireReauth,
  WireServiceActionResult,
  WireServiceDetail,
  WireServicesEvent,
  WireServicesList,
  WireServiceUnit,
  WireSessionUser,
  WireUpdatePlan,
  WireUpdateProgress,
} from './protocol';
import type {
  DataSource,
  FileRead,
  FileWrite,
  FsEntry,
  JournalPage,
  JournalQuery,
  LoadSample,
  LogLine,
  LogPriority,
  NetworkConfig,
  NetworkConfirmation,
  NetworkSnapshot,
  PendingNetworkChange,
  PowerAction,
  PowerSchedule,
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
} from './source';
import { ApiError, apiGet, apiPost, apiPut, csrfToken, onSessionExpired as onSessionExpiredListener } from './transport';
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
    bootId: entry.fields?._BOOT_ID ?? '',
    fields: { ...(entry.fields ?? {}) },
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

function joinUnderHome(homeDir: string, path: string[]): string {
  const rest = path.slice(1).join('/');
  return rest ? `${homeDir}/${rest}` : homeDir;
}

export class LiveDataSource implements DataSource {
  readonly kind = 'live' as const;
  readonly capabilities: SourceCapabilities = {
    isLive: true,
    canServiceActions: true,
    canTerminal: true,
    canWriteFiles: true,
    canManageUpdates: true,
    canPowerControl: true,
    canConfigureNetwork: true,
  };

  private socket = new LumioSocket();
  private identity: SystemIdentity | null = null;
  private homeDir = '/home/user';
  private bootedAt = Date.now();
  private lastMetrics: WireMetricsSample | null = null;
  private cpuHistory: number[] = [];
  private units = new Map<string, WireServiceUnit>();

  async login(username: string, password: string): Promise<SessionUser> {
    const data = await apiPost<WireAuthLogin>('/auth/login', { username, password });
    if (data.csrf && !csrfToken()) {
      document.cookie = `lumio_csrf=${encodeURIComponent(data.csrf)}; path=/; SameSite=Strict`;
    }
    return this.noteSessionUser(data.user);
  }

  async logout(): Promise<void> {
    await apiPost('/auth/logout', {});
  }

  async getSession(): Promise<SessionUser | null> {
    try {
      const data = await apiGet<WireAuthSession>('/auth/session');
      return this.noteSessionUser(data.user);
    } catch (err) {
      if (err instanceof ApiError && err.code === 'unauthorized') return null;
      throw err;
    }
  }

  async reauth(password: string): Promise<void> {
    await apiPost<WireReauth>('/auth/reauth', { password });
  }

  onSessionExpired(listener: () => void): Unsubscribe {
    return onSessionExpiredListener(listener);
  }

  private noteSessionUser(user: WireSessionUser): SessionUser {
    if (user.home && user.home.startsWith('/')) {
      this.homeDir = user.home.replace(/\/+$/, '') || '/';
    }
    return { name: user.name, uid: user.uid, gid: user.gid, home: user.home };
  }

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
      const home = data.user?.home;
      if (home && home.startsWith('/')) {
        this.homeDir = home.replace(/\/+$/, '') || '/';
      }
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

  async runPowerAction(action: PowerAction): Promise<PowerSchedule> {
    return apiPost<PowerSchedule>('/system/power', { requestId: crypto.randomUUID(), action });
  }

  async getNetworkSnapshot(): Promise<NetworkSnapshot> {
    return apiGet<NetworkSnapshot>('/network');
  }

  async applyNetworkConfig(
    config: NetworkConfig,
    expectedRevision: string,
    confirmTimeoutSec = 90,
  ): Promise<PendingNetworkChange> {
    return apiPost<PendingNetworkChange>('/network/apply', {
      requestId: crypto.randomUUID(),
      config,
      expectedRevision,
      confirmTimeoutSec,
    });
  }

  async confirmNetworkConfig(token: string): Promise<NetworkConfirmation> {
    return apiPost<NetworkConfirmation>('/network/confirm', {
      requestId: crypto.randomUUID(),
      token,
    });
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
    const handle = this.socket.subscribe({
      capability: 'system.metrics',
      params: () => ({ intervalMs }),
      onEvent: (data) => {
        const sample = data as WireMetricsSample;
        this.noteMetrics(sample);
        onSample(mapMetricsSample(sample));
      },
    });
    return handle.close;
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

  async getServiceDetail(name: string): Promise<ServiceDetail> {
    const detail = await apiGet<WireServiceDetail>('/services/detail', { name });
    return {
      name: detail.name,
      documentation: detail.documentation ?? [],
      dependencies: detail.dependencies ?? [],
      files: (detail.files ?? []).map((file) => ({
        path: file.path,
        content: file.content ?? null,
        override: file.override,
        error: file.error ?? null,
      })),
    };
  }

  subscribeServices(onChange: (units: ServiceUnit[]) => void): Unsubscribe {
    const handle = this.socket.subscribe({
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
    return handle.close;
  }

  async runServiceAction(name: string, action: ServiceAction, expectedActiveState?: string): Promise<ServiceUnit> {
    const data = await apiPost<WireServiceActionResult>('/services/action', {
      requestId: crypto.randomUUID(),
      action,
      unit: name,
      ...(expectedActiveState ? { expected: { activeState: expectedActiveState } } : {}),
    });
    const fallback: WireServiceUnit = this.units.get(name) ?? {
      name,
      description: '',
      loadState: 'loaded',
      activeState: 'inactive',
      subState: 'dead',
      enabledState: 'disabled',
    };
    const merged = { ...fallback, ...data.unit };
    this.units.set(name, merged);
    return mapServiceUnit(merged);
  }

  async queryJournal(query: JournalQuery = {}): Promise<JournalPage> {
    const data = await apiGet<WireJournalPage>('/journal', {
      unit: query.unit,
      priority: query.priority,
      since: query.since,
      boot: query.boot,
      limit: query.limit,
      'after-cursor': query.after,
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
    const handle = this.socket.subscribe({
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
    return handle.close;
  }

  async listJournalUnits(): Promise<string[]> {
    const data = await apiGet<WireJournalPage>('/journal', { limit: 200 });
    return [...new Set(data.entries.map((entry) => entry.unit))].sort();
  }

  homePath(): string[] {
    const segment = this.homeDir.split('/').filter(Boolean).pop();
    return [segment ?? 'user'];
  }

  async listDir(path: string[]): Promise<FsEntry[]> {
    const data = await apiGet<WireFilesList>('/files/list', { path: joinUnderHome(this.homeDir, path) });
    return data.entries.map(mapFileEntry).sort(sortEntries);
  }

  async readFile(path: string[]): Promise<FileRead> {
    const data = await apiGet<WireFileRead>('/files/read', { path: joinUnderHome(this.homeDir, path) });
    const content = data.encoding === 'utf-8' || data.encoding === 'ascii' ? base64ToText(data.content) : null;
    return {
      content,
      contentBase64: data.content,
      revision: data.revision,
      truncated: data.truncated,
      sizeBytes: data.sizeBytes,
    };
  }

  async readSystemFile(path: string): Promise<FileRead> {
    const data = await apiGet<WireFileRead>('/files/read', { path });
    const content = data.encoding === 'utf-8' || data.encoding === 'ascii' ? base64ToText(data.content) : null;
    return {
      content,
      contentBase64: data.content,
      revision: data.revision,
      truncated: data.truncated,
      sizeBytes: data.sizeBytes,
    };
  }

  async writeFile(path: string[], contentBase64: string, expectedRevision: string | null): Promise<FileWrite> {
    const data = await apiPut<WireFileWrite>('/files/write', {
      path: joinUnderHome(this.homeDir, path),
      content: contentBase64,
      ...(expectedRevision ? { expectedRevision } : {}),
      requestId: crypto.randomUUID(),
    });
    return { revision: data.revision, sizeBytes: data.sizeBytes };
  }

  async writePrivilegedFile(
    path: string,
    contentBase64: string,
    expectedRevision: string,
    restartUnit?: string,
  ): Promise<PrivilegedFileWrite> {
    const data = await apiPost<WirePrivilegedFileWrite>('/files/write-privileged', {
      path,
      content: contentBase64,
      expectedRevision,
      ...(restartUnit ? { restartUnit } : {}),
      requestId: crypto.randomUUID(),
    });
    return {
      revision: data.file.revision,
      sizeBytes: data.file.sizeBytes,
      rollbackRef: data.file.rollbackRef,
      validation: data.file.validation,
      restart: data.restart ?? null,
    };
  }

  async deleteFile(path: string[]): Promise<void> {
    await apiPost<{ trashed: boolean }>('/files/delete', {
      path: joinUnderHome(this.homeDir, path),
      requestId: crypto.randomUUID(),
    });
  }

  async refreshUpdates(): Promise<string> {
    const data = await apiPost<{ refreshedAt: string }>('/updates/refresh', { requestId: crypto.randomUUID() });
    return data.refreshedAt;
  }

  async calculateUpdatePlan(): Promise<UpdatePlan> {
    const data = await apiPost<{ plan: WireUpdatePlan }>('/updates/plan', { requestId: crypto.randomUUID() });
    return data.plan;
  }

  async applyUpdatePlan(planId: string): Promise<string> {
    const requestId = crypto.randomUUID();
    const data = await apiPost<{ requestId: string }>('/updates/apply', { requestId, planId });
    return data.requestId;
  }

  subscribeUpdateProgress(
    requestId: string,
    onProgress: (progress: UpdateProgress) => void,
    onError?: (err: Error) => void,
  ): Unsubscribe {
    const handle = this.socket.subscribe({
      capability: 'updates.progress',
      params: () => ({ requestId }),
      onEvent: (data) => onProgress(data as WireUpdateProgress),
      onError: (err) => onError?.(err),
    });
    return handle.close;
  }

  openTerminal(opts: TerminalOpenOptions, handlers: TerminalHandlers): TerminalSession {
    return new LiveTerminalSession(this.socket, opts, handlers);
  }
}

class LiveTerminalSession implements TerminalSession {
  private cols: number;
  private rows: number;
  private sessionToken: string | null = null;
  private exited = false;
  private handle: { channel: number; close: () => void };

  constructor(
    private socket: LumioSocket,
    opts: TerminalOpenOptions,
    private handlers: TerminalHandlers,
  ) {
    this.cols = opts.cols;
    this.rows = opts.rows;
    this.handle = this.socket.subscribe({
      capability: 'terminal.open',
      params: () => ({
        cols: this.cols,
        rows: this.rows,
        shell: null,
        ...(this.sessionToken ? { session: this.sessionToken } : {}),
      }),
      onSubscribed: (data, reattached) => {
        const token = (data as { session?: unknown } | undefined)?.session;
        if (typeof token === 'string') this.sessionToken = token;
        if (reattached) this.handlers.onReset?.();
      },
      onEvent: (data) => {
        const frame = data as { kind?: string; data?: string; code?: number };
        if (frame.kind === 'stdout' && typeof frame.data === 'string') {
          this.handlers.onData(base64ToText(frame.data));
        } else if (frame.kind === 'exit') {
          this.exited = true;
          this.handlers.onExit(typeof frame.code === 'number' ? frame.code : 0);
        }
      },
      onError: (err) => this.handlers.onError?.(err),
    });
  }

  write(data: string): void {
    if (this.exited) return;
    this.socket.sendInput(this.handle.channel, { kind: 'stdin', data: textToBase64(data) });
  }

  resize(cols: number, rows: number): void {
    if (this.exited || (cols === this.cols && rows === this.rows)) return;
    this.cols = cols;
    this.rows = rows;
    this.socket.sendInput(this.handle.channel, { kind: 'resize', cols, rows });
  }

  close(): void {
    this.handle.close();
  }
}
