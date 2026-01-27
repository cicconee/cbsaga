BEGIN;

ALTER TABLE orchestrator.idempotency_keys
  ADD CONSTRAINT ux_idem_withdrawal_id UNIQUE (withdrawal_id);

COMMIT;
