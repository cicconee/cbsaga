#!/usr/bin/env bash
set -euo pipefail

BIN_DIR="./bin"
LOG_DIR="./.run/logs"
PID_DIR="./.run/pids"
SERVICES_STR="$1"

mkdir -p "$LOG_DIR" "$PID_DIR"
read -r -a SERVICES <<< "$SERVICES_STR"

echo "Starting services..."

for svc in "${SERVICES[@]}"; do
  bin="${BIN_DIR}/${svc}"
  pidfile="${PID_DIR}/${svc}.pid"
  logfile="${LOG_DIR}/${svc}.log"

  if [[ ! -x "$bin" ]]; then
    echo "❌ missing binary: $bin"
    exit 1
  fi

  if [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    echo "➡️  $svc already running"
    continue
  fi

  nohup "$bin" >>"$logfile" 2>&1 &
  echo "$!" > "$pidfile"
  echo "➡️  started $svc (pid=$!)"
done

echo "✅ services running"
