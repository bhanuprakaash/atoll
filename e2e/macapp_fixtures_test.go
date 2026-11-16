package e2e

import (
	"time"
)

const (
	_workingPrompt  = "Fix the wizard repo select and run its tests."
	_workingReply   = "Fixed the wizard repo select; tests pass."
	_claudeLoginURL = "https://login.reef.test/claude/oauth?code=true"
	_codexLoginURL  = "https://login.reef.test/codex/oauth?client_id=fake"
	_workingPause   = time.Second // each tool call of the working scenario takes this long
)

var _probeModel = map[string]string{"claude": "reef-probe", "codex": "reef-probe-codex"}
