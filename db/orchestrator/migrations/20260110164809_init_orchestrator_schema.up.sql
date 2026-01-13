BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SCHEMA IF NOT EXISTS orchestrator;

CREATE TABLE IF NOT EXISTS orchestrator.withdrawals (
  id               UUID PRIMARY KEY,
  user_id          UUID NOT NULL,
  asset            TEXT NOT NULL,
  amount_minor     BIGINT NOT NULL CHECK (amount_minor > 0),
  destination_addr TEXT NOT NULL,
  status           TEXT NOT NULL, -- REQUESTED | IN_PROGRESS | COMPLETED | FAILED | CANCELED
  failure_reason   TEXT NULL,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_withdrawals_user_created_at
  ON orchestrator.withdrawals (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_withdrawals_status_created_at
  ON orchestrator.withdrawals (status, created_at DESC);

CREATE TABLE IF NOT EXISTS orchestrator.saga_instances (
  saga_id         UUID PRIMARY KEY,
  withdrawal_id   UUID NOT NULL
                   REFERENCES orchestrator.withdrawals(id)
                   ON DELETE CASCADE,
  state           TEXT NOT NULL,
  current_step    TEXT NOT NULL,
  attempt         INT NOT NULL DEFAULT 0,
  locked_by       TEXT NULL,
  lock_expires_at TIMESTAMPTZ NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_saga_withdrawal_id
  ON orchestrator.saga_instances (withdrawal_id);

CREATE INDEX IF NOT EXISTS idx_saga_state
  ON orchestrator.saga_instances (state);

CREATE INDEX IF NOT EXISTS idx_saga_lock_expiry
  ON orchestrator.saga_instances (lock_expires_at);

CREATE TABLE IF NOT EXISTS orchestrator.idempotency_keys (
  id                 UUID PRIMARY KEY,
  user_id            UUID NOT NULL,
  idempotency_key    TEXT NOT NULL,
  withdrawal_id      UUID NOT NULL,
  request_hash       TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'COMPLETED',
  grpc_code          INT NOT NULL DEFAULT 0,
  response_code      INT NOT NULL,
  response_body_json TEXT NOT NULL,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT ux_idem_user_key UNIQUE (user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_idem_created_at
  ON orchestrator.idempotency_keys (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_idem_withdrawal_id
  ON orchestrator.idempotency_keys (withdrawal_id);

CREATE INDEX IF NOT EXISTS idx_idem_status_updated
  ON orchestrator.idempotency_keys (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS orchestrator.outbox_events (
  event_id       UUID PRIMARY KEY,
  aggregate_type TEXT NOT NULL,
  aggregate_id   UUID NOT NULL,
  event_type     TEXT NOT NULL,
  payload_json   TEXT NOT NULL,
  trace_id       TEXT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_outbox_created_at
  ON orchestrator.outbox_events (created_at ASC);

CREATE INDEX IF NOT EXISTS idx_outbox_aggregate
  ON orchestrator.outbox_events (aggregate_type, aggregate_id);

COMMIT;
