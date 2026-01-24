BEGIN;

DROP INDEX IF EXISTS idx_identity_outbox_route_created;

ALTER TABLE identity.outbox_events
  DROP COLUMN IF EXISTS route_key;

COMMIT;
