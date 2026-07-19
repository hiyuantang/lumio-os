// SPDX-License-Identifier: AGPL-3.0-only

export interface FsEntry {
  name: string;
  kind: 'dir' | 'file';
  size: number;
  modified: string;
  content?: string;
  children?: FsEntry[];
}

const HOME: FsEntry = {
  name: 'user',
  kind: 'dir',
  size: 4096,
  modified: 'Jul 12 09:14',
  children: [
    {
      name: 'Documents',
      kind: 'dir',
      size: 4096,
      modified: 'Jul 16 18:02',
      children: [
        {
          name: 'server-notes.md',
          kind: 'file',
          size: 1284,
          modified: 'Jul 16 18:02',
          content:
            '# Atlas server notes\n\n- Host: atlas.lan (192.168.1.20)\n- Backups run nightly at 02:30 via lumio-backup.service\n- PostgreSQL data lives on /srv/postgres\n- Renew TLS certificates before Sep 4\n',
        },
        {
          name: 'upgrade-plan.txt',
          kind: 'file',
          size: 642,
          modified: 'Jul 10 11:47',
          content:
            'Upgrade plan\n============\n1. Snapshot the VM\n2. apt update && apt upgrade\n3. Reboot into the new kernel\n4. Verify nginx, postgresql and fail2ban are active\n',
        },
        { name: 'invoice-june.pdf', kind: 'file', size: 182_044, modified: 'Jul 02 08:15' },
      ],
    },
    {
      name: 'Pictures',
      kind: 'dir',
      size: 4096,
      modified: 'Jun 28 21:33',
      children: [
        { name: 'rack-photo.jpg', kind: 'file', size: 2_418_330, modified: 'Jun 28 21:33' },
        { name: 'network-map.png', kind: 'file', size: 812_410, modified: 'Jun 20 14:05' },
      ],
    },
    {
      name: 'projects',
      kind: 'dir',
      size: 4096,
      modified: 'Jul 17 22:41',
      children: [
        {
          name: 'lumio-agent',
          kind: 'dir',
          size: 4096,
          modified: 'Jul 17 22:41',
          children: [
            {
              name: 'README.md',
              kind: 'file',
              size: 930,
              modified: 'Jul 17 22:41',
              content:
                '# lumio-agent\n\nSmall node agent that reports host metrics to the Lumio OS broker.\n\nRun with: systemctl start lumio-agent.service\n',
            },
            {
              name: 'agent.ts',
              kind: 'file',
              size: 5210,
              modified: 'Jul 17 22:40',
              content:
                'export async function collectMetrics() {\n  const load = await readLoadavg();\n  const mem = await readMeminfo();\n  return { load, mem, at: new Date().toISOString() };\n}\n',
            },
            { name: 'agent.test.ts', kind: 'file', size: 3120, modified: 'Jul 15 09:12' },
          ],
        },
        {
          name: 'deploy.sh',
          kind: 'file',
          size: 812,
          modified: 'Jul 14 16:20',
          content:
            '#!/bin/sh\nset -eu\nrsync -az --delete dist/ atlas.lan:/srv/www/lumio/\nssh atlas.lan systemctl reload nginx.service\n',
        },
      ],
    },
    {
      name: 'backups',
      kind: 'dir',
      size: 4096,
      modified: 'Jul 18 02:30',
      children: [
        { name: 'postgres-2026-07-18.dump', kind: 'file', size: 96_304_112, modified: 'Jul 18 02:30' },
        { name: 'postgres-2026-07-17.dump', kind: 'file', size: 95_881_204, modified: 'Jul 17 02:30' },
      ],
    },
    {
      name: '.bashrc',
      kind: 'file',
      size: 3771,
      modified: 'May 30 10:02',
      content: "export EDITOR=vim\nalias ll='ls -alF'\nalias gs='git status'\n",
    },
    {
      name: 'notes.txt',
      kind: 'file',
      size: 218,
      modified: 'Jul 11 19:26',
      content: 'Remember to rotate the off-site backup key and tidy /var/log before the end of the month.\n',
    },
  ],
};

export function homePath(): string[] {
  return ['user'];
}

export function getEntry(path: string[]): FsEntry | undefined {
  if (path.length === 0 || path[0] !== HOME.name) return undefined;
  let node: FsEntry = HOME;
  for (const segment of path.slice(1)) {
    const next = node.children?.find((c) => c.name === segment);
    if (!next) return undefined;
    node = next;
  }
  return node;
}

export function listDir(path: string[]): FsEntry[] {
  const node = getEntry(path);
  if (!node || node.kind !== 'dir') return [];
  return [...(node.children ?? [])].sort((a, b) => {
    if (a.kind !== b.kind) return a.kind === 'dir' ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 100 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}
