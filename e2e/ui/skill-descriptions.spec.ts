// A skill's description is read as YAML, so every way to write it shows as plain text in Settings >
// Skills, Bot settings and the New Bot skill list, and editing it writes YAML that reads back the
// same. Each test runs its own machine and creates the skills through the API.
import { expect, type Page, test } from "@playwright/test";
import { api, type Machine, newBotAPI, sidebarRow, startAtoll } from "./atoll";

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const skills = [
  {
    name: "quoted-skill",
    yaml: 'description: "Use for \\"how does X work\\", walkthroughs"',
    text: 'Use for "how does X work", walkthroughs',
  },
  {
    name: "single-skill",
    yaml: "description: 'It''s for \"quotes\" and ''single'' ones'",
    text: "It's for \"quotes\" and 'single' ones",
  },
  {
    name: "plain-skill",
    yaml: "description: Plain words, no quotes",
    text: "Plain words, no quotes",
  },
  {
    name: "folded-skill",
    yaml: "description: >\n  Folded text that\n  spans two lines",
    text: "Folded text that spans two lines",
  },
];

const createSkills = () =>
  Promise.all(
    skills.map((s) =>
      api(m, "POST", "/api/skills", {
        name: s.name,
        content: `---\nname: ${s.name}\n${s.yaml}\n---\nBody.\n`,
      }),
    ),
  );

async function expectTexts(
  scope: ReturnType<Page["locator"]> | Page,
  names = skills,
) {
  for (const s of names) {
    await expect(scope.getByText(s.text, { exact: true })).toBeVisible();
  }
}

test("Settings > Skills shows quoted, single-quoted, plain and folded descriptions as text", async ({
  page,
}) => {
  await createSkills();
  await page.goto(`${m.url}/settings/skills`);
  await expectTexts(page);
});

test("Bot settings shows the descriptions of its skills as text", async ({
  page,
}) => {
  const made = await createSkills();
  await newBotAPI(m, "Kelp", "claude", {
    skills: made.map((s: { id: string }) => s.id),
  });
  await page.goto(m.url);
  await sidebarRow(page, "Kelp").click();
  await page.getByRole("button", { name: "Bot settings" }).click();
  await expectTexts(page.getByRole("dialog", { name: "Bot settings" }));
});

test("the New Bot skill list shows the descriptions as text", async ({
  page,
}) => {
  await createSkills();
  await page.goto(m.url);
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menuitem", { name: "New Bot" }).click();
  await page.getByRole("button", { name: "Add skill" }).click();
  await expectTexts(page.getByRole("dialog", { name: "Add skill" }));
});

test("editing a description in the editor writes YAML that reads back as the same text", async ({
  page,
}) => {
  const [quoted] = await createSkills();
  const edited = 'Use for "x": a #1 case, then more';
  await page.goto(`${m.url}/settings/skills`);
  await page
    .getByRole("button", { name: /quoted-skill/ })
    .click({ position: { x: 24, y: 12 } });
  const panel = page.getByRole("dialog", { name: "Edit quoted-skill" });
  const field = panel.getByRole("textbox", { name: "When to use it" });
  await expect(field).toHaveValue(skills[0].text);
  await field.fill(edited);
  await Promise.all([
    page.waitForResponse(
      (r) => r.request().method() === "PUT" && r.url().includes("/api/skills/"),
    ),
    panel.getByRole("button", { name: /^Save/ }).click(),
  ]);
  expect((await api(m, "GET", `/api/skills/${quoted.id}`)).content).toContain(
    "Body.",
  );

  await page.reload();
  await expect(page.getByText(edited, { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: /quoted-skill/ })
    .click({ position: { x: 24, y: 12 } });
  await expect(
    page
      .getByRole("dialog", { name: "Edit quoted-skill" })
      .getByRole("textbox", { name: "When to use it" }),
  ).toHaveValue(edited);
});
