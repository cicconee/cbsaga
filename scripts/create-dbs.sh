#!/usr/bin/env bash
set -euo pipefail

echo "Ensuring databases exist..."

# Include -q otherwise these commands will print "DO" to console each time, harmless but very
# annoying. Include -v ON_ERROR_STOP=1 to fail immediately if SQL errors. This is important 
# for automating.
if ! docker exec -i cbsaga-postgres \
  psql -q -tAc "SELECT 1 FROM pg_database WHERE datname='orchestrator'" \
  -U postgres -d postgres | grep -q 1; then

  docker exec -i cbsaga-postgres \
    psql -q -v ON_ERROR_STOP=1 \
    -U postgres -d postgres -c \
    "CREATE DATABASE orchestrator;"
fi

if ! docker exec -i cbsaga-identity-postgres \
  psql -q -tAc "SELECT 1 FROM pg_database WHERE datname='identity'" \
  -U postgres -d postgres | grep -q 1; then

  docker exec -i cbsaga-identity-postgres \
    psql -q -v ON_ERROR_STOP=1 \
    -U postgres -d postgres -c \
    "CREATE DATABASE identity;"
fi

echo "✅ databases ensured"
