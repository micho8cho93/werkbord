-- Worktree integrity.
--
-- Removing a worktree deletes a directory, which cannot be undone. Whatever
-- does that will trust these rows, so the database refuses to hold a row that
-- could mislead it, regardless of which code wrote it. Rules that need the
-- filesystem or other projects (symlinks, overlap with repositories) live in
-- the service; these are the ones the database can state on its own.
--
-- Triggers are used rather than CHECK constraints or changed foreign keys
-- because SQLite cannot alter those in place, and rebuilding `worktrees` would
-- fire `runs.worktree_id ON DELETE SET NULL` and detach every run.

-- Compare-and-swap, like tasks and runs.
ALTER TABLE worktrees ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

-- Removal is two steps: first removing_since is set (refused while a run uses
-- the worktree, and from then on no run can start on it), then the directory
-- is deleted, then state becomes 'removed'. A single "mark removed" after the
-- deletion would check for a live run too late, and a run could start between
-- a check and the deletion.
ALTER TABLE worktrees ADD COLUMN removing_since INTEGER;

-- Shape: an absolute, clean path that is not the filesystem root, and a branch
-- and base ref that cannot be mistaken for a command-line option.
CREATE TRIGGER worktrees_well_formed BEFORE INSERT ON worktrees
WHEN substr(NEW.path, 1, 1) != '/'
  OR NEW.path = '/'
  OR substr(NEW.path, -1) = '/'
  OR instr(NEW.path, '//') > 0
  OR instr(NEW.path, '/./') > 0
  OR instr(NEW.path, '/../') > 0
  OR NEW.path LIKE '%/.'
  OR NEW.path LIKE '%/..'
  OR NEW.branch = '' OR substr(NEW.branch, 1, 1) = '-'
  OR NEW.base_ref = '' OR substr(NEW.base_ref, 1, 1) = '-'
BEGIN
  SELECT RAISE(ABORT, 'malformed worktree');
END;

-- What a worktree is cannot change after the fact: a cleanup that read the
-- row earlier must still be talking about the same directory and branch.
CREATE TRIGGER worktrees_identity_is_immutable BEFORE UPDATE ON worktrees
WHEN NEW.id != OLD.id OR NEW.project_id != OLD.project_id OR NEW.path != OLD.path
  OR NEW.branch != OLD.branch OR NEW.base_ref != OLD.base_ref OR NEW.created_at != OLD.created_at
BEGIN
  SELECT RAISE(ABORT, 'worktree identity is immutable');
END;

CREATE TRIGGER worktrees_stay_removed BEFORE UPDATE OF state ON worktrees
WHEN OLD.state = 'removed' AND NEW.state != 'removed'
BEGIN
  SELECT RAISE(ABORT, 'a removed worktree cannot become active again');
END;

-- Beginning removal under a live run would destroy that run's uncommitted work.
CREATE TRIGGER worktrees_not_removed_while_in_use BEFORE UPDATE OF removing_since ON worktrees
WHEN OLD.removing_since IS NULL AND NEW.removing_since IS NOT NULL
  AND EXISTS (SELECT 1 FROM runs WHERE worktree_id = OLD.id AND state IN ('starting', 'running', 'waiting_for_user'))
BEGIN
  SELECT RAISE(ABORT, 'worktree is in use by an active run');
END;

-- Once a deletion has been decided on, it cannot be taken back: the directory
-- may already be partly gone.
CREATE TRIGGER worktrees_removal_is_one_way BEFORE UPDATE OF removing_since ON worktrees
WHEN OLD.removing_since IS NOT NULL AND (NEW.removing_since IS NULL OR NEW.removing_since != OLD.removing_since)
BEGIN
  SELECT RAISE(ABORT, 'removal cannot be cancelled');
END;

-- 'removed' is only reachable through the first step, in a separate statement.
CREATE TRIGGER worktrees_removed_only_after_removal_begins BEFORE UPDATE OF state ON worktrees
WHEN NEW.state = 'removed' AND OLD.state != 'removed' AND OLD.removing_since IS NULL
BEGIN
  SELECT RAISE(ABORT, 'removal has not begun');
END;

-- An active record is the only evidence a directory exists. `projects` and
-- `runs` cascade or null out around it, so this also stops a project delete
-- from erasing the records of directories that are still on disk.
CREATE TRIGGER worktrees_active_records_are_kept BEFORE DELETE ON worktrees
WHEN OLD.state = 'active'
BEGIN
  SELECT RAISE(ABORT, 'worktree is active: mark it removed before deleting its record');
END;

-- Git refuses to check one branch out twice; so does the record.
CREATE UNIQUE INDEX worktrees_active_branch ON worktrees (project_id, branch) WHERE state = 'active';

-- Concurrent runs never share a working directory.
CREATE UNIQUE INDEX runs_active_worktree ON runs (worktree_id)
WHERE worktree_id IS NOT NULL AND state IN ('starting', 'running', 'waiting_for_user');

-- A run only ever uses a worktree of its own project, and a live run is never
-- attached to one that is being, or has been, removed.
CREATE TRIGGER runs_worktree_is_usable_on_insert BEFORE INSERT ON runs
WHEN NEW.worktree_id IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'run and worktree belong to different projects')
  WHERE (SELECT project_id FROM worktrees WHERE id = NEW.worktree_id) != NEW.project_id;
  SELECT RAISE(ABORT, 'worktree is removed or being removed')
  WHERE NEW.state IN ('starting', 'running', 'waiting_for_user')
    AND (SELECT state = 'removed' OR removing_since IS NOT NULL FROM worktrees WHERE id = NEW.worktree_id);
END;

CREATE TRIGGER runs_worktree_is_usable_on_update BEFORE UPDATE OF worktree_id, project_id ON runs
WHEN NEW.worktree_id IS NOT NULL AND (NEW.worktree_id IS NOT OLD.worktree_id OR NEW.project_id != OLD.project_id)
BEGIN
  SELECT RAISE(ABORT, 'run and worktree belong to different projects')
  WHERE (SELECT project_id FROM worktrees WHERE id = NEW.worktree_id) != NEW.project_id;
  SELECT RAISE(ABORT, 'worktree is removed or being removed')
  WHERE NEW.state IN ('starting', 'running', 'waiting_for_user')
    AND (SELECT state = 'removed' OR removing_since IS NOT NULL FROM worktrees WHERE id = NEW.worktree_id);
END;
