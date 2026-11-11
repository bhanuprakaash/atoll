package e2e

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Contract: bot-mode-contract.md, "Tool caches".

// _cacheVars are the env vars a Codex Auto run points at its Bot's cache root. pnpm_config_store_dir
// is pnpm's store-dir setting (pnpm 12 reads pnpm_config_<setting>, not npm_config_<setting>); PNPM_HOME and the XDG data and
// state folders hold the rest of pnpm's state.
var _cacheVars = []string{"XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "GOCACHE", "GOLANGCI_LINT_CACHE", "pnpm_config_store_dir", "PNPM_HOME"}

func TestCodexAuto_RunGetsReefOwnedCaches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, data string }{
		{"plain data dir", ""},
		{"data dir with a space", "Application Support/Reef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := ""
			if tc.data != "" {
				data = filepath.Join(t.TempDir(), tc.data)
				if err := os.MkdirAll(data, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			f := newRepoFixtureIn(t, "codex", data)
			sentinels := plantUserCaches(t, f.m.home)
			before := snapshot(t, sentinels...)

			b := f.modeBot(t, "codex", "auto")
			root := filepath.Join(f.m.data, "cache", b.ID)
			f.runThread(t, b, _fixRepoPrompt, _resumeRepoPrompt)

			sessions := f.m.sessions(t, "codex")
			if len(sessions) != 2 {
				t.Fatalf("codex ran %d sessions, want 2", len(sessions))
			}
			for i, c := range sessions {
				method, _, _, roots, _ := codexWorkspaceWrite(t, c)
				if !slices.Contains(resolved(t, roots...), resolveLoose(t, root)) {
					t.Errorf("session %d (%s): writable_roots %q lack the Bot's cache root %s", i+1, method, roots, root)
				}
				assertCacheEnv(t, c, root, f.m.home)
			}

			if after := snapshot(t, sentinels...); after != before {
				t.Errorf("a Bot run changed the user's ~/.cache or ~/go:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestCodexAuto_CachesArePerBotAndGoWithTheBot(t *testing.T) {
	t.Parallel()
	f := newRepoFixture(t, "codex")
	a, b := f.modeBot(t, "codex", "auto"), f.modeBot(t, "codex", "auto")
	rootA, rootB := filepath.Join(f.m.data, "cache", a.ID), filepath.Join(f.m.data, "cache", b.ID)
	f.runThread(t, a, _questionRepoPrompt)
	f.runThread(t, a, _questionRepoPrompt)
	f.runThread(t, b, _questionRepoPrompt)

	sessions := f.m.sessions(t, "codex")
	if len(sessions) != 3 {
		t.Fatalf("codex ran %d sessions, want 3", len(sessions))
	}
	for i, root := range []string{rootA, rootA, rootB} {
		assertCacheEnv(t, sessions[i], root, f.m.home)
	}
	if got, other := resolveLoose(t, sessions[0].env["GOCACHE"]), resolveLoose(t, sessions[2].env["GOCACHE"]); got == other || strings.HasPrefix(got, other+string(filepath.Separator)) || strings.HasPrefix(other, got+string(filepath.Separator)) {
		t.Errorf("two Bots share a cache: GOCACHE %s and %s", got, other)
	}

	for _, root := range []string{rootA, rootB} {
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("the Bot's cache folder was never made: %v", err)
		}
	}
	f.s.do(t, "DELETE", "/api/bots/"+a.ID, nil, nil, 204)
	if _, err := os.Stat(rootA); err == nil {
		t.Errorf("deleting the Bot left its cache folder %s", rootA)
	}
	if _, err := os.Stat(rootB); err != nil {
		t.Errorf("deleting one Bot removed another's cache folder: %v", err)
	}
}

func TestCodexAuto_OtherModesAndClaudeGetNoCaches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ provider, mode string }{{"codex", "ask"}, {"codex", "full"}, {"claude", "auto"}, {"claude", "ask"}, {"claude", "full"}} {
		t.Run(tc.provider+"/"+tc.mode, func(t *testing.T) {
			t.Parallel()
			f := newRepoFixture(t, tc.provider)
			b := f.modeBot(t, tc.provider, tc.mode)
			f.runThread(t, b, _fixRepoPrompt)

			sessions := f.m.sessions(t, tc.provider)
			if len(sessions) != 1 {
				t.Fatalf("%s ran %d sessions, want 1", tc.provider, len(sessions))
			}
			cache := filepath.Join(f.m.data, "cache")
			for _, name := range _cacheVars {
				if v, ok := sessions[0].env[name]; ok && strings.HasPrefix(resolveLoose(t, v), resolveLoose(t, cache)+string(filepath.Separator)) {
					t.Errorf("%s %s: %s=%s points into Reef's cache folder %s", tc.provider, tc.mode, name, v, cache)
				}
			}
			if tc.provider == "codex" {
				if _, _, present, _, _ := codexWorkspaceWrite(t, sessions[0]); present {
					t.Errorf("codex on %s: config.sandbox_workspace_write is set", tc.mode)
				}
			}
			if _, err := os.Stat(filepath.Join(cache, b.ID)); err == nil {
				t.Errorf("%s on %s: a cache folder was made for the Bot", tc.provider, tc.mode)
			}
		})
	}
}

// assertCacheEnv checks the run's env: every cache var is a folder strictly under root, none is
// under the user's home, the three XDG folders differ, and root existed and was writable when the
// run started (what existed of each var's path then was root or something inside it).
func assertCacheEnv(t *testing.T, c *call, root, home string) {
	t.Helper()
	wantRoot := resolveLoose(t, root)
	sep := string(filepath.Separator)
	for _, name := range _cacheVars {
		v, ok := c.env[name]
		if !ok || v == "" {
			t.Errorf("env %s is unset, want a folder under %s", name, root)
			continue
		}
		if got := resolveLoose(t, v); !strings.HasPrefix(got, wantRoot+sep) {
			t.Errorf("env %s=%s, want a folder under %s", name, v, root)
		}
		if h := resolveLoose(t, home); strings.HasPrefix(resolveLoose(t, v), h+sep) {
			t.Errorf("env %s=%s is under the user's home %s", name, v, home)
		}
		d, ok := c.dirs[name]
		if !ok {
			continue
		}
		if e := resolveLoose(t, d.Existing); e != wantRoot && !strings.HasPrefix(e, wantRoot+sep) {
			t.Errorf("when the run started, the nearest existing folder of %s was %s, want the cache root %s or a folder in it", name, d.Existing, root)
		}
		if !d.Writable {
			t.Errorf("when the run started, %s of %s was not writable", d.Existing, name)
		}
	}
	xdg := map[string]bool{}
	for _, name := range []string{"XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		xdg[resolveLoose(t, c.env[name])] = true
	}
	if len(xdg) != 3 {
		t.Errorf("XDG_CACHE_HOME, XDG_DATA_HOME and XDG_STATE_HOME are %q, %q, %q: want three different folders", c.env["XDG_CACHE_HOME"], c.env["XDG_DATA_HOME"], c.env["XDG_STATE_HOME"])
	}
}

// resolveLoose is p cleaned with symlinks resolved, though p, or part of it, may not exist yet:
// the nearest existing folder is resolved and the rest appended. macOS temp folders sit behind a
// symlink.
func resolveLoose(t *testing.T, p string) string {
	t.Helper()
	p = filepath.Clean(p)
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest, p = filepath.Join(filepath.Base(p), rest), parent
	}
}

func (f *repoFixture) modeBot(t *testing.T, provider, mode string) bot {
	t.Helper()
	var b bot
	f.s.do(t, "POST", "/api/bots", map[string]any{"name": "Cache Bot", "persona": "Fix carefully.", "provider": provider, "mode": mode, "repo_access": "all"}, &b, 201)
	return b
}

// runThread opens a repo thread of b and sends each prompt in turn, waiting for it to finish.
func (f *repoFixture) runThread(t *testing.T, b bot, prompts ...string) {
	t.Helper()
	th := f.thread(t, b)
	ch := f.s.events(t, th)
	for _, prompt := range prompts {
		r := f.s.send(t, th, prompt)
		if done := f.s.finished(t, ch, r.ID); done.Status != "done" {
			t.Fatalf("run %q: %+v", prompt, done)
		}
	}
}

// plantUserCaches makes the user's Go, pnpm and golangci-lint caches, with files in them, and
// returns the folders.
func plantUserCaches(t *testing.T, home string) []string {
	t.Helper()
	for _, p := range []string{".cache/go-build/ab/entry", ".cache/golangci-lint/entry", ".cache/pnpm/store/entry", "go/pkg/mod/entry"} {
		writeFile(t, filepath.Join(home, p), "user's own\n")
	}
	return []string{filepath.Join(home, ".cache"), filepath.Join(home, "go")}
}

// snapshot lists every file and folder under dirs with its mode, mtime and contents.
func snapshot(t *testing.T, dirs ...string) string {
	t.Helper()
	var sb strings.Builder
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			body := ""
			if info.Mode().IsRegular() {
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				body = string(b)
			}
			sb.WriteString(p + " " + info.Mode().String() + " " + info.ModTime().String() + " " + body + "\n")
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return sb.String()
}
