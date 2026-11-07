#!/bin/bash
# install-systemd.sh — install Aurelia as a user systemd service.
#
# Idempotent: safe to re-run. Existing service file is overwritten.
#
# Paths follow the convention used by ~/.aurelia/bin/aurelia:
#   service → ~/.config/systemd/user/aurelia.service
#   binary  → ~/.aurelia/bin/aurelia
#
# Set AURELIA_SYSTEMD_START=0 or AURELIA_SYSTEMD_ENABLE=0 to render and
# inspect the unit without starting/enabling it. This is the supported
# staging path; it never consumes the production Telegram token by itself.

set -euo pipefail

SERVICE_NAME="aurelia"
SERVICE_FILE="$HOME/.config/systemd/user/${SERVICE_NAME}.service"
BINARY="$HOME/.aurelia/bin/aurelia"
AURELIA_HOME="$HOME/.aurelia"
NODE_BIN="$HOME/.local/node/bin"
AI_JAIL_BIN="$HOME/.local/bin/ai-jail"
SYSTEM_AI_JAIL="/usr/local/bin/ai-jail"
SYSTEM_JAIL_WRAPPER="/usr/local/lib/aurelia/aurelia-jail"
START_SERVICE="${AURELIA_SYSTEMD_START:-1}"
ENABLE_SERVICE="${AURELIA_SYSTEMD_ENABLE:-1}"
USE_JAIL="${AURELIA_USE_JAIL:-1}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/aurelia.service.tmpl"

if [[ ! -f "$TEMPLATE" ]]; then
    echo "error: template not found: $TEMPLATE" >&2
    exit 1
fi

if [[ ! -x "$BINARY" ]]; then
    echo "error: binary not executable: $BINARY" >&2
    exit 1
fi

if [[ ! -x "$NODE_BIN/node" ]]; then
    echo "error: Node.js not found at $NODE_BIN/node" >&2
    echo "install Node >=22.19.0 under $NODE_BIN or adjust the unit PATH" >&2
    exit 1
fi

mkdir -p "$HOME/.config/systemd/user" "$(dirname "$BINARY")"

EXEC_START="$BINARY"
if [[ "$USE_JAIL" == "0" ]]; then
    echo "explicit opt-out: starting the binary directly (AURELIA_USE_JAIL=0)"
elif [[ "$USE_JAIL" != "1" ]]; then
    echo "error: AURELIA_USE_JAIL must be 0 or 1, got: $USE_JAIL" >&2
    exit 1
else
    if [[ ! -x "$SYSTEM_AI_JAIL" ]]; then
        if [[ ! -x "$AI_JAIL_BIN" ]]; then
            echo "error: ai-jail is required but not executable: $AI_JAIL_BIN" >&2
            echo "install ai-jail or explicitly opt out with AURELIA_USE_JAIL=0" >&2
            exit 1
        fi
        sudo install -d -m 0755 /usr/local/lib/aurelia
        sudo install -m 0755 "$AI_JAIL_BIN" "$SYSTEM_AI_JAIL"
    fi
    if [[ ! -f "${SCRIPT_DIR}/aurelia-jail.sh" ]]; then
        echo "error: jail wrapper not found: ${SCRIPT_DIR}/aurelia-jail.sh" >&2
        exit 1
    fi
    sudo install -d -m 0755 /usr/local/lib/aurelia
    sudo install -m 0755 "${SCRIPT_DIR}/aurelia-jail.sh" "$SYSTEM_JAIL_WRAPPER"
    EXEC_START="$SYSTEM_JAIL_WRAPPER"
    echo "using root-owned ai-jail wrapper: $SYSTEM_JAIL_WRAPPER"
fi

# Render template
sed \
    -e "s|__HOME__|${HOME}|g" \
    -e "s|__BINARY__|${BINARY}|g" \
    -e "s|__EXEC_START__|${EXEC_START}|g" \
    -e "s|__NODE_BIN__|${NODE_BIN}|g" \
    -e "s|__AURELIA_HOME__|${AURELIA_HOME}|g" \
    -e "s|__PATH__|${PATH}|g" \
    "$TEMPLATE" > "${SERVICE_FILE}.new"
chmod 644 "${SERVICE_FILE}.new"
mv "${SERVICE_FILE}.new" "$SERVICE_FILE"

echo "installed: $SERVICE_FILE"

# Reload systemd so the rendered unit is visible.
systemctl --user daemon-reload

if [[ "$ENABLE_SERVICE" == "1" ]]; then
    systemctl --user enable "$SERVICE_NAME"
else
    echo "not enabled (AURELIA_SYSTEMD_ENABLE=0)"
fi

if [[ "$START_SERVICE" == "1" ]]; then
    if systemctl --user is-active "$SERVICE_NAME" >/dev/null 2>&1; then
        systemctl --user restart "$SERVICE_NAME"
        echo "service restarted: $SERVICE_NAME"
    else
        systemctl --user start "$SERVICE_NAME"
        echo "service started: $SERVICE_NAME"
    fi
else
    echo "not started (AURELIA_SYSTEMD_START=0)"
fi

if command -v loginctl >/dev/null 2>&1; then
    linger="$(loginctl show-user "$USER" -p Linger --value 2>/dev/null || true)"
    if [[ "$linger" != "yes" ]]; then
        echo "warning: lingering is not enabled for $USER; the user service will stop after logout" >&2
        echo "         enable it once with: sudo loginctl enable-linger $USER" >&2
    fi
fi

echo ""
echo "View logs: journalctl --user -u aurelia -f"
echo "Stop:      systemctl --user stop aurelia"
echo "Disable:   systemctl --user disable aurelia"
