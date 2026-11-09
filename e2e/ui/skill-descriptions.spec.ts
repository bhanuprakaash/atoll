// A skill's description is read as YAML, so every way to write it shows as plain text in Settings >
// Skills, Bot settings and the New Bot skill list, and editing it writes YAML that reads back the
// same. Each test runs its own machine and creates the skills through the API.
import { expect, type Page, test } from "@playwright/test";
import { parse } from "yaml";
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

type Case = { name: string; yaml: string; text: string; descFirst?: boolean };

const create = (s: Case, eol = "\n") => {
  const lines = [`name: ${s.name}`, s.yaml];
  if (s.descFirst) lines.reverse();
  return api(m, "POST", "/api/skills", {
    name: s.name,
    content: `---\n${lines.join("\n")}\n---\nBody.\n`.replaceAll("\n", eol),
  });
};

const createSkills = () => Promise.all(skills.map((s) => create(s)));

async function expectTexts(
  scope: ReturnType<Page["locator"]> | Page,
  names: Case[] = skills,
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

const edgeCases: Case[] = [
  { name: "null-skill", yaml: "description:", text: "", descFirst: true },
  {
    name: "comment-skill",
    yaml: "description: # c",
    text: "",
    descFirst: true,
  },
  {
    name: "colon-skill",
    yaml: "description: Use when: needed",
    text: "Use when: needed",
  },
];

for (const { name, yaml, text, descFirst } of [
  skills[0],
  skills[3],
  ...edgeCases,
] as Case[]) {
  test(`editing the description of ${name} writes YAML that parses back to the same text`, async ({
    page,
  }) => {
    const made = await create({ name, yaml, text, descFirst });
    const edited = 'Use for "x": a #1 case, then more';
    await page.goto(`${m.url}/settings/skills`);
    await page
      .getByRole("button", { name: new RegExp(name) })
      .click({ position: { x: 24, y: 12 } });
    const panel = page.getByRole("dialog", { name: `Edit ${name}` });
    const field = panel.getByRole("textbox", { name: "When to use it" });
    await expect(field).toHaveValue(text);
    await field.fill(edited);
    await Promise.all([
      page.waitForResponse(
        (r) =>
          r.request().method() === "PUT" && r.url().includes("/api/skills/"),
      ),
      panel.getByRole("button", { name: /^Save/ }).click(),
    ]);

    const { content } = await api(m, "GET", `/api/skills/${made.id}`);
    const [, front, body] = content.split(/^---\n/m);
    expect(parse(front)).toEqual({ name, description: edited });
    expect(body).toBe("Body.\n");
  });
}

test("a plain description with a colon shows its text, not a blank", async ({
  page,
}) => {
  await create(edgeCases[2]);
  await page.goto(`${m.url}/settings/skills`);
  await expectTexts(page, [edgeCases[2]]);
});

test("a SKILL.md with CRLF line endings shows its description", async ({
  page,
}) => {
  const crlf = {
    name: "crlf-skill",
    yaml: "description: Works with CRLF files",
    text: "Works with CRLF files",
  };
  const made = await create(crlf, "\r\n");
  await newBotAPI(m, "Kelp", "claude", { skills: [made.id] });
  await page.goto(`${m.url}/settings/skills`);
  await expectTexts(page, [crlf]);
  await page.goto(m.url);
  await sidebarRow(page, "Kelp").click();
  await page.getByRole("button", { name: "Bot settings" }).click();
  await expectTexts(page.getByRole("dialog", { name: "Bot settings" }), [crlf]);
});
