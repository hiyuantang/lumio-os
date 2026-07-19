// SPDX-License-Identifier: AGPL-3.0-only
import { expect, test, type Page, type Route } from '@playwright/test';

const LIVE = 'http://localhost:5200';

const IDENTITY = {
  hostname: 'atlas-live',
  os: { id: 'ubuntu', versionId: '26.04', prettyName: 'Ubuntu 26.04 LTS', kernel: '7.0.0-12-generic' },
  architecture: 'x86_64',
  bootId: 'boot-1',
  serverTime: '2026-07-19T00:12:44Z',
};

const OVERVIEW = {
  uptimeSeconds: 938214,
  cpuUsagePercent: 12.4,
  memoryUsedBytes: 1610612736,
  memoryTotalBytes: 4294967296,
  updatesPending: 3,
  securityUpdatesPending: 1,
  failedUnits: 0,
  rebootRequired: false,
};

const METRICS = {
  ts: 1721400164000,
  cpu: { usagePercent: 12.4, load1: 0.42, load5: 0.38, load15: 0.31, cores: 2 },
  memory: { totalBytes: 4294967296, usedBytes: 1610612736, availableBytes: 2684354560 },
  disks: [{ mount: '/', totalBytes: 53687091200, usedBytes: 12884901888 }],
  network: [{ interface: 'eth0', rxBytesPerSec: 15234, txBytesPerSec: 8211 }],
};

const SERVICES = {
  units: [
    {
      name: 'nginx.service',
      description: 'A high performance web server',
      loadState: 'loaded',
      activeState: 'active',
      subState: 'running',
      enabledState: 'enabled',
    },
    {
      name: 'backup.service',
      description: 'Nightly backup',
      loadState: 'loaded',
      activeState: 'inactive',
      subState: 'dead',
      enabledState: 'disabled',
    },
  ],
};

const JOURNAL = {
  entries: [
    {
      cursor: 'cursor-1',
      ts: '2026-07-19T00:10:02.113Z',
      priority: 'warning',
      unit: 'nginx.service',
      message: 'upstream timed out while reading response header',
      fields: { _PID: '812' },
    },
    {
      cursor: 'cursor-2',
      ts: '2026-07-19T00:11:02.113Z',
      priority: 'info',
      unit: 'cron.service',
      message: '(root) CMD (run-parts /etc/cron.hourly)',
      fields: { _PID: '2401' },
    },
  ],
  nextCursor: 'cursor-3',
};

const FILES_LIST = {
  path: '/home/user',
  entries: [
    { name: 'notes.txt', type: 'file', sizeBytes: 27, mode: '0644', modifiedAt: '2026-05-02T18:44:10Z', symlinkTarget: null },
    { name: 'projects', type: 'directory', sizeBytes: 4096, mode: '0755', modifiedAt: '2026-05-02T18:44:10Z', symlinkTarget: null },
  ],
};

const FILE_READ = {
  path: '/home/user/notes.txt',
  sizeBytes: 27,
  revision: 'sha256:9f2cdemo',
  encoding: 'utf-8',
  content: 'aGVsbG8gZnJvbSB0aGUgbGl2ZSBzZXJ2ZXIK',
  truncated: false,
};

function fulfillData(route: Route, data: unknown) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ ok: true, data }),
  });
}

function fulfillFailure(route: Route, code: string, status: number) {
  return route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify({ ok: false, error: { code, message: `fixture ${code}`, details: {} } }),
  });
}

async function stubRest(page: Page) {
  await page.route('**/api/v1/system/identity', (route) => fulfillData(route, IDENTITY));
  await page.route('**/api/v1/system/overview', (route) => fulfillData(route, OVERVIEW));
  await page.route('**/api/v1/system/metrics', (route) => fulfillData(route, METRICS));
  await page.route('**/api/v1/services', (route) => fulfillData(route, SERVICES));
  await page.route('**/api/v1/journal**', (route) => fulfillData(route, JOURNAL));
  await page.route('**/api/v1/files/list**', (route) => fulfillData(route, FILES_LIST));
  await page.route('**/api/v1/files/read**', (route) => fulfillData(route, FILE_READ));
}

async function login(page: Page) {
  await page.goto(LIVE);
  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('demo');
  await page.getByTestId('login-submit').click();
  await expect(page.getByTestId('menu-bar')).toBeVisible();
}

test('live mode: home, services, logs and terminal placeholder render from REST fixtures', async ({ page }) => {
  await stubRest(page);
  await login(page);

  await expect(page.getByTestId('menu-bar')).toContainText('atlas-live');

  await page.getByTestId('dock-app-home').click();
  const home = page.getByTestId('app-home');
  await expect(home).toContainText('atlas-live');
  await expect(home).toContainText('Ubuntu 26.04 LTS');
  await expect(home).toContainText('7.0.0-12-generic');

  await page.getByTestId('dock-app-services').click();
  const services = page.getByTestId('app-services');
  await expect(services.getByTestId('service-row-nginx.service')).toBeVisible();
  await expect(services.getByTestId('service-row-backup.service')).toBeVisible();
  await services.getByTestId('service-row-nginx.service').click();
  await expect(services.getByTestId('service-action-restart')).toBeDisabled();
  await expect(services.getByTestId('services-actions-note')).toBeVisible();

  await page.getByTestId('dock-app-logs').click();
  const logs = page.getByTestId('app-logs');
  await expect(logs).toContainText('upstream timed out while reading response header');

  await page.getByTestId('dock-app-terminal').click();
  await expect(page.getByTestId('app-terminal').getByTestId('terminal-new-tab')).toBeVisible();
});

test('live mode: files quick look shows decoded content and revision', async ({ page }) => {
  await stubRest(page);
  await login(page);

  await page.getByTestId('dock-app-files').click();
  const files = page.getByTestId('app-files');
  await expect(files.getByTestId('file-row-notes.txt')).toBeVisible();
  await files.getByTestId('file-row-notes.txt').dblclick();

  const quickLook = page.getByTestId('quick-look');
  await expect(quickLook).toBeVisible();
  await expect(quickLook).toContainText('hello from the live server');
  await expect(page.getByTestId('quick-look-revision')).toHaveText('sha256:9f2cdemo');
});

test('live mode: failed initial load shows a retry affordance and recovers', async ({ page }) => {
  await stubRest(page);
  let identityDown = true;
  await page.route('**/api/v1/system/identity', (route) =>
    identityDown ? fulfillFailure(route, 'unavailable', 503) : fulfillData(route, IDENTITY),
  );
  await login(page);

  await page.getByTestId('dock-app-home').click();
  const home = page.getByTestId('app-home');
  await expect(home).toContainText('Cannot reach the server', { timeout: 15_000 });

  identityDown = false;
  await home.getByTestId('home-retry').click();
  await expect(home).toContainText('atlas-live', { timeout: 15_000 });
});

test('live mode: services state flips over the channel without a refresh', async ({ page }) => {
  await stubRest(page);
  await page.routeWebSocket(/\/api\/v1\/ws/, (ws) => {
    ws.onMessage((message) => {
      let frame: { type?: string; channel?: number; capability?: string };
      try {
        frame = JSON.parse(String(message)) as typeof frame;
      } catch {
        return;
      }
      if (frame.type !== 'subscribe' || typeof frame.channel !== 'number') return;
      const channel = frame.channel;
      ws.send(JSON.stringify({ type: 'subscribed', channel }));
      if (frame.capability === 'services.subscribe') {
        ws.send(
          JSON.stringify({ type: 'event', channel, seq: 1, data: { kind: 'snapshot', units: SERVICES.units } }),
        );
        setTimeout(() => {
          ws.send(
            JSON.stringify({
              type: 'event',
              channel,
              seq: 2,
              data: {
                kind: 'changed',
                unit: { name: 'backup.service', activeState: 'active', subState: 'running' },
              },
            }),
          );
        }, 300);
      }
      if (frame.capability === 'system.metrics') {
        ws.send(JSON.stringify({ type: 'event', channel, seq: 1, data: METRICS }));
      }
    });
  });
  await login(page);

  await page.getByTestId('dock-app-services').click();
  const row = page.getByTestId('app-services').getByTestId('service-row-backup.service');
  await expect(row.locator('.pill')).toHaveText('inactive');
  await expect(row.locator('.pill')).toHaveText('active', { timeout: 10_000 });
});

test('live mode: terminal renders stdout and sends stdin frames', async ({ page }) => {
  await stubRest(page);
  const inputFrames: { kind?: string; data?: string }[] = [];
  await page.routeWebSocket(/\/api\/v1\/ws/, (ws) => {
    ws.onMessage((message) => {
      let frame: { type?: string; channel?: number; capability?: string; data?: { kind?: string; data?: string } };
      try {
        frame = JSON.parse(String(message)) as typeof frame;
      } catch {
        return;
      }
      if (frame.type === 'input') {
        inputFrames.push(frame.data ?? {});
        return;
      }
      if (frame.type !== 'subscribe' || typeof frame.channel !== 'number') return;
      const channel = frame.channel;
      if (frame.capability === 'terminal.open') {
        ws.send(JSON.stringify({ type: 'subscribed', channel, data: { session: 'sess-1' } }));
        ws.send(
          JSON.stringify({
            type: 'event',
            channel,
            seq: 1,
            data: { kind: 'stdout', data: 'd2VsY29tZSB0byB0aGUgbGl2ZSB0dHkNCiQg' },
          }),
        );
      } else {
        ws.send(JSON.stringify({ type: 'subscribed', channel }));
      }
    });
  });
  await login(page);

  await page.getByTestId('dock-app-terminal').click();
  const terminal = page.getByTestId('app-terminal');
  await expect(terminal).toContainText('welcome to the live tty', { timeout: 10_000 });

  await page.getByTestId('terminal-input').fill('x');
  await expect
    .poll(() => inputFrames.filter((frame) => frame.kind === 'stdin').length, { timeout: 5_000 })
    .toBe(1);
  expect(inputFrames.find((frame) => frame.kind === 'stdin')?.data).toBe('eA==');
});

test('live mode: terminal exit shows exited state and restart resubscribes', async ({ page }) => {
  await stubRest(page);
  let terminalSubscribes = 0;
  await page.routeWebSocket(/\/api\/v1\/ws/, (ws) => {
    ws.onMessage((message) => {
      let frame: { type?: string; channel?: number; capability?: string };
      try {
        frame = JSON.parse(String(message)) as typeof frame;
      } catch {
        return;
      }
      if (frame.type !== 'subscribe' || typeof frame.channel !== 'number') return;
      const channel = frame.channel;
      if (frame.capability === 'terminal.open') {
        terminalSubscribes += 1;
        ws.send(JSON.stringify({ type: 'subscribed', channel, data: { session: 'sess-1' } }));
        ws.send(
          JSON.stringify({
            type: 'event',
            channel,
            seq: 1,
            data: { kind: 'stdout', data: 'd2VsY29tZSB0byB0aGUgbGl2ZSB0dHkNCiQg' },
          }),
        );
        if (terminalSubscribes <= 2) {
          ws.send(JSON.stringify({ type: 'event', channel, seq: 2, data: { kind: 'exit', code: 0 } }));
          ws.send(JSON.stringify({ type: 'closed', channel, error: null }));
        }
      } else {
        ws.send(JSON.stringify({ type: 'subscribed', channel }));
      }
    });
  });
  await login(page);

  await page.getByTestId('dock-app-terminal').click();
  await expect(page.getByTestId('terminal-exited')).toBeVisible({ timeout: 10_000 });

  const before = terminalSubscribes;
  await page.getByTestId('terminal-restart').click();
  await expect.poll(() => terminalSubscribes, { timeout: 5_000 }).toBeGreaterThan(before);
});

test('live mode: files edit saves with the read revision', async ({ page }) => {
  await stubRest(page);
  const writes: { path?: string; content?: string; expectedRevision?: string; requestId?: string }[] = [];
  await page.route('**/api/v1/files/write', async (route) => {
    writes.push(route.request().postDataJSON() as (typeof writes)[number]);
    await fulfillData(route, { path: '/home/user/notes.txt', revision: 'sha256:new1', sizeBytes: 21 });
  });
  await login(page);

  await page.getByTestId('dock-app-files').click();
  const files = page.getByTestId('app-files');
  await files.getByTestId('file-row-notes.txt').dblclick();
  await page.getByTestId('quicklook-edit').click();

  const input = page.getByTestId('editor-input');
  await expect(input).toHaveValue('hello from the live server\n');
  await input.fill('updated live content\n');
  await input.press('ControlOrMeta+s');

  await expect(page.getByTestId('file-editor')).toHaveCount(0);
  expect(writes).toHaveLength(1);
  expect(writes[0].path).toBe('/home/user/notes.txt');
  expect(writes[0].expectedRevision).toBe('sha256:9f2cdemo');
  expect(writes[0].content).toBe('dXBkYXRlZCBsaXZlIGNvbnRlbnQK');
  expect(writes[0].requestId).toBeTruthy();
});

test('live mode: stale revision shows conflict banner and reload resolves it', async ({ page }) => {
  await stubRest(page);
  let stale = true;
  let writeAttempts = 0;
  await page.route('**/api/v1/files/write', async (route) => {
    writeAttempts += 1;
    if (stale) {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: false,
          error: {
            code: 'stale_revision',
            message: 'The file changed on disk since it was read.',
            details: { expectedRevision: 'sha256:9f2cdemo', actualRevision: 'sha256:beef' },
          },
        }),
      });
      return;
    }
    await fulfillData(route, { path: '/home/user/notes.txt', revision: 'sha256:new2', sizeBytes: 21 });
  });
  await login(page);

  await page.getByTestId('dock-app-files').click();
  const files = page.getByTestId('app-files');
  await files.getByTestId('file-row-notes.txt').dblclick();
  await page.getByTestId('quicklook-edit').click();
  await page.getByTestId('editor-input').fill('conflicting edit\n');
  await page.getByTestId('editor-save').click();

  await expect(page.getByTestId('editor-conflict')).toBeVisible();
  await expect(page.getByTestId('editor-conflict')).toContainText('changed on disk');

  stale = false;
  await page.getByTestId('editor-reload').click();
  await expect(page.getByTestId('editor-input')).toHaveValue('hello from the live server\n');
  await expect(page.getByTestId('editor-conflict')).toHaveCount(0);

  await page.getByTestId('editor-save').click();
  await expect(page.getByTestId('file-editor')).toHaveCount(0);
  expect(writeAttempts).toBe(2);
});

test('live mode: delete asks for confirmation and removes the row', async ({ page }) => {
  await stubRest(page);
  const deletes: { path?: string; requestId?: string }[] = [];
  let deleted = false;
  await page.route('**/api/v1/files/list**', async (route) => {
    const entries = deleted ? FILES_LIST.entries.filter((entry) => entry.name !== 'notes.txt') : FILES_LIST.entries;
    await fulfillData(route, { ...FILES_LIST, entries });
  });
  await page.route('**/api/v1/files/delete', async (route) => {
    deletes.push(route.request().postDataJSON() as (typeof deletes)[number]);
    deleted = true;
    await fulfillData(route, { trashed: true });
  });
  await login(page);

  await page.getByTestId('dock-app-files').click();
  const files = page.getByTestId('app-files');
  await files.getByTestId('file-row-notes.txt').dblclick();
  await page.getByTestId('quicklook-delete').click();

  const confirm = page.getByTestId('delete-confirm');
  await expect(confirm).toBeVisible();
  await confirm.getByTestId('delete-confirm-button').click();

  await expect(confirm).toHaveCount(0);
  await expect(files.getByTestId('file-row-notes.txt')).toHaveCount(0);
  expect(deletes).toHaveLength(1);
  expect(deletes[0].path).toBe('/home/user/notes.txt');
  expect(deletes[0].requestId).toBeTruthy();
});
