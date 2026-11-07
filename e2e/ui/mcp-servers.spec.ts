// Settings > MCP servers: the user adds local and remote servers, each Bot picks its own, and a
// call to a user-added server asks first. Contract: e2e/mcp-servers-contract.md. Each test runs
// its own machine (temporary HOME and data, a private Secret Service) and checks what the UI did
// through the API. The Go tests prove the secrets reach the OS secret store and the providers.
import { expect, type Page, test } from "@playwright/test";
import { api, type Machine, newBotAPI, sidebarRow, startAtoll } from "./atoll";

const envSecret = "fake-mcp-env-1";
const headerSecret = "fake-mcp-key-1";

let m: Machine;
test.beforeEach(async () => {
  m = await startAtoll({ secretStore: true });
});
test.afterEach(() => m?.stop());

const stdio = () => `${process.env.ATOLL_E2E_OUT}/mcpstdio`;
const servers = async (): Promise<
  {
    id: string;
    name: string;
    kind: string;
    args?: string[];
    env_names?: string[];
  }[]
> => api(m, "GET", "/api/mcp-servers");

async function openPage(page: Page) {
  await page.goto(`${m.url}/settings`);
  await page.getByRole("link", { name: "MCP servers" }).click();
  await expect(page.getByRole("list", { name: "Catalog" })).toBeVisible();
}

const yours = (page: Page) => page.getByRole("list", { name: "Your servers" });
const row = (page: Page, name: string) =>
  yours(page).getByRole("listitem").filter({ hasText: name });

test("the page lists Context7, adds a local and a remote server, and shows names only for secrets", async ({
  page,
}) => {
  await openPage(page);
  const items = page
    .getByRole("list", { name: "Catalog" })
    .getByRole("listitem");
  await expect(items).toHaveCount(5);
  const catalog = items.first();
  await expect(catalog).toContainText("Context7");
  await expect(catalog).toContainText("https://mcp.context7.com/mcp");
  await expect(yours(page).getByRole("listitem")).toHaveCount(0);

  await page.getByRole("button", { name: "Add local" }).click();
  const local = page.getByRole("dialog", { name: "Add local" });
  await local.getByRole("textbox", { name: "Name" }).fill("docs-local");
  await local.getByRole("textbox", { name: "Command" }).fill(stdio());
  await local.getByRole("textbox", { name: "Args" }).fill("--tag\none");
  await local
    .getByRole("textbox", { name: "Env" })
    .fill(`FAKE_MCP_TOKEN=${envSecret}`);
  await local.getByRole("button", { name: "Add", exact: true }).click();
  await expect(local).toHaveCount(0);
  await expect(row(page, "docs-local")).toContainText("FAKE_MCP_TOKEN");

  await page.getByRole("button", { name: "Add by URL" }).click();
  const remote = page.getByRole("dialog", { name: "Add by URL" });
  await remote.getByRole("textbox", { name: "Name" }).fill("docs-remote");
  await remote
    .getByRole("textbox", { name: "URL" })
    .fill("https://mcp.fake.test/mcp");
  await remote
    .getByRole("textbox", { name: "Headers" })
    .fill(`X-Fake-Key: ${headerSecret}`);
  await remote.getByRole("button", { name: "Add", exact: true }).click();
  await expect(remote).toHaveCount(0);
  await expect(row(page, "docs-remote")).toContainText(
    "https://mcp.fake.test/mcp",
  );
  await expect(row(page, "docs-remote")).toContainText("X-Fake-Key");

  const list = await servers();
  expect(list.map((s) => [s.id, s.name, s.kind])).toEqual([
    ["context7", "Context7", "catalog"],
    ["notion", "Notion", "catalog"],
    ["figma", "Figma", "catalog"],
    ["cloudflare", "Cloudflare", "catalog"],
    ["slack", "Slack", "catalog"],
    [list[5].id, "docs-local", "local"],
    [list[6].id, "docs-remote", "remote"],
  ]);
  expect(list[5]).toMatchObject({
    args: ["--tag", "one"],
    env_names: ["FAKE_MCP_TOKEN"],
  });
  expect(list[6]).toMatchObject({ header_names: ["X-Fake-Key"] });
  for (const secret of [envSecret, headerSecret]) {
    expect(JSON.stringify(list)).not.toContain(secret);
    expect(await page.locator("body").innerText()).not.toContain(secret);
  }
  await page.reload();
  await expect(row(page, "docs-local")).toBeVisible();
  expect(await page.locator("body").innerText()).not.toContain(envSecret);
});

test("Edit keeps the secrets it is not given, and Remove drops the server from every Bot", async ({
  page,
}) => {
  const local = await api(m, "POST", "/api/mcp-servers", {
    name: "docs-local",
    command: stdio(),
    args: ["--tag", "one"],
    env: { FAKE_MCP_TOKEN: envSecret },
  });
  const remote = await api(m, "POST", "/api/mcp-servers", {
    name: "docs-remote",
    url: "https://mcp.fake.test/mcp",
    headers: { "X-Fake-Key": headerSecret },
  });
  const kelp = await newBotAPI(m, "Kelp", "claude", {
    mcp_servers: [local.id, remote.id],
  });
  await openPage(page);

  await row(page, "docs-local").getByRole("button", { name: "Edit" }).click();
  const edit = page.getByRole("dialog", { name: "Edit docs-local" });
  await expect(edit.getByRole("textbox", { name: "Name" })).toHaveValue(
    "docs-local",
  );
  await expect(edit.getByRole("textbox", { name: "Command" })).toHaveValue(
    stdio(),
  );
  await expect(edit.getByRole("textbox", { name: "Args" })).toHaveValue(
    "--tag\none",
  );
  await expect(edit.getByRole("textbox", { name: "Env" })).toHaveValue(
    "FAKE_MCP_TOKEN=",
  );
  await edit.getByRole("textbox", { name: "Args" }).fill("--tag\ntwo");
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect(edit).toHaveCount(0);
  await expect
    .poll(async () => (await servers()).find((s) => s.id === local.id))
    .toMatchObject({ args: ["--tag", "two"], env_names: ["FAKE_MCP_TOKEN"] });

  await row(page, "docs-remote")
    .getByRole("button", { name: "Remove" })
    .click();
  await expect(row(page, "docs-remote")).toHaveCount(0);
  expect((await servers()).map((s) => s.name)).toEqual([
    "Context7",
    "Notion",
    "Figma",
    "Cloudflare",
    "Slack",
    "docs-local",
  ]);
  expect((await api(m, "GET", `/api/bots/${kelp.id}`)).mcp_servers).toEqual([
    local.id,
  ]);
});

test("Bot settings lists MCP servers as switches; a call to a picked user-added server asks first", async ({
  page,
}) => {
  const local = await api(m, "POST", "/api/mcp-servers", {
    name: "docs-local",
    command: stdio(),
    args: ["--tag", "one"],
    env: { FAKE_MCP_TOKEN: envSecret },
  });
  const kelp = await newBotAPI(m, "Kelp");
  await page.goto(m.url);
  await sidebarRow(page, "Kelp").click();
  await page.getByRole("button", { name: "Bot settings" }).click();
  const panel = page.getByRole("dialog", { name: "Bot settings" });
  const group = panel.getByRole("group", { name: "MCP servers" });
  await expect(group.getByRole("switch")).toHaveCount(6);
  await expect(
    group.getByRole("switch", { name: "Context7", exact: true }),
  ).not.toBeChecked();
  const docs = group.getByRole("switch", { name: "docs-local", exact: true });
  await expect(docs).not.toBeChecked();
  await docs.click();
  await expect(docs).toBeChecked();
  await expect
    .poll(async () => (await api(m, "GET", `/api/bots/${kelp.id}`)).mcp_servers)
    .toEqual([local.id]);
  await panel.getByRole("button", { name: "Close" }).click();

  const messages = page.getByRole("log", { name: "Messages" });
  const send = async (text: string) => {
    await page.getByRole("textbox", { name: "Message" }).fill(text);
    await page.getByRole("button", { name: "Send" }).click();
  };
  const use = `Use MCP tools. [{"server":"docs-local","tool":"answer","args":{}}]`;
  await send(use);
  await expect(page.getByText(/Wants to use.*docs-local/)).toBeVisible();
  await expect(messages.getByText("stdio-answer-42")).toHaveCount(0);
  await page.getByRole("button", { name: "Allow", exact: true }).click();
  await expect(
    messages.getByText("docs-local/answer: stdio-answer-42"),
  ).toBeVisible();

  // Switched off, the next run has no such server: nothing to ask, nothing to call.
  await page.getByRole("button", { name: "Bot settings" }).click();
  await panel.getByRole("switch", { name: "docs-local", exact: true }).click();
  await expect
    .poll(async () => (await api(m, "GET", `/api/bots/${kelp.id}`)).mcp_servers)
    .toEqual([]);
  await panel.getByRole("button", { name: "Close" }).click();
  await send(use);
  await expect(
    messages.getByText("docs-local/answer: unavailable"),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Allow", exact: true }),
  ).toHaveCount(0);
});
