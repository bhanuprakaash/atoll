DROP INDEX threads_bot;
CREATE INDEX threads_bot_activity ON threads (bot_id, last_activity);
