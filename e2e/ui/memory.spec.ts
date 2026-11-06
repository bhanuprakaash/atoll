import { expect, test } from "@playwright/test";
import { pickModel } from "./atoll";

test("a memory save shows its source and Retire keeps it in history", async ({
  page,
}) => {
  const name = `Memory Ada ${Date.now()}`;
  await page.goto("/");
  await page.getByRole("button", { name: "New Bot" }).click();
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
  await expect(saved.getByText("Retired", { exact: true })).toBeVisible();
  await expect(
    saved.getByRole("button", { name: "Retire", exact: true }),
  ).toHaveCount(0);
  await expect
    .poll(async () => {
      const response = await page.request.get("/api/memory");
      expect(response.status()).toBe(200);
      return (await response.json()).find(
        (entry: { id: string }) => entry.id === item.id,
      )?.status;
    })
    .toBe("retired");
  await expect(saved.getByText(item.body, { exact: true })).toBeVisible();
});
