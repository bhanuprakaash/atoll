ALTER TABLE threads ADD COLUMN session_provider TEXT NOT NULL DEFAULT '';
UPDATE threads SET session_provider = (SELECT provider FROM bots WHERE bots.id = threads.bot_id)
WHERE session != '';
