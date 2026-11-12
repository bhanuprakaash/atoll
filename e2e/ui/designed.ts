// What a designed error looks like on the page (docs/design.md, App and UI: Errors). Contract:
// designed-errors-contract.md.
import { expect, type Locator, type Page } from '@playwright/test';

const danger = 'rgb(229, 72, 77)';

export type Designed = {
  // The plain sentence the person reads first.
  title: string | RegExp;
  // The line that says what to do. A chat run line has none: its action stands in for it.
  next?: string | RegExp;
  // The action button or link, when there is one.
  action?: string;
  // What must not show while Details is closed: a Go chain, a status text, a provider's raw error.
  raw: RegExp;
  // What Details shows once opened.
  details: RegExp;
};

// notice is the one alert on the page (or in scope) that carries the title.
export function notice(scope: Page | Locator, title: string | RegExp) {
  return scope.getByRole('alert').filter({ hasText: title });
}

// bodyText is every text the page shows right now, a dialog included.
const bodyText = (page: Page) => page.locator('body').innerText();

// expectDesigned proves the shape: plain title, a line of what to do, the action, Details closed
// with the raw text behind it, no JSON anywhere, and only the icon in red.
export async function expectDesigned(page: Page, alert: Locator, want: Designed) {
  // The failure surfaced, as a notice or as its raw text; the rest says which.
  await expect(alert.or(page.getByText(want.raw).first()), 'the failure never showed').toBeVisible();
  await expect(alert, 'the failure shows without a notice').toHaveCount(1);
  await expect(alert).toBeVisible();
  await expect(alert).toContainText(want.title);
  if (want.next) await expect(alert).toContainText(want.next);
  if (want.action) {
    await expect(
      alert.getByRole('button', { name: want.action }).or(alert.getByRole('link', { name: want.action })),
    ).toBeVisible();
  }

  const details = alert.getByRole('button', { name: 'Details' });
  await expect(details).toHaveAttribute('aria-expanded', 'false');
  const closed = await bodyText(page);
  expect(closed, 'the raw text shows while Details is closed').not.toMatch(want.raw);
  expect(closed, 'JSON shows on the page').not.toMatch(/\{\s*"error"\s*:/);
  expect(closed, 'the old catch-all shows').not.toMatch(/internal error|Failed to fetch/i);

  const red = await alert.evaluate((el, color) => {
    const bad: string[] = [];
    for (const e of [el, ...el.querySelectorAll('*')]) {
      const own = [...e.childNodes].some((n) => n.nodeType === 3 && (n.textContent ?? '').trim());
      if (own && getComputedStyle(e).color === color) bad.push(e.textContent?.trim() ?? '');
    }
    return bad;
  }, danger);
  expect(red, 'text in the danger colour; only the icon may be red').toEqual([]);

  await details.click();
  await expect(details).toHaveAttribute('aria-expanded', 'true');
  await expect(alert).toContainText(want.details);
}
