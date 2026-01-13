#!/usr/bin/env bash
set -euo pipefail

PID_DIR="./.run/pids"
LOG_DIR="./.run/logs"
SERVICES_STR="$1"

read -r -a SERVICES <<< "$SERVICES_STR"

for svc in "${SERVICES[@]}"; do
  pidfile="${PID_DIR}/${svc}.pid"
  logfile="${LOG_DIR}/${svc}.log"

  echo "== ${svc} =="

  if [[ -f "$pidfile" ]]; then
    pid="$(cat "$pidfile")"
    if kill -0 "$pid" 2>/dev/null; then
      echo "status: running (pid=$pid)"
    else
      echo "status: not running (stale pid)"
    fi
  else
    echo "status: not running"
  fi

  echo "-- last 10 log lines --"
  if [[ -f "$logfile" ]]; then
    tail -n 10 "$logfile"
  else
    echo "(no logs yet)"
  fi
  echo
done
