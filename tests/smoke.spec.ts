// SPDX-License-Identifier: AGPL-3.0-only
import { expect, test } from '@playwright/test';

test('phase 1 shell: login, services, notifications, layout restore', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByTestId('login-screen')).toBeVisible();

  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('demo');
  await page.getByTestId('login-submit').click();

  await expect(page.getByTestId('menu-bar')).toBeVisible();
  await expect(page.getByTestId('dock')).toBeVisible();

  await page.getByTestId('dock-app-services').click();
  const servicesWindow = page.getByTestId('window-services');
  await expect(servicesWindow).toBeVisible();
  await expect(servicesWindow).toHaveAttribute('role', 'dialog');

  await servicesWindow.getByTestId('service-row-nginx.service').click();
  await servicesWindow.getByTestId('service-action-restart').click();

  await expect(page.getByTestId('notifications-badge')).toHaveText('1', { timeout: 5000 });
  await page.getByTestId('notifications-button').click();
  await expect(page.getByTestId('notification-center')).toBeVisible();
  await expect(page.getByTestId('notification-item').first()).toContainText('nginx.service');

  await page.reload();
  await expect(page.getByTestId('menu-bar')).toBeVisible();
  await expect(page.getByTestId('window-services')).toBeVisible();
});

test('command center opens apps by keyboard', async ({ page }) => {
  await page.goto('/');
  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('demo');
  await page.getByTestId('login-submit').click();
  await expect(page.getByTestId('dock')).toBeVisible();

  await page.keyboard.press('ControlOrMeta+k');
  await expect(page.getByTestId('command-center')).toBeVisible();
  await page.keyboard.type('terminal');
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('window-terminal')).toBeVisible();

  const terminalInput = page.getByTestId('terminal-input');
  await terminalInput.fill('whoami');
  await terminalInput.press('Enter');
  await expect(page.getByTestId('app-terminal')).toContainText('demo');
});
