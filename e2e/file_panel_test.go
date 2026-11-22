package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A Bot reply plays testdata/claude/file-links.jsonl: inline-code paths into the seeded files.
const _filePrompt = "Show me where the file panel code lives."

const _goMod = "module example.test/hello\n\ngo 1.27\n"

type (
	// fileAnswer is GET /api/threads/{id}/file for a text file or a bare name: the file's
	// checkout-relative path, its text, and the checkout's branch, or its short commit when it has
	// none; or, for a bare name with several tracked matches, their paths.
	fileAnswer struct {
		Path    string   `json:"path"`
		Text    string   `json:"text"`
		Branch  string   `json:"branch"`
		Commit  string   `json:"commit"`
		Matches []string `json:"matches"`
	}

	fileRefusal struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
)

// numbered is n lines "// <name> line <i>".
func numbered(name string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "// %s line %d\n", name, i)
	}
	return b.String()
}

// seedFiles commits what the file panel reads into the repo at dir and pushes it to origin.
func seedFiles(t *testing.T, dir string, png []byte) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "go.mod"), _goMod)
	writeFile(t, filepath.Join(dir, "server", "attachments.go"), "package server\n"+numbered("attachments", 199))
	writeFile(t, filepath.Join(dir, "run", "work.go"), "package run\n"+numbered("work", 599))
	writeFile(t, filepath.Join(dir, "cmd", "a", "main.go"), "package main\n\n// a main\n")
	writeFile(t, filepath.Join(dir, "cmd", "b", "main.go"), "package main\n\n// b main\n")
	writeFile(t, filepath.Join(dir, "data", "blob.txt"), "text first\x00then a NUL byte\n")
	writeFile(t, filepath.Join(dir, "data", "huge.txt"), strings.Repeat("a", 1<<20+1))
	writeFile(t, filepath.Join(dir, "shots", "x.png"), string(png))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "seed files")
	gitIn(t, dir, "push", "origin", "main")
}

// readFile GETs a thread file and returns the status and body.
func (s *server) readFile(t *testing.T, th, path string) (int, []byte) {
	t.Helper()
	status, _, body := s.get(t, "/api/threads/"+th+"/file?path="+url.QueryEscape(path))
	return status, body
}

// textFile wants a 200 text answer for path and returns it.
func (s *server) textFile(t *testing.T, th, path string) fileAnswer {
	t.Helper()
	status, body := s.readFile(t, th, path)
	var a fileAnswer
	if status != http.StatusOK || json.Unmarshal(body, &a) != nil {
		t.Fatalf("GET file %q: status %d, body %.300q; want 200 and a JSON text answer", path, status, body)
	}
	return a
}

// refused wants path refused with status and the designed {error, code}; the error's closing
// full stop is optional.
func (s *server) refused(t *testing.T, th, path string, status int, want fileRefusal) {
	t.Helper()
	got, body := s.readFile(t, th, path)
	var r fileRefusal
	if json.Unmarshal(body, &r) == nil {
		r.Error = strings.TrimSuffix(r.Error, ".")
	}
	if got != status || r != want {
		t.Errorf("GET file %q: status %d, body %.300q; want %d %+v", path, got, body, status, want)
	}
}

var _notInCheckout = fileRefusal{Error: "Not in this thread's checkout", Code: "not_found"}

// TestFilePanel_ReadOnlyCheckout: GET /api/threads/{id}/file reads text from the thread's read-only
// checkout with its short commit, resolves a bare name through tracked files, refuses outside,
// binary and too large files with designed answers, and still serves images.
func TestFilePanel_ReadOnlyCheckout(t *testing.T) {
	t.Parallel()
	f := newRepoFixture(t, "claude")
	png := tinyPNG(t, color.RGBA{R: 255, A: 255})
	seedFiles(t, f.repo.Path, png)
	b := f.bot(t, "claude", "all", nil)
	th := f.thread(t, b)
	f.ask(t, th, _filePrompt)
	dir := f.checkoutDir(th)
	head := gitIn(t, dir, "rev-parse", "HEAD")
	if gitIn(t, dir, "show", "HEAD:go.mod") != strings.TrimSpace(_goMod) {
		t.Fatal("fixture: the read-only checkout lacks the seeded go.mod")
	}

	got := f.s.textFile(t, th.ID, "go.mod")
	if got.Text != _goMod || got.Path != "go.mod" || got.Branch != "" || got.Commit != head[:7] {
		t.Errorf("go.mod answer %+v, want path go.mod, its text, no branch and commit %s (the detached checkout's short HEAD)", got, head[:7])
	}
	if abs := f.s.textFile(t, th.ID, filepath.Join(dir, "go.mod")); abs.Text != _goMod {
		t.Errorf("absolute path inside the checkout: text %q, want go.mod's", abs.Text)
	}
	if w := f.s.textFile(t, th.ID, "run/work.go"); !strings.Contains(w.Text, "// work line 530\n") || w.Path != "run/work.go" {
		t.Errorf("run/work.go answer path %q, %d bytes; want the whole file", w.Path, len(w.Text))
	}

	one := f.s.textFile(t, th.ID, "attachments.go")
	if one.Path != "server/attachments.go" || !strings.Contains(one.Text, "// attachments line 74\n") || len(one.Matches) > 1 {
		t.Errorf("bare name with one tracked match: %+v, want server/attachments.go and its text", one)
	}
	several := f.s.textFile(t, th.ID, "main.go")
	slices.Sort(several.Matches)
	if !slices.Equal(several.Matches, []string{"cmd/a/main.go", "cmd/b/main.go"}) || several.Text != "" {
		t.Errorf("bare name with two tracked matches: %+v, want matches [cmd/a/main.go cmd/b/main.go] and no text", several)
	}

	elsewhere := filepath.Join(t.TempDir(), "elsewhere.go")
	writeFile(t, elsewhere, "package elsewhere\n")
	writeFile(t, filepath.Join(filepath.Dir(dir), "x"), "outside the checkout\n")
	if err := os.Symlink(elsewhere, filepath.Join(dir, "link.go")); err != nil {
		t.Fatal(err)
	}
	missing, missingBody := f.s.readFile(t, th.ID, "docs/gone.md")
	for _, p := range []string{"docs/gone.md", "nowhere.go", "../x", elsewhere, "link.go"} {
		f.s.refused(t, th.ID, p, http.StatusNotFound, _notInCheckout)
		if status, body := f.s.readFile(t, th.ID, p); status != missing || !bytes.Equal(body, missingBody) {
			t.Errorf("GET file %q: %d %s, want the same answer as a missing file: %d %s", p, status, body, missing, missingBody)
		}
	}

	f.s.refused(t, th.ID, "data/blob.txt", http.StatusUnsupportedMediaType, fileRefusal{Error: "This is a binary file", Code: "binary"})
	f.s.refused(t, th.ID, "data/huge.txt", http.StatusRequestEntityTooLarge, fileRefusal{Error: "Too large to show here (over 1 MB)", Code: "too_large"})
	f.s.wantImage(t, "/api/threads/"+th.ID+"/file?path="+url.QueryEscape("shots/x.png"), "image/png", png)
}

// TestFilePanel_WorktreeBranch: a thread with a worktree reads from it, as the file is now, and the
// answer carries its branch.
func TestFilePanel_WorktreeBranch(t *testing.T) {
	t.Parallel()
	f := newRepoFixture(t, "claude")
	seedFiles(t, f.repo.Path, tinyPNG(t, color.RGBA{G: 255, A: 255}))
	b := f.bot(t, "claude", "all", nil)
	th, work, _, _ := f.work(t, b, _unfinishedRepoPrompt, "fix/seeded-bug")

	got := f.s.textFile(t, th.ID, "go.mod")
	if got.Text != _goMod || got.Branch != "fix/seeded-bug" {
		t.Errorf("go.mod in the worktree: %+v, want its text and branch fix/seeded-bug", got)
	}
	if a := f.s.textFile(t, th.ID, "answer.txt"); a.Text != "42\n" {
		t.Errorf("answer.txt: text %q, want the uncommitted edit 42", a.Text)
	}
	if a := f.s.textFile(t, th.ID, filepath.Join(work, "answer.txt")); a.Text != "42\n" {
		t.Errorf("absolute path inside the worktree: text %q, want 42", a.Text)
	}
}

// TestFilePanel_HelperOwnCheckout: a helper thread reads its own worktree, the boss thread the
// boss's.
func TestFilePanel_HelperOwnCheckout(t *testing.T) {
	t.Parallel()
	f := newRepoFixture(t, "claude")
	seedFiles(t, f.repo.Path, tinyPNG(t, color.RGBA{B: 255, A: 255}))
	s := f.s
	poteto, tester := s.newBot(t, "Poteto", "claude"), s.newBot(t, "Tester", "claude")
	setDelegates(t, s, poteto, tester)
	boss := f.thread(t, poteto)
	ev := s.events(t, boss)
	own := bossWorktree(t, s, boss, ev)

	h := handedOff(t, useAtoll(t, s, boss, ev, delegateCall("Tester", _filePrompt, ""))[0])
	if r := settled(t, s, h.ThreadID); r.Status != "done" {
		t.Fatalf("helper run %+v, want done", r)
	}
	helperWork := worktreeOf(t, f.repo.Path, own.Branch+"--tester")
	if helperWork == "" {
		t.Fatalf("no helper worktree on %s--tester", own.Branch)
	}
	writeFile(t, filepath.Join(own.Path, "who.md"), "boss\n")
	writeFile(t, filepath.Join(helperWork, "who.md"), "helper\n")

	if got := s.textFile(t, h.ThreadID, "who.md"); got.Text != "helper\n" || got.Branch != own.Branch+"--tester" {
		t.Errorf("helper thread who.md: %+v, want the helper's text and branch %s--tester", got, own.Branch)
	}
	if got := s.textFile(t, boss.ID, "who.md"); got.Text != "boss\n" || got.Branch != own.Branch {
		t.Errorf("boss thread who.md: %+v, want the boss's text and branch %s", got, own.Branch)
	}
}
