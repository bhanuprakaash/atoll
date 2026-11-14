UPDATE bots
SET persona = CASE WHEN persona = '' THEN '' ELSE persona || char(10) END ||
	'Never merge: ' || COALESCE(NULLIF((SELECT value FROM settings WHERE key = 'user_name'), ''), 'The user') || ' merges.'
WHERE id <> 'chief' AND (repo_access IS NULL OR repo_access <> '[]');
