// SPDX-License-Identifier: AGPL-3.0-only
import { expect, test } from '@playwright/test';

test('network change stays pending until it is explicitly kept', async ({ page }) => {
  await page.goto('/');
  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('demo');
  await page.getByTestId('login-submit').click();

  await page.getByTestId('dock-app-network').click();
  const network = page.getByTestId('app-network');
  await expect(network).toBeVisible();
  await expect(network.getByTestId('network-interface-eth0')).toContainText('Connected');
  await network.getByTestId('network-mode-dhcp').click();
  await network.getByTestId('network-apply').click();
  await expect(page.getByTestId('network-confirm-dialog')).toContainText('within 90 seconds');
  await page.getByTestId('network-confirm-apply').click();

  await expect(network.getByTestId('network-pending')).toContainText('Automatic rollback');
  await network.getByTestId('network-keep').click();
  await expect(network.getByTestId('network-pending')).toHaveCount(0);
  await expect(page.getByTestId('notifications-badge')).toHaveText('2');
});
