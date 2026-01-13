# Coinbase Saga

Coinbase Saga is a distributed transaction orchestration system that models how high risk financial actions are safely initiated, validated, and progressed across multiple services using saga, transactional outbox, and idempotency patterns.

## Note for the Coinbase Team

This repository was started very recently (within the last day as of the first commit) as a way to demonstrate how I think about designing reliable, money-movement systems. Specifically around distributed transactions, state transitions, idempotency, and failure handling using a saga-style approach with a transactional outbox.

The project is actively being developed, and updates will continue leading up to the interview and beyond. As time allows, I plan to expand it with additional services, clearer failure paths, and deeper functionality to better reflect a real-world distributed system.

Given the short time window so far, I intentionally prioritized:

- Core system architecture and data flow
- Explicit state transitions and invariants
- Cross-service coordination patterns
- Correctness and failure awareness over completeness

As a result, some production-critical concerns, such as exhaustive test coverage, extensive comments, and broader documentation are intentionally lighter than they would be in a mature or long-lived codebase. In a real production setting, these would be developed alongside the system as it evolved.

Similarly, some commits are larger than would be typical in a long-lived repository, which is a byproduct of the exploratory, time constrained nature of the work rather than how I’d normally structure commits.

This repository is best viewed as a living system under construction, focused first on correctness and design clarity rather than polish.

The goal is not completeness, but to make design decisions and tradeoffs explicit and discussable.

## Overview

### Tech Stack

- Go v1.24
- Golang-Migrate v4.16
- Docker
- Postgres v16
- RedPanda (Kafka compatible)
- Debezium v2.6
- Protobuf v33.2
- GRPCurl v1.9

### High-Level Flow

```
Client (gRPC)
    ↓
Orchestrator
    ↓ (Outbox + Debezium)
Redpanda (Kafka)
    ↓
Identity Service
    ↓ (Outbox + Debezium)
Redpanda (Kafka)
    ↓
Orchestrator (Saga Advancement / Execution)
```

### Core Workflow

1. **Ingress (gRPC)**

   - Withdrawals enter via the orchestrator’s gRPC API.
   - A request-scoped trace ID is generated or propagated.

2. **Idempotency & Concurrency Safety**

   - A small Postgres transaction records the idempotency key.
   - Ensures at-most-once initiation under retries or concurrent requests.

3. **Transactional Outbox (Orchestrator)**

   - The withdrawal and a orchestrator outbox event are written atomically.
   - No external calls occur inside the transaction.

4. **Event Propagation**

   - Debezium detects outbox inserts and publishes events to Redpanda.

5. **Identity Verification**

   - The identity service consumes withdrawal events.
   - Verifies user identity (simplified in this demo).
   - Emits an identity decision via its own outbox.

6. **Saga Advancement**
   - Debezium publishes identity events back to Redpanda.
   - The orchestrator consumes them to advance or finalize the withdrawal.

### Design Principles

- **Event-driven coordination:** services communicate via events, not synchronous calls
- **Transactional outbox:** eliminates dual-write problems
- **Idempotent workflows:** safe under retries and replays
- **Request-scoped tracing:** a single trace ID links all events in a withdrawal flow
- **At-least-once delivery:** handlers are written to tolerate duplicates

### Scope

This is a **demonstration repo**, intentionally focused on correctness and clarity over completeness.  
It mirrors patterns used in real financial systems without claiming to be production-ready.

## Getting Started

Start up the required Docker containers in detached mode.

```zsh
docker compose -f deployments/docker-compose.yml up -d
```

To shutdown the Docker containers run the following command. You can append `-v` to the end of the command to destroy volumes.

```zsh
docker compose -f deployments/docker-compose.yml down
```

### Database

Ensure the databases are created on the docker container using the following commands.

```zsh
docker exec -it cbsaga-postgres psql -U postgres -d postgres -c "CREATE DATABASE orchestrator;"
docker exec -it cbsaga-identity-postgres psql -U postgres -d postgres -c "CREATE DATABASE identity;"
```

Then run the database migrations.

```zsh
migrate \
  -path db/orchestrator/migrations \
  -database "postgres://postgres:postgres@localhost:5432/orchestrator?sslmode=disable" \
  up

migrate \
  -path db/identity/migrations \
  -database "postgres://postgres:postgres@localhost:5433/identity?sslmode=disable" \
  up
```

Verify the tables exist with the following commands.

```zsh
docker exec -it cbsaga-postgres \
  psql -U postgres -d orchestrator -c "\dt orchestrator.*"

docker exec -it cbsaga-identity-postgres \
  psql -U postgres -d identity -c "\dt identity.*"
```

### Debezium

Create the connector that will watch for withdrawal events via the `orchestrator_outbox_events` table.

```zsh
curl -s -X POST http://localhost:8083/connectors \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "cbsaga-orchestrator-outbox",
    "config": {
      "connector.class": "io.debezium.connector.postgresql.PostgresConnector",
      "tasks.max": "1",
      "database.hostname": "postgres",
      "database.port": "5432",
      "database.user": "postgres",
      "database.password": "postgres",
      "database.dbname": "orchestrator",
      "plugin.name": "pgoutput",
      "topic.prefix": "cbsaga.orchestrator",
      "schema.include.list": "orchestrator",
      "table.include.list": "orchestrator.outbox_events",
      "publication.autocreate.mode": "filtered",
      "publication.name": "cbsaga_orch_pub",
      "slot.name": "cbsaga_orch_slot",
      "tombstones.on.delete": "false",
      "transforms": "outbox",
      "transforms.outbox.type": "io.debezium.transforms.outbox.EventRouter",
      "transforms.outbox.route.by.field": "aggregate_type",
      "transforms.outbox.route.topic.replacement": "cbsaga.outbox.${routedByValue}",
      "transforms.outbox.table.field.event.id": "event_id",
      "transforms.outbox.table.field.event.key": "aggregate_id",
      "transforms.outbox.table.field.event.type": "event_type",
      "transforms.outbox.table.field.event.payload": "payload_json",
      "transforms.outbox.table.expand.json.payload": "true",
      "transforms.outbox.table.fields.additional.placement": "trace_id:header,created_at:header"
    }
  }' | cat
```

Create the connector that will watch for identity events via the `identity.outbox_events`.

```zsh
curl -s -X POST http://localhost:8083/connectors \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "cbsaga-identity-outbox",
    "config": {
      "connector.class": "io.debezium.connector.postgresql.PostgresConnector",
      "tasks.max": "1",
      "database.hostname": "identity-postgres",
      "database.port": "5432",
      "database.user": "postgres",
      "database.password": "postgres",
      "database.dbname": "identity",
      "plugin.name": "pgoutput",
      "topic.prefix": "cbsaga.identity",
      "schema.include.list": "identity",
      "table.include.list": "identity.outbox_events",
      "publication.autocreate.mode": "filtered",
      "publication.name": "cbsaga_identity_pub",
      "slot.name": "cbsaga_identity_slot",
      "tombstones.on.delete": "false",
      "transforms": "outbox",
      "transforms.outbox.type": "io.debezium.transforms.outbox.EventRouter",
      "transforms.outbox.route.by.field": "aggregate_type",
      "transforms.outbox.route.topic.replacement": "cbsaga.outbox.${routedByValue}",
      "transforms.outbox.table.field.event.id": "event_id",
      "transforms.outbox.table.field.event.key": "aggregate_id",
      "transforms.outbox.table.field.event.type": "event_type",
      "transforms.outbox.table.field.event.payload": "payload_json",
      "transforms.outbox.table.expand.json.payload": "true",
      "transforms.outbox.table.fields.additional.placement": "trace_id:header,created_at:header"
    }
  }' | cat
```

Then verify the connector is running.

```zsh
curl -s http://localhost:8083/connectors | cat
```

You can also check the status using these command.

```zsh
curl -s http://localhost:8083/connectors/cbsaga-orchestrator-outbox/status | cat
curl -s http://localhost:8083/connectors/cbsaga-identity-outbox/status | cat
```

With the connector configured, you can watch topics on the RedPanda console hosted at `http://localhost:8080`.

### Orchestrator

Start up the orchestrator by running the `cmd/orchestrator/main.go` file. Verify the `grpc` server is running with the following commands.

```zsh
grpcurl -plaintext localhost:9000 grpc.health.v1.Health/Check
grpcurl -plaintext localhost:9000 list
```

### Identity Service

Start up the identity service by running the `cmd/identity/main.go` file.

## Using the gRPC server

The following command can be used to initiate a new withdrawal event.

```zsh
grpcurl -plaintext -d '{
  "user_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "asset":"ASSET",
  "amount_minor":1000000,
  "destination_addr":"DESTINATION_ADDR",
  "idempotency_key":"1"
}' localhost:9000 cbsaga.orchestrator.v1.OrchestratorService/CreateWithdrawal
```

As you mess around with different requests, you can view the different topics at `http://localhost:8080`.

## Developer Guide

This section holds helpful commands when developing in this repo.

### Creating Migrations

To create a new migration at a specified location, run the following command replacing the necessary values.

```zsh
migrate create \
  -ext sql \
  -dir $MIGRATION_PATH \
  $MIGRATION_NAME
```

### Generating Protobuf Code

This section shows how to generate the protobuf code from the `.proto` files for gRPC.

```zsh
protoc \
  -I proto \
  --go_out=gen --go_opt=paths=source_relative \
  --go-grpc_out=gen --go-grpc_opt=paths=source_relative \
  $PROTO_FILE_PATH
```
