ALTER TABLE threads ADD COLUMN mr_changed_at INTEGER NOT NULL DEFAULT 0;

INSERT INTO settings (key, value) VALUES ('island_seen_at', CAST(unixepoch('subsec') * 1000000 AS INTEGER));
