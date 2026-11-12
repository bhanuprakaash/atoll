ALTER TABLE threads ADD COLUMN last_heard INTEGER NOT NULL DEFAULT 0;
UPDATE threads SET last_heard = last_activity;
