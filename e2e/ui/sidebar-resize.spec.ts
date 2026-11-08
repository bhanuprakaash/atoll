// Resizable sidebar: 340px by default, 260–480px, double-click resets, the width survives a
// reload, and the handle is a focusable separator that arrows move by 16px. Each test opens a
// fresh browser context, so its storage starts empty, as on first launch.
import { expect, type Page, test } from '@playwright/test';

const sidebar = (page: Page) => page.getByRole('navigation', { name: 'Bots' });
const handle = (page: Page) => page.getByRole('separator', { name: 'Resize sidebar' });

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

test('on first launch the sidebar is 340px and its handle is a vertical separator', async ({
  page,
}) => {
  await page.goto('/');
  await expectWidth(page, 340);
  const h = handle(page);
  await expect(h).toHaveAttribute('aria-orientation', 'vertical');
  await expect(h).toHaveAttribute('aria-valuenow', '340');
  await expect(h).toHaveAttribute('aria-valuemin', '260');
  await expect(h).toHaveAttribute('aria-valuemax', '480');
});

test('dragging the handle right by 100px makes 440px; far left stops at 260, far right at 480', async ({
  page,
}) => {
  await page.goto('/');
  await expectWidth(page, 340);

  await drag(page, 100);
  await expectWidth(page, 440);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '440');

  await drag(page, -400);
  await expectWidth(page, 260);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '260');

  await drag(page, 600);
  await expectWidth(page, 480);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '480');
});

test('after a reload the sidebar keeps the dragged width', async ({ page }) => {
  await page.goto('/');
  await expectWidth(page, 340);
  await drag(page, 100);
  await expectWidth(page, 440);

  await page.reload();
  await expectWidth(page, 440);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '440');
});

test('double-clicking the handle sets the sidebar back to 340px', async ({ page }) => {
  await page.goto('/');
  await expectWidth(page, 340);
  await drag(page, 100);
  await expectWidth(page, 440);

  await handle(page).dblclick();
  await expectWidth(page, 340);
  await expect(handle(page)).toHaveAttribute('aria-valuenow', '340');
});

test('with the handle focused, Right arrow widens by 16px and Left arrow narrows by 16px', async ({
  page,
}) => {
  await page.goto('/');
  await expectWidth(page, 340);
  const h = handle(page);
  await h.focus();
  await expect(h).toBeFocused();

  await page.keyboard.press('ArrowRight');
  await expectWidth(page, 356);
  await expect(h).toHaveAttribute('aria-valuenow', '356');

  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowLeft');
  await expectWidth(page, 324);
  await expect(h).toHaveAttribute('aria-valuenow', '324');
});
