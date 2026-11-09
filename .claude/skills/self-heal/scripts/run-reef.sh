#!/bin/sh
# Start Reef with its own persistent Secret Service, so sign-ins survive restarts. Linux only.
# Run in the background; log goes wherever the caller sends stdout/stderr.
set -eu
D=${REEF_HOME:-$HOME/reef-dogfood}
if [ "${REEF_SECRET_SESSION:-}" != 1 ]; then
    mkdir -p "$D/keyring/data" "$D/keyring/run"; chmod 700 "$D/keyring/run"
    export XDG_DATA_HOME=$D/keyring/data XDG_RUNTIME_DIR=$D/keyring/run
    exec dbus-run-session -- env REEF_SECRET_SESSION=1 "$0" "$@"
fi
printf 'reef-dogfood\n' | gnome-keyring-daemon --unlock --components=secrets >/dev/null
printf probe | secret-tool store --label=probe service reef-probe account probe
secret-tool clear service reef-probe account probe
echo "keyring ok"
exec env -u GH_TOKEN -u GITHUB_TOKEN -u CLAUDE_CODE_MESSAGING_TOKEN ATOLL_DATA_DIR="$D/data" "$D/bin/atoll" serve
