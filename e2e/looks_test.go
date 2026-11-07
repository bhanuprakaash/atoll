package e2e

import (
	"fmt"
	"testing"
)

// Contract: mac-app-contract.md → Bots.

func TestLooks_RetiredLooksComeBackAsNewOnes(t *testing.T) {
	m, s := start(t, "claude")
	retired := map[string]string{
		"octopus": "shell", "turtle": "kelp", "seahorse": "sun",
		"otter": "sand_dollar", "clownfish": "starfish", "manta": "wave",
	}
	ids := map[string]string{}
	for old := range retired {
		ids[old] = s.newBot(t, "Old "+old, "claude").ID
	}
	s.kill()
	for old, id := range ids {
		dbExec(t, m, fmt.Sprintf(`UPDATE bots SET look = '%s' WHERE id = '%s'`, old, id))
	}

	s = m.serve(t)
	for old, want := range retired {
		if got := s.bot(t, ids[old]).Look; got != want {
			t.Errorf("Bot saved with look %s: look %q after restart, want %q", old, got, want)
		}
	}
}
