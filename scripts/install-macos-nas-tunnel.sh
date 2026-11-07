#!/bin/sh
# Install or remove the launchd agent that keeps the Aurelia NAS tunnel up.
# macOS only: the tunnel uses ssh UseKeychain and a local Unix socket.
set -eu

LABEL="com.aurelia.nas-tunnel"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="$HOME/.aurelia/bin/aurelia-nas-tunnel"
AGENT="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG_DIR="$HOME/.aurelia/logs"
MARKER="$HOME/.aurelia/nas-tunnel.enabled"
ENV_FILE="$HOME/.aurelia/nas-tunnel.env"
# Reuse the installed values so a plain reinstall does not overwrite the real
# host/user with the generic repository defaults.
if [ -f "$ENV_FILE" ]; then
    # shellcheck disable=SC1090
    . "$ENV_FILE"
fi
NAS_HOST="${AURELIA_NAS_HOST:-aurelia-nas}"
NAS_USER="${AURELIA_NAS_USER:-aurelia}"
LOCAL_SOCKET="${AURELIA_NAS_LOCAL_SOCKET:-$HOME/.aurelia/nas.sock}"
REMOTE_SOCKET="${AURELIA_NAS_REMOTE_SOCKET:-/home/$NAS_USER/.aurelia/aurelia.sock}"

usage() {
    echo "usage: $0 [--uninstall]" >&2
    exit 2
}

if [ "$(uname -s)" != "Darwin" ]; then
    echo "this installer is macOS-only" >&2
    exit 1
fi

case "${1:-}" in
    --uninstall)
        launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
        rm -f "$AGENT" "$TARGET" "$MARKER" "$ENV_FILE" "$LOCAL_SOCKET"
        echo "removed $LABEL"
        exit 0
        ;;
    "")
        ;;
    *)
        usage
        ;;
esac

mkdir -p "$HOME/.aurelia/bin" "$LOG_DIR" "$HOME/Library/LaunchAgents"
install -m 755 "$REPO_ROOT/scripts/macos/aurelia-nas-tunnel.sh" "$TARGET"

cat >"$ENV_FILE" <<EOF
AURELIA_NAS_HOST=$NAS_HOST
AURELIA_NAS_USER=$NAS_USER
AURELIA_NAS_REMOTE_SOCKET=$REMOTE_SOCKET
AURELIA_NAS_LOCAL_SOCKET=$LOCAL_SOCKET
EOF
chmod 600 "$ENV_FILE"
: >"$MARKER"
chmod 600 "$MARKER"

cat >"$AGENT" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/sh</string>
        <string>-c</string>
        <string>exec "$TARGET"</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/bin:/bin:/usr/sbin:/sbin</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>15</integer>
    <key>ProcessType</key>
    <string>Background</string>
    <key>LimitLoadToSessionType</key>
    <string>Aqua</string>
    <key>StandardOutPath</key>
    <string>$LOG_DIR/nas-tunnel.launchd.log</string>
    <key>StandardErrorPath</key>
    <string>$LOG_DIR/nas-tunnel.launchd.log</string>
</dict>
</plist>
PLIST
chmod 644 "$AGENT"
plutil -lint "$AGENT"

if ! /usr/bin/ssh -o BatchMode=yes -o UseKeychain=yes -o ConnectTimeout=10 \
    "$NAS_HOST" true >/dev/null 2>&1; then
    echo "warning: SSH to $NAS_HOST failed; the launch agent will keep retrying" >&2
fi

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$AGENT"
launchctl kickstart -k "gui/$(id -u)/$LABEL"

echo "installed $LABEL"
echo "socket: $LOCAL_SOCKET"
echo "logs:   $LOG_DIR/nas-tunnel.log"
echo "remove: $0 --uninstall"
