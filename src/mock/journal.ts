// SPDX-License-Identifier: AGPL-3.0-only
import type { LogLine, LogPriority } from '../api/source';

export type { LogLine, LogPriority } from '../api/source';

const PRIORITY_CODE: Record<LogPriority, 3 | 4 | 6 | 7> = {
  err: 3,
  warning: 4,
  info: 6,
  debug: 7,
};

const TEMPLATES: { unit: string; priority: LogPriority; message: string }[] = [
  { unit: 'ssh.service', priority: 'info', message: 'Accepted publickey for user from 192.168.1.4 port 51122' },
  { unit: 'ssh.service', priority: 'warning', message: 'reverse mapping checking getaddrinfo failed — possible break-in attempt' },
  { unit: 'cron.service', priority: 'info', message: '(user) CMD (cd /home/user && ./scripts/housekeeping.sh)' },
  { unit: 'nginx.service', priority: 'info', message: '192.168.1.12 "GET /api/health HTTP/1.1" 200 12 2ms' },
  { unit: 'nginx.service', priority: 'err', message: 'upstream timed out (110: Connection timed out) while reading response header' },
  { unit: 'postgresql.service', priority: 'info', message: 'checkpoint complete: wrote 412 buffers (2.5%)' },
  { unit: 'postgresql.service', priority: 'warning', message: 'automatic vacuum of table "app.events": index scans: 1' },
  { unit: 'redis-server.service', priority: 'info', message: 'Background saving terminated with success' },
  { unit: 'fail2ban.service', priority: 'warning', message: 'Ban 203.0.113.44 (sshd jail) after 5 attempts' },
  { unit: 'systemd-resolved.service', priority: 'debug', message: 'Cache miss for atlas.lan IN A' },
  { unit: 'lumio-backup.service', priority: 'err', message: 'snapshot upload failed: endpoint unreachable (retry 3 of 3)' },
  { unit: 'kernel', priority: 'info', message: 'EXT4-fs (sda1): mounted filesystem with ordered data mode' },
  { unit: 'kernel', priority: 'debug', message: 'TCP: request_sock_TCP: Possible SYN flooding on port 443' },
];

let nextId = 1;

export function makeLogLine(at: number = Date.now()): LogLine {
  const t = TEMPLATES[Math.floor(Math.random() * TEMPLATES.length)];
  return {
    id: nextId++,
    timestamp: at,
    priority: t.priority,
    priorityCode: PRIORITY_CODE[t.priority],
    unit: t.unit,
    message: t.message,
    pid: 400 + Math.floor(Math.random() * 4000),
    hostname: 'atlas.lan',
  };
}

export function seedLogLines(count: number): LogLine[] {
  const now = Date.now();
  return Array.from({ length: count }, (_, i) => makeLogLine(now - (count - i) * 2000));
}

export const LOG_UNITS = [...new Set(TEMPLATES.map((t) => t.unit))].sort();
