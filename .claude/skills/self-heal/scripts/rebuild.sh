#!/bin/sh
# Merge the given PR branches into the local integration branch, build Reef, and swap the binary.
# Restart Reef yourself afterwards (stop the old `atoll serve`, start run-reef.sh again in the background).
# Usage: rebuild.sh [origin/<branch> ...]
set -eu
D=${REEF_HOME:-$HOME/reef-dogfood}
cd "$D/build"
git fetch -q origin
git merge -q --no-edit origin/main
for b in "$@"; do git merge -q --no-edit "$b"; done
make ui >/dev/null
go build -o "$D/bin/atoll.new" ./cmd/atoll
git log --oneline -1
echo "built $D/bin/atoll.new; stop Reef, mv it over bin/atoll, start run-reef.sh"
