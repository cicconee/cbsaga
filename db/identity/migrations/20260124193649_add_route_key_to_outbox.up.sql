BEGIN;

ALTER TABLE identity.outbox_events 
  ADD COLUMN IF NOT EXISTS route_key TEXT NOT NULL DEFAULT 'evt.identity';

CREATE INDEX IF NOT EXISTS idx_identity_outbox_route_created
  ON identity.outbox_events (route_key, created_at ASC);

COMMIT;
