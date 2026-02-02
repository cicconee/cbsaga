BEGIN;

ALTER TABLE orchestrator.idempotency_keys
  ADD COLUMN IF NOT EXISTS app_error_code TEXT,
  ADD COLUMN IF NOT EXISTS error_message TEXT,
  ALTER COLUMN response_body_json DROP NOT NULL,
  ALTER COLUMN response_body_json DROP DEFAULT;

COMMIT;

