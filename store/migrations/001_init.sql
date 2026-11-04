CREATE TABLE bots (
	id       TEXT PRIMARY KEY,
	name     TEXT NOT NULL,
	persona  TEXT NOT NULL,
	provider TEXT NOT NULL
);

CREATE TABLE threads (
	id      TEXT PRIMARY KEY,
	bot_id  TEXT NOT NULL REFERENCES bots (id),
	session TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL
);

CREATE TABLE runs (
	id        TEXT PRIMARY KEY,
	thread_id TEXT NOT NULL REFERENCES threads (id),
	status    TEXT NOT NULL,
	reply     TEXT NOT NULL DEFAULT '',
	error     TEXT NOT NULL DEFAULT '',
	report    TEXT
);

CREATE TABLE messages (
	id        TEXT PRIMARY KEY,
	thread_id TEXT NOT NULL REFERENCES threads (id),
	role      TEXT NOT NULL,
	text      TEXT NOT NULL,
	run_id    TEXT NOT NULL REFERENCES runs (id)
);

CREATE INDEX threads_bot ON threads (bot_id);
CREATE INDEX messages_thread ON messages (thread_id);
