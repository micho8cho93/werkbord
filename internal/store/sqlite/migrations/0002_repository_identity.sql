-- A repository's identity is its shared Git directory, not its working-tree
-- path: a linked worktree has a different path but is the same repository.
-- Rows written before this migration keep '' (unknown) until their next
-- inspection, and unknown values are exempt from the uniqueness rule.

ALTER TABLE git_repositories ADD COLUMN common_dir TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX git_repositories_common_dir ON git_repositories (common_dir) WHERE common_dir != '';
