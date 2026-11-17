import { expect, test } from '@playwright/test';

const password = process.env['NETRAVE_PASSWORD'] ?? '';

test.skip(!password, 'NETRAVE_PASSWORD is not set');

async function csrf(page: import('@playwright/test').Page): Promise<string> {
  return page.evaluate(async () => {
    const response = await fetch('/api/me');
    const session = await response.json() as { csrf: string };
    return session.csrf;
  });
}

async function createLab(page: import('@playwright/test').Page, name: string): Promise<void> {
  const token = await csrf(page);
  const result = await page.evaluate(async ({ token, name }) => {
    const response = await fetch('/api/labs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
      body: JSON.stringify({ name, description: '' }),
    });
    return response.status;
  }, { token, name });
  expect(result).toBe(201);
}

test('notification centre is a single live dropdown with persistent read state', async ({ page, browser }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/');
  await page.locator('#auth-user').fill('admin');
  await page.locator('#auth-pass').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Laboratories' })).toBeVisible();

  const bell = page.getByRole('button', { name: 'Notifications' });
  const badge = page.locator('[data-notification-count]');
  const panel = page.locator('#notifications-panel');
  const rows = panel.locator('[data-notification-item]');

  // The login event is delivered and counted without a page reload.
  await expect(badge).toBeVisible();
  await expect(badge).toHaveText(/^[1-9]/);

  // Toggle, placement and z-index.
  await bell.click();
  await expect(panel).toHaveAttribute('data-open', 'true');
  const bellBox = await bell.boundingBox();
  const panelBox = await panel.boundingBox();
  if (!bellBox || !panelBox) throw new Error('missing boxes');
  expect(panelBox.y).toBeGreaterThanOrEqual(bellBox.y + bellBox.height);
  expect(Math.abs(bellBox.x + bellBox.width - (panelBox.x + panelBox.width))).toBeLessThan(24);
  expect(await panel.evaluate((el) => getComputedStyle(el).zIndex)).toBe('105');

  await bell.click();
  await expect(panel).not.toHaveAttribute('data-open', 'true');

  // Escape and outside click close the dropdown.
  await bell.click();
  await expect(panel).toHaveAttribute('data-open', 'true');
  await page.keyboard.press('Escape');
  await expect(panel).not.toHaveAttribute('data-open', 'true');
  await bell.click();
  await expect(panel).toHaveAttribute('data-open', 'true');
  await page.getByRole('heading', { name: 'Laboratories' }).click();
  await expect(panel).not.toHaveAttribute('data-open', 'true');

  // One unified list with title, description, timestamp and read state.
  await bell.click();
  await expect(rows.first()).toBeVisible();
  await expect(rows.first().locator('[data-notification-title]')).not.toBeEmpty();
  await expect(rows.first().locator('[data-notification-meta] time')).not.toBeEmpty();
  await expect(rows.first().locator('[data-notification-state]')).toHaveText(/Unread|Read/);
  await expect(panel.locator('[data-notification-categories]')).toHaveCount(0);

  // Opening does not mark anything read.
  await expect(badge).toBeVisible();

  // Mark all read through the backend.
  await panel.getByRole('button', { name: 'Read all' }).click();
  await expect(badge).toBeHidden();
  await expect(panel.locator('[data-notification-item][data-read="false"]')).toHaveCount(0);
  const readState = await page.evaluate(async () => {
    const response = await fetch('/api/notifications');
    return response.json() as Promise<{ unread: number; items: Array<{ read: boolean }> }>;
  });
  expect(readState.unread).toBe(0);
  expect(readState.items.every((item) => item.read)).toBe(true);

  // Read state survives a full page reload.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Laboratories' })).toBeVisible();
  await expect(page.locator('[data-notification-count]')).toBeHidden();
  await bell.click();
  await expect(rows.first()).toBeVisible();
  await expect(panel.locator('[data-notification-item][data-read="false"]')).toHaveCount(0);
  await bell.click();

  // A new backend notification arrives over WebSocket and is shown live at the top of the list.
  const countItems = (): Promise<number> => page.evaluate(async () => {
    const response = await fetch('/api/notifications');
    const body = await response.json() as { items: unknown[] };
    return body.items.length;
  });
  const before = await countItems();
  const firstLab = `Notify ${Date.now()}`;
  await createLab(page, firstLab);
  await expect(page.locator('[data-notification-count]')).toHaveText('1');
  expect(await countItems()).toBe(before + 1);
  await bell.click();
  await expect(panel).toHaveAttribute('data-open', 'true');
  await expect(rows.first()).toContainText(firstLab);
  await expect(rows.first()).toHaveClass(/unread/);
  // The panel was opened, but nothing was marked read automatically.
  await expect(page.locator('[data-notification-count]')).toHaveText('1');

  // A second event updates count and list live, without reloading.
  const secondLab = `Notify ${Date.now()} B`;
  await createLab(page, secondLab);
  await expect(page.locator('[data-notification-count]')).toHaveText('2');
  await expect(rows.first()).toContainText(secondLab);
  expect(await countItems()).toBe(before + 2);

  // Delete one notification and verify the backend no longer returns it.
  await rows.first().getByRole('button', { name: 'Delete notification' }).click();
  await expect.poll(countItems).toBe(before + 1);

  // Another user only sees their own notification history.
  const username = `notif${Date.now()}`;
  const token = await csrf(page);
  const created = await page.evaluate(async ({ token, username }) => {
    const response = await fetch('/api/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
      body: JSON.stringify({ username, password: 'Passw0rd!23', role: 'user' }),
    });
    return response.status;
  }, { token, username });
  expect(created).toBe(201);

  const adminNotifications = await page.evaluate(async () => {
    const response = await fetch('/api/notifications');
    const body = await response.json() as { items: Array<{ id: number }> };
    return body.items.map((item) => item.id);
  });

  const other = await browser.newContext({ baseURL: process.env['NETRAVE_URL'] ?? 'http://127.0.0.1:18080' });
  const otherPage = await other.newPage();
  await otherPage.goto('/');
  await otherPage.locator('#auth-user').fill(username);
  await otherPage.locator('#auth-pass').fill('Passw0rd!23');
  await otherPage.getByRole('button', { name: 'Sign in' }).click();
  await expect(otherPage.getByRole('heading', { name: 'Laboratories' })).toBeVisible();
  // The new account sees only its own login event, live, with an unread badge of 1.
  await expect(otherPage.locator('[data-notification-count]')).toHaveText('1');
  const otherNotifications = await otherPage.evaluate(async () => {
    const response = await fetch('/api/notifications');
    const body = await response.json() as { unread: number; items: Array<{ id: number }> };
    return { unread: body.unread, ids: body.items.map((item) => item.id) };
  });
  expect(otherNotifications.unread).toBe(1);
  expect(otherNotifications.ids).toHaveLength(1);
  expect(otherNotifications.ids.every((id) => !adminNotifications.includes(id))).toBe(true);
  await other.close();
});
