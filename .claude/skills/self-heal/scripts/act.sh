#!/bin/sh
# Log one driver action to the shared feed every observer watches. Run it right after the action.
# Usage: act.sh "<what you did, in plain words>" [thread_id|PR|issue] [expected outcome]
D=${REEF_HOME:-$HOME/reef-dogfood}
jq -cn --arg t "$(date -u +%FT%TZ)" --arg us "$(date +%s%6N)" --arg what "$1" --arg ref "${2:-}" --arg expect "${3:-}" \
  '{t:$t, us:($us|tonumber), what:$what, ref:$ref, expect:$expect}' >> "$D/actions.jsonl"
tail -1 "$D/actions.jsonl"
