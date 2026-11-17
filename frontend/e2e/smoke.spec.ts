import { expect, test } from '@playwright/test';

const password = process.env['NETRAVE_PASSWORD'] ?? '';
const username = process.env['NETRAVE_USER'] ?? 'admin';

test.skip(!password, 'NETRAVE_PASSWORD не задан');

test('вход и основные разделы', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Log in to NetRave' })).toBeVisible();
  await page.locator('#auth-user').fill(username);
  await page.locator('#auth-pass').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Laboratories' })).toBeVisible();

  for (const [route, heading] of [
    ['dashboard', 'Dashboard'],
    ['users', 'Users'],
    ['settings', 'System settings'],
    ['logs', 'Journal'],
    ['account', 'Account'],
    ['templates', 'Device templates'],
    ['images', 'Images'],
    ['icons', 'Icons'],
    ['themes', 'Theme settings'],
    ['running', 'Running laboratories'],
  ]) {
    await page.goto(`/#/${route}`);
    await expect(page.getByRole('heading', { name: heading, exact: false })).toBeVisible();
  }
});

test('создание лаборатории и редактор', async ({ page }) => {
  await page.goto('/');
  await page.locator('#auth-user').fill(username);
  await page.locator('#auth-pass').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.getByRole('button', { name: 'New laboratory' }).click();
  await page.locator('#lab-name').fill(`E2E ${Date.now()}`);
  await page.getByRole('button', { name: 'Create' }).click();
  await expect(page.getByRole('button', { name: 'Tools' })).toBeVisible();
  await page.getByRole('button', { name: 'Machine' }).click();
  await expect(page.getByRole('dialog', { name: 'Add machine' })).toBeVisible();
});

test('language switch persists without reload', async ({ page }) => {
  await page.goto('/');
  await page.locator('#auth-user').fill(username);
  await page.locator('#auth-pass').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Laboratories' })).toBeVisible();

  await page.getByRole('button', { name: 'Language' }).click();
  await page.getByRole('menuitemradio', { name: 'Русский' }).click();
  await expect(page.getByRole('heading', { name: 'Лаборатории' })).toBeVisible();

  await page.reload();
  await expect(page.getByRole('heading', { name: 'Лаборатории' })).toBeVisible();

  await page.getByRole('button', { name: 'Язык' }).click();
  await page.getByRole('menuitemradio', { name: 'English' }).click();
  await expect(page.getByRole('heading', { name: 'Laboratories' })).toBeVisible();
});
