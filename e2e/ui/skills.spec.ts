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
    has: page.getByRole("button", { name: /Created .*Lagoon/ }),
  });
  const line = change.getByRole("button", { name: /Created .*Lagoon/ });
  await expect(line).toHaveAttribute("aria-expanded", "false");
  await line.click();
  await expect(line).toHaveAttribute("aria-expanded", "true");
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

test("a reply that created 2 Bots folds into one line: Undo all works folded, the line opens and folds, Blocked stays out", async ({
  page,
}) => {
  await page.goto("/");
  const nav = page.getByRole("navigation", { name: "Bots" });
  await nav.getByText("Chief", { exact: true }).click();
  await nav.getByText("Chief", { exact: true }).hover();
  await nav.getByRole("button", { name: "New thread with Chief" }).click();
  const create = (name: string) => ({
    tool: "create_bot",
    args: { name, persona: "A careful reviewer.", provider: "claude" },
  });
  await page
    .getByRole("textbox", { name: "Message" })
    .fill(
      `Call platform tools. ${JSON.stringify([create("Poteto Agent"), create("Comment Sicko")])}`,
    );
  await page.getByRole("button", { name: "Send" }).click();
  const log = page.getByRole("log", { name: "Messages" });
  await expect(
    log.getByText("Made 2 platform calls.", { exact: true }),
  ).toBeVisible();

  const summary = /Created 2 Bots: Poteto Agent, Comment Sicko/;
  const change = page.getByRole("region", { name: "Platform change" }).filter({
    has: page.getByRole("button", { name: summary }),
  });
  await expect(change).toHaveCount(1);
  const line = change.getByRole("button", { name: summary });
  await expect(line).toHaveAttribute("aria-expanded", "false");
  await expect(
    change.getByRole("button", { name: "Undo all", exact: true }),
  ).toBeVisible();
  await expect(
    change.getByRole("link", { name: "Open", exact: true }),
  ).toHaveCount(0);
  await expect(
    change.getByRole("button", { name: "Undo", exact: true }),
  ).toHaveCount(0);

  const blocked = log.getByText(
    "Blocked: Comment Sicko needs a GitLab connection to review MRs.",
  );
  await expect(blocked).toBeVisible();
  await expect(log.getByRole("button", { name: /Blocked:/ })).toHaveCount(0);

  await line.click();
  await expect(line).toHaveAttribute("aria-expanded", "true");
  await expect(
    change.getByRole("link", { name: "Open", exact: true }),
  ).toHaveCount(2);
  await expect(
    change.getByRole("button", { name: "Undo", exact: true }),
  ).toHaveCount(2);
  await expect(change.getByText("Poteto Agent", { exact: true })).toBeVisible();
  await expect(
    change.getByText("Comment Sicko", { exact: true }),
  ).toBeVisible();
  await line.click();
  await expect(line).toHaveAttribute("aria-expanded", "false");
  await expect(
    change.getByRole("link", { name: "Open", exact: true }),
  ).toHaveCount(0);
  await expect(
    change.getByRole("button", { name: "Undo", exact: true }),
  ).toHaveCount(0);
  await expect(blocked).toBeVisible();

  const response = await page.request.get("/api/bots");
  expect(response.status()).toBe(200);
  const ids = (await response.json())
    .filter((b: { name: string }) =>
      ["Poteto Agent", "Comment Sicko"].includes(b.name),
    )
    .map((b: { id: string }) => b.id);
  expect(ids).toHaveLength(2);
  await change.getByRole("button", { name: "Undo all", exact: true }).click();
  await expect(change.getByText("Undone", { exact: true })).toBeVisible();
  await expect(
    change.getByRole("button", { name: "Undo all", exact: true }),
  ).toHaveCount(0);
  await expect(line).toHaveAttribute("aria-expanded", "false");
  await expect
    .poll(async () => {
      const bots = await page.request.get("/api/bots");
      expect(bots.status()).toBe(200);
      return (await bots.json()).filter((b: { id: string }) =>
        ids.includes(b.id),
      ).length;
    })
    .toBe(0);
});
