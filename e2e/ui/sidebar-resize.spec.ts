// Resizable thread pane (#469): 284px by default, 240–420px, double-click resets, the width
// survives a reload, the handle is a focusable separator that arrows move by 16px, and the 64px
// rail never resizes. Each test opens a fresh browser context, so its storage starts empty, as on
// first launch.
import { expect, type Page, test } from '@playwright/test';

// Chief's New thread screen: the pane shows Chief's threads.
const home = '/bots/chief/new';
const sidebar = (page: Page) => page.getByRole('region', { name: 'Chief', exact: true });
const rail = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const handle = (page: Page) => page.getByRole('separator', { name: 'Resize thread pane' });

async function expectRail(page: Page) {
  expect(Math.abs((await rail(page).boundingBox())!.width - 64)).toBeLessThanOrEqual(1);
}

async function expectWidth(page: Page, px: number) {
  await expect
    .poll(
      async () => {
        const w = (await sidebar(page).boundingBox())?.width ?? 0;
        return Math.abs(w - px) <= 1 ? px : w;
      },
      { message: `pane width ${px}px ±1` },
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

test('on first launch the pane is 284px and its handle is a vertical separator', async ({
  page,
}) => {
  await page.goto(home);
  await expectWidth(page, 284);
  const h = handle(page);
  await expect(h).toHaveAttribute('aria-orientation', 'vertical');
  await expect(h).toHaveAttribute('aria-valuenow', '284');
  await expect(h).toHaveAttribute('aria-valuemin', '240');
  await expect(h).toHaveAttribute('aria-valuemax', '420');
});

test('dragging the handle right by 100px makes 384px; far left stops at 240, far right at 420; the rail stays 64px', async ({
  page,
}) => {
  await page.goto(home);
  await expectWidth(page, 284);

  await drag(page, 100);
  await expectWidth(page, 384);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '384');

  await drag(page, -400);
  await expectWidth(page, 240);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '240');

  await drag(page, 600);
  await expectWidth(page, 420);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '420');
  await expectRail(page);
});

test('after a reload the pane keeps the dragged width', async ({ page }) => {
  await page.goto(home);
  await expectWidth(page, 284);
  await drag(page, 100);
  await expectWidth(page, 384);

  await page.reload();
  await expectWidth(page, 384);
  await expectRail(page);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '384');
});

test('double-clicking the handle sets the pane back to 284px', async ({ page }) => {
  await page.goto(home);
  await expectWidth(page, 284);
  await drag(page, 100);
  await expectWidth(page, 384);

  await handle(page).dblclick();
  await expectWidth(page, 284);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '284');
});

test('with the handle focused, Right arrow widens by 16px and Left arrow narrows by 16px', async ({
  page,
}) => {
  await page.goto(home);
  await expectWidth(page, 284);
  const h = handle(page);
  await h.focus();
  await expect(h).toBeFocused();

  await page.keyboard.press('ArrowRight');
  await expectWidth(page, 300);
  await expect(h).toHaveAttribute('aria-valuenow', '300');

  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowLeft');
  await expectWidth(page, 268);
  await expect(h).toHaveAttribute('aria-valuenow', '268');
});
