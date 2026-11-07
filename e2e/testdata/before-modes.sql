-- atoll.db as `atoll serve` left it before Bots had a mode (schema version 11): Chief and two Bots.
CREATE TABLE bots (
	id       TEXT PRIMARY KEY,
	name     TEXT NOT NULL,
	persona  TEXT NOT NULL,
	provider TEXT NOT NULL
, repo_access TEXT, connections TEXT NOT NULL DEFAULT '[]', skills TEXT NOT NULL DEFAULT '[]', can_manage_platform INTEGER NOT NULL DEFAULT 0, removed INTEGER NOT NULL DEFAULT 0, look TEXT NOT NULL DEFAULT 'jellyfish', model TEXT NOT NULL DEFAULT '', pinned_at INTEGER, mcp_servers TEXT NOT NULL DEFAULT '[]');
INSERT INTO bots VALUES('chief','Chief','I help configure Reef.','claude','[]','[]','[]',1,0,'jellyfish','',NULL,'[]');
INSERT INTO bots VALUES('c31d276614b94b87','Old claude','Was here before modes.','claude',NULL,'[]','[]',0,0,'shark','',NULL,'[]');
INSERT INTO bots VALUES('34bf601fa249914c','Old codex','Was here before modes.','codex',NULL,'[]','[]',0,0,'shell','',NULL,'[]');
CREATE TABLE threads (
	id      TEXT PRIMARY KEY,
	bot_id  TEXT NOT NULL REFERENCES bots (id),
	session TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL
, repo_id TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL DEFAULT '', worktree TEXT NOT NULL DEFAULT '', mr_url TEXT NOT NULL DEFAULT '', mr_state TEXT NOT NULL DEFAULT '', closed INTEGER NOT NULL DEFAULT 0, last_activity INTEGER NOT NULL DEFAULT 0, mr_title TEXT NOT NULL DEFAULT '', worktree_removed INTEGER NOT NULL DEFAULT 0, closed_at INTEGER, snapshot_ref TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '');
CREATE TABLE runs (
	id        TEXT PRIMARY KEY,
	thread_id TEXT NOT NULL REFERENCES threads (id),
	status    TEXT NOT NULL,
	reply     TEXT NOT NULL DEFAULT '',
	error     TEXT NOT NULL DEFAULT '',
	report    TEXT
, started_at INTEGER NOT NULL DEFAULT 0, finished_at INTEGER NOT NULL DEFAULT 0, tools TEXT NOT NULL DEFAULT '{}', error_kind TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '');
CREATE TABLE messages (
	id        TEXT PRIMARY KEY,
	thread_id TEXT NOT NULL REFERENCES threads (id),
	role      TEXT NOT NULL,
	text      TEXT NOT NULL,
	run_id    TEXT NOT NULL REFERENCES runs (id)
);
CREATE TABLE repos (
	id   TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	path TEXT NOT NULL
);
CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE connections (
	id           TEXT PRIMARY KEY,
	kind         TEXT NOT NULL,
	instance_url TEXT NOT NULL,
	status       TEXT NOT NULL
);
CREATE TABLE skills (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 content TEXT NOT NULL
, source TEXT NOT NULL DEFAULT '', imported TEXT NOT NULL DEFAULT '');
CREATE TABLE changes (
 id TEXT PRIMARY KEY,
 entity TEXT NOT NULL,
 entity_id TEXT NOT NULL,
 action TEXT NOT NULL,
 before TEXT NOT NULL,
 after TEXT NOT NULL,
 undone INTEGER NOT NULL DEFAULT 0,
 source_bot_id TEXT NOT NULL,
 source_thread_id TEXT NOT NULL,
 source_run_id TEXT NOT NULL,
 created INTEGER NOT NULL
);
CREATE TABLE skill_files (
 skill_id TEXT NOT NULL REFERENCES skills (id) ON DELETE CASCADE,
 path TEXT NOT NULL,
 content TEXT NOT NULL,
 executable INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY (skill_id, path)
);
CREATE TABLE mcp_servers (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 kind TEXT NOT NULL,
 command TEXT NOT NULL DEFAULT '',
 args TEXT NOT NULL DEFAULT '[]',
 url TEXT NOT NULL DEFAULT '',
 env_names TEXT NOT NULL DEFAULT '[]',
 header_names TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE mcp_sign_ins (
 server_id TEXT PRIMARY KEY,
 status TEXT NOT NULL
);
CREATE INDEX threads_bot ON threads (bot_id);
CREATE INDEX messages_thread ON messages (thread_id);
CREATE INDEX changes_thread ON changes (source_thread_id);
PRAGMA user_version = 11;
