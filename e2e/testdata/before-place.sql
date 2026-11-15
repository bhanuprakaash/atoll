-- atoll.db as `atoll serve` left it before Bots had a position (schema version 34): Chief, Bots Fir
-- and Ash pinned in that order, Ivy and Oak in Backend (Oak moved there first), Yew in Frontend, Elm
-- ungrouped.
/* WARNING: Script requires that SQLITE_DBCONFIG_DEFENSIVE be disabled */
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE bots (
	id       TEXT PRIMARY KEY,
	name     TEXT NOT NULL,
	persona  TEXT NOT NULL,
	provider TEXT NOT NULL
, repo_access TEXT, connections TEXT NOT NULL DEFAULT '[]', skills TEXT NOT NULL DEFAULT '[]', can_manage_platform INTEGER NOT NULL DEFAULT 0, removed INTEGER NOT NULL DEFAULT 0, look TEXT NOT NULL DEFAULT 'jellyfish', model TEXT NOT NULL DEFAULT '', pinned_at INTEGER, mcp_servers TEXT NOT NULL DEFAULT '[]', mode TEXT NOT NULL DEFAULT 'auto', delegates TEXT NOT NULL DEFAULT '[]', group_id TEXT REFERENCES bot_groups (id) ON DELETE SET NULL);
INSERT INTO bots VALUES('chief','Chief','I help configure Reef.','claude','[]','[]','[]',1,0,'jellyfish','',NULL,'[]','auto','[]',NULL);
INSERT INTO bots VALUES('7ee82b61ac592deb','Ash','Old.','claude',NULL,'[]','[]',0,0,'pufferfish','',1794673845592550,'[]','auto','[]',NULL);
INSERT INTO bots VALUES('72f1d469730c4ad5','Ivy','Old.','claude',NULL,'[]','[]',0,0,'octopus','',NULL,'[]','auto','[]','5a1e2abe3a367346');
INSERT INTO bots VALUES('3cb1aad5c1760712','Oak','Old.','claude',NULL,'[]','[]',0,0,'octopus','',NULL,'[]','auto','[]','5a1e2abe3a367346');
INSERT INTO bots VALUES('74767a8041e9d3e8','Elm','Old.','claude',NULL,'[]','[]',0,0,'manta','',NULL,'[]','auto','[]',NULL);
INSERT INTO bots VALUES('a338e0ee05ca5499','Fir','Old.','claude',NULL,'[]','[]',0,0,'crab','',1794673845586130,'[]','auto','[]',NULL);
INSERT INTO bots VALUES('9599aad57e538cd9','Yew','Old.','claude',NULL,'[]','[]',0,0,'whale','',NULL,'[]','auto','[]','6f7b7cba926517dc');
CREATE TABLE threads (
	id      TEXT PRIMARY KEY,
	bot_id  TEXT NOT NULL REFERENCES bots (id),
	session TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL
, repo_id TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL DEFAULT '', worktree TEXT NOT NULL DEFAULT '', mr_url TEXT NOT NULL DEFAULT '', mr_state TEXT NOT NULL DEFAULT '', closed INTEGER NOT NULL DEFAULT 0, last_activity INTEGER NOT NULL DEFAULT 0, mr_title TEXT NOT NULL DEFAULT '', worktree_removed INTEGER NOT NULL DEFAULT 0, closed_at INTEGER, snapshot_ref TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', parent_thread_id TEXT NOT NULL DEFAULT '', pinned_at INTEGER, last_opened_at INTEGER NOT NULL DEFAULT 0, session_provider TEXT NOT NULL DEFAULT '', checkout_git_dir TEXT NOT NULL DEFAULT '', worktree_git_dir TEXT NOT NULL DEFAULT '', mr_ci TEXT NOT NULL DEFAULT '', mr_woke TEXT NOT NULL DEFAULT '', mr_wake INTEGER NOT NULL DEFAULT 0, mr_head TEXT NOT NULL DEFAULT '', mr_base_woke INTEGER NOT NULL DEFAULT 0, session_cost_usd REAL NOT NULL DEFAULT 0, mr_changed_at INTEGER NOT NULL DEFAULT 0, last_heard INTEGER NOT NULL DEFAULT 0, island_dismissed_at INTEGER NOT NULL DEFAULT 0, last_failed INTEGER NOT NULL DEFAULT 0, limited_until INTEGER NOT NULL DEFAULT 0);
CREATE TABLE runs (
	id        TEXT PRIMARY KEY,
	thread_id TEXT NOT NULL REFERENCES threads (id),
	status    TEXT NOT NULL,
	reply     TEXT NOT NULL DEFAULT '',
	error     TEXT NOT NULL DEFAULT '',
	report    TEXT
, started_at INTEGER NOT NULL DEFAULT 0, finished_at INTEGER NOT NULL DEFAULT 0, tools TEXT NOT NULL DEFAULT '{}', error_kind TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '', resets_at TEXT NOT NULL DEFAULT '', usage TEXT, cost_usd REAL);
CREATE TABLE messages (
	id        TEXT PRIMARY KEY,
	thread_id TEXT NOT NULL REFERENCES threads (id),
	role      TEXT NOT NULL,
	text      TEXT NOT NULL,
	run_id    TEXT NOT NULL REFERENCES runs (id)
, from_bot_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL DEFAULT 0);
CREATE TABLE repos (
	id   TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	path TEXT NOT NULL
);
CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
INSERT INTO settings VALUES('island_seen_at','1794673833191000');
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
, dismissed_at INTEGER NOT NULL DEFAULT 0);
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
CREATE TABLE inbox (
	id          TEXT PRIMARY KEY,
	thread_id   TEXT NOT NULL,
	from_bot_id TEXT NOT NULL,
	text        TEXT NOT NULL,
	created     INTEGER NOT NULL
, kind TEXT NOT NULL DEFAULT '', queued INTEGER NOT NULL DEFAULT 0, from_user INTEGER NOT NULL DEFAULT 0, notice TEXT NOT NULL DEFAULT '');
CREATE TABLE attachments (
	id         TEXT PRIMARY KEY,
	message_id TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
	mime       TEXT NOT NULL,
	data       BLOB NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE bot_groups (
	id       TEXT PRIMARY KEY,
	name     TEXT NOT NULL,
	position INTEGER NOT NULL,
	folded   INTEGER NOT NULL DEFAULT 0
);
INSERT INTO bot_groups VALUES('5a1e2abe3a367346','Backend',1,0);
INSERT INTO bot_groups VALUES('6f7b7cba926517dc','Frontend',2,0);
CREATE TABLE inbox_images (
	id         TEXT PRIMARY KEY,
	inbox_id   TEXT NOT NULL REFERENCES inbox (id) ON DELETE CASCADE,
	mime       TEXT NOT NULL,
	data       BLOB NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE answered_approvals (
	id         TEXT PRIMARY KEY,
	message_id TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
	tool       TEXT NOT NULL,
	input      TEXT NOT NULL
);
CREATE TABLE memory (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    body TEXT NOT NULL,
    why TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL CHECK (scope IN ('user', 'repo', 'bot')),
    scope_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('candidate', 'accepted', 'retired')),
    source_bot_id TEXT NOT NULL DEFAULT '',
    source_thread_id TEXT NOT NULL DEFAULT '',
    source_run_id TEXT NOT NULL DEFAULT '',
    replaces TEXT NOT NULL DEFAULT '[]',
    replaced_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    retired_at TEXT,
    vector BLOB
, ticket TEXT NOT NULL DEFAULT '', retired_reason TEXT NOT NULL DEFAULT '');
PRAGMA writable_schema=ON;
INSERT INTO sqlite_schema(type,name,tbl_name,rootpage,sql)VALUES('table','memory_fts','memory_fts',0,'CREATE VIRTUAL TABLE memory_fts USING fts5(
    body, why, content=''memory'', content_rowid=''seq'', tokenize=''porter unicode61''
)');
CREATE TABLE IF NOT EXISTS 'memory_fts_data'(id INTEGER PRIMARY KEY, block BLOB);
INSERT INTO memory_fts_data VALUES(1,x'');
INSERT INTO memory_fts_data VALUES(10,x'00000000000000');
CREATE TABLE IF NOT EXISTS 'memory_fts_idx'(segid, term, pgno, PRIMARY KEY(segid, term)) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS 'memory_fts_docsize'(id INTEGER PRIMARY KEY, sz BLOB);
CREATE TABLE IF NOT EXISTS 'memory_fts_config'(k PRIMARY KEY, v) WITHOUT ROWID;
INSERT INTO memory_fts_config VALUES('version',4);
CREATE TABLE memory_seen (
    session TEXT NOT NULL,
    item_id TEXT NOT NULL,
    PRIMARY KEY (session, item_id)
);
CREATE TABLE memory_version (version INTEGER NOT NULL);
INSERT INTO memory_version VALUES(2);
CREATE TRIGGER memory_fts_insert AFTER INSERT ON memory BEGIN
    INSERT INTO memory_fts (rowid, body, why) VALUES (new.seq, new.body, new.why);
END;
CREATE TRIGGER memory_fts_delete AFTER DELETE ON memory BEGIN
    INSERT INTO memory_fts (memory_fts, rowid, body, why) VALUES ('delete', old.seq, old.body, old.why);
END;
CREATE TRIGGER memory_fts_update AFTER UPDATE OF body, why ON memory BEGIN
    INSERT INTO memory_fts (memory_fts, rowid, body, why) VALUES ('delete', old.seq, old.body, old.why);
    INSERT INTO memory_fts (rowid, body, why) VALUES (new.seq, new.body, new.why);
END;
CREATE INDEX messages_thread ON messages (thread_id);
CREATE INDEX changes_thread ON changes (source_thread_id);
CREATE INDEX inbox_thread ON inbox (thread_id);
CREATE INDEX threads_parent_thread_id ON threads (parent_thread_id);
CREATE INDEX attachments_message ON attachments (message_id);
CREATE INDEX runs_thread ON runs (thread_id);
CREATE INDEX threads_activity ON threads (last_activity);
CREATE INDEX threads_bot_activity ON threads (bot_id, last_activity);
CREATE INDEX inbox_images_inbox ON inbox_images (inbox_id);
CREATE INDEX runs_cost_finished ON runs (finished_at) WHERE cost_usd IS NOT NULL;
CREATE INDEX answered_approvals_message ON answered_approvals (message_id);
CREATE INDEX memory_scope ON memory (scope, scope_id, status);
PRAGMA writable_schema=OFF;
COMMIT;
PRAGMA user_version = 34;
