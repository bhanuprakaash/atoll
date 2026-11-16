CREATE TABLE thread_prs (
	thread_id  TEXT NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
	url        TEXT NOT NULL,
	state      TEXT NOT NULL,
	title      TEXT NOT NULL DEFAULT '',
	ci         TEXT NOT NULL DEFAULT '',
	woke       TEXT NOT NULL DEFAULT '',
	wake       INTEGER NOT NULL DEFAULT 0,
	head       TEXT NOT NULL DEFAULT '',
	base_woke  INTEGER NOT NULL DEFAULT 0,
	draft      INTEGER NOT NULL DEFAULT 0,
	body       TEXT NOT NULL DEFAULT '',
	base       TEXT NOT NULL DEFAULT '',
	changed_at INTEGER NOT NULL DEFAULT 0,
	tracked_at INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (thread_id, url)
);
INSERT INTO thread_prs (thread_id, url, state, title, ci, woke, wake, head, base_woke, changed_at, tracked_at)
	SELECT id, mr_url, mr_state, mr_title, mr_ci, mr_woke, mr_wake, mr_head, mr_base_woke, mr_changed_at, created
	FROM threads WHERE mr_url != '';
ALTER TABLE threads DROP COLUMN mr_url;
ALTER TABLE threads DROP COLUMN mr_state;
ALTER TABLE threads DROP COLUMN mr_title;
ALTER TABLE threads DROP COLUMN mr_ci;
ALTER TABLE threads DROP COLUMN mr_woke;
ALTER TABLE threads DROP COLUMN mr_wake;
ALTER TABLE threads DROP COLUMN mr_head;
ALTER TABLE threads DROP COLUMN mr_base_woke;
ALTER TABLE threads DROP COLUMN mr_changed_at;
