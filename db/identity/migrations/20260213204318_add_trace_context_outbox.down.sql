BEGIN;

ALTER TABLE identity.outbox_events
  DROP COLUMN IF EXISTS traceparent,
  DROP COLUMN IF EXISTS tracestate;

COMMIT;
