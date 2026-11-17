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

async function asUser(browser: Browser, username: string, pass: string): Promise<{ page: Page; close: () => Promise<void> }> {
  const ctx = await browser.newContext({ baseURL: test.info().project.use.baseURL });
  const page = await ctx.newPage();
  await login(page, username, pass);
  return { page, close: () => ctx.close() };
}

test('роли: права, папки, участники, объединение и запрет эскалации', async ({ page, browser }) => {
  test.setTimeout(150_000);
  const stamp = Date.now();
  const folderA = `Role folder A ${stamp}`;
  const folderB = `Role folder B ${stamp}`;
  const labA = `Role lab A ${stamp}`;
  const labB = `Role lab B ${stamp}`;
  const roleName = `Teacher ${stamp}`;
  const roleName2 = `Assistant ${stamp}`;
  const user1 = `e2e_r1_${stamp}`;
  const user2 = `e2e_r2_${stamp}`;
  const userPass = 'Passw0rd!123';

  await login(page, admin, password);

  // Users and folders.
  for (const username of [user1, user2]) {
    const created = await api(page, 'POST', '/api/users', { username, password: userPass, role: 'user' });
    expect(created.status).toBeLessThan(300);
  }
  const createdA = await api(page, 'POST', '/api/lab-folders', { name: folderA, parent_id: null });
  const createdB = await api(page, 'POST', '/api/lab-folders', { name: folderB, parent_id: null });
  expect(createdA.status).toBeLessThan(300);
  expect(createdB.status).toBeLessThan(300);
  const folderAID = Number(createdA.body?.['id']);
  const folderBID = Number(createdB.body?.['id']);

  const labs: Array<{ id: number; name: string; folder: number }> = [
    { id: 0, name: labA, folder: folderAID },
    { id: 0, name: labB, folder: folderBID },
  ];
  for (const lab of labs) {
    const created = await api(page, 'POST', '/api/labs', { name: lab.name, description: 'rbac' });
    expect(created.status).toBeLessThan(300);
    lab.id = Number(created.body?.['id']);
    expect((await api(page, 'PUT', `/api/labs/${lab.id}/public`, { level: 'none' })).status).toBeLessThan(300);
    expect((await api(page, 'PUT', `/api/labs/${lab.id}/folder`, { folder_id: lab.folder })).status).toBeLessThan(300);
  }

  // 1-3. Creating a role via the UI: permissions and several folders.
  await page.goto('/#/roles');
  await expect(page.getByRole('heading', { name: 'Roles', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'New role' }).click();
  const dialog = page.getByRole('dialog', { name: 'New role' });
  await dialog.getByPlaceholder('Role name').fill(roleName);
  await dialog.getByPlaceholder('Description').fill('E2E teacher role');
  await dialog.getByRole('checkbox', { name: 'View laboratories', exact: true }).check();
  await dialog.getByRole('checkbox', { name: 'Start and stop machines', exact: true }).check();
  await dialog.getByRole('checkbox', { name: new RegExp(folderA) }).check();
  await dialog.getByRole('checkbox', { name: new RegExp(folderB) }).check();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  const roleRow = page.getByRole('row', { name: new RegExp(roleName) });
  await expect(roleRow).toBeVisible();

  const roles = await api(page, 'GET', '/api/roles');
  const role = (roles.body as unknown as Array<Record<string, unknown>>).find((item) => item['name'] === roleName);
  expect(role).toBeTruthy();
  const roleID = Number(role?.['id']);
  expect((role?.['permissions'] as string[])).toEqual(expect.arrayContaining(['laboratories.view', 'laboratories.start_stop']));
  expect((role?.['folder_ids'] as number[])).toEqual(expect.arrayContaining([folderAID, folderBID]));

  // 4. Adding several members via the role card.
  await roleRow.getByRole('button', { name: 'Edit' }).click();
  const edit = page.getByRole('dialog', { name: new RegExp(`Edit: ${roleName}`) });
  await edit.getByPlaceholder('Search users by login…').fill(user1);
  await edit.getByRole('checkbox', { name: user1 }).check();
  await edit.getByPlaceholder('Search users by login…').fill(user2);
  await edit.getByRole('checkbox', { name: user2 }).check();
  await edit.getByRole('button', { name: /Add selected/ }).click();
  await expect(edit).toContainText(user1);
  await expect(edit).toContainText(user2);
  await edit.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(dialog).toHaveCount(0);

  const afterMembers = await api(page, 'GET', '/api/roles');
  const roleAfter = (afterMembers.body as unknown as Array<Record<string, unknown>>).find((item) => item['id'] === roleID);
  expect(roleAfter?.['member_count']).toBe(2);

  const u1 = await asUser(browser, user1, userPass);
  const u2 = await asUser(browser, user2, userPass);
  try {
    // 5. All members automatically receive permissions and folder access.
    for (const userPage of [u1.page, u2.page]) {
      const me = await api(userPage, 'GET', '/api/me');
      const meUser = me.body?.['user'] as Record<string, unknown>;
      expect(meUser['role']).toBe('user');
      expect(meUser['roles'] as Array<Record<string, unknown>>).toEqual(
        expect.arrayContaining([expect.objectContaining({ name: roleName })]),
      );
      expect(meUser['role_perms'] as string[]).toEqual(expect.arrayContaining(['laboratories.view', 'laboratories.start_stop']));
      const folders = await api(userPage, 'GET', '/api/lab-folders');
      const names = (folders.body as unknown as Array<{ name: string }>).map((item) => item.name);
      expect(names).toEqual(expect.arrayContaining([folderA, folderB]));
      const list = await api(userPage, 'GET', '/api/labs');
      const labNames = (list.body as unknown as Array<{ name: string }>).map((item) => item.name);
      expect(labNames).toEqual(expect.arrayContaining([labA, labB]));
    }
    const labAPerms = await api(u1.page, 'GET', `/api/labs/${labs[0]!.id}`);
    expect(labAPerms.status).toBe(200);
    expect((labAPerms.body?.['perms'] as Record<string, boolean>)['machine.start']).toBe(true);

    // 6. Changing the role's permissions applies to all members.
    const changed = await api(page, 'PUT', `/api/roles/${roleID}`, {
      permissions: ['laboratories.view', 'laboratories.copy'],
    });
    expect(changed.status).toBe(200);
    for (const userPage of [u1.page, u2.page]) {
      const me = await api(userPage, 'GET', '/api/me');
      const perms = (me.body?.['user'] as Record<string, unknown>)['role_perms'] as string[];
      expect(perms).toEqual(expect.arrayContaining(['laboratories.view', 'laboratories.copy']));
      expect(perms).not.toContain('laboratories.start_stop');
    }
    const labAPerms2 = await api(u1.page, 'GET', `/api/labs/${labs[0]!.id}`);
    const perms2 = labAPerms2.body?.['perms'] as Record<string, boolean>;
    expect(perms2['lab.export']).toBe(true);
    expect(perms2['machine.start']).toBeFalsy();

    // 8. Second role: permissions are combined.
    const role2 = await api(page, 'POST', '/api/roles', {
      name: roleName2,
      description: 'assistant',
      permissions: ['laboratories.view', 'laboratories.delete'],
      folder_ids: [folderBID],
    });
    expect(role2.status).toBeLessThan(300);
    const role2ID = Number(role2.body?.['id']);

    // Find the first user's id via the user list (administrator).
    const users = await api(page, 'GET', '/api/users');
    const uid1 = Number((users.body as unknown as Array<Record<string, unknown>>).find((u) => u['username'] === user1)?.['id']);
    expect(uid1).toBeGreaterThan(0);
    expect((await api(page, 'POST', `/api/roles/${role2ID}/members`, { user_ids: [uid1] })).status).toBe(200);
    const union = await api(u1.page, 'GET', '/api/me');
    const unionPerms = (union.body?.['user'] as Record<string, unknown>)['role_perms'] as string[];
    expect(unionPerms).toEqual(expect.arrayContaining(['laboratories.view', 'laboratories.copy', 'laboratories.delete']));

    // 7. Removing from the first role revokes only its permissions and folder.
    expect((await api(page, 'DELETE', `/api/roles/${roleID}/members/${uid1}`)).status).toBe(200);
    const revoked = await api(u1.page, 'GET', '/api/me');
    const revokedPerms = (revoked.body?.['user'] as Record<string, unknown>)['role_perms'] as string[];
    expect(revokedPerms).toContain('laboratories.delete');
    expect(revokedPerms).not.toContain('laboratories.copy');
    expect((await api(u1.page, 'GET', `/api/labs/${labs[0]!.id}`)).status).toBe(404);
    expect((await api(u1.page, 'GET', `/api/labs/${labs[1]!.id}`)).status).toBe(200);

    // 9-10. The role does not grant administrator rights and does not open the role management API.
    expect((await api(u1.page, 'GET', '/api/roles')).status).toBe(403);
    expect((await api(u1.page, 'POST', '/api/roles', { name: 'hack' })).status).toBe(403);
    expect((await api(u1.page, 'PUT', `/api/users/${uid1}`, { role: 'admin' })).status).toBe(403);
    const finalMe = await api(u1.page, 'GET', '/api/me');
    expect((finalMe.body?.['user'] as Record<string, unknown>)['role']).toBe('user');

    // The user card shows assigned roles.
    await page.goto('/#/users');
    const userRow = page.getByRole('row', { name: new RegExp(user1) });
    await expect(userRow).toContainText(roleName2);
  } finally {
    await u1.close();
    await u2.close();
  }

  // 11. Role data is preserved after a page reload (server restart is checked by a database test).
  await page.reload();
  await page.goto('/#/roles');
  await expect(page.getByRole('row', { name: new RegExp(roleName) })).toBeVisible();
  await expect(page.getByRole('row', { name: new RegExp(roleName2) })).toBeVisible();
});
