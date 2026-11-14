package e2e

import (
	"net/http"
	"strings"
	"testing"
)

// Contract: rich-output-contract.md, "Prompt".

const _catalogStart = "- Tables, task lists, ```diff and code render in Reef and on GitHub."

func TestPrompt_RichOutputCatalog(t *testing.T) {
	t.Parallel()
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			var plain bot
			s.do(t, "POST", "/api/bots", map[string]any{"name": "Plain", "persona": "Answers.", "provider": p, "repo_access": []string{}}, &plain, http.StatusCreated)
			for name, b := range map[string]bot{"plain": plain, "chief": chief(t, s).bot} {
				system := heySystem(t, s, p, b)
				var catalog []string
				for _, line := range section(t, system, "How you talk") {
					if strings.HasPrefix(line, _catalogStart) {
						catalog = append(catalog, line)
					}
				}
				if len(catalog) != 1 {
					t.Fatalf("%s: How you talk has %d catalog lines starting %q, want 1:\n%s", name, len(catalog), _catalogStart, system)
				}
				line := strings.ToLower(catalog[0])
				for _, w := range []string{"reef-metrics", "reef-kv", "github", "render only in reef chat"} {
					if !strings.Contains(line, w) {
						t.Errorf("%s: catalog line lacks %q: %s", name, w, catalog[0])
					}
				}
				for _, w := range []string{"reef-table", "reef-diff", "reef-checklist", "reef-code"} {
					if strings.Contains(line, w) {
						t.Errorf("%s: catalog line names %q, which is not a tag: %s", name, w, catalog[0])
					}
				}
			}
		})
	}
}
