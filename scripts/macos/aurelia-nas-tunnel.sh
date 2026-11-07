#!/bin/sh
# SSH streamlocal tunnel to the Aurelia daemon running on the NAS.
# Installed by scripts/install-macos-nas-tunnel.sh and kept alive by launchd.
set -eu
umask 077

HOME="${HOME:?HOME is not set}"

ENV_FILE="$HOME/.aurelia/nas-tunnel.env"
if [ -f "$ENV_FILE" ]; then
    # shellcheck disable=SC1090
    . "$ENV_FILE"
fi

NAS_HOST="${AURELIA_NAS_HOST:-aurelia-nas}"
NAS_USER="${AURELIA_NAS_USER:-aurelia}"
LOCAL_SOCKET="${AURELIA_NAS_LOCAL_SOCKET:-$HOME/.aurelia/nas.sock}"
REMOTE_SOCKET="${AURELIA_NAS_REMOTE_SOCKET:-/home/$NAS_USER/.aurelia/aurelia.sock}"
LOG="$HOME/.aurelia/logs/nas-tunnel.log"

/bin/mkdir -p "$HOME/.aurelia/logs" "$(/usr/bin/dirname "$LOCAL_SOCKET")"
if [ -f "$LOG" ] && [ "$(/usr/bin/wc -c <"$LOG")" -gt 1048576 ]; then
    /bin/mv -f "$LOG" "$LOG.1"
fi
: >>"$LOG"
/bin/chmod 600 "$LOG"
exec >>"$LOG" 2>&1

# The local daemon, when running, owns ~/.aurelia/aurelia.sock. The NAS tunnel
# uses a separate socket, but refuse to start if a local daemon is active so a
# stale forward can never be mistaken for local state.
if /usr/bin/pgrep -x aurelia >/dev/null 2>&1; then
    echo "$(/bin/date -u +%Y-%m-%dT%H:%M:%SZ) local aurelia daemon is running; refusing to replace $LOCAL_SOCKET"
    exit 1
fi
if [ -S "$LOCAL_SOCKET" ]; then
    /bin/rm -f "$LOCAL_SOCKET"
fi

echo "$(/bin/date -u +%Y-%m-%dT%H:%M:%SZ) connecting $NAS_HOST -> $REMOTE_SOCKET (local $LOCAL_SOCKET)"
exec /usr/bin/ssh -N -T \
    -o BatchMode=yes \
    -o UseKeychain=yes \
    -o IdentitiesOnly=yes \
    -o PreferredAuthentications=publickey \
    -o PasswordAuthentication=no \
    -o KbdInteractiveAuthentication=no \
    -o StrictHostKeyChecking=yes \
    -o ExitOnForwardFailure=yes \
    -o ServerAliveInterval=30 \
    -o ServerAliveCountMax=3 \
    -o TCPKeepAlive=yes \
    -o ConnectTimeout=15 \
    -o ControlMaster=no \
    -o ControlPath=none \
    -o StreamLocalBindUnlink=no \
    -L "$LOCAL_SOCKET:$REMOTE_SOCKET" \
    "$NAS_HOST"
