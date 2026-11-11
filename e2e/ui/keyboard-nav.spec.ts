// Keyboard: the composer has focus when a thread opens, a printable key with nothing focused
// types into it, Ctrl+K (⌘K on macOS) opens a switcher over the Bots and the open threads of
// every Bot, and the sidebar search matches thread titles. Contract: e2e/keyboard-nav-contract.md.
import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { expect, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sidebarRow,
  startAtoll,
  threadTree,
} from "./atoll";

let m: Machine;
const ids: Record<string, string> = {};

async function waitStatus(threadID: string, status: string) {
  await expect
    .poll(
      async () => (await api(m, "GET", `/api/threads/${threadID}`)).run_status,
      {
        timeout: 15_000,
      },
    )
    .toBe(status);
}

// thread opens a thread one after the other, so the last activity runs in the order called.
async function thread(
  name: string,
  botID: string,
  text: string,
  extra = {},
  status = "done",
) {
  const t = await api(m, "POST", `/api/bots/${botID}/threads`, {
    text,
    ...extra,
  });
  await waitStatus(t.id, status);
  await api(m, "PATCH", `/api/threads/${t.id}`, { title: name });
  ids[name] = t.id;
  return t;
}

test.beforeAll(async () => {
  test.setTimeout(180_000);
  m = await startAtoll();
  const path = join(m.dir, "seeded");
  mkdirSync(path);
  writeFileSync(join(path, "answer.txt"), "41\n");
  const git = (...args: string[]) =>
    execFileSync("git", args, {
      cwd: path,
      env: {
        ...process.env,
        GIT_AUTHOR_NAME: "Fixture",
        GIT_AUTHOR_EMAIL: "fixture@example.test",
        GIT_COMMITTER_NAME: "Fixture",
        GIT_COMMITTER_EMAIL: "fixture@example.test",
        GIT_CONFIG_NOSYSTEM: "1",
      },
    });
  git("init", "-b", "main");
  git("add", "answer.txt");
  git("commit", "-m", "seed bug");
  const repo = await api(m, "POST", "/api/repos", { name: "seeded", path });

  const atlas = await newBotAPI(m, "Atlas", "claude", {
    repo_access: [repo.id],
  });
  const beacon = await newBotAPI(m, "Beacon");
  const asker = await newBotAPI(m, "Asker");
  ids.Atlas = atlas.id;
  ids.Beacon = beacon.id;
  ids.Asker = asker.id;
  const track = (url: string) =>
    `Use MCP tools. [{"server":"atoll","tool":"track_mr","args":{"url":"${url}","title":"Tidy the logs"}}]`;

  // The oldest thread, and the only one that needs the user.
  await thread("Waiting notes", asker.id, prompts.write, {}, "waiting");
  await thread("Alpha notes", atlas.id, prompts.keep);
  await thread(
    "Bravo notes",
    atlas.id,
    "Edit the seeded answer but leave the work uncommitted.",
    {
      repo_id: repo.id,
    },
  );
  await thread(
    "Hotel notes",
    atlas.id,
    track("https://gitlab.example/fixtures/seeded/-/merge_requests/7"),
  );
  await thread(
    "India notes",
    atlas.id,
    track("https://github.com/acme/widgets/pull/161"),
  );
  await thread("Delta notes", beacon.id, prompts.keep);
  await thread("Atlassian migration", beacon.id, prompts.keep);
  const closed = await thread("Zulu closed", asker.id, prompts.keep);
  await api(m, "POST", `/api/threads/${closed.id}/close`);
  expect(
    (await api(m, "GET", `/api/threads/${ids["Bravo notes"]}`)).branch,
  ).toBe("fix/seeded-bug");
});
test.afterAll(() => m?.stop());

const composer = (page: Page, bot: string) =>
  page.getByRole("textbox", { name: new RegExp(`^Message ${bot}`) });
const threadURL = (bot: string, name: string) =>
  `${m.url}/bots/${ids[bot]}/threads/${ids[name]}`;
const switcher = (page: Page) =>
  page.getByRole("dialog", { name: "Go to", exact: true });
const options = (page: Page) => switcher(page).getByRole("option");

// visit opens a thread by its address, once its messages are on screen.
async function visit(page: Page, bot: string, name: string) {
  await page.goto(threadURL(bot, name));
  await expect(
    page.getByRole("log", { name: "Messages" }).getByRole("article").first(),
  ).toBeVisible();
}

async function openSwitcher(page: Page) {
  await page.keyboard.press("ControlOrMeta+k");
  await expect(switcher(page)).toBeVisible();
  return switcher(page).getByRole("combobox", { name: "Go to", exact: true });
}

// This test is first: opening a thread marks it seen, which changes what the tree shows.
test("sidebar search also matches thread titles, and shows the matching threads of a Bot", async ({
  page,
}) => {
  await page.goto(m.url);
  const nav = page.getByRole("navigation", { name: "Bots" });
  const search = nav.getByRole("textbox", { name: "Search" });
  const tree = threadTree(page, "Atlas");
  const more = tree.getByRole("button", { name: /^\d+ more$/ });
  await expect(more).toBeVisible();
  const usual = await tree.getByRole("listitem").count();
  let hidden = "";
  for (const title of ["Alpha", "Bravo", "Hotel", "India"]) {
    if (
      (await tree.getByRole("link", { name: `${title} notes` }).count()) === 0
    )
      hidden = title;
  }
  expect(hidden, "the tree shows at most 3 of the 4 threads of Atlas").not.toBe(
    "",
  );

  await search.fill(hidden.toLowerCase());
  await expect(sidebarRow(page, "Atlas")).toBeVisible();
  for (const bot of ["Beacon", "Asker", "Chief"])
    await expect(sidebarRow(page, bot)).toHaveCount(0);
  await expect(tree.getByRole("listitem")).toHaveCount(1);
  await expect(
    tree.getByRole("link", { name: `${hidden} notes` }),
  ).toBeVisible();
  await expect(more).toHaveCount(0);

  await search.fill("migration");
  await expect(sidebarRow(page, "Atlas")).toHaveCount(0);
  await expect(sidebarRow(page, "Beacon")).toBeVisible();
  const beacon = threadTree(page, "Beacon");
  await expect(beacon.getByRole("listitem")).toHaveCount(1);
  await expect(beacon).toContainText("Atlassian migration");

  await search.fill("atlas");
  await expect(sidebarRow(page, "Atlas")).toBeVisible();
  await expect(tree.getByRole("listitem")).toHaveCount(usual);
  await expect(more).toBeVisible();
  await expect(sidebarRow(page, "Beacon")).toBeVisible();
  await expect(beacon.getByRole("listitem")).toHaveCount(1);
  await expect(beacon).toContainText("Atlassian migration");
  await expect(sidebarRow(page, "Asker")).toHaveCount(0);
});

test("opening a thread, or the New thread screen, focuses its composer", async ({
  page,
}) => {
  await page.goto(m.url);
  await sidebarRow(page, "Atlas").click();
  await expect(page).toHaveURL(new RegExp(`/bots/${ids.Atlas}/threads/`));
  await expect(composer(page, "Atlas")).toBeFocused();
  await page.keyboard.type("hello");
  await expect(composer(page, "Atlas")).toHaveValue("hello");

  await page.goto(threadURL("Beacon", "Delta notes"));
  await expect(composer(page, "Beacon")).toBeFocused();

  await threadTree(page, "Atlas").getByRole("link").first().click();
  await expect(composer(page, "Atlas")).toBeFocused();

  await page.keyboard.press("Meta+n");
  await expect(
    page.getByRole("heading", { name: "What should Atlas work on?" }),
  ).toBeVisible();
  await expect(composer(page, "Atlas")).toBeFocused();
  await page.keyboard.type("a new thread");
  await expect(composer(page, "Atlas")).toHaveValue("a new thread");
});

test("a printable key with nothing focused types into the composer; other fields and modifier keys are left alone", async ({
  page,
}) => {
  await visit(page, "Atlas", "Alpha notes");
  const box = composer(page, "Atlas");
  await expect(box).toBeFocused();
  const blur = async () => {
    await page.evaluate(() =>
      (document.activeElement as HTMLElement | null)?.blur(),
    );
    expect(
      await page.evaluate(() => document.activeElement === document.body),
    ).toBe(true);
  };

  await blur();
  await page.keyboard.type("hi");
  await expect(box).toHaveValue("hi");
  await expect(box).toBeFocused();

  await blur();
  await page.keyboard.press("Control+j");
  await page.keyboard.press("Alt+x");
  await expect(box).toHaveValue("hi");

  await page
    .getByRole("navigation", { name: "Bots" })
    .getByRole("textbox", { name: "Search" })
    .fill("zz");
  await expect(box).toHaveValue("hi");
});

test("Ctrl+K opens one switcher over Bots and threads; with no query needs you comes first, then last activity, then Bots", async ({
  page,
}) => {
  await visit(page, "Atlas", "Alpha notes");
  await expect(composer(page, "Atlas")).toBeFocused();
  const search = await openSwitcher(page);
  await expect(search).toBeFocused();

  const threads = [
    "Waiting notes",
    "Atlassian migration",
    "Delta notes",
    "India notes",
    "Hotel notes",
    "Bravo notes",
    "Alpha notes",
  ];
  const all = options(page);
  await expect(all).toHaveCount(threads.length + 4);
  await expect(all.nth(0)).toHaveAttribute("aria-selected", "true");
  for (const [i, title] of threads.entries()) {
    await expect(all.nth(i)).toHaveAttribute("data-kind", "thread");
    await expect(all.nth(i)).toContainText(title);
  }
  for (const i of [7, 8, 9, 10])
    await expect(all.nth(i)).toHaveAttribute("data-kind", "bot");
  await expect(all.nth(7)).toContainText("Chief");
  await expect(all.filter({ hasText: "Zulu closed" })).toHaveCount(0);

  const row = (title: string) => all.filter({ hasText: title });
  await expect(row("Bravo notes")).toContainText("Atlas");
  await expect(row("Bravo notes")).toContainText("fix/seeded-bug");
  await expect(row("Hotel notes")).toContainText("!7");
  await expect(row("India notes")).toContainText("#161");
  await expect(row("Delta notes")).toContainText("Beacon");
});

test("the switcher finds a thread by title, branch and PR number, ignoring case", async ({
  page,
}) => {
  await visit(page, "Atlas", "Alpha notes");
  const search = await openSwitcher(page);
  for (const [query, title] of [
    ["ALPHA", "Alpha notes"],
    ["seeded-bug", "Bravo notes"],
    ["!7", "Hotel notes"],
    ["#161", "India notes"],
    ["161", "India notes"],
    ["atlas bravo", "Bravo notes"],
  ]) {
    await search.fill(query);
    await expect(options(page)).toHaveCount(1);
    await expect(options(page).first()).toContainText(title);
    await expect(options(page).first()).toHaveAttribute(
      "aria-selected",
      "true",
    );
  }
  await search.fill("zulu");
  await expect(options(page)).toHaveCount(0);
  await expect(switcher(page)).toContainText("No threads or Bots match");
});

test("with a query the Bot comes first, then title matches, then the Bot's own threads; needs you leads inside a group", async ({
  page,
}) => {
  await visit(page, "Atlas", "Alpha notes");
  const search = await openSwitcher(page);

  await search.fill("atlas");
  const all = options(page);
  await expect(all).toHaveCount(6);
  await expect(all.nth(0)).toHaveAttribute("data-kind", "bot");
  await expect(all.nth(0)).toContainText("Atlas");
  for (const [i, title] of [
    "Atlassian migration",
    "India notes",
    "Hotel notes",
    "Bravo notes",
    "Alpha notes",
  ].entries()) {
    await expect(all.nth(i + 1)).toHaveAttribute("data-kind", "thread");
    await expect(all.nth(i + 1)).toContainText(title);
  }

  await search.fill("notes");
  await expect(all).toHaveCount(6);
  await expect(all).toContainText([
    "Waiting notes",
    "Delta notes",
    "India notes",
    "Hotel notes",
    "Bravo notes",
    "Alpha notes",
  ]);
});

test("arrow keys move the selection, Enter opens a thread and focuses its composer, Esc closes with the draft kept", async ({
  page,
}) => {
  await visit(page, "Atlas", "Alpha notes");
  await page.keyboard.type("draft");
  const search = await openSwitcher(page);
  await search.fill("notes");
  await expect(search).toBeFocused();
  await expect(composer(page, "Atlas")).toHaveValue("draft");
  const all = options(page);
  await expect(all.nth(0)).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("ArrowDown");
  await expect(all.nth(1)).toHaveAttribute("aria-selected", "true");
  await expect(all.nth(0)).toHaveAttribute("aria-selected", "false");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowUp");
  await expect(all.nth(1)).toHaveAttribute("aria-selected", "true");

  await page.keyboard.press("Escape");
  await expect(switcher(page)).toHaveCount(0);
  await expect(page).toHaveURL(threadURL("Atlas", "Alpha notes"));
  await expect(composer(page, "Atlas")).toBeFocused();
  await expect(composer(page, "Atlas")).toHaveValue("draft");

  await openSwitcher(page);
  await page.keyboard.type("delta");
  await expect(options(page)).toHaveCount(1);
  await page.keyboard.press("Enter");
  await expect(switcher(page)).toHaveCount(0);
  await expect(page).toHaveURL(threadURL("Beacon", "Delta notes"));
  await expect(composer(page, "Beacon")).toBeFocused();
  await page.keyboard.type("hello");
  await expect(composer(page, "Beacon")).toHaveValue("hello");
});

test("Enter on a Bot opens the Bot, as its sidebar row does; the switcher opens from a page with no composer", async ({
  page,
}) => {
  await page.goto(`${m.url}/settings`);
  const search = await openSwitcher(page);
  await search.fill("beacon");
  await expect(options(page).first()).toHaveAttribute("data-kind", "bot");
  await expect(options(page).first()).toContainText("Beacon");
  await page.keyboard.press("Enter");
  await expect(switcher(page)).toHaveCount(0);
  await expect(page).toHaveURL(new RegExp(`/bots/${ids.Beacon}/threads/`));
  await expect(composer(page, "Beacon")).toBeFocused();
});
