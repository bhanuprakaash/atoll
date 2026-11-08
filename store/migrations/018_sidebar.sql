ALTER TABLE threads ADD COLUMN pinned_at INTEGER;
ALTER TABLE threads ADD COLUMN last_opened_at INTEGER NOT NULL DEFAULT 0;
UPDATE threads SET last_opened_at = last_activity;

CREATE TABLE bot_groups (
	id       TEXT PRIMARY KEY,
	name     TEXT NOT NULL,
	position INTEGER NOT NULL,
	folded   INTEGER NOT NULL DEFAULT 0
);

ALTER TABLE bots ADD COLUMN group_id TEXT REFERENCES bot_groups (id) ON DELETE SET NULL;

CREATE INDEX runs_thread ON runs (thread_id);
CREATE INDEX threads_activity ON threads (last_activity);
