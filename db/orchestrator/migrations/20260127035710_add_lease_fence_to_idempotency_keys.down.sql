BEGIN;

ALTER TABLE orchestrator.idempotency_keys
  DROP COLUMN IF EXISTS lease_fence;

COMMIT;
