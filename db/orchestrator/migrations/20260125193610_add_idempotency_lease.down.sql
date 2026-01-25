BEGIN;

DROP INDEX IF EXISTS orchestrator.idx_idempotency_keys_lease_owner;
DROP INDEX IF EXISTS orchestrator.idx_idempotency_keys_status_lease_expires;

ALTER TABLE orchestrator.idempotency_keys
  DROP COLUMN IF EXISTS lease_expires_at,
  DROP COLUMN IF EXISTS lease_owner;

COMMIT;
