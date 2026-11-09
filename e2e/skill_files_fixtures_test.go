package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"atoll/e2e/fakes/fake"
)

// Contract: skill-files-contract.md.

const (
	_potetoDir  = "~/.claude/skills/poteto-mode"
	_potetoHome = "~/.claude/skills/poteto-mode"
	_skillDir   = "{{ATOLL_SKILL_DIR}}"
	_potetoMD   = "---\nname: poteto-mode\ndescription: Work the poteto way.\n---\n" +
		"Scripts: `S=~/.claude/skills/poteto-mode/scripts`. Always call scripts by that full path.\n" +
		"Proof goes in .reef-verify/<id> when a run ends, and notes in .poteto/state while it runs.\n" +
		"Read playbooks/ship.md before shipping. Proof is still in .reef-verify/<id> every time.\n"
	_checkScript = "#!/bin/sh\necho check-ok\n"
)

var (
	_potetoScripts   = []string{"build", "check", "cleanup", "lint", "report", "snapshot", "status", "sync", "test", "verify"}
	_potetoPlaybooks = []string{"audit", "debug", "design", "explore", "fix", "improve", "interrogate", "plan", "review", "ship", "verify"}
)

type (
	// skillFile is a file of a skill besides SKILL.md, as the API has it.
	skillFile struct {
		Path       string `json:"path"`
		Content    string `json:"content"`
		Executable bool   `json:"executable"`
	}

	fileSkill struct {
		ID      string      `json:"id"`
		Name    string      `json:"name"`
		Content string      `json:"content"`
		Source  string      `json:"source"`
		Files   []skillFile `json:"files"`
	}

	// fixtureFile is a file of a local skill folder.
	fixtureFile struct {
		path, content string
		exec          bool
	}
)

// potetoFixture is what ~/.claude/skills/poteto-mode holds: SKILL.md and 22 files.
func potetoFixture() []fixtureFile {
	files := []fixtureFile{{path: "SKILL.md", content: _potetoMD}}
	for _, name := range _potetoScripts {
		content := "#!/bin/sh\necho " + name + "-ok\n"
		if name == "check" {
			content = _checkScript
		}
		files = append(files, fixtureFile{path: "scripts/" + name, content: content, exec: true})
	}
	for _, name := range _potetoPlaybooks {
		files = append(files, fixtureFile{path: "playbooks/" + name + ".md", content: "# " + name + "\nRun ~/.claude/skills/poteto-mode/scripts/check first.\n"})
	}
	return append(files, fixtureFile{path: "references/improve-report.md", content: "# Improve report\nHow to write one.\n"})
}

// stored is text as Reef stores it: the skill's own folder as the placement token.
func stored(text string) string {
	return strings.ReplaceAll(text, _potetoHome, _skillDir)
}

// potetoPlaced is SKILL.md as Reef stores it.
func potetoPlaced() string {
	return stored(_potetoMD)
}

func writeFixture(t *testing.T, root string, files []fixtureFile) {
	t.Helper()
	for _, f := range files {
		path := filepath.Join(root, filepath.FromSlash(f.path))
		writeFile(t, path, f.content)
		mode := os.FileMode(0o644)
		if f.exec {
			mode = 0o755
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
}

// libraryFiles is what the library should hold of a fixture: everything but SKILL.md, by path,
// with the skill's own folder as the placement token.
func libraryFiles(files []fixtureFile) []skillFile {
	var out []skillFile
	for _, f := range files {
		if f.path != "SKILL.md" {
			out = append(out, skillFile{Path: f.path, Content: stored(f.content), Executable: f.exec})
		}
	}
	slices.SortFunc(out, func(a, b skillFile) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// withFile returns files with path set to content, added when absent.
func withFile(files []fixtureFile, path, content string) []fixtureFile {
	out := slices.Clone(files)
	for i := range out {
		if out[i].path == path {
			out[i].content = content
			return out
		}
	}
	return append(out, fixtureFile{path: path, content: content})
}

func withoutFile(files []fixtureFile, path string) []fixtureFile {
	return slices.DeleteFunc(slices.Clone(files), func(f fixtureFile) bool { return f.path == path })
}

// treeSum hashes every path, mode, modification time and byte under root.
func treeSum(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s %o %d\n", path, info.Mode(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = h.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// rawDo sends a request body as given, so a test can send bytes no JSON encoder would, and
// returns the reply body.
func (s *server) rawDo(t *testing.T, method, path string, body []byte, want int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, s.url+path, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d; body: %s", method, path, resp.StatusCode, want, b)
	}
	return b
}

// seedPotetoBot creates the skill poteto-mode with files and a Bot that has it.
func seedPotetoBot(t *testing.T, s *server, p string, files []skillFile) (platformBot, fileSkill) {
	t.Helper()
	var skill fileSkill
	s.do(t, "POST", "/api/skills", map[string]any{"name": "poteto-mode", "content": potetoPlaced(), "files": files}, &skill, 201)
	var b platformBot
	s.do(t, "POST", "/api/bots", map[string]any{"name": "Skilled", "persona": "A guide.", "provider": p, "skills": []string{skill.ID}}, &b, 201)
	return b, skill
}

// skillInventories is every skill listing the fake logged, in order: each run's discovery, and
// a held run's second look once released.
func skillInventories(t *testing.T, m *machine, p string) [][]fake.SkillCopy {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.bin, p+".log.skills"))
	if err != nil {
		t.Fatal(err)
	}
	var all [][]fake.SkillCopy
	for line := range strings.Lines(string(data)) {
		var entry struct {
			Skills []fake.SkillCopy `json:"skills"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		all = append(all, entry.Skills)
	}
	return all
}

// potetoCopy is the poteto-mode skill of a listing.
func potetoCopy(t *testing.T, listing []fake.SkillCopy) fake.SkillCopy {
	t.Helper()
	for _, c := range listing {
		if c.Name == "poteto-mode" {
			return c
		}
	}
	t.Fatalf("poteto-mode not in %+v", listing)
	return fake.SkillCopy{}
}

// placedDir is where a run's copy of poteto-mode lives: in the run's folder under the provider's
// Atoll folder, never under HOME.
func placedDir(m *machine, p, runID string) string {
	dir := filepath.Join(m.providerDir(p), "runs", runID)
	if p == "claude" {
		dir = filepath.Join(dir, ".claude", "skills")
	}
	return filepath.Join(dir, "poteto-mode")
}

// checkPlaced compares a placed folder at dir with SKILL.md and files: every file is there with its
// bytes, the placement token replaced by dir, scripts marked executable 0555 and the rest,
// SKILL.md included, 0444.
func checkPlaced(t *testing.T, got []fake.SkillFile, dir string, files []skillFile) {
	t.Helper()
	want := []fake.SkillFile{{Path: "SKILL.md", Mode: 0o444, Text: strings.ReplaceAll(potetoPlaced(), _skillDir, dir)}}
	for _, f := range files {
		mode := uint32(0o444)
		if f.Executable {
			mode = 0o555
		}
		want = append(want, fake.SkillFile{Path: f.Path, Mode: mode, Text: strings.ReplaceAll(f.Content, _skillDir, dir)})
	}
	got = slices.Clone(got)
	for _, list := range [][]fake.SkillFile{got, want} {
		slices.SortFunc(list, func(a, b fake.SkillFile) int { return strings.Compare(a.Path, b.Path) })
	}
	if len(got) != len(want) {
		t.Fatalf("placed %d files, want %d:\n%s\nwant:\n%s", len(got), len(want), placedNames(got), placedNames(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("placed %s mode %o text %q, want %s mode %o text %q", got[i].Path, got[i].Mode, got[i].Text, want[i].Path, want[i].Mode, want[i].Text)
		}
	}
}

func placedNames(files []fake.SkillFile) string {
	var out []string
	for _, f := range files {
		out = append(out, fmt.Sprintf("%s %o", f.Path, f.Mode))
	}
	return strings.Join(out, "\n")
}

// folderGone waits for a run's folder to be removed: it goes when the run ends.
func folderGone(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run folder %s still there after the run ended", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitReady waits for the fake to hold after discovering its skills and returns its marker file.
func waitReady(t *testing.T, m *machine, s *server, p, runID string) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if paths, _ := filepath.Glob(filepath.Join(m.bin, p+".ready.*")); len(paths) > 0 {
			return paths[0]
		}
		if run := s.run(t, runID); run.Status == "failed" {
			t.Fatalf("hold: %+v", run)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("skill discovery barrier never reached")
	return ""
}

// TestSkillFilesFixtures_ProviderScenarios drives the two new scenarios of both fakes with no
// product: a folder placed by hand, and an MCP endpoint that answers import_skill.
func TestSkillFilesFixtures_ProviderScenarios(t *testing.T) {
	for _, p := range _providers {
		t.Run(p+"/run", func(t *testing.T) {
			for _, prompt := range []string{"Run the poteto-mode script scripts/check.", "Wait, then run the poteto-mode script scripts/check."} {
				m := newMachine(t, p)
				extra := t.TempDir()
				root := extra
				if p == "claude" {
					root = filepath.Join(extra, ".claude", "skills")
				}
				dir := filepath.Join(root, "poteto-mode")
				writeFixture(t, dir, []fixtureFile{{path: "SKILL.md", content: potetoPlaced()}, {path: "scripts/check", content: _checkScript, exec: true}})
				stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					serveStub(t, w, r, nil, nil)
				}))
				defer stub.Close()
				released := make(chan error, 1)
				go func() {
					if !strings.HasPrefix(prompt, "Wait") {
						released <- nil
						return
					}
					for range 500 {
						if paths, _ := filepath.Glob(filepath.Join(m.bin, p+".ready.*")); len(paths) > 0 {
							released <- os.WriteFile(paths[0]+".release", nil, 0o600)
							return
						}
						time.Sleep(20 * time.Millisecond)
					}
					released <- fmt.Errorf("fake never signaled skills ready")
				}()
				out := runSkillFake(t, m, p, t.TempDir(), extra, prompt, stub.URL, nil)
				if err := <-released; err != nil {
					t.Fatal(err)
				}
				if want := "Ran " + filepath.Join(dir, "scripts", "check") + ": check-ok"; !strings.Contains(out, want) {
					t.Fatalf("%q: no %q in %s", prompt, want, out)
				}
				listings := skillInventories(t, m, p)
				placed := potetoCopy(t, listings[len(listings)-1])
				if len(placed.Files) != 2 || placed.Files[1].Path != "scripts/check" || placed.Files[1].Mode != 0o755 {
					t.Fatalf("%q: files %+v", prompt, placed.Files)
				}
			}
		})
		t.Run(p+"/import", func(t *testing.T) {
			tests := []struct {
				name, prompt, result, want string
				replace                    bool
			}{
				{"created", "Import the skill folder /skills/poteto-mode", `{"status":"created","skill":{"id":"s1","name":"poteto-mode"},"added":[],"changed":[],"removed":[],"skipped":[{"path":".git","reason":"git"},{"path":"a.png","reason":"binary"}],"home_paths":[]}`,
					"Import created: poteto-mode. Skipped: .git (git), a.png (binary).", false},
				{"confirm", "Import the skill folder /skills/poteto-mode", `{"status":"confirm","added":["a"],"changed":["b"],"removed":["c"],"skipped":[],"home_paths":[],"warning":"Reef edits\nwould be overwritten."}`,
					"Import needs confirmation: Reef edits would be overwritten. Added: a. Changed: b. Removed: c.", false},
				{"replaced", "Yes, replace the skill folder /skills/poteto-mode", `{"status":"replaced","skill":{"id":"s1","name":"poteto-mode"},"added":[],"changed":["b"],"removed":[],"skipped":[],"home_paths":[]}`,
					"Import replaced: poteto-mode. Skipped: none.", true},
			}
			for _, tt := range tests {
				m := newMachine(t, p)
				var args map[string]any
				stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					serveStub(t, w, r, &args, &tt.result)
				}))
				defer stub.Close()
				out := runSkillFake(t, m, p, t.TempDir(), t.TempDir(), tt.prompt, stub.URL, nil)
				reply, _ := json.Marshal(tt.want + "\n" + tt.result)
				if !strings.Contains(out, string(reply[1:len(reply)-1])) {
					t.Fatalf("%s: reply %q then the result is not in %s", tt.name, tt.want, out)
				}
				if _, replace := args["replace"]; args["path"] != "/skills/poteto-mode" || replace != tt.replace {
					t.Fatalf("%s: import_skill args %v", tt.name, args)
				}
			}
		})
	}
}

// serveStub is an MCP endpoint with every platform tool and import_skill; import_skill answers
// result and records its arguments.
func serveStub(t *testing.T, w http.ResponseWriter, r *http.Request, args *map[string]any, result *string) {
	t.Helper()
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
		w.WriteHeader(400)
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(202)
		return
	}
	var out any = map[string]any{}
	switch req.Method {
	case "initialize":
		out = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]string{"name": "fixture", "version": "0"}}
	case "tools/list":
		tools := []map[string]any{{"name": "finish_run", "inputSchema": map[string]any{"type": "object"}}}
		for _, name := range fake.PlatformTools() {
			tools = append(tools, map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}})
		}
		tools = append(tools, map[string]any{"name": "import_skill", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}, "replace": map[string]string{"type": "boolean"}}}})
		out = map[string]any{"tools": tools}
	case "tools/call":
		text := "Report saved."
		if req.Params.Name == "import_skill" && args != nil {
			*args, text = req.Params.Arguments, *result
		}
		out = map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": out})
}
