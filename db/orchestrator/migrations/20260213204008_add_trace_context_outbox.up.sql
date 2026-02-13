BEGIN;

ALTER TABLE orchestrator.outbox_events
  ADD COLUMN IF NOT EXISTS traceparent TEXT NULL,
  ADD COLUMN IF NOT EXISTS tracestate TEXT NULL;

COMMIT;
