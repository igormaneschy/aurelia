#!/bin/bash
# aurelia-jail.sh — run the Aurelia daemon under ai-jail.
#
# This is an accident-prevention profile for a personal assistant that runs as
# the personal owner. It is not an OS privilege boundary against hostile code.
# The profile permits network research, grants read/write access to the
# personal root, and denies service/credential directories, Docker control,
# the ai-memory database/wiki, and the NAS recycle directory.
#
# Installed root-owned at /usr/local/lib/aurelia/aurelia-jail by
# install-systemd.sh. Paths are hardcoded so an agent-writable environment file
# cannot redirect the launcher. A missing ai-jail is a hard failure: the
# caller must not silently fall back to an unrestricted run.

set -euo pipefail

AI_JAIL_BIN="/usr/local/bin/ai-jail"
AURELIA_BINARY="$HOME/.aurelia/bin/aurelia"

if [[ ! -x "$AI_JAIL_BIN" ]]; then
    echo "aurelia-jail: ai-jail not executable: $AI_JAIL_BIN" >&2
    exit 1
fi
if [[ ! -x "$AURELIA_BINARY" ]]; then
    echo "aurelia-jail: Aurelia binary not executable: $AURELIA_BINARY" >&2
    exit 1
fi

DENY_PATHS=(
    "$HOME/.ssh"
    "$HOME/.config"
    "$HOME/.cache"
    "$HOME/.docker"
    "$HOME/.npmrc"
    "$HOME/.pi/agent/sessions"
    "$HOME/.pi/agent/bin"
    "$HOME/.local/share/ai-memory/db"
    "$HOME/.local/share/ai-memory/wiki"
    "$HOME/.local/share/ai-memory/raw"
    "$HOME/.local/share/ai-memory/config.toml"
    "$HOME/.local/share/ai-memory/logs"
    "$HOME/.local/share/ai-memory/models"
    "$HOME/.local/share/ai-memory/auth-tokens"
    "$HOME/.local/share/ai-memory/.serve.lock"
    "$HOME/.local/share/ai-memory/.serve.lock.holder"
    "$HOME/#recycle"
)

ARGS=(--network --no-save-config --rw-map "$HOME")
for path in "${DENY_PATHS[@]}"; do
    ARGS+=(--deny-path "$path")
done

# ai-jail starts with a clear environment. Forward only the product switches
# and paths that must reach the daemon; --env copies the value from the parent
# without putting it on the command line.
for name in \
    AURELIA_ENABLE_TELEGRAM \
    AURELIA_ENABLE_CRON \
    AURELIA_HOME \
    AURELIA_ALLOWED_CWD_PREFIXES; do
    if printenv "$name" >/dev/null 2>&1; then
        ARGS+=(--env "$name")
    fi
done

exec "$AI_JAIL_BIN" "${ARGS[@]}" -- "$AURELIA_BINARY" "$@"
