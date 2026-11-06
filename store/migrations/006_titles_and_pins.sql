ALTER TABLE threads ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE bots ADD COLUMN pinned_at INTEGER;

WITH RECURSIVE first(id, rest) AS (
	SELECT t.id, trim(replace(replace(replace(m.text, char(9), ' '), char(10), ' '), char(13), ' '))
	FROM threads t
	JOIN messages m ON m.rowid = (
		SELECT min(rowid) FROM messages WHERE thread_id = t.id AND role = 'user')
),
words(id, n, word, rest) AS (
	SELECT id, 0, '', rest FROM first WHERE rest != ''
	UNION ALL
	SELECT id, n + 1,
		CASE WHEN instr(rest, ' ') = 0 THEN rest ELSE substr(rest, 1, instr(rest, ' ') - 1) END,
		CASE WHEN instr(rest, ' ') = 0 THEN '' ELSE ltrim(substr(rest, instr(rest, ' ') + 1)) END
	FROM words WHERE rest != '' AND n < 6
),
kept AS (
	SELECT id, n, word, max(n) OVER (PARTITION BY id) AS last FROM words WHERE n > 0
)
UPDATE threads SET title = (
	SELECT group_concat(CASE WHEN n = last THEN rtrim(word, '.,;:!?') ELSE word END, ' ')
	FROM (SELECT * FROM kept WHERE kept.id = threads.id ORDER BY n))
WHERE id IN (SELECT id FROM kept);
