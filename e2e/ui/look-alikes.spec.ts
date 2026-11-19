// Two Bots should not look the same by accident: the new-Bot form starts on a look and colour no
// other Bot has, and the colour picker dims each colour a Bot of the chosen creature already wears,
// with one sentence naming that Bot. Nothing is blocked.
import { expect, test, type Locator, type Page } from '@playwright/test';
import { api, type Machine, newBotAPI, pickModel, sidebarRow, startAtoll } from './atoll';

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const colours = ['Brown', 'Orange', 'Amber', 'Green', 'Teal', 'Sky', 'Blue', 'Purple', 'Pink'];

const bot = async (name: string) =>
  (await api(m, 'GET', '/api/bots')).find((b: { name: string }) => b.name === name);

const creature = (scope: Page | Locator, name: string) =>
  scope.getByRole('radiogroup', { name: 'Creature' }).getByRole('radio', { name, exact: true });
const colour = (scope: Page | Locator, name: string) =>
  scope.getByRole('radiogroup', { name: 'Colour' }).getByRole('radio', { name, exact: true });

async function openNewBot(page: Page) {
  await page.goto(m.url);
  await page.getByRole('button', { name: 'New', exact: true }).click();
  await page.getByRole('menuitem', { name: 'New Bot' }).click();
  await expect(page.getByRole('heading', { name: 'New Bot' })).toBeVisible();
}

// shownPair is the checked creature and colour, e.g. "seal pink".
async function shownPair(page: Page) {
  const checked = (group: string) =>
    page
      .getByRole('radiogroup', { name: group })
      .getByRole('radio', { checked: true })
      .getAttribute('aria-label');
  return `${await checked('Creature')} ${await checked('Colour')}`.toLowerCase();
}

// expectDimmed checks that each colour in dimmed is described by its sentence, shown on screen,
// and that every other colour has no description.
async function expectDimmed(scope: Page | Locator, dimmed: Record<string, string>) {
  for (const [name, sentence] of Object.entries(dimmed)) {
    await expect(colour(scope, name)).toHaveAccessibleDescription(sentence);
    await expect(scope.getByText(sentence, { exact: true })).toBeVisible();
  }
  for (const name of colours.filter((c) => !(c in dimmed))) {
    await expect(colour(scope, name)).toHaveAccessibleDescription('');
  }
}

test('the new-Bot form starts on a look and colour pair no other Bot has', async ({ page }) => {
  await newBotAPI(m, 'Octo', 'claude', { look: 'octopus', colour: 'purple' });
  await newBotAPI(m, 'Wally', 'claude', { look: 'whale', colour: 'blue' });
  const taken = ['jellyfish orange', 'octopus purple', 'whale blue'];

  await openNewBot(page);
  let first = '';
  await expect(async () => {
    first = await shownPair(page);
    expect(taken).not.toContain(first);
  }).toPass({ timeout: 15_000 });
  await page.getByRole('textbox', { name: 'Name' }).fill('Fresh');
  await pickModel(page, page, 'Claude Code', 'Reef Probe');
  await page.getByRole('button', { name: 'Create Bot' }).click();
  await expect(page.getByRole('heading', { name: 'What should Fresh work on?' })).toBeVisible();
  const fresh = await bot('Fresh');
  expect(`${fresh.look} ${fresh.colour}`).toBe(first);

  await openNewBot(page);
  await expect(async () => {
    expect([...taken, first]).not.toContain(await shownPair(page));
  }).toPass({ timeout: 15_000 });
});

test('a colour another Bot of the chosen creature wears is dimmed and named, and can still be picked and saved', async ({
  page,
}) => {
  await newBotAPI(m, 'Reviewer', 'claude', { look: 'seal', colour: 'pink' });
  await newBotAPI(m, 'Tester', 'claude', { look: 'seal', colour: 'green' });
  await newBotAPI(m, 'Swimmer', 'claude', { look: 'whale', colour: 'teal' });

  await openNewBot(page);
  await page.getByRole('textbox', { name: 'Name' }).fill('Second Reviewer');
  await creature(page, 'Seal').check();
  await expectDimmed(page, {
    Green: 'A green seal is Tester.',
    Pink: 'A pink seal is Reviewer.',
  });
  await expect(page.getByText('A teal whale is Swimmer.')).toHaveCount(0);

  await colour(page, 'Pink').check();
  await expect(colour(page, 'Pink')).toBeChecked();
  await pickModel(page, page, 'Claude Code', 'Reef Probe');
  await page.getByRole('button', { name: 'Create Bot' }).click();
  await expect(
    page.getByRole('heading', { name: 'What should Second Reviewer work on?' }),
  ).toBeVisible();
  expect(await bot('Second Reviewer')).toMatchObject({
    look: 'seal',
    colour: 'pink',
  });
});

test('Bot settings dims and names the same colours, leaving out the Bot being edited', async ({
  page,
}) => {
  await newBotAPI(m, 'Reviewer', 'claude', { look: 'seal', colour: 'pink' });
  await newBotAPI(m, 'Tester', 'claude', { look: 'seal', colour: 'green' });
  await newBotAPI(m, 'Kelp', 'claude', { look: 'seal', colour: 'pink' });

  await page.goto(m.url);
  await sidebarRow(page, 'Kelp').click();
  await page.getByRole('button', { name: 'Bot settings' }).click();
  const panel = page.getByRole('dialog', { name: 'Bot settings' });
  await expect(creature(panel, 'Seal')).toBeChecked();
  await expectDimmed(panel, {
    Green: 'A green seal is Tester.',
    Pink: 'A pink seal is Reviewer.',
  });
  await expect(panel.getByText(/^An? \w+ seal is Kelp\.$/)).toHaveCount(0);
});

test('picking another creature updates which colours are dimmed', async ({ page }) => {
  await newBotAPI(m, 'Tester', 'claude', { look: 'seal', colour: 'green' });
  await newBotAPI(m, 'Swimmer', 'claude', { look: 'whale', colour: 'teal' });

  await openNewBot(page);
  await creature(page, 'Seal').check();
  await expectDimmed(page, { Green: 'A green seal is Tester.' });

  await creature(page, 'Whale').check();
  await expectDimmed(page, { Teal: 'A teal whale is Swimmer.' });
  await expect(page.getByText('A green seal is Tester.')).toHaveCount(0);

  await creature(page, 'Turtle').check();
  await expectDimmed(page, {});
  await expect(page.getByText('A teal whale is Swimmer.')).toHaveCount(0);
});
