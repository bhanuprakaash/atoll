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
);

CREATE INDEX memory_scope ON memory (scope, scope_id, status);

CREATE VIRTUAL TABLE memory_fts USING fts5(
    body, why, content='memory', content_rowid='seq', tokenize='porter unicode61'
);

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

CREATE TABLE memory_seen (
    session TEXT NOT NULL,
    item_id TEXT NOT NULL,
    PRIMARY KEY (session, item_id)
);
