import { expect, test, type Browser, type Page } from '@playwright/test';

const password = process.env['NETRAVE_PASSWORD'] ?? '';
const admin = process.env['NETRAVE_USER'] ?? 'admin';

test.skip(!password, 'NETRAVE_PASSWORD не задан');

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

test('папки: создание, перемещение, назначение, видимость и запрет обхода', async ({ page, browser }) => {
  test.setTimeout(120_000);
  const stamp = Date.now();
  const folderA = `E2E A ${stamp}`;
  const folderB = `E2E B ${stamp}`;
  const labA = `E2E lab A ${stamp}`;
  const labB = `E2E lab B ${stamp}`;
  const username = `e2e_f_${stamp}`;
  const userPass = 'Passw0rd!123';

  await login(page, admin, password);

  // User for testing access grants.
  await page.goto('/#/users');
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible();
  await page.getByPlaceholder('Username').fill(username);
  await page.getByPlaceholder('Password (≥ 8 characters)').fill(userPass);
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('cell', { name: username })).toBeVisible();

  const createdA = await api(page, 'POST', '/api/labs', { name: labA, description: 'folder access' });
  const createdB = await api(page, 'POST', '/api/labs', { name: labB, description: 'other folder' });
  expect(createdA.status).toBeLessThan(300);
  expect(createdB.status).toBeLessThan(300);
  const labAID = Number(createdA.body?.['id']);
  const labBID = Number(createdB.body?.['id']);

  // 1. The administrator creates two folders.
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

  // 3. Assigning folder A to the user.
  await page.goto('/#/users');
  const row = page.getByRole('row', { name: new RegExp(username) });
  await row.getByRole('button', { name: 'Folders' }).click();
  const accessDialog = page.getByRole('dialog', { name: `Folder access: ${username}` });
  const folderCheckbox = accessDialog.getByRole('checkbox', { name: new RegExp(folderA) });
  await expect(folderCheckbox).toBeVisible();
  await folderCheckbox.check();
  await accessDialog.getByRole('button', { name: 'Save' }).click();
  await expect(accessDialog).toHaveCount(0);

  const { ctx, page: userPage } = await newUserContext(browser, username, userPass);
  try {
    // 4. The user sees the assigned folder and its lab.
    const userFolders = await api(userPage, 'GET', '/api/lab-folders');
    expect(userFolders.status).toBe(200);
    const visibleNames = (userFolders.body as unknown as Array<{ name: string }>).map((f) => f.name);
    expect(visibleNames).toContain(folderA);
    // 5. Folder B and its lab are not visible to the user.
    expect(visibleNames).not.toContain(folderB);
    expect(userPage.locator('[data-folder-card]', { hasText: folderB })).toHaveCount(0);
    await userPage.locator('[data-folder-card]', { hasText: folderA }).click();
    await expect(userPage.locator('[data-lab-card]', { hasText: labA })).toBeVisible();
    await expect(userPage.locator('[data-lab-card]', { hasText: labB })).toHaveCount(0);
    // Administrative actions are not shown to the user.
    await expect(userPage.getByRole('button', { name: 'New folder' })).toHaveCount(0);
    await expect(userPage.locator('[data-lab-card]', { hasText: labA }).getByRole('button', { name: 'Move to folder…' })).toHaveCount(0);
    await expect(userPage.locator('[data-lab-card]', { hasText: labA })).not.toHaveAttribute('draggable', 'true');

    // 7. Direct API requests by the user: changing folders and other users' labs is forbidden.
    expect((await api(userPage, 'POST', '/api/lab-folders', { name: 'hack' })).status).toBe(403);
    expect((await api(userPage, 'PUT', `/api/labs/${labBID}/folder`, { folder_id: null })).status).toBe(403);
    expect((await api(userPage, 'GET', `/api/labs/${labBID}`)).status).toBe(404);
    expect((await api(userPage, 'GET', `/api/labs/${labAID}`)).status).toBe(200);

    // 6. Revoking access hides the folder and the lab.
    await page.goto('/#/users');
    await page.getByRole('row', { name: new RegExp(username) }).getByRole('button', { name: 'Folders' }).click();
    const revokeDialog = page.getByRole('dialog', { name: `Folder access: ${username}` });
    await revokeDialog.getByRole('checkbox', { name: new RegExp(folderA) }).uncheck();
    await revokeDialog.getByRole('button', { name: 'Save' }).click();
    await expect(revokeDialog).toHaveCount(0);

    await userPage.goto('/#/labs');
    await userPage.reload();
    await expect(userPage.locator('[data-folder-card]', { hasText: folderA })).toHaveCount(0);
    await expect(userPage.locator('[data-lab-card]', { hasText: labA })).toHaveCount(0);
    await expect(userPage.getByRole('link', { name: 'No folder' })).toBeVisible();
    expect((await api(userPage, 'GET', `/api/labs/${labAID}`)).status).toBe(404);

    // 8. Deleting a folder does not delete labs: they return to "No folder".
    await page.goto('/#/labs');
    page.once('dialog', (dialog) => void dialog.accept());
    await page.locator('[data-folder-card]', { hasText: folderA }).getByTitle('Delete').click();
    await expect(page.locator('[data-folder-card]', { hasText: folderA })).toHaveCount(0);
    await expect(page.locator('[data-lab-card]', { hasText: labA })).toBeVisible();
    expect((await api(page, 'GET', `/api/labs/${labAID}`)).status).toBe(200);
  } finally {
    await ctx.close();
    await api(page, 'DELETE', `/api/labs/${labAID}`);
    await api(page, 'DELETE', `/api/labs/${labBID}`);
    // Folder B remains: delete it and the user so as not to clutter the shared database.
    const folders = await api(page, 'GET', '/api/lab-folders');
    for (const folder of (folders.body as unknown as Array<{ id: number; name: string }>) ?? []) {
      if (folder.name === folderB) await api(page, 'DELETE', `/api/lab-folders/${folder.id}`);
    }
    const users = await api(page, 'GET', '/api/users');
    for (const user of (users.body as unknown as Array<{ id: number; username: string }>) ?? []) {
      if (user.username === username) await api(page, 'DELETE', `/api/users/${user.id}`);
    }
  }
});
