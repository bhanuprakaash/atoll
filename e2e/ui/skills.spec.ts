import { expect, test } from "@playwright/test";
import { openThreads } from "./atoll";

test("Chief creates a skill and Bot in chat, and Undo removes the Bot", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByRole("navigation", { name: "Bots" })
    .getByText("Chief", { exact: true })
    .click();
  const threads = await openThreads(page);
  await threads.getByRole("button", { name: "New thread" }).click();
  await page
    .getByRole("textbox", { name: "Message" })
    .fill("Create reef-greeting and a Lagoon Bot using claude.");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(
    page.getByRole("log", { name: "Messages" }).getByText("Created reef-greeting and Lagoon.", { exact: true }),
  ).toBeVisible();

  const change = page.getByRole("region", { name: "Platform change" }).filter({
    has: page.getByText("Lagoon", { exact: true }),
  });
  await expect(change.getByText("Chief", { exact: true })).toBeVisible();
  await expect(
    change.getByRole("button", { name: "Undo", exact: true }),
  ).toBeVisible();
  const response = await page.request.get("/api/bots");
  expect(response.status()).toBe(200);
  const bot = (await response.json()).find(
    (b: { name: string }) => b.name === "Lagoon",
  );
  expect(bot?.skills).toHaveLength(1);
  await change.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(change.getByText("Undone", { exact: true })).toBeVisible();
  await expect(
    change.getByRole("button", { name: "Undo", exact: true }),
  ).toHaveCount(0);
  await expect(change.getByText("Lagoon", { exact: true })).toBeVisible();
  await expect
    .poll(async () => {
      const bots = await page.request.get("/api/bots");
      expect(bots.status()).toBe(200);
      return (await bots.json()).some(
        (entry: { id: string }) => entry.id === bot.id,
      );
    })
    .toBe(false);
});
