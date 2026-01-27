# Coinbase Saga

Coinbase Saga is a distributed transaction orchestration system that models how high risk financial actions are safely initiated, validated, and progressed across multiple services using saga, transactional outbox, and idempotency patterns.

## Note for the Coinbase Team

**<del>On hold while I prep for CodeSignal assessment.</del>**

**Jan 23rd: CodeSignal assessment complete. Development continues.**


This repository was started very recently (within the last day as of the first commit) as a way to demonstrate how I think about designing reliable, money-movement systems. Specifically around distributed transactions, state transitions, idempotency, and failure handling using a saga-style approach with a transactional outbox.

The project is actively being developed, and updates will continue leading up to the interview and beyond. As time allows, I plan to expand it with additional services, clearer failure paths, and deeper functionality to better reflect a real-world distributed system.

Given the short time window so far, I intentionally prioritized:

- Core system architecture and data flow
- Explicit state transitions and invariants
- Cross-service coordination patterns
- Correctness and failure awareness over completeness
- Fast local deployability to respect reviewer time (`make demo` brings the system up end-to-end).

As a result, some production-critical concerns, such as exhaustive test coverage, extensive comments, and broader documentation are intentionally lighter than they would be in a mature or long-lived codebase. In a real production setting, these would be developed alongside the system as it evolved.

Similarly, some commits are larger than one would typically see in a long-lived repository. This is a byproduct of the exploratory, time constrained nature of the work rather than how I’d normally structure commits.

This repository is best viewed as a living system under construction. It's focused first on correctness and design clarity, rather than polish.

The goal is not completeness, but to make design decisions and tradeoffs explicit and discussable.

## Overview

### Tech Stack

- Go v1.24
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

The quickest way to get started is by using the `make` command. Make sure the bash scripts are marked as executable.

```zsh
chmod +x scripts/*.sh
```

By running the following command, you will spin up infrastructure, run migrations, register Debezium connectors on the Postgres WAL, build binaries, run the services, and tail the logs.

```zsh
make demo
```

To kill the run you can run `make stop`. Also, `make clean` will run the `stop` command followed by the removal of the `bin` and `.run` directories.

## Using the gRPC server

### Create Withdrawal

Once you are up and running, you can play around sending in withdrawal requests to the gRPC server. I recommend playing around with different requests (unique requests, duplicate requests, different requests with same idempotency key, etc.).

```zsh
grpcurl -plaintext -d '{
  "user_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "asset":"ASSET",
  "amount_minor":1000000,
  "destination_addr":"DESTINATION_ADDR",
  "idempotency_key":"1"
}' localhost:9000 cbsaga.orchestrator.v1.OrchestratorService/CreateWithdrawal
```

Reflection is enabled for this demo environment. But if you decide to disable it, gRPC commands need to be sent with the proto files explicitly.

```zsh
grpcurl -plaintext \
  -import-path ./proto \
  -proto cbsaga/orchestrator/v1/orchestrator.proto \
  -d '{
    "user_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
    "asset":"ASSET",
    "amount_minor":1000000,
    "destination_addr":"DESTINATION_ADDR",
    "idempotency_key":"1"
  }' \
  localhost:9000 cbsaga.orchestrator.v1.OrchestratorService/CreateWithdrawal
```

**Note: You can view the RedPanda console at `http://localhost:8080`.**

### Get Withdrawal 

Using the `withdrawalId` field returned by `CreateWithdrawal`, you can query the `GetWithdrawal` endpoint to see the status.

```zsh
grpcurl -plaintext -d '{
  "withdrawal_id":"WITHDRAWAL_ID"
}' localhost:9000 cbsaga.orchestrator.v1.OrchestratorService/GetWithdrawal

```
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

Or you can use the migrate docker container.

```zsh
docker run --rm \
  -v "$(pwd)/$MIGRATION_PATH:/migrations" \
  migrate/migrate:v4.17.1 \
  create -ext sql -dir /migrations $MIGRATION_NAME
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
