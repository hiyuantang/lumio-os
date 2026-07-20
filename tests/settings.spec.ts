// SPDX-License-Identifier: AGPL-3.0-only
import { expect, test } from '@playwright/test';

test('settings confirms and schedules a restart', async ({ page }) => {
  await page.goto('/');
  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('demo');
  await page.getByTestId('login-submit').click();

  await page.getByTestId('dock-app-settings').click();
  const settings = page.getByTestId('app-settings');
  await expect(settings).toBeVisible();
  await settings.getByTestId('settings-reboot').click();
  await expect(page.getByTestId('settings-power-confirm')).toContainText('Active sessions will be disconnected');
  await page.getByTestId('settings-confirm-action').click();

  await expect(page.getByTestId('notifications-badge')).toHaveText('1');
  await page.getByTestId('notifications-button').click();
  await expect(page.getByTestId('notification-item').first()).toContainText('Restart scheduled');
});
