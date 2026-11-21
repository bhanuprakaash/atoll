// A helper whose run failed does not wait on the person by itself: its thread is a row under
// Threads in its own Bot's pane with the red dot, and it is neither listed under Now's Needs you
// nor counted on the Now button. The boss's report carries the failure.
import { expect, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  marks,
  nowPane,
  showPane,
  startAtoll,
  waitStatus,
} from "./atoll";
import { bossHandsOff } from "./helpers-fixture";

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll();
});
test.afterEach(() => m?.stop());

const nowButton = (page: Page) =>
  page
    .getByRole("navigation", { name: "Bots" })
    .getByRole("button", { name: /^Now\b/ });

// The helper has no recording for its brief, so its run fails.
const failingBrief = "This brief has no recording, so the helper run fails.";

// onlyHelperFailed hands off to Tester, whose run fails, and leaves the page on Settings once the
// boss has answered the report: nothing else waits.
async function onlyHelperFailed(page: Page) {
  const { poteto, bots, boss, thread } = await bossHandsOff(m, page, [
    ["Tester", failingBrief],
  ]);
  const helper = thread("Tester");
  await waitStatus(m, helper, "failed");
  await expect
    .poll(async () =>
      (await api(m, "GET", `/api/threads/${boss}/messages`)).some(
        (s: { text: string }) => s.text === "Thanks, Tester.",
      ),
    )
    .toBe(true);
  await waitStatus(m, boss, "done");
  await page.goto(`${m.url}/settings`);
  await page.mouse.move(1270, 5);
  return { poteto, bots, boss, helper };
}

test("a failed helper thread is a row under Threads in its Bot pane with the red dot", async ({
  page,
}) => {
  const { bots, helper } = await onlyHelperFailed(page);

  const pane = await showPane(page, m, bots.Tester.id, "Tester");
  await page.mouse.move(1270, 5);
  const row = pane
    .getByRole("list", { name: "Threads", exact: true })
    .getByRole("listitem")
    .filter({ has: page.locator(`a[href$="/threads/${helper}"]`) });
  await expect(row).toHaveCount(1);
  await expect
    .poll(async () => (await marks(row)).map((d) => d.colour))
    .toEqual(["red"]);
  await expect(
    pane.getByRole("list", { name: "Needs you", exact: true }),
  ).toHaveCount(0);
});

test("a failed helper thread is not listed under Needs you on Now and the Now button does not count it", async ({
  page,
}) => {
  const { boss, helper } = await onlyHelperFailed(page);

  await expect(nowButton(page)).toHaveAccessibleName("Now");
  const now = await nowPane(page);
  await expect(
    now.getByRole("list", { name: "Needs you", exact: true }),
  ).toHaveCount(0);
  await expect(now.locator(`a[href$="/threads/${helper}"]`)).toHaveCount(0);
  await expect(now.locator(`a[href$="/threads/${boss}"]`)).toHaveCount(0);
});
