BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SCHEMA IF NOT EXISTS identity;

CREATE TABLE IF NOT EXISTS identity.verifications (
  verification_id UUID PRIMARY KEY,
  withdrawal_id   UUID NOT NULL,
  user_id         UUID NOT NULL,
  status          TEXT NOT NULL,  -- VERIFIED | REJECTED
  reason          TEXT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_identity_withdrawal
  ON identity.verifications (withdrawal_id);

CREATE INDEX IF NOT EXISTS idx_identity_user_created
  ON identity.verifications (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS identity.outbox_events (
  event_id       UUID PRIMARY KEY,
  aggregate_type TEXT NOT NULL,  -- "identity"
  aggregate_id   UUID NOT NULL,  -- withdrawal_id
  event_type     TEXT NOT NULL,  -- IdentityVerified | IdentityRejected
  payload_json   TEXT NOT NULL,
  trace_id       TEXT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_identity_outbox_created
  ON identity.outbox_events (created_at ASC);

CREATE INDEX IF NOT EXISTS idx_identity_outbox_aggregate
  ON identity.outbox_events (aggregate_type, aggregate_id);

COMMIT;

