ALTER TABLE threads ADD COLUMN checkout_git_dir TEXT NOT NULL DEFAULT '';
ALTER TABLE threads ADD COLUMN worktree_git_dir TEXT NOT NULL DEFAULT '';
