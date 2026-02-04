#!/usr/bin/env bash
set -euo pipefail

LOG_DIR="./.run/logs"
SERVICES_STR="$1"

read -r -a SERVICES <<<"$SERVICES_STR"

for svc in "${SERVICES[@]}"; do
  touch "${LOG_DIR}/${svc}.log"
done

cleanup() {
  local pids
  pids="$(jobs -pr || true)"
  if [[ -n "${pids}" ]]; then
    kill ${pids} 2>/dev/null || true
    sleep 0.1
    kill -9 ${pids} 2>/dev/null || true
  fi
}

trap cleanup EXIT INT TERM

echo "Tailing service logs (Ctrl-C to stop)..."

for svc in "${SERVICES[@]}"; do
  logfile="${LOG_DIR}/${svc}.log"
  tail -n 200 -f "$logfile" | sed -u "s/^/[${svc}] /" &
done

wait
