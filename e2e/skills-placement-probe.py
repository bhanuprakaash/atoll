#!/usr/bin/env python3
"""Local discovery probe: real CLIs, synthetic Anthropic response, no model override.

No credentials are copied, HOME is unchanged, temporary state is always removed.
"""
import http.server
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading


def skill(root, name, extra=""):
    path = root / name / "SKILL.md"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(f"---\nname: {name}\ndescription: DISCOVER_{name}\n{extra}---\nBODY_{name}\n")
    return path


def git(cwd, *args):
    subprocess.run(["git", "-C", str(cwd), *args], check=True, capture_output=True,
                   env={**os.environ, "GIT_AUTHOR_NAME": "Fixture", "GIT_AUTHOR_EMAIL": "fixture@example.com",
                        "GIT_COMMITTER_NAME": "Fixture", "GIT_COMMITTER_EMAIL": "fixture@example.com"})


def main():
    for cli in ("claude", "codex"):
        print(subprocess.check_output([cli, "--version"], text=True).strip(), flush=True)
    with tempfile.TemporaryDirectory(prefix="atoll-placement-") as tmp:
        root = Path(tmp)
        repo = root / "repo"
        repo.mkdir()
        git(repo, "init", "-b", "main")
        skill(repo / ".claude/skills", "repo-claude")
        skill(repo / ".agents/skills", "repo-agents")
        git(repo, "add", ".")
        git(repo, "commit", "-m", "seed skills")
        checkout, worktree = root / "checkout", root / "worktree"
        git(repo, "worktree", "add", "--detach", str(checkout))
        git(repo, "worktree", "add", "-b", "fix/probe", str(worktree))
        # Shared provider folders hold no mutable Bot skill selection.
        claude_config, codex_config = root / "claude", root / "codex"
        claude_config.mkdir()
        codex_config.mkdir()
        extra = root / "run-skills"
        skill(extra / ".claude/skills", "bot-skill", "allowed-tools: Read\ncontext: fork\n")
        skill(extra / "codex", "bot-skill")
        captures = []

        class Capture(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                captures.append(json.loads(self.rfile.read(int(self.headers["Content-Length"]))))
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps({"id": "probe", "type": "message", "role": "assistant",
                    "model": "probe", "content": [{"type": "text", "text": "ok"}],
                    "stop_reason": "end_turn", "usage": {"input_tokens": 1, "output_tokens": 1}}).encode())

            def log_message(self, *_):
                pass

        server = http.server.HTTPServer(("127.0.0.1", 0), Capture)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            for cwd in (checkout, worktree):
                env = {k: v for k, v in os.environ.items() if not k.startswith(("CLAUDE", "ANTHROPIC"))}
                env.update(CLAUDE_CONFIG_DIR=str(claude_config), ENABLE_CLAUDEAI_MCP_SERVERS="false",
                           ANTHROPIC_API_KEY="synthetic-probe", ANTHROPIC_BASE_URL=f"http://127.0.0.1:{server.server_port}")
                captures.clear()
                result = subprocess.run(["claude", "-p", "--output-format", "json", "--strict-mcp-config",
                    "--settings", '{"disableAllHooks":true}', "--add-dir=" + str(extra), "Reply ok"],
                    cwd=cwd, env=env, capture_output=True, text=True, timeout=45)
                assert result.returncode == 0, result.stderr
                payload = json.dumps(captures)
                assert "DISCOVER_bot-skill" in payload and "DISCOVER_repo-claude" in payload
                assert "DISCOVER_repo-agents" not in payload
                print(f"Claude {cwd.name}: Bot + .claude repo skill discovered; .agents absent", flush=True)
        finally:
            server.shutdown()
            server.server_close()
            worker.join()
        # Codex list returns disabled entries too; only enabled:true means available.
        unselected = skill(codex_config / "skills", "unselected")
        off = [{"path": str(unselected), "enabled": False}]
        for path in (Path.home() / ".agents/skills").glob("*/SKILL.md"):
            off.append({"path": str(path), "enabled": False})
        for cwd in (checkout, worktree):
            proc = subprocess.Popen(["codex", "app-server", "-c", "skills.bundled.enabled=false",
                "-c", "features.apps=false", "-c", "features.plugins=false", "-c", "features.remote_plugin=false",
                "-c", "skills.config=" + json.dumps(off).replace(': ', ' = ')], cwd=cwd,
                env={**os.environ, "CODEX_HOME": str(codex_config)}, stdin=subprocess.PIPE,
                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
            try:
                for number, (method, params) in enumerate([
                    ("initialize", {"clientInfo": {"name": "placement-probe", "version": "0"}}),
                    ("skills/extraRoots/set", {"extraRoots": [str(extra / "codex")]}),
                    ("skills/list", {"cwds": [str(cwd)], "forceReload": True})], 1):
                    proc.stdin.write(json.dumps({"id": number, "method": method, "params": params}) + "\n")
                    proc.stdin.flush()
                    while True:
                        line = proc.stdout.readline()
                        assert line, "app-server exited"
                        reply = json.loads(line)
                        if reply.get("id") == number:
                            break
                    assert "error" not in reply, reply
                skills = reply["result"]["data"][0]["skills"]
                enabled = {s["name"] for s in skills if s["enabled"]}
                assert enabled == {"bot-skill", "repo-agents"}, enabled
                print(f"Codex {cwd.name}: extra root Bot + .agents repo skill only; path disable works", flush=True)
            finally:
                proc.terminate()
                proc.wait(timeout=10)


if __name__ == "__main__":
    main()
