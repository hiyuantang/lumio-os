// SPDX-License-Identifier: AGPL-3.0-only
import type { LoadSample, ServiceAction, ServiceUnit, SystemOverview } from '../api/source';

export type {
  LoadSample,
  ServiceAction,
  ServiceState,
  ServiceUnit,
  SystemAlert,
  SystemOverview,
} from '../api/source';

const SERVICES: ServiceUnit[] = [
  { name: 'ssh.service', description: 'OpenSSH server daemon', state: 'active', enabled: true, pid: 812, memoryMb: 6, since: 'boot' },
  { name: 'cron.service', description: 'Regular background program processing daemon', state: 'active', enabled: true, pid: 655, memoryMb: 2, since: 'boot' },
  { name: 'nginx.service', description: 'A high performance web server', state: 'active', enabled: true, pid: 1042, memoryMb: 38, since: 'boot' },
  { name: 'postgresql.service', description: 'PostgreSQL RDBMS', state: 'active', enabled: true, pid: 1310, memoryMb: 214, since: 'boot' },
  { name: 'redis-server.service', description: 'Advanced key-value store', state: 'active', enabled: true, pid: 1121, memoryMb: 12, since: 'boot' },
  { name: 'docker.service', description: 'Docker Application Container Engine', state: 'inactive', enabled: false, pid: null, memoryMb: 0, since: '—' },
  { name: 'ufw.service', description: 'Uncomplicated firewall', state: 'active', enabled: true, pid: 402, memoryMb: 1, since: 'boot' },
  { name: 'fail2ban.service', description: 'Ban hosts that cause multiple authentication errors', state: 'active', enabled: true, pid: 980, memoryMb: 22, since: 'boot' },
  { name: 'systemd-resolved.service', description: 'Network Name Resolution manager', state: 'active', enabled: true, pid: 510, memoryMb: 9, since: 'boot' },
  { name: 'lumio-backup.service', description: 'Nightly off-site backup job', state: 'failed', enabled: true, pid: null, memoryMb: 0, since: '2h ago' },
];

let services = SERVICES.map((s) => ({ ...s }));
const listeners = new Set<() => void>();

function emit() {
  listeners.forEach((fn) => fn());
}

export function listServices(): ServiceUnit[] {
  return services.map((s) => ({ ...s }));
}

export function getService(name: string): ServiceUnit | undefined {
  const found = services.find((s) => s.name === name);
  return found ? { ...found } : undefined;
}

export function subscribeServices(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

const ACTION_DELAY_MS = 600;

export function runServiceAction(name: string, action: ServiceAction): Promise<ServiceUnit> {
  return new Promise((resolve, reject) => {
    const unit = services.find((s) => s.name === name);
    if (!unit) {
      reject(new Error(`Unknown unit ${name}`));
      return;
    }
    setTimeout(() => {
      const nextPid = () => 2000 + Math.floor(Math.random() * 20000);
      switch (action) {
        case 'start':
          unit.state = 'active';
          unit.pid = nextPid();
          unit.memoryMb = unit.memoryMb || 8;
          unit.since = 'now';
          break;
        case 'stop':
          unit.state = 'inactive';
          unit.pid = null;
          unit.memoryMb = 0;
          unit.since = 'now';
          break;
        case 'restart':
          unit.state = 'active';
          unit.pid = nextPid();
          unit.memoryMb = unit.memoryMb || 8;
          unit.since = 'now';
          break;
        case 'enable':
          unit.enabled = true;
          break;
        case 'disable':
          unit.enabled = false;
          break;
      }
      emit();
      resolve({ ...unit });
    }, ACTION_DELAY_MS);
  });
}

let bootedAt = Date.now() - (3 * 24 * 60 * 60 + 7 * 60 * 60 + 42 * 60) * 1000;
let cpu = 11;
const history: number[] = Array.from({ length: 24 }, () => 6 + Math.random() * 16);

export function sampleLoad(): LoadSample {
  cpu = Math.min(96, Math.max(2, cpu + (Math.random() - 0.5) * 9));
  history.push(cpu);
  if (history.length > 24) history.shift();
  return {
    cpuPercent: Math.round(cpu),
    netDownKbps: Math.round(120 + Math.random() * 2400),
    netUpKbps: Math.round(40 + Math.random() * 500),
  };
}

export function getOverview(): SystemOverview {
  const load = sampleLoad();
  return {
    hostname: 'atlas.lan',
    os: 'Ubuntu 24.04.1 LTS',
    kernel: '6.8.0-45-generic',
    bootedAt,
    cpuPercent: load.cpuPercent,
    memoryUsedMb: 2680,
    memoryTotalMb: 8192,
    storageUsedGb: 84,
    storageTotalGb: 240,
    pendingUpdates: 14,
    securityUpdates: 3,
    alerts: [
      { id: 'a1', level: 'critical', text: 'lumio-backup.service failed during last run' },
      { id: 'a2', level: 'warning', text: '3 security updates are ready to install' },
      { id: 'a3', level: 'info', text: '/srv volume reached 35% usage' },
    ],
    cpuHistory: [...history],
  };
}

export function uptimeSeconds(): number {
  return Math.max(0, Math.floor((Date.now() - bootedAt) / 1000));
}
