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
async function uiPass(base, provider) {
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1280, height: 800 },
  });
  page.setDefaultTimeout(limit);
  const shot = (name) => page.screenshot({ path: join(out, `ui-${name}.png`) });
  const say = async (text) => {
    await page.getByRole("textbox", { name: "Message" }).fill(text);
    await page.getByRole("button", { name: "Send" }).click();
  };
  const seen = (re) => page.getByText(re).first().waitFor();
  try {
    const name = `Ada ${Date.now()}`;
    await page.goto(base);
    await page.getByRole("button", { name: "New Bot" }).click();
    await page.getByRole("textbox", { name: "Name" }).fill(name);
    await page
      .getByRole("textbox", { name: "Persona" })
      .fill("A careful Go engineer.");
    await page
      .getByRole("combobox", { name: "Model" })
      .selectOption(provider === "claude" ? "Claude Code" : "Codex");
    await shot("1-bot-form");
    await page.getByRole("button", { name: "Create" }).click();
    await page.getByRole("navigation").getByText(name).click();
    await page.getByRole("button", { name: "New thread" }).click();
    await say(keep);
    await seen(/^\W*ok\W*$/i);
    await shot("2-first-reply");
    await say(recall);
    await seen(/^\W*kestrel\W*$/i);
    if (fake) {
      const reply = page.getByText(/notes\.txt has 3 lines/);
      await say(tools);
      await seen(/\bBash\b/);
      await shot("3-tools");
      await reply.waitFor();
      await say(write);
      const approve = page.getByRole("button", { name: "Approve" });
      await approve.waitFor();
      await page.getByRole("button", { name: "Reject" }).waitFor();
      await shot("4-approval");
      await approve.click();
      await page.getByText(/Created .*hello\.txt/).waitFor();
    }
    await shot("5-done");
    check(true, `ui: ${provider} flow done`);
  } catch (err) {
    await shot("failed").catch(() => {});
    fail(`ui: ${provider} flow`, err);
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
    env = {
      HOME: homedir(),
      PATH: process.env.PATH ?? "",
      ATOLL_DATA_DIR: data,
    };
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
  for (const p of providers) {
    step(`API pass: ${p}`);
    await apiPass(base, p);
  }
  step(`UI pass: ${providers[0]}`);
  await uiPass(base, providers[0]);
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
