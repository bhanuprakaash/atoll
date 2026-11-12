ALTER TABLE runs ADD COLUMN usage TEXT;
ALTER TABLE runs ADD COLUMN cost_usd REAL;
ALTER TABLE threads ADD COLUMN session_cost_usd REAL NOT NULL DEFAULT 0;
CREATE INDEX runs_cost_finished ON runs (finished_at) WHERE cost_usd IS NOT NULL;
