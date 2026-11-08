import { expect, test } from "@playwright/test";
import { pickModel } from "./atoll";

test("a memory save shows its source, and Retire takes it out of the thread", async ({
  page,
}) => {
  const name = `Memory Ada ${Date.now()}`;
  await page.goto("/");
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menuitem", { name: "New Bot" }).click();
  await page.getByRole("textbox", { name: "Name" }).fill(name);
  await page
    .getByRole("textbox", { name: "Persona" })
    .fill("A careful Go engineer.");
  await pickModel(page, page, "Claude Code");
  await page.getByRole("button", { name: "Create Bot" }).click();
  await expect(
    page.getByRole("heading", { name: `What should ${name} work on?` }),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: "Message" })
    .fill("Save the SQLite journal decision in user memory.");
  await page.getByRole("button", { name: "Send" }).click();

  const saved = page.getByRole("region", { name: "Saved memory" });
  const line = saved.getByRole("button", {
    name: /Saved a memory: Use SQLite/,
  });
  await expect(line).toHaveAttribute("aria-expanded", "false");
  await line.click();
  await expect(line).toHaveAttribute("aria-expanded", "true");
  await expect(
    saved.getByText("Use SQLite WAL for the journal.", { exact: true }),
  ).toBeVisible();
  await expect(
    saved.getByText("Readers must not block journal writes.", { exact: true }),
  ).toBeVisible();
  await expect(saved.getByText(name, { exact: true })).toBeVisible();
  await expect(saved.getByText("decision", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("log", { name: "Messages" }).getByText("Saved SQLite decision.", { exact: true }),
  ).toBeVisible();

  const before = await page.request.get("/api/memory");
  expect(before.status()).toBe(200);
  const item = (await before.json()).find(
    (entry: { body: string; source_bot_id: string }) =>
      entry.body === "Use SQLite WAL for the journal.",
  );
  expect(item?.status).toBe("accepted");
  await saved.getByRole("button", { name: "Retire", exact: true }).click();
  await expect
    .poll(async () => {
      const response = await page.request.get("/api/memory");
      expect(response.status()).toBe(200);
      return (await response.json()).find(
        (entry: { id: string }) => entry.id === item.id,
      )?.status;
    })
    .toBe("retired");
  await expect(
    page.getByRole("log", { name: "Messages" }).getByText(item.body, { exact: true }),
  ).toHaveCount(0);
});

test("a reply that saved 2 memories folds into one line; opening it shows Details and Retire per memory", async ({
  page,
}) => {
  const name = `Memory Pair ${Date.now()}`;
  await page.goto("/");
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menuitem", { name: "New Bot" }).click();
  await page.getByRole("textbox", { name: "Name" }).fill(name);
  await page
    .getByRole("textbox", { name: "Persona" })
    .fill("A careful Go engineer.");
  await pickModel(page, page, "Claude Code");
  await page.getByRole("button", { name: "Create Bot" }).click();
  await expect(
    page.getByRole("heading", { name: `What should ${name} work on?` }),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: "Message" })
    .fill("Save the journal finding and decision in user memory.");
  await page.getByRole("button", { name: "Send" }).click();
  const log = page.getByRole("log", { name: "Messages" });
  await expect(
    log.getByText("Saved a finding and a decision.", { exact: true }),
  ).toBeVisible();

  const saved = page.getByRole("region", { name: "Saved memory" });
  const line = saved.getByRole("button", {
    name: /Saved 2 memories: a finding, a decision/,
  });
  await expect(line).toHaveAttribute("aria-expanded", "false");
  await expect(
    saved.getByRole("button", { name: "Details", exact: true }),
  ).toHaveCount(0);
  await expect(
    saved.getByRole("button", { name: "Retire", exact: true }),
  ).toHaveCount(0);

  await line.click();
  await expect(line).toHaveAttribute("aria-expanded", "true");
  const bodies = [
    "The journal fsync takes 40 ms on CI.",
    "Batch journal writes every 50 ms.",
  ];
  for (const body of bodies) {
    await expect(saved.getByText(body, { exact: true })).toBeVisible();
  }
  await expect(
    saved.getByRole("button", { name: "Details", exact: true }),
  ).toHaveCount(2);
  await expect(
    saved.getByRole("button", { name: "Retire", exact: true }),
  ).toHaveCount(2);

  const before = await page.request.get("/api/memory");
  expect(before.status()).toBe(200);
  const items = (await before.json()).filter((entry: { body: string }) =>
    bodies.includes(entry.body),
  );
  expect(items.map((item: { status: string }) => item.status)).toEqual([
    "accepted",
    "accepted",
  ]);
  await saved
    .getByRole("button", { name: "Retire", exact: true })
    .first()
    .click();
  await expect
    .poll(async () => {
      const response = await page.request.get("/api/memory");
      expect(response.status()).toBe(200);
      const all: { id: string; status: string }[] = await response.json();
      return items
        .map(
          (item: { id: string }) =>
            all.find((entry) => entry.id === item.id)?.status,
        )
        .sort();
    })
    .toEqual(["accepted", "retired"]);
  const after: { body: string; status: string }[] = await (
    await page.request.get("/api/memory")
  ).json();
  const retired = after.find(
    (e) => bodies.includes(e.body) && e.status === "retired",
  )!;
  const kept = after.find(
    (e) => bodies.includes(e.body) && e.status === "accepted",
  )!;
  await expect(log.getByText(retired.body, { exact: true })).toHaveCount(0);
  await expect(saved.getByText(kept.body, { exact: true })).toBeVisible();
  await expect(
    saved.getByRole("button", { name: "Retire", exact: true }),
  ).toHaveCount(1);
});
