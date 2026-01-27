BEGIN;

ALTER TABLE orchestrator.idempotency_keys
  DROP CONSTRAINT IF EXISTS ux_idem_withdrawal_id;

COMMIT;
