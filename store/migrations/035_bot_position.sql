ALTER TABLE bots ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
UPDATE bots SET position = (
	SELECT n FROM (
		SELECT id, row_number() OVER (ORDER BY pinned_at IS NULL, pinned_at, rowid) AS n FROM bots
	) ranked WHERE ranked.id = bots.id
);
