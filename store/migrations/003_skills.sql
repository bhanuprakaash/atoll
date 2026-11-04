ALTER TABLE bots ADD COLUMN skills TEXT NOT NULL DEFAULT '[]';
ALTER TABLE bots ADD COLUMN can_manage_platform INTEGER NOT NULL DEFAULT 0;
ALTER TABLE bots ADD COLUMN removed INTEGER NOT NULL DEFAULT 0;

CREATE TABLE skills (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 content TEXT NOT NULL
);

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
CREATE INDEX changes_thread ON changes (source_thread_id);
