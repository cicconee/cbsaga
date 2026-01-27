BEGIN;

ALTER TABLE orchestrator.withdrawals
  DROP CONSTRAINT IF EXISTS fk_withdrawals_idem_withdrawal;

COMMIT;
