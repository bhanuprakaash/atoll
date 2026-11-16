CREATE TABLE pr_dismissals (
	thread_id    TEXT NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
	url          TEXT NOT NULL,
	dismissed_at INTEGER NOT NULL,
	PRIMARY KEY (thread_id, url)
);
ALTER TABLE threads DROP COLUMN island_dismissed_at;
