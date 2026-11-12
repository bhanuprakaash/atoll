CREATE TABLE answered_approvals (
	id         TEXT PRIMARY KEY,
	message_id TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
	tool       TEXT NOT NULL,
	input      TEXT NOT NULL
);
CREATE INDEX answered_approvals_message ON answered_approvals (message_id);
