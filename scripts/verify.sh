#!/usr/bin/env bash
set -euo pipefail

DEBEZIUM_URL="http://localhost:8083"

check_db_connection() {
  local name="$1"
  local container="$2"

  echo "➡️ [$name] checking db connnection (container: ${container})"

  if ! docker exec -i $container \
    pg_isready -U postgres -d $name >/dev/null 2>&1; then
    echo "❌ Cannot connect to ${name} database (container: ${container})"
    exit 1
  fi
}

check_connector() {
  local name="$1"

  echo "➡️ Checking connector: $name"

  status="$(curl -fsS "${DEBEZIUM_URL}/connectors/${name}/status")"

  if ! echo "$status" | grep -q '"state":"RUNNING"'; then
    echo "❌ connector $name is not RUNNING"
    echo "$status"
    exit 1
  fi

  if echo "$status" | grep -q '"state":"FAILED"'; then
    echo "❌ connector $name has FAILED tasks"
    echo "$status"
    exit 1
  fi
}

echo "Verifying Postgres connectivity..."

check_db_connection "orchestrator" "cbsaga-postgres" 
check_db_connection "identity" "cbsaga-identity-postgres"

echo "✅ database connectivity OK"

echo "Verifying Debezium connectors..."

check_connector "orchestrator-connector"
check_connector "identity-connector"

echo "✅ Debezium connectors OK"

