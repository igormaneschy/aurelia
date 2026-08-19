#!/bin/sh
# deploy-guard.sh — refuses `make deploy` kickstart while runs are active.
#
# The daemon restarts via `launchctl kickstart -k` (SIGTERM), which cuts any
# in-flight run. This guard consults the runlog (via `aurelia debug metrics
# --json`, which reads the DB directly) and either waits for active runs to
# drain (up to DRAIN_WAIT seconds) or aborts with instructions.
#
# Usage:
#   deploy-guard.sh [binary] [drain-wait-seconds]
#   FORCE=1 make deploy        # bypass the guard (run will be interrupted)
#
# Exit codes: 0 = proceed with kickstart; 1 = abort (active runs or an
# ambiguous safety check).
set -u

BIN="${1:-$HOME/.aurelia/bin/aurelia}"
WAIT="${2:-5}"

if [ "${FORCE:-0}" = "1" ]; then
  echo "deploy-guard: FORCE=1 set — skipping active-run check (in-flight runs will be interrupted)"
  exit 0
fi

if [ ! -x "$BIN" ]; then
	echo "deploy-guard: executable $BIN is missing or not executable — refusing restart" >&2
	exit 1
fi

if ! printf '%s' "$WAIT" | grep -Eq '^[0-9]+$'; then
	echo "deploy-guard: drain wait must be a non-negative integer, got: $WAIT — refusing restart" >&2
	exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
	echo "deploy-guard: jq is required to validate metrics JSON — refusing restart" >&2
	exit 1
fi

now=$(date +%s) || {
	echo "deploy-guard: date failed — refusing restart" >&2
	exit 1
}
deadline=$((now + WAIT))
metrics_file=$(mktemp "${TMPDIR:-/tmp}/deploy-guard.XXXXXX") || {
	echo "deploy-guard: cannot create temporary metrics file — refusing restart" >&2
	exit 1
}
trap 'rm -f "$metrics_file"' EXIT HUP INT TERM

while :; do
	if ! "$BIN" debug metrics --json >"$metrics_file" 2>/dev/null; then
		echo "deploy-guard: daemon/metrics command failed — refusing restart" >&2
		exit 1
	fi

	running=$(jq -e -s -r '
	if length != 1 or (.[0] | type) != "object" or (.[0] | has("RunsRunning") | not)
	   or (.[0].RunsRunning | type) != "number" or (.[0].RunsRunning < 0)
	   or (.[0].RunsRunning != (.[0].RunsRunning | floor))
	then error("invalid RunsRunning metrics") else .[0].RunsRunning end
' "$metrics_file" 2>/dev/null) || {
		echo "deploy-guard: metrics JSON is malformed or RunsRunning is missing/non-numeric/negative — refusing restart" >&2
		exit 1
	}

  if [ "$running" -le 0 ]; then
    echo "deploy-guard: no active runs — safe to restart"
    exit 0
  fi

  now=$(date +%s) || {
    echo "deploy-guard: date failed — refusing restart" >&2
    exit 1
  }
  if [ "$now" -ge "$deadline" ]; then
    echo "deploy-guard: $running active run(s) still in flight after ${WAIT}s drain window." >&2
    echo "deploy-guard: ABORTING kickstart. Options:" >&2
    echo "deploy-guard:   - wait for the run to finish, then run make deploy again" >&2
    echo "deploy-guard:   - FORCE=1 make deploy to restart anyway (run will be marked interrupted)" >&2
    exit 1
  fi

  echo "deploy-guard: $running active run(s), waiting for drain (${WAIT}s window)..."
  sleep 1 || {
    echo "deploy-guard: sleep failed — refusing restart" >&2
    exit 1
  }
done
