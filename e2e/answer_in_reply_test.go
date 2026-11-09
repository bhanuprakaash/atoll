package e2e

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Contract: prompt-contract.md, "The reply carries the answer".

// said reports whether one sentence or line of text holds a word from every group.
func said(text string, groups ...[]string) bool {
	for _, sentence := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '.' }) {
		if sentenceHas(sentence, groups) {
			return true
		}
	}
	return false
}

func sentenceHas(sentence string, groups [][]string) bool {
	for _, g := range groups {
		if !slices.ContainsFunc(g, func(w string) bool { return strings.Contains(sentence, w) }) {
			return false
		}
	}
	return true
}

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
				if !said(finish, []string{"reply", "response"}, []string{"answer"}, []string{"itself", "in full", "carries", "holds", "contains", "includes", "has the"}) {
					t.Errorf("%s: Finish does not say the reply holds the answer itself:\n%s", name, finish)
				}
				if !said(finish, []string{"2-3"}, []string{"status", "progress", "what you did", "what was done"}, []string{"answer"}, []string{"not", "never", "n't", "no "}) {
					t.Errorf("%s: Finish does not limit 2-3 lines to status, not an asked-for answer:\n%s", name, finish)
				}
				if !said(finish, []string{"finish_run"}, []string{"report"}, []string{"answer"}, []string{"never", "not", "n't", "no "}) || !said(finish, []string{"thinking"}, []string{"answer"}, []string{"never", "not", "n't", "only"}) {
					t.Errorf("%s: Finish does not say the answer is never only in finish_run or thinking:\n%s", name, finish)
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
