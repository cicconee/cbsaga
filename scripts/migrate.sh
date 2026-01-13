#!/usr/bin/env bash
set -euo pipefail

MIGRATE_IMAGE="migrate/migrate:v4.17.1"
DOCKER_NETWORK="deployments_default"
ORCHESTRATOR_DSN="postgres://postgres:postgres@postgres:5432/orchestrator?sslmode=disable"
IDENTITY_DSN="postgres://postgres:postgres@identity-postgres:5432/identity?sslmode=disable"

migrate_up() {
  local name="$1"
  local migrate_dir="$2"
  local dsn="$3"
  local container="$4"

  if [[ -z "$dsn" ]]; then
    echo "❌ [$name] DSN is empty" >&2
    exit 1
  fi

  if [[ ! -d "$migrate_dir" ]]; then
    echo "❌ [$name] migrations dir not found: $migrate_dir" >&2
    exit 1
  fi

  if ! docker exec -i "$container" \
    psql -q -v ON_ERROR_STOP=1 -U postgres -d postgres \
    -tAc "SELECT 1 FROM pg_database WHERE datname='${name}'" | grep -q 1; then
    echo "❌ [${name}] database '${name}' does not exist."
    exit 1
  fi

  echo "➡️ [$name] running migrations from $migrate_dir"

  docker run --rm \
    --network "$DOCKER_NETWORK" \
    -v "$(pwd)/$migrate_dir:/migrations" \
    "$MIGRATE_IMAGE" \
    -path=/migrations -database="$dsn" up
}

echo "Running migrations..."

migrate_up "orchestrator" "db/orchestrator/migrations" "$ORCHESTRATOR_DSN" "cbsaga-postgres"
migrate_up "identity" "db/identity/migrations" "$IDENTITY_DSN" "cbsaga-identity-postgres"

echo "✅ migrations complete"
