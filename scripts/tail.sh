#!/usr/bin/env bash
set -euo pipefail

LOG_DIR="./.run/logs"
SERVICES_STR="$1"

read -r -a SERVICES <<< "$SERVICES_STR"

for svc in "${SERVICES[@]}"; do
  touch "${LOG_DIR}/${svc}.log"
done

echo "Tailing service logs (Ctrl-C to stop)..."

for svc in "${SERVICES[@]}"; do
  logfile="${LOG_DIR}/${svc}.log"
  tail -n 200 -f "$logfile" | sed -u "s/^/[${svc}] /" &
done

wait
