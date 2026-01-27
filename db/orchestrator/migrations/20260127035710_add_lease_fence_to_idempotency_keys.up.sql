BEGIN;

ALTER TABLE orchestrator.idempotency_keys
  ADD COLUMN IF NOT EXISTS lease_fence BIGINT;

UPDATE orchestrator.idempotency_keys
SET lease_fence = COALESCE(lease_fence, 1)
  WHERE lease_fence IS NULL OR lease_fence = 0;

ALTER TABLE orchestrator.idempotency_keys
  ALTER COLUMN lease_fence SET NOT NULL,
  ALTER COLUMN lease_fence SET DEFAULT 1;

COMMIT;
