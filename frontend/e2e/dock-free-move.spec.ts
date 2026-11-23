import { expect, test } from '@playwright/test';

const password = process.env['NETRAVE_PASSWORD'] ?? '';

test.skip(!password, 'NETRAVE_PASSWORD is not set');

test('floating console moves outside the viewport and stays put', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/');
  await page.locator('#auth-user').fill('admin');
  await page.locator('#auth-pass').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Laboratories' })).toBeVisible();

  const dockUrl = await page.evaluate(async () => {
    const script = document.querySelector<HTMLScriptElement>('script[type="module"]');
    if (!script) throw new Error('main module script not found');
    const main = await (await fetch(script.src)).text();
    const match = main.match(/(?:\.\/|\/assets\/)dock-[A-Za-z0-9_-]+\.js/);
    if (!match) throw new Error('dock chunk not found');
    return new URL(match[0], script.src).href;
  });

  await page.evaluate(async (url) => {
    const mod = await import(url) as { getDock: () => { open: (device: unknown, force: boolean) => void } };
    const dock = mod.getDock();
    dock.open({ rid: 900001, name: 'E2E Dock', lab_id: '', state: 'running', console: 'serial', lab_perms: {} }, false);
    dock.open({ rid: 900002, name: 'E2E Dock 2', lab_id: '', state: 'running', console: 'serial', lab_perms: {} }, false);
  }, dockUrl);

  const dock = page.locator('[data-dock]').first();
  await expect(dock).toBeVisible();
  await expect(dock.locator('[data-dock-tab]')).toHaveCount(2);
  await expect(dock).toHaveCSS('position', 'fixed');
  await expect(dock).toHaveCSS('z-index', '75');

  const initial = await dock.boundingBox();
  if (!initial) throw new Error('no dock box');
  const initialPos = await dock.evaluate((el) => ({
    left: (el as HTMLElement).style.left,
    top: (el as HTMLElement).style.top,
  }));

  // Pointer interaction inside the terminal content must not move the window.
  await page.mouse.move(initial.x + initial.width / 2, initial.y + initial.height / 2);
  await page.mouse.down();
  await page.mouse.move(initial.x + initial.width / 2 + 120, initial.y + initial.height / 2 + 80, { steps: 5 });
  await page.mouse.up();
  await expect.poll(() => dock.evaluate((el) => ({
    left: (el as HTMLElement).style.left,
    top: (el as HTMLElement).style.top,
  }))).toEqual(initialPos);

  // Resizing through the south-east handle still works and keeps coordinates.
  const handle = dock.locator('[data-dock-resize="se"]');
  const sizeBefore = await dock.boundingBox();
  const sb = await handle.boundingBox();
  if (!sizeBefore || !sb) throw new Error('no resize boxes');
  await page.mouse.move(sb.x + sb.width / 2, sb.y + sb.height / 2);
  await page.mouse.down();
  await page.mouse.move(sb.x + sb.width / 2 + 140, sb.y + sb.height / 2 + 90, { steps: 8 });
  await page.mouse.up();
  const sizeAfter = await dock.boundingBox();
  if (!sizeAfter) throw new Error('no size after resize');
  expect(sizeAfter.width).toBeGreaterThan(sizeBefore.width + 80);
  expect(sizeAfter.height).toBeGreaterThan(sizeBefore.height + 50);
  await expect.poll(() => dock.evaluate((el) => ({
    left: (el as HTMLElement).style.left,
    top: (el as HTMLElement).style.top,
  }))).toEqual(initialPos);

  // Drag by a large delta so the window leaves the viewport entirely.
  const head = dock.locator('[data-dock-head]');
  const hb = await head.boundingBox();
  if (!hb) throw new Error('no head box');
  await page.mouse.move(hb.x + hb.width / 2, hb.y + 10);
  await page.mouse.down();
  await page.mouse.move(hb.x + hb.width / 2 - 1600, hb.y + 10 - 1200, { steps: 12 });
  await page.mouse.up();

  const moved = await dock.evaluate((el) => ({
    left: parseFloat((el as HTMLElement).style.left),
    top: parseFloat((el as HTMLElement).style.top),
    rect: el.getBoundingClientRect().toJSON(),
  }));
  expect(moved.left).toBeLessThan(-200);
  expect(moved.top).toBeLessThan(-100);
  expect(moved.rect.right).toBeLessThan(0);
  expect(moved.rect.bottom).toBeLessThan(0);

  // Browser viewport resize must never reposition or clamp the window.
  const beforeResize = await dock.evaluate((el) => ({
    left: (el as HTMLElement).style.left,
    top: (el as HTMLElement).style.top,
  }));
  await page.setViewportSize({ width: 900, height: 620 });
  await page.waitForTimeout(400);
  const afterResize = await dock.evaluate((el) => ({
    left: (el as HTMLElement).style.left,
    top: (el as HTMLElement).style.top,
  }));
  expect(afterResize).toEqual(beforeResize);
});
