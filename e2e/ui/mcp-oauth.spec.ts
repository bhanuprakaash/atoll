// Settings > MCP servers: hosted servers connect with one click. Connect opens the service's Allow
// page in a popup; the page then shows Connected. Contract: e2e/mcp-oauth-contract.md. The remote
// server is a local fake (oauth-mcp-fixture.ts): no network, so Connect is never clicked on the
// catalog's Notion, Figma or Cloudflare. The Go tests prove tokens, runs and refresh.
import { expect, type Page, test } from "@playwright/test";
import {
  api,
  type Machine,
  newBotAPI,
  openThreadAPI,
  prompts,
  startAtoll,
} from "./atoll";
import {
  followAuthorize,
  type OAuthMCP,
  startOAuthMCP,
} from "./oauth-mcp-fixture";

let m: Machine;
const fakes: OAuthMCP[] = [];
test.beforeEach(async () => {
  m = await startAtoll({ secretStore: true });
});
test.afterEach(async () => {
  m?.stop();
  await Promise.all(fakes.splice(0).map((f) => f.close()));
});

const fake = async (config: Partial<OAuthMCP["config"]> = {}) => {
  const f = await startOAuthMCP();
  Object.assign(f.config, config);
  fakes.push(f);
  return f;
};
const servers = async (): Promise<
  { id: string; name: string; status?: string }[]
> => api(m, "GET", "/api/mcp-servers");

async function openPage(page: Page) {
  await page.goto(`${m.url}/settings`);
  await page.getByRole("link", { name: "MCP servers" }).click();
  await expect(page.getByRole("list", { name: "Catalog" })).toBeVisible();
}

const catalogItem = (page: Page, name: string) =>
  page
    .getByRole("list", { name: "Catalog" })
    .getByRole("listitem")
    .filter({ hasText: name });
const row = (page: Page, name: string) =>
  page
    .getByRole("list", { name: "Your servers" })
    .getByRole("listitem")
    .filter({ hasText: name });
const connect = (scope: ReturnType<typeof row>) =>
  scope.getByRole("button", { name: "Connect", exact: true });

async function addByURL(page: Page, name: string, url: string) {
  await page.getByRole("button", { name: "Add by URL" }).click();
  const dialog = page.getByRole("dialog", { name: "Add by URL" });
  await dialog.getByRole("textbox", { name: "Name" }).fill(name);
  await dialog.getByRole("textbox", { name: "URL" }).fill(url);
  await dialog.getByRole("button", { name: "Add", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(row(page, name)).toBeVisible();
}

test("the catalog lists Context7, Notion, Figma and Cloudflare; the hosted ones offer Connect", async ({
  page,
}) => {
  await openPage(page);
  const items = page
    .getByRole("list", { name: "Catalog" })
    .getByRole("listitem");
  await expect(items).toHaveCount(5);
  await expect(items.first()).toContainText("Context7");
  await expect(connect(catalogItem(page, "Context7"))).toHaveCount(0);
  for (const [name, url] of [
    ["Notion", "https://mcp.notion.com/mcp"],
    ["Figma", "https://mcp.figma.com/mcp"],
    ["Cloudflare", "https://observability.mcp.cloudflare.com/mcp"],
  ]) {
    const item = catalogItem(page, name);
    await expect(item).toContainText(url);
    await expect(connect(item)).toBeVisible();
    await expect(
      item.getByText("Not connected", { exact: true }),
    ).toBeVisible();
  }
});

test("Add by URL to a server that needs sign-in offers Connect; Allow in the popup shows Connected", async ({
  page,
}) => {
  const oauth = await fake();
  await openPage(page);
  await addByURL(page, "docs-oauth", oauth.url);
  const item = row(page, "docs-oauth");
  await expect(item.getByText("Not connected", { exact: true })).toBeVisible();
  expect((await servers()).find((s) => s.name === "docs-oauth")?.status).toBe(
    "not_connected",
  );

  const [popup] = await Promise.all([
    page.waitForEvent("popup"),
    connect(item).click(),
  ]);
  await popup.waitForURL(`${m.url}/oauth/callback?**`);
  await expect(item.getByText("Connected", { exact: true })).toBeVisible();
  await expect(connect(item)).toHaveCount(0);

  expect(oauth.registrations).toMatchObject([
    {
      redirect_uris: [`${m.url}/oauth/callback`],
      token_endpoint_auth_method: "none",
    },
  ]);
  expect(oauth.authorizes).toHaveLength(1);
  expect((await servers()).find((s) => s.name === "docs-oauth")?.status).toBe(
    "connected",
  );
  const { access, refresh } = oauth.tokens();
  const text = await page.locator("body").innerText();
  expect(text).not.toContain(access);
  expect(text).not.toContain(refresh);
  await page.reload();
  await expect(
    row(page, "docs-oauth").getByText("Connected", { exact: true }),
  ).toBeVisible();
});

test("a server that answers 200 is added without Connect; one with no registration says it needs a registered app", async ({
  page,
}) => {
  const open = await fake({ requireAuth: false });
  const noApp = await fake({ registration: false });
  await openPage(page);
  await addByURL(page, "docs-open", open.url);
  await addByURL(page, "docs-app", noApp.url);

  const plain = row(page, "docs-open");
  await expect(plain.getByRole("button", { name: "Remove" })).toBeVisible();
  await expect(connect(plain)).toHaveCount(0);
  await expect(plain.getByText(/connected|sign-in/i)).toHaveCount(0);

  const app = row(page, "docs-app");
  await expect(
    app.getByText(/this server needs a registered app/i),
  ).toBeVisible();
  await expect(connect(app)).toHaveCount(0);
  expect((await servers()).map((s) => [s.name, s.status ?? ""])).toEqual([
    ["Context7", ""],
    ["Notion", "not_connected"],
    ["Figma", "not_connected"],
    ["Cloudflare", "not_connected"],
    ["Slack", "needs_setup"],
    ["docs-open", ""],
    ["docs-app", "needs_app"],
  ]);
});

test("a rejected refresh shows Needs sign-in; Sign in again reconnects the same server", async ({
  page,
}) => {
  const oauth = await fake({ ttl: 300 });
  const added = await api(m, "POST", "/api/mcp-servers", {
    name: "docs-oauth",
    url: oauth.url,
  });
  const connected = await api(
    m,
    "POST",
    `/api/mcp-servers/${added.id}/connect`,
  );
  await followAuthorize(connected.authorize_url);
  const kelp = await newBotAPI(m, "Kelp", "claude", {
    mcp_servers: [added.id],
  });

  // The token expires within ten minutes, so the next run refreshes it first: GitLab-style, the
  // rejection is found by a run.
  oauth.config.reject = true;
  await openThreadAPI(m, kelp.id, prompts.keep);
  await expect
    .poll(
      async () => (await servers()).find((s) => s.id === added.id)?.status,
      {
        timeout: 20_000,
      },
    )
    .toBe("needs_sign_in");

  await openPage(page);
  const item = row(page, "docs-oauth");
  await expect(item.getByText("Needs sign-in", { exact: true })).toBeVisible();
  await expect(item.getByText("Connected", { exact: true })).toHaveCount(0);

  oauth.config.reject = false;
  oauth.config.ttl = 7200;
  const [popup] = await Promise.all([
    page.waitForEvent("popup"),
    item.getByRole("button", { name: "Sign in again" }).click(),
  ]);
  await popup.waitForURL(`${m.url}/oauth/callback?**`);
  await expect(item.getByText("Connected", { exact: true })).toBeVisible();
  await expect(item.getByText("Needs sign-in", { exact: true })).toHaveCount(0);
  await expect(item.getByRole("button", { name: "Sign in again" })).toHaveCount(
    0,
  );

  const after = await servers();
  expect(
    after.filter((s) => s.name === "docs-oauth").map((s) => [s.id, s.status]),
  ).toEqual([[added.id, "connected"]]);
  expect((await api(m, "GET", `/api/bots/${kelp.id}`)).mcp_servers).toEqual([
    added.id,
  ]);
});
