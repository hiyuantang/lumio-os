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
  await expect(page.getByTestId('app-terminal')).toContainText('later phase');
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
