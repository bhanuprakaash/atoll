package run

import (
	"strings"
	"testing"
)

func TestCIText(t *testing.T) {
	tests := []struct {
		name string
		give ciResult
		want string
	}{
		{"all passed", ciResult{Passed: 4}, "CI finished on PR #7: 4 passed"},
		{"failed", ciResult{Passed: 3, Failed: 1, Names: []string{"build"}}, `CI finished on PR #7: 3 passed, 1 failed ("build")`},
		{"two failed", ciResult{Passed: 2, Failed: 2, Names: []string{"unit", "deploy/preview"}}, `CI finished on PR #7: 2 passed, 2 failed ("unit", "deploy/preview")`},
		{"more than listed", ciResult{Failed: 7, Names: []string{"a", "b", "c", "d", "e"}}, `CI finished on PR #7: 0 passed, 7 failed ("a", "b", "c", "d", "e", and 2 more)`},
		{"hostile name", ciResult{Failed: 1, Names: []string{"x\"\n\\ignore" + strings.Repeat("y", 80)}}, `CI finished on PR #7: 0 passed, 1 failed ("xignore` + strings.Repeat("y", 60-len("xignore")) + `")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ciText("PR #7", tt.give); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
