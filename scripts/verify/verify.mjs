// Starts the real atoll, drives its API and UI, and saves the proof under --out.
// Usage: node scripts/verify/verify.mjs [--fake] [--provider claude|codex|both] [--out <dir>]
import { execFileSync, spawn } from "node:child_process";
import {
  closeSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  openSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { setTimeout as sleep } from "node:timers/promises";
import { parseArgs } from "node:util";
import { chromium } from "playwright";
const keep = "Remember this fact: the codeword is kestrel. Reply with just: ok";
const recall = "Which codeword did I give you? Reply with just the word.";
const tools =
  "How many lines does notes.txt have? List the folder first, then read the file.";
const write = "Use the Write tool to create hello.txt containing the word hi.";
const remember = {
  fake: "Save the SQLite journal decision in user memory.",
  real: 'Use the remember tool with scope user to save the decision "Use SQLite WAL for the journal." because "Readers must not block journal writes.". Reply with just: saved',
};
const root = resolve(import.meta.dirname, "../..");
const { values: opt } = parseArgs({
  options: {
    fake: { type: "boolean", default: false },
    provider: { type: "string", default: "both" },
    out: { type: "string" },
    help: { type: "boolean", default: false },
  },
});
if (opt.help) {
  console.log(
    "usage: node scripts/verify/verify.mjs [--fake] [--provider claude|codex|both] [--out <dir>]",
  );
  process.exit(0);
}
const fake = opt.fake;
const providers =
  opt.provider === "both" ? ["claude", "codex"] : [opt.provider];
if (!providers.every((p) => p === "claude" || p === "codex"))
  throw new Error(`bad --provider ${opt.provider}`);
const out = resolve(
  opt.out ??
    join(root, ".verify", new Date().toISOString().replace(/[:.]/g, "-")),
);
const limit = fake ? 30_000 : 180_000;
const checks = [];
const check = (ok, name, detail = "") => {
  checks.push({ ok, name, detail });
  console.log(
    `  ${ok ? "PASS" : "FAIL"} ${name}${detail ? ` (${detail})` : ""}`,
  );
};
const step = (s) => console.log(`\n== ${s}`);
const fail = (name, err) =>
  check(false, name, err instanceof Error ? err.message : String(err));
async function until(what, f) {
  for (const end = Date.now() + limit; Date.now() < end; await sleep(50)) {
    const v = f();
    if (v) return v;
  }
  throw new Error(`timed out after ${limit} ms waiting for ${what}`);
}
async function apiPass(base, provider) {
  const trace = [];
  const events = [];
  const ctrl = new AbortController();
  const note = (o) => trace.push({ time: new Date().toISOString(), ...o });
  try {
    const call = async (method, path, body) => {
      const res = await fetch(base + path, {
        method,
        headers: body ? { "content-type": "application/json" } : {},
        body: body ? JSON.stringify(body) : undefined,
      });
      const text = await res.text();
      let json = text;
      try {
        json = JSON.parse(text);
      } catch {}
      note({ method, path, request: body, status: res.status, response: json });
      if (!res.ok)
        throw new Error(
          `${method} ${path} -> ${res.status} ${text.slice(0, 200)}`,
        );
      return json;
    };
    await call("GET", "/api/providers");
    const bot = await call("POST", "/api/bots", {
      name: `verify ${provider} ${Date.now()}`,
      persona: "A careful Go engineer.",
      provider,
    });
    const thread = await call("POST", `/api/bots/${bot.id}/threads`);
    const path = `/api/threads/${thread.id}/events`;
    const sse = await fetch(base + path, { signal: ctrl.signal });
    note({ method: "GET", path, status: sse.status });
    void (async () => {
      let buf = "";
      const dec = new TextDecoder();
      try {
        for await (const chunk of sse.body) {
          buf += dec.decode(chunk, { stream: true });
          for (let i; (i = buf.indexOf("\n\n")) >= 0; buf = buf.slice(i + 2)) {
            const block = buf.slice(0, i);
            const type = /^event: ?(.*)$/m.exec(block)?.[1];
            const raw = /^data: ?(.*)$/m.exec(block)?.[1];
            if (!type || !raw) continue;
            const data = JSON.parse(raw);
            events.push({ type, data, at: Date.now() });
            note({ event: type, data });
          }
        }
      } catch {}
    })();
    const send = async (text) => {
      const { run } = await call("POST", `/api/threads/${thread.id}/messages`, {
        text,
      });
      return run.id;
    };
    const finished = (id) =>
      until(`run_finished of run ${id}`, () =>
        events.find((e) => e.type === "run_finished" && e.data.run_id === id),
      );
    const run = (id) => call("GET", `/api/runs/${id}`);
    const id1 = await send(keep);
    await finished(id1);
    const id2 = await send(recall);
    await finished(id2);
    const [run1, run2] = [await run(id1), await run(id2)];
    await call("GET", `/api/threads/${thread.id}/messages`);
    check(
      /kestrel/i.test(run2.reply ?? ""),
      `${provider}: reply of run 2 has kestrel`,
      JSON.stringify(run2.reply),
    );
    const reported = (r) => r.status === "done" && r.report != null;
    check(
      reported(run1) && reported(run2),
      `${provider}: both runs done with a report`,
      `${run1.status}, ${run2.status}`,
    );
    if (!fake) return;
    const id3 = await send(tools);
    const end = await finished(id3);
    const first = events.find(
      (e) => e.type === "tool_call" && e.data.run_id === id3,
    );
    check(
      !!first && first.at < end.at,
      `${provider}: tool_call before run_finished`,
      first ? `${end.at - first.at} ms earlier` : "no tool_call",
    );
    const id4 = await send(write);
    const ask = await until("approval event", () =>
      events.find((e) => e.type === "approval" && e.data.run_id === id4),
    );
    const waiting = await run(id4);
    await call("POST", `/api/approvals/${ask.data.id}`, { approve: true });
    await finished(id4);
    const run4 = await run(id4);
    check(
      waiting.status === "waiting" &&
        run4.status === "done" &&
        /Created .*hello\.txt/.test(run4.reply ?? ""),
      `${provider}: approval pauses, approve continues`,
      `${waiting.status} then ${run4.status}`,
    );
  } catch (err) {
    fail(`${provider}: API pass`, err);
  } finally {
    ctrl.abort();
    writeFileSync(
      join(out, `api-${provider}.json`),
      JSON.stringify(trace, null, 2),
    );
  }
}
async function pickModel(page, provider) {
  const box = page.getByRole("combobox", { name: "Model" });
  const label = provider === "claude" ? "Claude Code" : "Codex";
  if ((await box.evaluate((el) => el.tagName)) === "SELECT") {
    const option = box.locator(`optgroup[label="${label}"] option`).first();
    await box.selectOption({ value: await option.getAttribute("value") });
  } else {
    await box.click();
    await page
      .getByRole("group", { name: label })
      .getByRole("option")
      .first()
      .click();
  }
}
// send types text and sends it, typing again if the composer was replaced meanwhile.
async function send(page, text) {
  const box = page.getByRole("textbox", { name: "Message" });
  const button = page.getByRole("button", { name: "Send" });
  for (const end = Date.now() + limit; Date.now() < end; await sleep(200)) {
    await box.fill(text);
    if (
      (await button.isEnabled()) &&
      (await button.click({ timeout: 2000 }).then(
        () => true,
        () => false,
      ))
    )
      return;
  }
  throw new Error("Send stayed disabled");
}
async function openThreads(page) {
  await page.getByRole("button", { name: /^Threads/ }).click();
  const panel = page.getByRole("dialog", { name: /^Threads with / });
  await panel.waitFor();
  return panel;
}
async function uiPass(base, provider) {
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1280, height: 800 },
  });
  page.setDefaultTimeout(limit);
  const shot = (name) => page.screenshot({ path: join(out, `ui-${name}.png`) });
  const say = (text) => send(page, text);
  const seen = (re) => page.getByText(re).first().waitFor();
  try {
    const name = `Ada ${Date.now()}`;
    await page.goto(base);
    await page.getByRole("button", { name: "New Bot" }).click();
    await page.getByRole("textbox", { name: "Name" }).fill(name);
    await page
      .getByRole("textbox", { name: "Persona" })
      .fill("A careful Go engineer.");
    await pickModel(page, provider);
    await shot("1-bot-form");
    await page.getByRole("button", { name: "Create Bot" }).click();
    await page
      .getByRole("heading", { name: `What should ${name} work on?` })
      .waitFor();
    await say(keep);
    await seen(/^\W*ok\W*$/i);
    await shot("2-first-reply");
    await say(recall);
    await seen(/^\W*kestrel\W*$/i);
    if (fake) {
      const reply = page.getByText(/notes\.txt has 3 lines/);
      await say(tools);
      await page
        .getByRole("log", { name: "Messages" })
        .getByRole("status")
        .waitFor();
      await shot("3-tools");
      await reply.waitFor();
      await say(write);
      const approve = page.getByRole("button", { name: "Allow" });
      await approve.waitFor();
      await page.getByRole("button", { name: "Deny" }).waitFor();
      await shot("4-approval");
      await approve.click();
      await page.getByText(/Created .*hello\.txt/).waitFor();
    }
    await shot("5-done");
    await say(fake ? remember.fake : remember.real);
    const saved = page.getByRole("region", { name: "Saved memory" });
    await saved.waitFor();
    await shot("6-memory-saved");
    await saved.getByRole("button", { name: "Retire", exact: true }).click();
    await seen(/^Retired$/);
    await shot("7-memory-retired");
    check(true, `ui: ${provider} flow done`);
  } catch (err) {
    await shot("failed").catch(() => {});
    fail(`ui: ${provider} flow`, err);
  } finally {
    await browser.close();
  }
}
async function firstRun(base) {
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1280, height: 800 },
  });
  page.setDefaultTimeout(limit);
  try {
    const settings = await (await fetch(base + "/api/settings")).json();
    if (!settings.user_name) {
      await page.goto(base);
      await page.getByRole("heading", { name: "Welcome to Reef" }).waitFor();
      await page.screenshot({ path: join(out, "screen-first-run.png") });
      const res = await fetch(base + "/api/settings", {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ ...settings, user_name: "Verifier" }),
      });
      if (!res.ok) throw new Error(`setting user name: ${res.status}`);
      check(true, "screens: first run");
    } else console.log("  first run omitted: user_name is already set");
  } catch (err) {
    fail("screens: first run", err);
  } finally {
    await browser.close();
  }
}
async function screensPass(base, provider) {
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1280, height: 800 },
  });
  page.setDefaultTimeout(limit);
  const shot = (name) =>
    page.screenshot({ path: join(out, `screen-${name}.png`) });
  const say = (text) => send(page, text);
  try {
    const name = `Screens ${Date.now()}`;
    await page.goto(base);
    await page.getByRole("navigation", { name: "Bots" }).waitFor();
    await shot("sidebar");
    await page.getByRole("button", { name: "New Bot" }).click();
    await page.getByRole("heading", { name: "New Bot" }).waitFor();
    await shot("new-bot");
    await page.getByRole("textbox", { name: "Name" }).fill(name);
    await page
      .getByRole("textbox", { name: "Persona" })
      .fill("A careful Go engineer.");
    await pickModel(page, provider);
    await page.getByRole("button", { name: "Create Bot" }).click();
    await page
      .getByRole("heading", { name: `What should ${name} work on?` })
      .waitFor();
    await shot("empty-thread");
    const panel = await openThreads(page);
    await shot("threads");
    await panel.getByRole("button", { name: "New thread" }).click();
    await page.getByRole("button", { name: "Bot settings" }).click();
    const settings = page.getByRole("dialog", { name: "Bot settings" });
    await settings.waitFor();
    await shot("bot-settings");
    await settings.getByRole("button", { name: "Close" }).click();
    await say(fake ? "Fix the wizard repo select and run its tests." : tools);
    const log = page.getByRole("log", { name: "Messages" });
    await log.getByRole("status").waitFor();
    await shot("working");
    await log
      .getByText(/^Worked \d+(m \d+)?s · /)
      .last()
      .waitFor();
    await shot("summary");
    if (fake) {
      // The fake records write only as a thread's first message.
      await (
        await openThreads(page)
      )
        .getByRole("button", { name: "New thread" })
        .click();
      await page.waitForURL((u) => u.href.endsWith("/new"));
      await say(write);
      await log.getByRole("button", { name: "Allow", exact: true }).waitFor();
      await shot("approval");
      await shot("sidebar-needs-you");
      await log.getByRole("button", { name: "Allow", exact: true }).click();
      await log.getByText(/Created .*hello\.txt/).waitFor();
    } else
      console.log(
        "  approval screen omitted: the real provider may not request approval",
      );
    await page
      .getByRole("navigation", { name: "Bots" })
      .getByRole("link", { name: /Settings/ })
      .click();
    for (const section of [
      "General",
      "Providers",
      "Connections",
      "Skills",
      "Repos",
      "Memory",
    ]) {
      await page.getByRole("link", { name: section, exact: true }).click();
      if (section === "Connections" || section === "Repos") {
        await page
          .getByRole("textbox", {
            name:
              section === "Connections" ? "GitLab instance URL" : "Repo name",
          })
          .waitFor();
      } else {
        await page
          .getByRole("heading", { name: section, exact: true })
          .waitFor();
      }
      if (section === "Skills") {
        await page.getByRole("button", { name: "New skill" }).click();
        await page.getByRole("dialog", { name: /^Edit / }).waitFor();
      }
      await shot(section.toLowerCase());
    }
    check(true, "screens: Mac app pages");
  } catch (err) {
    await shot("failed").catch(() => {});
    fail("screens: Mac app pages", err);
  } finally {
    await browser.close();
  }
}
const greeting =
  "---\nname: reef-greeting\ndescription: Give the Reef greeting.\nallowed-tools: Read\ncontext: fork\n---\nHello from the lagoon.\n";
const ask = {
  fake: (p) => `Create reef-greeting and a Lagoon Bot using ${p}.`,
  real: (p) =>
    `Use your create_skill tool to create a skill named reef-greeting with exactly this SKILL.md content:\n\n${greeting}\nThen use your create_bot tool to create a Bot named Lagoon with persona "A lagoon guide.", provider ${p}, and skills set to the id create_skill returned. Reply with just: created`,
};
const useSkill = {
  fake: "List your skills and use reef-greeting.",
  real: "List the names of the skills available to you, then use the reef-greeting skill and quote exactly what it tells you to say.",
};
// Chief makes a skill and a Bot that has it; the Bot uses it, alone and in a repo with its own
// committed skills; Undo of the Bot's creation removes it.
async function chiefPass(base, provider, undo) {
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1280, height: 800 },
  });
  page.setDefaultTimeout(limit);
  const shot = (name) =>
    page.screenshot({ path: join(out, `chief-${provider}-${name}.png`) });
  const say = (text) => send(page, text);
  const newThread = async () => {
    const panel = await openThreads(page);
    await panel.getByRole("button", { name: "New thread" }).click();
    await page.waitForURL((u) => u.href.endsWith("/new"));
    await page
      .getByRole("heading", { name: /^What should .* work on\?$/ })
      .waitFor();
  };
  // Claude asks before it uses a skill; the user approves in the thread.
  const answered = async (target, name) => {
    const approve = page.getByRole("button", { name: "Allow" }).first();
    let shown = false;
    for (const end = Date.now() + limit; Date.now() < end; await sleep(200)) {
      if (await target.isVisible()) return;
      if (await approve.isVisible()) {
        if (!shown) await shot(`${name}-approval`);
        shown = true;
        await approve.click().catch(() => {});
      }
    }
    throw new Error(`timed out after ${limit} ms waiting for ${name}`);
  };
  const api = async (path, init) => {
    const res = await fetch(base + "/api" + path, init);
    return res.status === 204 ? null : res.json();
  };
  try {
    const before = new Set((await api("/bots")).map((b) => b.id));
    await page.goto(base);
    await page
      .getByRole("navigation")
      .getByText("Chief", { exact: true })
      .click();
    await newThread();
    await say((fake ? ask.fake : ask.real)(provider));
    const change = page
      .getByRole("region", { name: "Platform change" })
      .filter({ has: page.getByText("Lagoon", { exact: true }) })
      .last();
    await change.waitFor();
    await shot("1-chief-changes");
    const chiefURL = page.url();
    const lagoon = (await api("/bots")).find(
      (b) => !before.has(b.id) && b.name === "Lagoon",
    );
    const skills = await api("/skills");
    const skill = skills.find((s) => s.id === lagoon?.skills?.[0]);
    check(
      lagoon?.provider === provider &&
        skill?.content.trimEnd() === greeting.trimEnd(),
      `chief: ${provider} created skill and Bot`,
      JSON.stringify({ bot: lagoon, skill: skill?.name }),
    );
    await page.goto(`${base}/settings/skills`);
    await shot("2-skill-library");
    await page.goto(`${base}/bots/${lagoon.id}`);
    await newThread();
    await say(fake ? useSkill.fake : useSkill.real);
    await answered(page.getByText(/Hello from the lagoon/).first(), "3");
    await shot("3-bot-uses-skill");
    check(true, `chief: ${provider} Bot lists and uses the skill`);

    const repoDir = join(tmp, `atoll-fixture-${provider}`);
    const git = (...args) =>
      execFileSync("git", args, { cwd: repoDir, stdio: "ignore" });
    for (const dir of [".claude", ".agents"]) {
      mkdirSync(join(repoDir, dir, "skills", "repo-skill"), {
        recursive: true,
      });
      writeFileSync(
        join(repoDir, dir, "skills", "repo-skill", "SKILL.md"),
        "---\nname: repo-skill\ndescription: A skill this repo commits.\n---\nSay: the repo skill is here.\n",
      );
    }
    git("init", "-q", "-b", "main");
    git("add", ".");
    git("-c", "user.name=v", "-c", "user.email=v@v", "commit", "-qm", "skills");
    await api("/repos", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: `atoll-fixture-${provider}`,
        path: repoDir,
      }),
    });
    await page.goto(`${base}/bots/${lagoon.id}/new`);
    await page
      .getByRole("combobox", { name: "Repo" })
      .selectOption({ label: `atoll-fixture-${provider}` });
    await say(fake ? useSkill.fake : useSkill.real);
    await answered(page.getByText(/repo-skill/).first(), "4");
    await page
      .getByText(/reef-greeting/)
      .last()
      .waitFor();
    await shot("4-repo-and-bot-skills");
    check(true, `chief: ${provider} Bot sees repo skill next to its own`);

    if (undo) {
      await page.goto(chiefURL);
      await change.getByRole("button", { name: "Undo", exact: true }).click();
      await change.getByText("Undone", { exact: true }).waitFor();
      await shot("5-undone");
      const gone = !(await api("/bots")).some((b) => b.id === lagoon.id);
      check(gone, `chief: ${provider} Undo removed the Bot`);
    }
  } catch (err) {
    await shot("failed").catch(() => {});
    fail(`chief: ${provider}`, err);
  } finally {
    await browser.close();
  }
}
mkdirSync(join(out, "bin"), { recursive: true });
const tmp = mkdtempSync(join(tmpdir(), "atoll-verify-"));
const bin = join(tmp, "bin");
const data = join(tmp, "data");
let server;
const logFd = openSync(join(out, "server.log"), "w");
try {
  step("build atoll");
  const go = (...args) =>
    execFileSync("go", args, { cwd: root, stdio: "inherit" });
  go("build", "-o", join(out, "bin", "atoll"), "./cmd/atoll");
  step(fake ? "machine: fake providers" : "machine: real providers");
  mkdirSync(bin, { recursive: true });
  mkdirSync(join(data, "providers"), { recursive: true });
  let env;
  if (fake) {
    const home = join(tmp, "home");
    mkdirSync(home);
    for (const [name, login] of [
      ["claude", ".credentials.json"],
      ["codex", "auth.json"],
    ]) {
      go(
        "build",
        "-o",
        join(bin, name),
        "-ldflags",
        `-X atoll/e2e/fakes/fake._testdata=${join(root, "e2e", "testdata")}`,
        `./e2e/fakes/${name}`,
      );
      mkdirSync(join(data, "providers", name), { recursive: true });
      writeFileSync(join(data, "providers", name, login), "{}");
    }
    env = {
      HOME: home,
      PATH: `${bin}:/usr/bin:/bin`,
      ATOLL_DATA_DIR: data,
      ATOLL_PROVIDER_PATH: bin,
    };
  } else {
    const real =
      process.platform === "darwin"
        ? join(homedir(), "Library", "Application Support", "Atoll")
        : join(homedir(), ".config", "Atoll");
    for (const name of providers) {
      const dir = join(real, "providers", name);
      if (!existsSync(dir))
        throw new Error(`${dir} not found: sign in to ${name} in Atoll first`);
      symlinkSync(dir, join(data, "providers", name));
    }
    // Discovery reads the login shell's PATH and the ATOLL_*_PATH overrides.
    env = { ...process.env, HOME: homedir(), ATOLL_DATA_DIR: data };
  }
  step("start server");
  const child = spawn(
    join(out, "bin", "atoll"),
    ["serve", "--addr", "127.0.0.1:0"],
    {
      cwd: tmp,
      env,
      stdio: ["ignore", "pipe", logFd],
    },
  );
  server = child;
  const first = await new Promise((ok, no) => {
    const timer = setTimeout(
      () => no(new Error("atoll serve printed nothing in 15 s")),
      15_000,
    );
    createInterface({ input: child.stdout }).once(
      "line",
      (l) => (clearTimeout(timer), ok(l)),
    );
    child.once("exit", (code) =>
      no(new Error(`atoll serve exited with ${code} before listening`)),
    );
  });
  const base = first.match(/^listening on (http:\/\/127\.0\.0\.1:\d+)$/)?.[1];
  if (!base)
    throw new Error(
      `first line ${JSON.stringify(first)}, want "listening on http://127.0.0.1:<port>"`,
    );
  console.log(`  ${base}`);
  step("First run screen");
  await firstRun(base);
  for (const p of providers) {
    step(`API pass: ${p}`);
    await apiPass(base, p);
  }
  step(`UI pass: ${providers[0]}`);
  await uiPass(base, providers[0]);
  step(`Screens pass: ${providers[0]}`);
  await screensPass(base, providers[0]);
  for (const [i, p] of providers.entries()) {
    step(`Chief pass: ${p}`);
    await chiefPass(base, p, i === providers.length - 1);
  }
} catch (err) {
  fail("setup", err);
} finally {
  step("clean up");
  if (server && server.exitCode === null && server.signalCode === null) {
    const exited = new Promise((ok) => server.once("exit", ok));
    server.kill("SIGTERM");
    const timer = setTimeout(() => server.kill("SIGKILL"), 5000);
    await exited;
    clearTimeout(timer);
  }
  closeSync(logFd);
  for (const name of providers) {
    const log = join(bin, `${name}.log`);
    if (existsSync(log)) {
      // A fake provider is not killed with the server; its log names every process it ran.
      for (const line of readFileSync(log, "utf8")
        .split("\n")
        .filter(Boolean)) {
        try {
          process.kill(JSON.parse(line).pid, "SIGKILL");
        } catch {}
      }
    }
  }
  rmSync(tmp, { recursive: true, force: true });
}
console.log(`\nproof saved in ${out}`);
for (const c of checks)
  console.log(
    `${c.ok ? "PASS" : "FAIL"}  ${c.name}${c.detail ? `  ${c.detail}` : ""}`,
  );
if (checks.length === 0 || checks.some((c) => !c.ok)) process.exitCode = 1;
