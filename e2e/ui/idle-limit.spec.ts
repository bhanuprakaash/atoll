// A run with no stream event for ATOLL_IDLE_LIMIT is stopped and stored failed, error_kind stuck.
// Its chat line reads "Stopped: no progress for <limit>." with the limit's real value, here 2 s.
// In a person's thread the line's action shows on the person's newest run; a helper thread's line
// has no action, since its boss decides from its wake. Design: docs/design.md, Threads and runs
// (Idle limit) and App and UI (chat lines).
import { expect, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  prompts,
  startAtoll,
  waitStatus,
} from "./atoll";
import {
  delegate,
  helperThreads,
  makeBoss,
  startThread,
} from "./helpers-fixture";

let m: Machine;
test.beforeAll(async () => {
  m = await startAtoll({ env: { ATOLL_IDLE_LIMIT: "2s" } });
});
test.afterAll(() => m?.stop());

const line = "Stopped: no progress for 2 seconds.";
// The action of a failed run's line: Continue once that ticket lands, Retry before it.
const action = /^(Continue|Retry)$/;

async function stuckRun(threadID: string) {
  await waitStatus(m, threadID, "failed", 20_000);
  const { run_id } = await api(m, "GET", `/api/threads/${threadID}`);
  return api(m, "GET", `/api/runs/${run_id}`);
}

const messages = (page: Page) => page.getByRole("log", { name: "Messages" });

test("a stuck run in a person's thread reads Stopped: no progress, with its action", async ({
  page,
}) => {
  await newBotAPI(m, "Wren");
  await page.goto(m.url);
  const thread = await startThread(page, "Wren", prompts.hang);

  const run = await stuckRun(thread);
  expect(run.error_kind, "stored as stuck").toBe("stuck");

  const log = messages(page);
  await expect(log.getByText(line, { exact: true })).toBeVisible({
    timeout: 10_000,
  });
  await expect(log.getByRole("button", { name: action })).toHaveCount(1);

  await page.reload();
  await expect(log.getByText(line, { exact: true })).toBeVisible();
  await expect(log.getByRole("button", { name: action })).toHaveCount(1);
});

test("a stuck helper's thread reads the same line, with no action", async ({
  page,
}) => {
  const { bots } = await makeBoss(m, ["Tester"]);
  await page.goto(m.url);
  const boss = await startThread(
    page,
    "Poteto",
    delegate([["Tester", prompts.hang]]),
  );
  const [helper] = await helperThreads(m, boss, 1);

  const run = await stuckRun(helper.id);
  expect(run.error_kind, "stored as stuck").toBe("stuck");

  await page.goto(`${m.url}/bots/${bots.Tester.id}/threads/${helper.id}`);
  const log = messages(page);
  await expect(log.getByText(line, { exact: true })).toBeVisible({
    timeout: 10_000,
  });
  await expect(log.getByRole("button", { name: action })).toHaveCount(0);
});
