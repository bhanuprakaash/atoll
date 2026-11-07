ALTER TABLE messages ADD COLUMN from_bot_id TEXT NOT NULL DEFAULT '';

CREATE TABLE inbox (
	id          TEXT PRIMARY KEY,
	thread_id   TEXT NOT NULL,
	from_bot_id TEXT NOT NULL,
	text        TEXT NOT NULL,
	created     INTEGER NOT NULL
);
CREATE INDEX inbox_thread ON inbox (thread_id);
