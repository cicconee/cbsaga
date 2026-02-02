BEGIN;

ALTER TABLE orchestrator.idempotency_keys
  DROP COLUMN IF EXISTS grpc_code,
  DROP COLUMN IF EXISTS response_code;

COMMIT;
