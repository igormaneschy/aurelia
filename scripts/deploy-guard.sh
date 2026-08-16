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
# Exit codes: 0 = proceed with kickstart; 1 = abort (active runs).
set -u

BIN="${1:-$HOME/.aurelia/bin/aurelia}"
WAIT="${2:-5}"

if [ "${FORCE:-0}" = "1" ]; then
  echo "deploy-guard: FORCE=1 set — skipping active-run check (in-flight runs will be interrupted)"
  exit 0
fi

if [ ! -x "$BIN" ]; then
  echo "deploy-guard: $BIN not found — skipping check"
  exit 0
fi

running=0
deadline=$(( $(date +%s) + WAIT ))

while :; do
  out=$("$BIN" debug metrics --json 2>/dev/null) || {
    echo "deploy-guard: daemon/metrics not reachable — skipping check"
    exit 0
  }
  running=$(printf '%s' "$out" | grep -o '"RunsRunning": *[0-9]*' | grep -o '[0-9]*$' | head -1)
  running=${running:-0}

  if [ "$running" -le 0 ]; then
    echo "deploy-guard: no active runs — safe to restart"
    exit 0
  fi

  now=$(date +%s)
  if [ "$now" -ge "$deadline" ]; then
    echo "deploy-guard: $running active run(s) still in flight after ${WAIT}s drain window." >&2
    echo "deploy-guard: ABORTING kickstart. Options:" >&2
    echo "deploy-guard:   - wait for the run to finish, then run make deploy again" >&2
    echo "deploy-guard:   - FORCE=1 make deploy to restart anyway (run will be marked interrupted)" >&2
    exit 1
  fi

  echo "deploy-guard: $running active run(s), waiting for drain (${WAIT}s window)..."
  sleep 1
done
