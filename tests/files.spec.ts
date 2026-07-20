// SPDX-License-Identifier: AGPL-3.0-only
import { expect, test } from '@playwright/test';

test('mock files: edit, save, re-read and delete round-trip', async ({ page }) => {
  await page.goto('/');
  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('demo');
  await page.getByTestId('login-submit').click();
  await expect(page.getByTestId('menu-bar')).toBeVisible();

  await page.getByTestId('dock-app-files').click();
  const files = page.getByTestId('app-files');

  await files.getByTestId('file-row-notes.txt').dblclick();
  await page.getByTestId('quicklook-edit').click();

  const input = page.getByTestId('editor-input');
  await expect(input).toHaveValue(/Remember to rotate/);
  await input.fill('Updated notes from the mock test\n');
  await page.getByTestId('editor-save').click();
  await expect(page.getByTestId('file-editor')).toHaveCount(0);

  await files.getByTestId('file-row-notes.txt').dblclick();
  await expect(page.getByTestId('quick-look')).toContainText('Updated notes from the mock test');

  await page.getByTestId('quicklook-delete').click();
  await expect(page.getByTestId('delete-confirm')).toBeVisible();
  await page.getByTestId('delete-confirm-button').click();
  await expect(page.getByTestId('delete-confirm')).toHaveCount(0);
  await expect(files.getByTestId('file-row-notes.txt')).toHaveCount(0);
});

test('mock files: protected editor shows diff, validates and keeps rollback', async ({ page }) => {
  await page.goto('/');
  await page.getByTestId('login-username').fill('demo');
  await page.getByTestId('login-password').fill('demo');
  await page.getByTestId('login-submit').click();
  await page.getByTestId('dock-app-files').click();

  const files = page.getByTestId('app-files');
  await files.getByTestId('system-file-button').click();
  await page.getByTestId('system-file-open').click();
  const input = page.getByTestId('system-file-input');
  await expect(input).toContainText('listen 80');
  await input.fill('server {\n    listen 8080;\n}\n');
  await expect(page.getByTestId('system-file-diff')).toContainText('+     listen 8080;');
  await page.getByTestId('system-file-save').click();
  await expect(page.getByTestId('system-file-result')).toContainText('nginx validation passed');
  await expect(page.getByTestId('system-file-result')).toContainText('Rollback copy kept');
});
