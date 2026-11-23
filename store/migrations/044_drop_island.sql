DROP TABLE pr_dismissals;
ALTER TABLE changes DROP COLUMN dismissed_at;
DELETE FROM settings WHERE key = 'island_seen_at';
