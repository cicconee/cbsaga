BEGIN;

ALTER TABLE orchestrator.idempotency_keys
  ADD COLUMN IF NOT EXISTS lease_owner UUID,
  ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;

UPDATE orchestrator.idempotency_keys
SET
  lease_owner = COALESCE(lease_owner, gen_random_uuid()),
  lease_expires_at = COALESCE(lease_expires_at, updated_at + interval '30 seconds')
WHERE lease_owner IS NULL OR lease_expires_at IS NULL;

ALTER TABLE orchestrator.idempotency_keys
  ALTER COLUMN lease_owner SET NOT NULL,
  ALTER COLUMN lease_expires_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_status_lease_expires
  ON orchestrator.idempotency_keys (status, lease_expires_at);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_lease_owner
  ON orchestrator.idempotency_keys (lease_owner);

COMMIT;
