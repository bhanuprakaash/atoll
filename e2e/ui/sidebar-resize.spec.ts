// Resizable sidebar: 260px by default, 240–420px, double-click resets, the width survives a reload
// (localStorage atoll.sidebarWidth), and the handle on its right edge is a focusable separator that
// arrows move by 16px. Each test opens a fresh browser context, so its storage starts empty, as on
// first launch.
import { expect, type Page, test } from '@playwright/test';

const home = '/bots/chief/new';
const sidebar = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const handle = (page: Page) => page.getByRole('separator', { name: 'Resize thread pane' });

async function expectWidth(page: Page, px: number) {
  await expect
    .poll(
      async () => {
        const w = (await sidebar(page).boundingBox())?.width ?? 0;
        return Math.abs(w - px) <= 1 ? px : w;
      },
      { message: `sidebar width ${px}px ±1` },
    )
    .toBe(px);
}

async function drag(page: Page, dx: number) {
  const box = (await handle(page).boundingBox())!;
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + dx, y, { steps: 10 });
  await page.mouse.up();
}

test('on first launch the sidebar is 260px and its handle is a vertical separator on its right edge', async ({
  page,
}) => {
  await page.goto(home);
  await expectWidth(page, 260);
  const h = handle(page);
  await expect(h).toHaveAttribute('aria-orientation', 'vertical');
  await expect(h).toHaveAttribute('aria-valuenow', '260');
  await expect(h).toHaveAttribute('aria-valuemin', '240');
  await expect(h).toHaveAttribute('aria-valuemax', '420');
  const nav = (await sidebar(page).boundingBox())!;
  const edge = (await h.boundingBox())!;
  expect(Math.abs(edge.x + edge.width / 2 - (nav.x + nav.width))).toBeLessThanOrEqual(8);
});

test('dragging the handle right by 100px makes 360px; far left stops at 240, far right at 420', async ({
  page,
}) => {
  await page.goto(home);
  await expectWidth(page, 260);

  await drag(page, 100);
  await expectWidth(page, 360);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '360');

  await drag(page, -400);
  await expectWidth(page, 240);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '240');

  await drag(page, 600);
  await expectWidth(page, 420);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '420');
});

test('after a reload the sidebar keeps the dragged width', async ({ page }) => {
  await page.goto(home);
  await expectWidth(page, 260);
  await drag(page, 100);
  await expectWidth(page, 360);
  expect(await page.evaluate(() => localStorage.getItem('atoll.sidebarWidth'))).toBe('360');

  await page.reload();
  await expectWidth(page, 360);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '360');
});

test('double-clicking the handle sets the sidebar back to 260px', async ({ page }) => {
  await page.goto(home);
  await expectWidth(page, 260);
  await drag(page, 100);
  await expectWidth(page, 360);

  await handle(page).dblclick();
  await expectWidth(page, 260);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '260');
});

test('with the handle focused, Right arrow widens by 16px and Left arrow narrows by 16px', async ({
  page,
}) => {
  await page.goto(home);
  await expectWidth(page, 260);
  const h = handle(page);
  await h.focus();
  await expect(h).toBeFocused();

  await page.keyboard.press('ArrowRight');
  await expectWidth(page, 276);
  await expect(h).toHaveAttribute('aria-valuenow', '276');

  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowLeft');
  await expectWidth(page, 244);
  await expect(h).toHaveAttribute('aria-valuenow', '244');
});
