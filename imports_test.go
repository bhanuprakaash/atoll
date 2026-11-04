package atoll_test

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// _allowed lists, per package, the packages of ours it may import. A package not listed fails.
var _allowed = map[string][]string{
	"atoll/cmd/atoll": {
		"atoll/server", "atoll/run", "atoll/provider", "atoll/provider/claude",
		"atoll/provider/codex", "atoll/memory", "atoll/repo", "atoll/skill", "atoll/connection",
		"atoll/store",
	},
	"atoll/server": {
		"atoll/run", "atoll/provider", "atoll/memory", "atoll/repo", "atoll/skill",
		"atoll/connection", "atoll/store",
	},
	"atoll/run": {
		"atoll/provider", "atoll/memory", "atoll/repo", "atoll/skill", "atoll/connection",
		"atoll/store",
	},
	"atoll/provider":        {},
	"atoll/provider/claude": {"atoll/provider"},
	"atoll/provider/codex":  {"atoll/provider"},
	"atoll/memory":          {"atoll/store"},
	"atoll/repo":            {"atoll/store"},
	"atoll/skill":           {"atoll/store"},
	"atoll/connection":      {"atoll/store"},
	"atoll/store":           {},
}

func TestImports_Directions(t *testing.T) {
	out, err := exec.Command("go", "list", "-json", "./...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p struct {
			ImportPath                         string
			Imports, TestImports, XTestImports []string
		}
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		if p.ImportPath == "atoll" || strings.HasPrefix(p.ImportPath, "atoll/e2e") ||
			strings.HasPrefix(p.ImportPath, "atoll/web/node_modules") {
			continue
		}
		allowed, ok := _allowed[p.ImportPath]
		if !ok {
			t.Errorf("%s has no import rule: add it to _allowed", p.ImportPath)
			continue
		}
		for _, imp := range slices.Concat(p.Imports, p.TestImports, p.XTestImports) {
			if strings.HasPrefix(imp, "atoll/") && imp != p.ImportPath && !slices.Contains(allowed, imp) {
				t.Errorf("%s imports %s", p.ImportPath, imp)
			}
		}
	}
}
