package e2e

import (
	"fmt"
	"testing"
)

// Contract: mac-app-contract.md → Bots.

func TestLooks_RetiredLooksComeBackAsNewOnes(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	retired := map[string]string{
		"starfish": "scallop", "sand_dollar": "seal", "sponge": "shark", "kelp": "narwhal",
		"shell": "octopus", "wave": "manta", "island": "seal", "sun": "pufferfish", "sailboat": "seal",
		"turtle": "narwhal", "seahorse": "pufferfish", "otter": "seal", "clownfish": "scallop",
		"octopus": "octopus", "manta": "manta",
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
