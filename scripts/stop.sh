#!/usr/bin/env bash
set -euo pipefail

PID_DIR="./.run/pids"
SERVICES_STR="$1"

read -r -a SERVICES <<< "$SERVICES_STR"

echo "Stopping services..."

for svc in "${SERVICES[@]}"; do
  pidfile="${PID_DIR}/${svc}.pid"

  [[ -f "$pidfile" ]] || continue

  pid="$(cat "$pidfile")"

  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    echo "➡️ stopped $svc (pid=$pid)"
  else
    echo "➡️ $svc not running (stale pid)"
  fi

  rm -f "$pidfile"
done

echo "✅ services stopped"
