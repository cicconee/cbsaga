BEGIN;

ALTER TABLE orchestrator.outbox_events
  ADD COLUMN IF NOT EXISTS route_key TEXT NOT NULL DEFAULT 'evt.withdrawal';

UPDATE orchestrator.outbox_events 
SET route_key = CASE 
  WHEN event_type = 'WithdrawalRequested' THEN 'cmd.identity'
  WHEN event_type = 'RiskCheckCreated' THEN 'cmd.risk'
  ELSE 'evt.withdrawal'
END
WHERE route_key IN ('', 'evt.withdrawal') OR route_key IS NULL;

CREATE INDEX IF NOT EXISTS idx_orch_outbox_route_created
  ON orchestrator.outbox_events (route_key, created_at ASC);

COMMIT;
