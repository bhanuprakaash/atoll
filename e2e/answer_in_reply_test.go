package e2e

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Contract: prompt-contract.md, "The reply carries the answer".

func TestPrompt_ReplyCarriesAnswerRule(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			repoPath := t.TempDir()
			gitIn(t, repoPath, "init", "-b", "main")
			writeFile(t, filepath.Join(repoPath, "answer.txt"), "41\n")
			gitIn(t, repoPath, "add", "answer.txt")
			gitIn(t, repoPath, "commit", "-m", "seed")
			s.do(t, "POST", "/api/repos", map[string]string{"name": "seeded", "path": repoPath}, nil, http.StatusCreated)
			bots := map[string]bot{"chief": chief(t, s).bot}
			for name, extra := range map[string]map[string]any{
				"plain":    {"repo_access": []string{}},
				"repo":     {"repo_access": "all"},
				"platform": {"repo_access": "all", "can_manage_platform": true},
			} {
				req := map[string]any{"name": name, "persona": "Answers.", "provider": p}
				for k, v := range extra {
					req[k] = v
				}
				var b bot
				s.do(t, "POST", "/api/bots", req, &b, http.StatusCreated)
				bots[name] = b
			}
			for name, b := range bots {
				system := heySystem(t, s, p, b)
				finish := strings.ToLower(strings.Join(section(t, system, "Finish"), "\n"))
				lines := strings.Split(finish, "\n")
				has := func(words ...string) bool {
					for _, l := range lines {
						ok := true
						for _, w := range words {
							ok = ok && strings.Contains(l, w)
						}
						if ok {
							return true
						}
					}
					return false
				}
				if !has("reply", "answer", "itself") {
					t.Errorf("%s: Finish does not say the reply holds the answer itself:\n%s", name, finish)
				}
				if !has("2-3", "status", "not", "answer") {
					t.Errorf("%s: Finish does not limit 2-3 lines to status, not an asked-for answer:\n%s", name, finish)
				}
				if !has("finish_run", "never", "answer") || !has("never", "thinking", "above") {
					t.Errorf("%s: Finish does not forbid the answer living only in finish_run, thinking or \"above\":\n%s", name, finish)
				}
				if n := strings.Count(system, "2-3"); n != 1 {
					t.Errorf("%s: system prompt says 2-3 %d times, want once (core only):\n%s", name, n, system)
				}
				if strings.Contains(strings.ToLower(system), "exempt") {
					t.Errorf("%s: system prompt still has an exemption from the short-reply rule:\n%s", name, system)
				}
			}
			if block := platformBlock(t, heySystem(t, s, p, bots["chief"])); len(block) > _platformBlockMaxBytes {
				t.Errorf("Chief platform block is %d bytes, want at most %d", len(block), _platformBlockMaxBytes)
			}
		})
	}
}
