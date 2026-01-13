#!/usr/bin/env bash
set -euo pipefail

name="$1"
url="$2"
timeout="${3:-90}"

echo "Waiting for ${name} at ${url} (timeout=${timeout}s)..."
start=$(date +%s)

while true; do
  if curl -fsS "$url" >/dev/null 2>&1; then
    echo "✅ ${name} is ready"
    exit 0
  fi

  now=$(date +%s)
  if (( now - start > timeout )); then
    echo "❌ timed out waiting for ${name} at ${url}"
    exit 1
  fi

  sleep 1
done

