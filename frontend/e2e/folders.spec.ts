import { expect, test, type Browser, type Page } from '@playwright/test';

const password = process.env['NETRAVE_PASSWORD'] ?? '';
const admin = process.env['NETRAVE_USER'] ?? 'admin';

test.skip(!password, 'NETRAVE_PASSWORD is not set');

async function login(page: Page, username: string, pass: string) {
  await page.goto('/');
  await page.locator('#auth-user').fill(username);
  await page.locator('#auth-pass').fill(pass);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Laboratories', exact: true })).toBeVisible();
}

// api calls REST as the logged-in user: the session cookie is already in the context,
// the CSRF token is taken from /api/me.
async function api(page: Page, method: string, path: string, body?: unknown) {
  return page.evaluate(async ({ method, path, body }) => {
    const session = await fetch('/api/me').then((res) => res.json());
    const res = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    let parsed: unknown = null;
    try { parsed = JSON.parse(text); } catch { /* not json */ }
    return { status: res.status, body: parsed as Record<string, unknown> | null };
  }, { method, path, body });
}

async function newUserContext(browser: Browser, username: string, pass: string): Promise<{ ctx: Awaited<ReturnType<Browser['newContext']>>; page: Page }> {
  const ctx = await browser.newContext({ baseURL: test.info().project.use.baseURL });
  const page = await ctx.newPage();
  await login(page, username, pass);
  return { ctx, page };
}

test('folders: create, move, derived visibility and access revocation', async ({ page, browser }) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  const folderA = `E2E A ${stamp}`;
  const folderB = `E2E B ${stamp}`;
  const labA = `E2E lab A ${stamp}`;
  const labB = `E2E lab B ${stamp}`;
  const username = `e2e_f_${stamp}`;
  const userPass = 'Passw0rd!123';

  await login(page, admin, password);

  await page.goto('/#/users');
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible();
  await page.getByPlaceholder('Username').fill(username);
  await page.getByPlaceholder('Password (≥ 8 characters)').fill(userPass);
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('cell', { name: username })).toBeVisible();

  const users = await api(page, 'GET', '/api/users');
  const userId = (users.body as unknown as Array<{ id: number; username: string }>).find((u) => u.username === username)?.id;
  expect(userId).toBeTruthy();

  const createdA = await api(page, 'POST', '/api/labs', { name: labA, description: 'folder access' });
  const createdB = await api(page, 'POST', '/api/labs', { name: labB, description: 'other folder' });
  expect(createdA.status).toBeLessThan(300);
  expect(createdB.status).toBeLessThan(300);

  // 1. The administrator creates two folders through the UI.
  await page.goto('/#/labs');
  await expect(page.getByRole('heading', { name: 'Laboratories' })).toBeVisible();
  for (const name of [folderA, folderB]) {
    await page.getByRole('button', { name: 'New folder' }).click();
    const dialog = page.getByRole('dialog', { name: 'New folder' });
    await dialog.locator('input').fill(name);
    await dialog.getByRole('button', { name: 'Save' }).click();
    await expect(page.locator('[data-folder-card]', { hasText: name })).toBeVisible();
  }

  // 2. Moving labs into different folders via the dialog.
  for (const [labName, folderName] of [[labA, folderA], [labB, folderB]] as const) {
    const card = page.locator('[data-lab-card]', { hasText: labName });
    await card.getByRole('button', { name: 'Move to folder…' }).click();
    const dialog = page.getByRole('dialog', { name: 'Move laboratory' });
    await dialog.locator('select').selectOption({ label: folderName });
    await dialog.getByRole('button', { name: 'Move laboratory' }).click();
    await expect(card).toHaveCount(0);
  }
  await expect(page.locator('[data-folder-card]', { hasText: folderA })).toContainText('1 labs');
  await expect(page.locator('[data-folder-card]', { hasText: folderB })).toContainText('1 labs');

  // 3. Access is granted on the lab itself: the folder becomes visible through it.
  expect((await api(page, 'PUT', `/api/labs/${encodeURIComponent(labA)}/permissions/${userId}`, { changes: { 'lab.view': true } })).status).toBeLessThan(300);

  const { ctx, page: userPage } = await newUserContext(browser, username, userPass);
  try {
    const userFolders = await api(userPage, 'GET', '/api/lab-folders');
    expect(userFolders.status).toBe(200);
    const visibleNames = (userFolders.body as unknown as Array<{ name: string }>).map((f) => f.name);
    expect(visibleNames).toContain(folderA);
    expect(visibleNames).not.toContain(folderB);
    expect(userPage.locator('[data-folder-card]', { hasText: folderB })).toHaveCount(0);
    await userPage.locator('[data-folder-card]', { hasText: folderA }).click();
    await expect(userPage.locator('[data-lab-card]', { hasText: labA })).toBeVisible();
    await expect(userPage.locator('[data-lab-card]', { hasText: labB })).toHaveCount(0);
    await expect(userPage.getByRole('button', { name: 'New folder' })).toHaveCount(0);
    await expect(userPage.locator('[data-lab-card]', { hasText: labA }).getByRole('button', { name: 'Move to folder…' })).toHaveCount(0);

    expect((await api(userPage, 'POST', '/api/lab-folders', { name: 'hack', parent: '' })).status).toBe(403);
    expect((await api(userPage, 'PUT', `/api/labs/${encodeURIComponent(labB)}/folder`, { folder: null })).status).toBe(403);
    expect((await api(userPage, 'GET', `/api/labs/${encodeURIComponent(labB)}`)).status).toBe(404);
    expect((await api(userPage, 'GET', `/api/labs/${encodeURIComponent(labA)}`)).status).toBe(200);

    // 4. Revoking the lab permission hides the lab and its folder.
    expect((await api(page, 'PUT', `/api/labs/${encodeURIComponent(labA)}/permissions/${userId}`, { changes: { 'lab.view': false } })).status).toBeLessThan(300);
    await userPage.goto('/#/labs');
    await userPage.reload();
    await expect(userPage.locator('[data-folder-card]', { hasText: folderA })).toHaveCount(0);
    await expect(userPage.locator('[data-lab-card]', { hasText: labA })).toHaveCount(0);
    expect((await api(userPage, 'GET', `/api/labs/${encodeURIComponent(labA)}`)).status).toBe(404);

    // 5. Deleting a folder does not delete labs: they return to the root.
    await page.goto('/#/labs');
    await page.locator('[data-folder-card]', { hasText: folderA }).getByTitle('Delete').click();
    const confirm = page.getByRole('dialog');
    if (await confirm.count()) await confirm.getByRole('button', { name: /Delete|Delete folder/i }).click();
    await expect(page.locator('[data-folder-card]', { hasText: folderA })).toHaveCount(0);
    await expect(page.locator('[data-lab-card]', { hasText: labA })).toBeVisible();
    expect((await api(page, 'GET', `/api/labs/${encodeURIComponent(labA)}`)).status).toBe(200);
  } finally {
    await ctx.close();
    await api(page, 'DELETE', `/api/labs/${encodeURIComponent(labA)}`);
    await api(page, 'DELETE', `/api/labs/${encodeURIComponent(labB)}`);
    const folders = await api(page, 'GET', '/api/lab-folders');
    for (const folder of (folders.body as unknown as Array<{ path: string; name: string }>) ?? []) {
      if (folder.name === folderB || folder.name === folderA) await api(page, 'DELETE', `/api/lab-folders?path=${encodeURIComponent(folder.path)}`);
    }
    const all = await api(page, 'GET', '/api/users');
    for (const user of (all.body as unknown as Array<{ id: number; username: string }>) ?? []) {
      if (user.username === username) await api(page, 'DELETE', `/api/users/${user.id}`);
    }
  }
});
