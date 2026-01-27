BEGIN;

ALTER TABLE orchestrator.withdrawals
  ADD CONSTRAINT fk_withdrawals_idem_withdrawal
  FOREIGN KEY (id) REFERENCES orchestrator.idempotency_keys(withdrawal_id)
  ON DELETE RESTRICT;

COMMIT;
