ALTER TABLE inbox ADD COLUMN queued INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inbox ADD COLUMN from_user INTEGER NOT NULL DEFAULT 0;

CREATE TABLE inbox_images (
	id         TEXT PRIMARY KEY,
	inbox_id   TEXT NOT NULL REFERENCES inbox (id) ON DELETE CASCADE,
	mime       TEXT NOT NULL,
	data       BLOB NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX inbox_images_inbox ON inbox_images (inbox_id);
