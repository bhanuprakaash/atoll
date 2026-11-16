CREATE TABLE pr_dismissals (
	thread_id    TEXT NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
	url          TEXT NOT NULL,
	dismissed_at INTEGER NOT NULL,
	PRIMARY KEY (thread_id, url)
);
INSERT INTO pr_dismissals (thread_id, url, dismissed_at)
	SELECT p.thread_id, p.url, t.island_dismissed_at
	FROM thread_prs p JOIN threads t ON t.id = p.thread_id
	WHERE t.island_dismissed_at > 0;
ALTER TABLE threads DROP COLUMN island_dismissed_at;
