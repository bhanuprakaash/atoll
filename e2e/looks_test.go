package e2e

import (
	"fmt"
	"testing"
)

func TestLooks_RetiredLooksComeBackAsNewOnes(t *testing.T) {
	t.Parallel()
	m, s := start(t, "claude")
	retired := map[string]string{
		"shark": "whale", "manta": "whale", "wave": "whale",
		"penguin": "seal", "sand_dollar": "seal", "island": "seal", "sailboat": "seal", "otter": "seal",
		"anglerfish": "octopus", "shell": "octopus",
		"pufferfish": "starfish", "sun": "starfish", "seahorse": "starfish",
		"crab": "coral", "scallop": "clam", "clownfish": "clam",
		"narwhal": "turtle", "kelp": "turtle",
		"starfish": "starfish", "sponge": "sponge", "turtle": "turtle", "octopus": "octopus",
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
