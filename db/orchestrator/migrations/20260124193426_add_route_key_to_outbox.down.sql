BEGIN;

DROP INDEX IF EXISTS idx_orch_outbox_route_created;

ALTER TABLE orchestrator.outbox_events
  DROP COLUMN IF EXISTS route_key;

COMMIT;
