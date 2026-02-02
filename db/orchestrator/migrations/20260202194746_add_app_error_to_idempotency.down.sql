BEGIN;

ALTER TABLE orchestrator.idempotency_keys
  ALTER COLUMN response_body_json SET DEFAULT '{}'::TEXT,
  ALTER COLUMN response_body_json SET NOT NULL,
  DROP COLUMN IF EXISTS app_error_code,
  DROP COLUMN IF EXISTS error_message;

COMMIT;
