import { createServer } from "node:http";
import { expect, test } from "@playwright/test";
import { type Machine, startAtoll } from "./atoll";

test("a failed GitHub keychain save shows a message and offers sign in again", async ({
  page,
  context,
}) => {
  const github = createServer((req, res) => {
    res.setHeader("Content-Type", "application/json");
    if (req.url === "/login/device/code") {
      res.end(
        JSON.stringify({
          device_code: "fixture-device",
          user_code: "REEF-1234",
          verification_uri: "https://github.com/login/device",
          expires_in: 900,
          interval: 1,
        }),
      );
    } else if (req.url === "/login/oauth/access_token") {
      res.end(
        JSON.stringify({ access_token: "fixture-token", token_type: "bearer" }),
      );
    } else {
      res.writeHead(404).end("{}");
    }
  });
  await new Promise<void>((resolve) => github.listen(0, "127.0.0.1", resolve));
  const address = github.address();
  if (!address || typeof address === "string")
    throw new Error("GitHub fixture has no port");
  let m: Machine | undefined;
  try {
    m = await startAtoll({
      env: {
        ATOLL_GITHUB_URL: `http://127.0.0.1:${address.port}`,
        DBUS_SESSION_BUS_ADDRESS: "unix:path=/nonexistent/atoll-e2e",
      },
    });
    await context.route("https://github.com/**", (route) =>
      route.fulfill({ body: "signing in" }),
    );
    await page.goto(`${m.url}/settings`);
    await page.getByRole("link", { name: "Connections" }).click();
    await page.getByRole("button", { name: "Connect GitHub" }).click();
    await expect(page.getByText("REEF-1234")).toBeVisible();
    const error = page.locator(".text-danger");
    await expect(error).toBeVisible();
    expect
      .soft(await page.locator("body").innerText())
      .not.toMatch(/\{\s*"error"\s*:/);
    await expect
      .soft(error)
      .toContainText(
        "Reef couldn't save the GitHub sign-in to the system keychain:",
        { timeout: 2000 },
      );
    await expect
      .soft(page.getByText("Pending", { exact: true }))
      .toHaveCount(0, { timeout: 2000 });
    await expect(
      page.getByRole("button", { name: "Sign in again" }),
    ).toBeVisible({ timeout: 2000 });
  } finally {
    m?.stop();
    github.close();
  }
});

test("a failed GitLab keychain save shows a message and offers sign in again", async ({
  page,
}) => {
  const gitlab = createServer((req, res) => {
    res.setHeader("Content-Type", "application/json");
    if (req.url === "/oauth/authorize_device") {
      res.end(
        JSON.stringify({
          device_code: "fixture-device",
          user_code: "REEF-1234",
          verification_uri: "https://gitlab.example/oauth/device",
          expires_in: 900,
          interval: 1,
        }),
      );
    } else if (req.url === "/oauth/token") {
      res.end(
        JSON.stringify({
          access_token: "fixture-token",
          refresh_token: "fixture-refresh",
          token_type: "bearer",
          expires_in: 7200,
          created_at: Math.floor(Date.now() / 1000),
        }),
      );
    } else {
      res.writeHead(404).end("{}");
    }
  });
  await new Promise<void>((resolve) => gitlab.listen(0, "127.0.0.1", resolve));
  const address = gitlab.address();
  if (!address || typeof address === "string")
    throw new Error("GitLab fixture has no port");
  let m: Machine | undefined;
  try {
    m = await startAtoll({
      env: { DBUS_SESSION_BUS_ADDRESS: "unix:path=/nonexistent/atoll-e2e" },
    });
    await page.goto(`${m.url}/settings`);
    await page.getByRole("link", { name: "Connections" }).click();
    await page
      .getByRole("textbox", { name: "GitLab instance URL" })
      .fill(`http://127.0.0.1:${address.port}`);
    await page.getByRole("button", { name: "Sign in to GitLab" }).click();
    await expect(page.getByText("REEF-1234")).toBeVisible();
    const error = page.locator(".text-danger");
    await expect(error).toBeVisible();
    expect
      .soft(await page.locator("body").innerText())
      .not.toMatch(/\{\s*"error"\s*:/);
    await expect
      .soft(error)
      .toContainText(
        "Reef couldn't save the GitLab sign-in to the system keychain:",
        { timeout: 2000 },
      );
    await expect
      .soft(page.getByText("Pending", { exact: true }))
      .toHaveCount(0, { timeout: 2000 });
    await expect(
      page.getByRole("button", { name: "Sign in again" }),
    ).toBeVisible({ timeout: 2000 });
  } finally {
    m?.stop();
    gitlab.close();
  }
});

test("a failed MCP secret save shows a keychain message without raw JSON", async ({
  page,
}) => {
  const m = await startAtoll({
    env: { DBUS_SESSION_BUS_ADDRESS: "unix:path=/nonexistent/atoll-e2e" },
  });
  try {
    await page.goto(`${m.url}/settings`);
    await page.getByRole("link", { name: "MCP servers" }).click();
    await page.getByRole("button", { name: "Add local" }).click();
    const dialog = page.getByRole("dialog", { name: "Add local" });
    await dialog.getByRole("textbox", { name: "Name" }).fill("docs-local");
    await dialog.getByRole("textbox", { name: "Command" }).fill("echo");
    await dialog.getByRole("textbox", { name: "Env" }).fill("TOKEN=secret");
    await dialog.getByRole("button", { name: "Add", exact: true }).click();
    const error = dialog.locator(".text-danger");
    await expect(error).toBeVisible();
    expect
      .soft(await page.locator("body").innerText())
      .not.toMatch(/\{\s*"error"\s*:/);
    await expect(error).toContainText(/keychain/i, { timeout: 2000 });
  } finally {
    m.stop();
  }
});
