// Issue #221: Settings > General shows the version, checks GitHub Releases for a newer Reef and
// installs it. The Tauri shell is not running here, so each test stands in for it with a scripted
// window.__TAURI_INTERNALS__.invoke (see app-update-contract.md).
import { expect, type Page, test } from "@playwright/test";
import { type Machine, startAtoll } from "./atoll";

type Answer = { ok: unknown } | { error: string; afterMs?: number } | "never";
type Shell = { check_update: Answer[]; install_update: Answer[] };

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll();
});
test.afterAll(() => m?.stop());

// fakeShell answers each command from its list in order, repeating the last answer, and records
// every call in window.__shellCalls.
async function fakeShell(page: Page, shell: Shell) {
  await page.addInitScript((shell: Shell) => {
    const w = window as unknown as Record<string, unknown>;
    const calls: string[] = [];
    w.__shellCalls = calls;
    w.__TAURI_INTERNALS__ = {
      invoke: (cmd: string) => {
        calls.push(cmd);
        const list = shell[cmd as keyof Shell];
        if (!list) return Promise.reject(`unknown command ${cmd}`);
        const answer = list.length > 1 ? list.shift()! : list[0];
        if (answer === "never") return new Promise(() => {});
        if ("ok" in answer) return Promise.resolve(answer.ok);
        return new Promise((_, fail) =>
          setTimeout(() => fail(answer.error), answer.afterMs ?? 0),
        );
      },
    };
  }, shell);
}

const shellCalls = (page: Page) =>
  page.evaluate(() => (window as any).__shellCalls as string[]);

async function openGeneral(page: Page) {
  await page
    .getByRole("navigation", { name: "Bots" })
    .getByRole("link", { name: /Settings/ })
    .click();
  await page.getByRole("link", { name: "General" }).click();
  return page.getByRole("group", { name: "Software update" });
}

test("shows the version and finds an update", async ({ page }) => {
  await fakeShell(page, {
    check_update: [{ ok: { current: "0.1.1", available: "0.1.2" } }],
    install_update: ["never"],
  });
  await page.goto(m.url);
  const settings = page
    .getByRole("navigation", { name: "Bots" })
    .getByRole("link", {
      name: /Settings/,
    });
  await expect(settings).toHaveAccessibleName(/Update available/);
  const group = await openGeneral(page);
  await expect(group).toContainText("Reef 0.1.1");
  await expect(group).toContainText("Reef 0.1.2 is available");
  await expect(
    group.getByRole("button", { name: "Update and restart" }),
  ).toBeVisible();
  await group.getByRole("button", { name: "Check for updates" }).click();
  await expect(group).toContainText("Reef 0.1.2 is available");
  await expect
    .poll(
      async () =>
        (await shellCalls(page)).filter((c) => c === "check_update").length,
    )
    .toBe(2);
});

test("says up to date", async ({ page }) => {
  await fakeShell(page, {
    check_update: [{ ok: { current: "0.1.2", available: null } }],
    install_update: ["never"],
  });
  await page.goto(m.url);
  await expect(
    page.getByRole("link", { name: /Settings/ }),
  ).not.toHaveAccessibleName(/Update available/);
  const group = await openGeneral(page);
  await expect(group).toContainText("Reef 0.1.2");
  await group.getByRole("button", { name: "Check for updates" }).click();
  await expect(group).toContainText("Up to date");
  await expect(
    group.getByRole("button", { name: "Update and restart" }),
  ).toHaveCount(0);
});

test("explains a failed check", async ({ page }) => {
  await fakeShell(page, {
    check_update: [
      { ok: { current: "0.1.1", available: null } },
      {
        error:
          "error sending request for url (https://github.com/…/latest.json)",
      },
    ],
    install_update: ["never"],
  });
  await page.goto(m.url);
  const group = await openGeneral(page);
  await expect(group).toContainText("Reef 0.1.1");
  await group.getByRole("button", { name: "Check for updates" }).click();
  await expect(group).toContainText("Couldn't check for updates. Try again.");
  const raw = group.getByText(/error sending request/);
  await expect(raw).toBeHidden();
  await group.getByText("Details").click();
  await expect(raw).toBeVisible();
});

test("update and restart installs", async ({ page }) => {
  await fakeShell(page, {
    check_update: [{ ok: { current: "0.1.1", available: "0.1.2" } }],
    install_update: [
      { error: "signature verification failed", afterMs: 1500 },
      "never",
    ],
  });
  await page.goto(m.url);
  const group = await openGeneral(page);
  await group.getByRole("button", { name: "Update and restart" }).click();
  await expect(group.getByRole("button", { name: "Updating…" })).toBeDisabled();
  await expect(group).toContainText("Couldn't update. Try again.");
  await expect(group.getByText(/signature verification failed/)).toBeHidden();
  const again = group.getByRole("button", { name: "Update and restart" });
  await expect(again).toBeEnabled();
  await again.click();
  await expect(group.getByRole("button", { name: "Updating…" })).toBeDisabled();
  expect(
    (await shellCalls(page)).filter((c) => c === "install_update"),
  ).toHaveLength(2);
});

test("plain browser has no updater", async ({ page }) => {
  await page.goto(m.url);
  await openGeneral(page);
  await expect(page.getByRole("textbox", { name: "Your name" })).toBeVisible();
  await expect(
    page.getByRole("group", { name: "Software update" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: /Settings/ }),
  ).not.toHaveAccessibleName(/Update available/);
});
