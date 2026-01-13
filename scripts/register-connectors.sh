#!/usr/bin/env bash
set -euo pipefail

DEBEZIUM_URL="http://localhost:8083"

./scripts/wait-http.sh "debezium connect" "${DEBEZIUM_URL}/connectors" 10

echo "Registering/updating Debezium connectors..."

orch_name="orchestrator-connector"
ident_name="identity-connector"

curl -fsS -X PUT \
  -H "Content-Type: application/json" \
  --data @"deployments/debezium/${orch_name}.json" \
  "${DEBEZIUM_URL}/connectors/${orch_name}/config" >/dev/null

curl -fsS -X PUT \
  -H "Content-Type: application/json" \
  --data @"deployments/debezium/${ident_name}.json" \
  "${DEBEZIUM_URL}/connectors/${ident_name}/config" >/dev/null

echo "✅ connectors registered"
