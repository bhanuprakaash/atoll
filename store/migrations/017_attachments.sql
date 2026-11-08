CREATE TABLE attachments (
	id         TEXT PRIMARY KEY,
	message_id TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
	mime       TEXT NOT NULL,
	data       BLOB NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX attachments_message ON attachments (message_id);
