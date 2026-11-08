import { expect, test } from "@playwright/test";

test("Chief creates a skill and Bot in chat, and Undo removes the Bot", async ({
  page,
}) => {
  await page.goto("/");
  const nav = page.getByRole("navigation", { name: "Bots" });
  await nav.getByText("Chief", { exact: true }).click();
  await nav.getByText("Chief", { exact: true }).hover();
  await nav.getByRole("button", { name: "New thread with Chief" }).click();
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
  await expect(change.getByText("Chief", { exact: true })).toHaveCount(0);
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
