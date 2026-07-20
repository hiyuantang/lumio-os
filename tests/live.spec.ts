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

const SERVICE_DETAIL = {
  name: 'nginx.service',
  documentation: ['man:nginx(8)'],
  dependencies: [
    { name: 'network.target', relation: 'requires' },
    { name: 'system.slice', relation: 'wants' },
  ],
  files: [
    {
      path: '/usr/lib/systemd/system/nginx.service',
      content: '[Service]\nExecStart=/usr/sbin/nginx -g daemon off;',
      override: false,
    },
  ],
};

const JOURNAL = {
  entries: [
    {
      cursor: 'cursor-1',
      ts: new Date(Date.now() - 10 * 60_000).toISOString(),
      priority: 'warning',
      unit: 'nginx.service',
      message: 'upstream timed out while reading response header',
      fields: { _PID: '812', _HOSTNAME: 'atlas-live', _BOOT_ID: 'boot-1', SYSLOG_IDENTIFIER: 'nginx' },
    },
    {
      cursor: 'cursor-2',
      ts: new Date(Date.now() - 5 * 60_000).toISOString(),
      priority: 'info',
      unit: 'cron.service',
      message: '(root) CMD (run-parts /etc/cron.hourly)',
      fields: { _PID: '2401', _HOSTNAME: 'atlas-live', _BOOT_ID: 'boot-1', SYSLOG_IDENTIFIER: 'cron' },
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
  await page.route('**/api/v1/auth/login', (route) =>
    fulfillData(route, { user: { name: 'demo', uid: 1000, gid: 1000, home: '/home/user' }, csrf: 'test-csrf' }),
  );
  await page.route('**/api/v1/auth/session', (route) => fulfillFailure(route, 'unauthorized', 401));
  await page.route('**/api/v1/auth/logout', (route) => fulfillData(route, {}));
  await page.route('**/api/v1/auth/reauth', (route) => fulfillData(route, { reauthenticatedUntil: Date.now() + 600_000 }));
  await page.route('**/api/v1/system/identity', (route) => fulfillData(route, IDENTITY));
  await page.route('**/api/v1/system/overview', (route) => fulfillData(route, OVERVIEW));
  await page.route('**/api/v1/system/metrics', (route) => fulfillData(route, METRICS));
  await page.route('**/api/v1/services/detail**', (route) => fulfillData(route, SERVICE_DETAIL));
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
  await expect(services.getByTestId('service-action-restart')).toBeEnabled();
  await expect(services.getByTestId('service-action-reload')).toBeEnabled();
  await expect(services.getByTestId('services-actions-note')).toHaveCount(0);
  await expect(services.getByTestId('service-dependencies')).toContainText('network.target');
  await expect(services.getByTestId('service-unit-files')).toContainText('/usr/lib/systemd/system/nginx.service');

  await services.getByTestId('service-open-logs').click();
  const relatedLogs = page.getByTestId('app-logs');
  await expect(relatedLogs.getByLabel('Filter by unit')).toHaveValue('nginx.service');
  await relatedLogs.getByTestId('logs-row').first().click();
  await expect(relatedLogs.getByTestId('logs-detail')).toContainText('SYSLOG_IDENTIFIER');
  await expect(relatedLogs.getByTestId('logs-detail')).toContainText('nginx');
  await relatedLogs.getByLabel('Search logs').fill('upstream');
  await relatedLogs.getByTestId('logs-save-search').click();
  await expect(relatedLogs.getByLabel('Saved searches')).not.toHaveValue('');
  const downloadPromise = page.waitForEvent('download');
  await relatedLogs.getByTestId('logs-export').click();
  await expect((await downloadPromise).suggestedFilename()).toMatch(/^lumio-journal-.*\.jsonl$/);
  const previousBootRequest = page.waitForRequest((request) => request.url().includes('/api/v1/journal?') && request.url().includes('boot=previous'));
  await relatedLogs.getByLabel('Filter by boot').selectOption('previous');
  await previousBootRequest;
  await relatedLogs.getByTestId('logs-row').first().click();
  await relatedLogs.getByTestId('logs-open-service').click();
  await expect(services.getByTestId('service-row-nginx.service')).toHaveClass(/selected/);

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

test('live mode: bad credentials show a calm error and stay on the login screen', async ({ page }) => {
  await stubRest(page);
  await page.route('**/api/v1/auth/login', (route) => fulfillFailure(route, 'unauthorized', 401));
  await page.goto(LIVE);
  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('wrong');
  await page.getByTestId('login-submit').click();
  await expect(page.getByTestId('login-error')).toHaveText('Incorrect username or password.');
  await expect(page.getByTestId('login-screen')).toBeVisible();
});

test('live mode: an existing session skips the login screen', async ({ page }) => {
  await stubRest(page);
  await page.route('**/api/v1/auth/session', (route) =>
    fulfillData(route, { user: { name: 'demo', uid: 1000, gid: 1000, home: '/home/user' } }),
  );
  await page.goto(LIVE);
  await expect(page.getByTestId('menu-bar')).toBeVisible();
  await expect(page.getByTestId('login-screen')).toHaveCount(0);
});

test('live mode: logging out calls the server and returns to the login screen', async ({ page }) => {
  await stubRest(page);
  let logoutCalls = 0;
  await page.route('**/api/v1/auth/logout', async (route) => {
    logoutCalls += 1;
    await fulfillData(route, {});
  });
  await login(page);
  await page.getByTestId('menu-bar').locator('[data-menu-button="user"]').click();
  await page.getByTestId('logout-button').click();
  await expect(page.getByTestId('login-screen')).toBeVisible();
  expect(logoutCalls).toBe(1);
});

test('live mode: an expired session returns to the login screen', async ({ page }) => {
  await stubRest(page);
  let expired = false;
  await page.route('**/api/v1/system/overview', async (route) => {
    if (expired) await fulfillFailure(route, 'unauthorized', 401);
    else await fulfillData(route, OVERVIEW);
  });
  await login(page);
  await page.getByTestId('dock-app-home').click();
  await expect(page.getByTestId('app-home')).toContainText('atlas-live');
  expired = true;
  await expect(page.getByTestId('login-screen')).toBeVisible({ timeout: 10_000 });
});

test('live mode: service restart sends expected state and the CSRF header', async ({ page }) => {
  await stubRest(page);
  const calls: { body: { action?: string; unit?: string; requestId?: string; expected?: { activeState?: string } }; csrf: string | undefined }[] = [];
  await page.route('**/api/v1/services/action', async (route) => {
    calls.push({
      body: route.request().postDataJSON() as (typeof calls)[number]['body'],
      csrf: route.request().headers()['x-lumio-csrf'],
    });
    await fulfillData(route, {
      unit: { name: 'nginx.service', activeState: 'active', subState: 'running', enabledState: 'enabled' },
    });
  });
  await login(page);

  await page.getByTestId('dock-app-services').click();
  const services = page.getByTestId('app-services');
  await services.getByTestId('service-row-nginx.service').click();
  await services.getByTestId('service-action-restart').click();

  await expect(page.getByTestId('notifications-badge')).toHaveText('1', { timeout: 5_000 });
  expect(calls).toHaveLength(1);
  expect(calls[0].body.action).toBe('restart');
  expect(calls[0].body.unit).toBe('nginx.service');
  expect(calls[0].body.expected?.activeState).toBe('active');
  expect(calls[0].body.requestId).toBeTruthy();
  expect(calls[0].csrf).toBe('test-csrf');
});

test('live mode: conflict refreshes the list and notifies calmly', async ({ page }) => {
  await stubRest(page);
  let listCalls = 0;
  await page.route('**/api/v1/services', async (route) => {
    listCalls += 1;
    await fulfillData(route, SERVICES);
  });
  await page.route('**/api/v1/services/action', (route) => fulfillFailure(route, 'conflict', 409));
  await login(page);

  await page.getByTestId('dock-app-services').click();
  const services = page.getByTestId('app-services');
  await services.getByTestId('service-row-nginx.service').click();
  const before = listCalls;
  await services.getByTestId('service-action-restart').click();

  await expect(page.getByTestId('notifications-badge')).toHaveText('1', { timeout: 5_000 });
  await expect.poll(() => listCalls, { timeout: 5_000 }).toBe(before + 1);
});

test('live mode: stop asks for confirmation before acting', async ({ page }) => {
  await stubRest(page);
  let actionCalls = 0;
  await page.route('**/api/v1/services/action', async (route) => {
    actionCalls += 1;
    await fulfillData(route, {
      unit: { name: 'nginx.service', activeState: 'inactive', subState: 'dead', enabledState: 'enabled' },
    });
  });
  await login(page);

  await page.getByTestId('dock-app-services').click();
  const services = page.getByTestId('app-services');
  await services.getByTestId('service-row-nginx.service').click();
  await services.getByTestId('service-action-stop').click();

  await expect(page.getByTestId('service-confirm')).toBeVisible();
  expect(actionCalls).toBe(0);
  await page.getByTestId('service-confirm-ok').click();
  await expect(page.getByTestId('notifications-badge')).toHaveText('1', { timeout: 5_000 });
  expect(actionCalls).toBe(1);
});

test('live mode: reauth sheet appears on reauthRequired and retries after success', async ({ page }) => {
  await stubRest(page);
  let actionCalls = 0;
  let reauthCalls = 0;
  await page.route('**/api/v1/services/action', async (route) => {
    actionCalls += 1;
    if (actionCalls === 1) {
      await route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: false,
          error: { code: 'forbidden', message: 'reauthentication required', details: { reauthRequired: true } },
        }),
      });
      return;
    }
    await fulfillData(route, {
      unit: { name: 'nginx.service', activeState: 'active', subState: 'running', enabledState: 'enabled' },
    });
  });
  await page.route('**/api/v1/auth/reauth', async (route) => {
    reauthCalls += 1;
    const body = route.request().postDataJSON() as { password?: string };
    if (body.password === 'wrong') {
      await fulfillFailure(route, 'unauthorized', 401);
      return;
    }
    await fulfillData(route, { reauthenticatedUntil: Date.now() + 600_000 });
  });
  await login(page);

  await page.getByTestId('dock-app-services').click();
  const services = page.getByTestId('app-services');
  await services.getByTestId('service-row-nginx.service').click();
  await services.getByTestId('service-action-restart').click();

  const sheet = page.getByTestId('reauth-sheet');
  await expect(sheet).toBeVisible();
  await sheet.getByTestId('reauth-password').fill('wrong');
  await sheet.getByTestId('reauth-submit').click();
  await expect(page.getByTestId('reauth-error')).toHaveText('Incorrect password.');

  await sheet.getByTestId('reauth-password').fill('demo');
  await sheet.getByTestId('reauth-submit').click();
  await expect(sheet).toHaveCount(0);
  await expect(page.getByTestId('notifications-badge')).toHaveText('1', { timeout: 5_000 });
  expect(actionCalls).toBe(2);
  expect(reauthCalls).toBe(2);
});
