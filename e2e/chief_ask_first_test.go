package e2e

import (
	"strings"
	"testing"
)

// Contract: prompt-contract.md, "Platform block text".

func TestPrompt_ChiefAskFirstCarriesProposal(t *testing.T) {
	for _, p := range _providers {
		t.Run(p, func(t *testing.T) {
			_, s := start(t, p)
			system := heySystem(t, s, p, chief(t, s).bot)
			ask := strings.ToLower(strings.Join(section(t, platformBlock(t, system), _askHeading), "\n"))
			if !strings.Contains(ask, "proposal") {
				t.Fatalf("Ask first does not say the message holds the full proposal:\n%s", ask)
			}
			for _, item := range []string{"model", "skills", "repo", "connections"} {
				if !strings.Contains(ask, item) {
					t.Fatalf("Ask first does not name %q in the proposal:\n%s", item, ask)
				}
			}
			if !strings.Contains(ask, "exempt") {
				t.Fatalf("Ask first does not exempt the message from the short-reply rule:\n%s", ask)
			}
		})
	}
}
