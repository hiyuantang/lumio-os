// SPDX-License-Identifier: AGPL-3.0-only
import { expect, test } from '@playwright/test';

test('mock updates: refresh, review and apply a saved plan', async ({ page }) => {
  await page.goto('/');
  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('demo');
  await page.getByTestId('login-submit').click();
  await page.getByTestId('dock-app-updates').click();

  const updates = page.getByTestId('app-updates');
  await updates.getByTestId('updates-refresh').click();
  await expect(updates).toContainText('Refreshed');
  await updates.getByTestId('updates-plan').click();
  await expect(updates.getByTestId('updates-plan-summary')).toContainText('2');
  await expect(updates.getByTestId('update-package-openssl')).toContainText('Security');
  await expect(updates).toContainText('cannot be rolled back automatically');
  await updates.getByTestId('updates-apply').click();
  await expect(page.getByTestId('updates-confirm')).toBeVisible();
  await expect(page.getByTestId('updates-confirm')).toContainText('not transactionally rollbackable');
  await page.getByTestId('updates-confirm-apply').click();
  await expect(updates.getByTestId('updates-progress')).toContainText('Updates installed', { timeout: 5_000 });
  await expect(updates.getByTestId('updates-progress')).toContainText('100%');
});
