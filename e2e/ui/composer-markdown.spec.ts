// Your own message renders a short Markdown set (lists, task lists, code), while headings, rules,
// indented code, tables and images stay as typed. In the composer, Shift+Enter continues a list and
// Enter adds lines inside an open ``` block. Enter on a normal line sends (composer.spec.ts); paste
// to attach (images.spec.ts, composer.spec.ts) and replying to an approval (approvals.spec.ts) are
// covered where they live.
import { expect, type Locator, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  sendMessageAPI,
  startAtoll,
} from "./atoll";

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

const box = (page: Page) => page.getByRole("textbox", { name: /^Message/ });
const fence = "```";

// sentBubble opens a new thread whose first message is text and returns the user's bubble.
async function sentBubble(page: Page, name: string, text: string) {
  const bot = await newBotAPI(m, name);
  const opened = await openThreadAPI(m, bot.id, text);
  const id: string = opened.thread?.id ?? opened.id;
  await page.goto(`${m.url}/bots/${bot.id}/threads/${id}`);
  const bubble = page
    .getByRole("log", { name: "Messages" })
    .getByRole("article", { name: "You" });
  await expect(bubble).toHaveCount(1);
  return { bubble, id };
}

// openComposer opens a thread that already has a run, so the composer is the thread's own.
async function openComposer(page: Page, name: string) {
  const bot = await newBotAPI(m, name);
  const thread = await api(m, "POST", `/api/bots/${bot.id}/threads`, {});
  await sendMessageAPI(m, thread.id, prompts.pong);
  await page.goto(`${m.url}/bots/${bot.id}/threads/${thread.id}`);
  const log = page.getByRole("log", { name: "Messages" });
  await expect(log.getByText("pong", { exact: true })).toBeVisible();
  const posts: string[] = [];
  page.on("request", (r) => {
    if (
      r.method() === "POST" &&
      r.url().endsWith(`/api/threads/${thread.id}/messages`)
    )
      posts.push(r.postDataJSON().text);
  });
  return { log, posts };
}

// contrast is the WCAG contrast ratio of el's text colour over the backgrounds behind it,
// composited from the page down, so tints and any colour syntax count.
function contrast(el: Locator) {
  return el.evaluate((node) => {
    const layers: string[] = [];
    for (let n: Element | null = node; n; n = n.parentElement)
      layers.unshift(getComputedStyle(n).backgroundColor);
    const ctx = document
      .createElement("canvas")
      .getContext("2d", { willReadFrequently: true })!;
    const paint = (colours: string[]) => {
      ctx.fillStyle = "#fff";
      ctx.fillRect(0, 0, 1, 1);
      for (const c of colours) {
        ctx.fillStyle = c;
        ctx.fillRect(0, 0, 1, 1);
      }
      return [...ctx.getImageData(0, 0, 1, 1).data.slice(0, 3)];
    };
    const lum = (rgb: number[]) => {
      const [r, g, b] = rgb.map((v) => {
        const s = v / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * r + 0.7152 * g + 0.0722 * b;
    };
    const bg = lum(paint(layers));
    const fg = lum(paint([...layers, getComputedStyle(node).color]));
    return (Math.max(bg, fg) + 0.05) / (Math.min(bg, fg) + 0.05);
  });
}

test("a sent message shows its numbered list, bullet list, task list and code box", async ({
  page,
}) => {
  const { bubble } = await sentBubble(
    page,
    "Lister",
    [
      "1. a",
      "2. b",
      "",
      "- x",
      "",
      "- [ ] y",
      "",
      `${fence}`,
      "go test ./...",
      fence,
    ].join("\n"),
  );
  await expect(bubble.locator("ol > li")).toHaveText(["a", "b"]);
  await expect(bubble.locator("ul > li").filter({ hasText: "x" })).toHaveCount(
    1,
  );
  await expect(
    bubble
      .locator("li")
      .filter({ hasText: "y" })
      .locator('input[type="checkbox"]'),
  ).toHaveCount(1);
  await expect(bubble.locator("pre")).toContainText("go test ./...");
  await expect(bubble).not.toContainText(fence);
});

test("headings, rules, indented code and tables stay as typed in a sent message", async ({
  page,
}) => {
  const { bubble } = await sentBubble(
    page,
    "Plain",
    [
      "# build failed",
      "",
      "Setext title",
      "---",
      "",
      "    indented line",
      "",
      "| a | b |",
      "| - | - |",
      "| 1 | 2 |",
      "",
      "**loud**",
    ].join("\n"),
  );
  await expect(bubble.locator("strong")).toHaveText("loud");
  await expect(
    bubble.locator("h1, h2, h3, h4, h5, h6, hr, table, pre"),
  ).toHaveCount(0);
  await expect(bubble).toContainText("# build failed");
  await expect(bubble).toContainText("---");
  await expect(bubble).toContainText("indented line");
  await expect(bubble).toContainText("| a | b |");
});

test("an image in a sent message is not shown or fetched", async ({ page }) => {
  const files: string[] = [];
  page.on("request", (r) => {
    if (/\/api\/threads\/[^/]+\/file/.test(r.url())) files.push(r.url());
  });
  const { bubble } = await sentBubble(page, "Painter", "![x](a.png)\n\n- seen");
  await expect(bubble.locator("ul > li")).toHaveText(["seen"]);
  await expect(bubble.locator("img")).toHaveCount(0);
  expect(files).toEqual([]);
});

test("inline code and the code box are readable in a sent message", async ({
  page,
}) => {
  const { bubble } = await sentBubble(
    page,
    "Reader",
    ["Run `make check` first.", "", fence, "make e2e", fence].join("\n"),
  );
  const inline = bubble.locator("code").filter({ hasText: "make check" });
  await expect(inline).toHaveCount(1);
  const block = bubble.locator("pre");
  await expect(block).toContainText("make e2e");
  expect(await contrast(inline)).toBeGreaterThanOrEqual(4.5);
  expect(await contrast(block)).toBeGreaterThanOrEqual(4.5);
});

test("a queued message shows its list and code box the same way", async ({
  page,
}) => {
  const bot = await newBotAPI(m, "Queue");
  const opened = await openThreadAPI(m, bot.id, prompts.hang);
  const id: string = opened.thread?.id ?? opened.id;
  const runID: string = opened.run?.id ?? opened.run_id;
  try {
    await page.goto(`${m.url}/bots/${bot.id}/threads/${id}`);
    const log = page.getByRole("log", { name: "Messages" });
    await expect(log.getByRole("article", { name: "You" })).toHaveText(
      prompts.hang,
    );
    await box(page).fill(
      ["1. one", "2. two", "", "- dot", "", fence, "ls -la", fence].join("\n"),
    );
    await page.getByRole("button", { name: "Send", exact: true }).click();

    const queued = log
      .getByRole("article", { name: "You" })
      .filter({ hasText: "Queued" });
    await expect(queued).toHaveCount(1);
    await expect(queued.locator("ol > li")).toHaveText(["one", "two"]);
    await expect(queued.locator("ul > li")).toHaveText(["dot"]);
    await expect(queued.locator("pre")).toContainText("ls -la");
  } finally {
    await api(m, "POST", `/api/runs/${runID}/stop`);
  }
});

test("Shift+Enter continues a numbered list and ends it on an empty item", async ({
  page,
}) => {
  await openComposer(page, "Counter");
  await box(page).pressSequentially("1. first");
  await box(page).press("Shift+Enter");
  await expect(box(page)).toHaveValue("1. first\n2. ");
  await box(page).press("Shift+Enter");
  await expect(box(page)).toHaveValue("1. first\n");
  await box(page).pressSequentially("after");
  await expect(box(page)).toHaveValue("1. first\nafter");
});

test("Shift+Enter continues every list marker", async ({ page }) => {
  await openComposer(page, "Markers");
  for (const [typed, next] of [
    ["- a", "- "],
    ["* a", "* "],
    ["+ a", "+ "],
    ["1) a", "2) "],
    ["- [ ] a", "- [ ] "],
    ["- [x] done", "- [ ] "],
  ]) {
    await box(page).fill("");
    await box(page).pressSequentially(typed);
    await box(page).press("Shift+Enter");
    await expect(box(page), typed).toHaveValue(`${typed}\n${next}`);
    await box(page).press("Shift+Enter");
    await expect(box(page), typed).toHaveValue(`${typed}\n`);
  }
});

test("Shift+Enter keeps a nested item’s indent", async ({ page }) => {
  await openComposer(page, "Nester");
  await box(page).pressSequentially("  - nested");
  await box(page).press("Shift+Enter");
  await expect(box(page)).toHaveValue("  - nested\n  - ");
});

test("Shift+Enter mid-line moves the rest of the line into the new item", async ({
  page,
}) => {
  await openComposer(page, "Splitter");
  await box(page).pressSequentially("1. ab");
  await box(page).press("ArrowLeft");
  await box(page).press("Shift+Enter");
  await expect(box(page)).toHaveValue("1. a\n2. b");
});

test("one undo removes only the auto marker", async ({ page }) => {
  await openComposer(page, "Undoer");
  await box(page).pressSequentially("1. first");
  await box(page).press("Shift+Enter");
  await expect(box(page)).toHaveValue("1. first\n2. ");
  await box(page).press("ControlOrMeta+z");
  await expect(box(page)).toHaveValue(/^1\. first\n?$/);
});

test("Enter adds lines inside a code block and sends once it is closed", async ({
  page,
}) => {
  const { log, posts } = await openComposer(page, "Fencer");
  await box(page).pressSequentially(fence);
  await box(page).press("Enter");
  await box(page).pressSequentially("a");
  await box(page).press("Enter");
  await box(page).pressSequentially("b");
  await box(page).press("Shift+Enter");
  await box(page).pressSequentially("c");
  await box(page).press("Enter");
  await expect(box(page)).toHaveValue(`${fence}\na\nb\nc\n`);
  await box(page).pressSequentially(fence);
  expect(posts).toEqual([]);

  await box(page).press("Enter");
  await expect.poll(() => posts).toEqual([`${fence}\na\nb\nc\n${fence}`]);
  await expect(box(page)).toHaveValue("");
  await expect(
    log.getByRole("article", { name: "You" }).last().locator("pre"),
  ).toContainText("c");
});

test("the Send button sends a block that is never closed", async ({ page }) => {
  const { posts } = await openComposer(page, "Paster");
  await box(page).fill(`${fence}\npanic: boom`);
  await box(page).press("End");
  await box(page).press("Enter");
  await expect(box(page)).toHaveValue(`${fence}\npanic: boom\n`);
  expect(posts).toEqual([]);

  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect.poll(() => posts.length).toBe(1);
  expect(posts[0].trim()).toBe(`${fence}\npanic: boom`);
});

test("Enter on a list line sends", async ({ page }) => {
  const { posts } = await openComposer(page, "Sender");
  await box(page).pressSequentially("- one");
  await box(page).press("Enter");
  await expect.poll(() => posts).toEqual(["- one"]);
});
