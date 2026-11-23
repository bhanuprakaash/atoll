// A helper whose run failed does not wait on the person by itself: its thread is neither listed
// under Now's Needs you nor counted on the Now button. The boss's report carries the failure.
import { expect, type Page, test } from "@playwright/test";
import { api, type Machine, openNow, startAtoll, waitStatus } from "./atoll";
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
  const { boss, thread } = await bossHandsOff(m, page, [
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
  return { boss, helper };
}

test("a failed helper thread is not listed under Needs you on Now and the Now button does not count it", async ({
  page,
}) => {
  const { boss, helper } = await onlyHelperFailed(page);

  await expect(nowButton(page)).toHaveAccessibleName(/^Now\D*$/);
  const now = await openNow(page);
  await expect(
    now.getByRole("list", { name: "Needs you", exact: true }),
  ).toHaveCount(0);
  await expect(now.locator(`a[href$="/threads/${helper}"]`)).toHaveCount(0);
  await expect(now.locator(`a[href$="/threads/${boss}"]`)).toHaveCount(0);
});
