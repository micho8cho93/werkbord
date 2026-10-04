-- migrate:foreign-keys-off
--
-- Execution policy, and the blocked run state.
--
-- * A task has an execution policy: how its runs are carried out. A run keeps a
--   copy of the one it started with. It is JSON in one column, so that finer
--   grained permissions can become further fields of it without another
--   migration; only what the controller acts on today (the interaction mode) is
--   constrained here.
-- * A run can be 'blocked': its policy forbids guessing and the agent reached a
--   decision it could not safely infer. It records a structured blocker, and a
--   blocked run has one.
-- * A question records who answered it: the user, or the run's policy, which
--   replies on the user's behalf to questions it does not put to them.
-- * Questions and runs are read per project, so they get indexes for it.
--
-- `runs.state` has a CHECK constraint, which SQLite cannot change in place, so the
-- table is rebuilt following SQLite's documented procedure for it. That is why
-- this migration runs with foreign keys off (the runner turns them off outside the
-- transaction, and verifies them before committing): dropping `runs` with them on
-- would cascade-delete every question and detach every worktree. Everything that
-- hangs off `runs` is recreated below, with 'blocked' counted as an active state.

-- ---- tasks ----

ALTER TABLE tasks ADD COLUMN policy TEXT NOT NULL DEFAULT '{"interaction":"interactive"}'
    CHECK (json_valid(policy) AND json_extract(policy, '$.interaction') IN ('interactive', 'autonomous', 'autonomous_stop_if_blocked'));

-- ---- questions ----

ALTER TABLE questions ADD COLUMN answered_by TEXT NOT NULL DEFAULT 'user' CHECK (answered_by IN ('user', 'policy'));

CREATE INDEX questions_pending_by_project ON questions (project_id, asked_at) WHERE state = 'pending';

-- ---- runs ----

-- A trigger on `worktrees` reads `runs`, which is about to be replaced; it is
-- recreated afterwards, counting blocked runs as in use.
DROP TRIGGER worktrees_not_removed_while_in_use;

CREATE TABLE runs_new (
    id          TEXT    PRIMARY KEY,
    task_id     TEXT    NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    project_id  TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id    TEXT    NOT NULL,
    state       TEXT    NOT NULL CHECK (state IN ('starting', 'running', 'waiting_for_user', 'blocked', 'completed', 'failed', 'stopped')),
    worktree_id TEXT    REFERENCES worktrees(id) ON DELETE SET NULL,
    session_ref TEXT    NOT NULL DEFAULT '',
    reason      TEXT    NOT NULL DEFAULT '',
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    ended_at    INTEGER,
    prompt      TEXT    NOT NULL DEFAULT '',
    waiting     TEXT    NOT NULL DEFAULT '' CHECK (waiting IN ('', 'question', 'idle')),
    activity    TEXT    NOT NULL DEFAULT '',
    activity_at INTEGER,
    exit_code   INTEGER,
    pid         INTEGER NOT NULL DEFAULT 0,
    process_id  TEXT    NOT NULL DEFAULT '',
    policy      TEXT    NOT NULL DEFAULT '{"interaction":"interactive"}'
        CHECK (json_valid(policy) AND json_extract(policy, '$.interaction') IN ('interactive', 'autonomous', 'autonomous_stop_if_blocked')),
    blocker     TEXT    NOT NULL DEFAULT '' CHECK (blocker = '' OR json_valid(blocker)),

    -- A blocked run says why.
    CHECK (state != 'blocked' OR blocker != '')
) STRICT;

INSERT INTO runs_new
    (id, task_id, project_id, agent_id, state, worktree_id, session_ref, reason, version, created_at, updated_at, ended_at,
     prompt, waiting, activity, activity_at, exit_code, pid, process_id)
SELECT id, task_id, project_id, agent_id, state, worktree_id, session_ref, reason, version, created_at, updated_at, ended_at,
       prompt, waiting, activity, activity_at, exit_code, pid, process_id
FROM runs ORDER BY rowid;

DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;

CREATE INDEX runs_by_task ON runs (task_id, created_at);
CREATE INDEX runs_by_project ON runs (project_id, created_at);
CREATE INDEX runs_active ON runs (created_at) WHERE state IN ('starting', 'running', 'waiting_for_user', 'blocked');

-- Concurrent runs never share a working directory.
CREATE UNIQUE INDEX runs_active_worktree ON runs (worktree_id)
WHERE worktree_id IS NOT NULL AND state IN ('starting', 'running', 'waiting_for_user', 'blocked');

CREATE TRIGGER runs_waiting_only_when_waiting_insert BEFORE INSERT ON runs
WHEN (NEW.state = 'waiting_for_user') != (NEW.waiting != '')
BEGIN
  SELECT RAISE(ABORT, 'waiting is set exactly while a run is waiting_for_user');
END;

CREATE TRIGGER runs_waiting_only_when_waiting_update BEFORE UPDATE OF state, waiting ON runs
WHEN (NEW.state = 'waiting_for_user') != (NEW.waiting != '')
BEGIN
  SELECT RAISE(ABORT, 'waiting is set exactly while a run is waiting_for_user');
END;

CREATE TRIGGER runs_no_process_when_ended BEFORE UPDATE OF state, pid ON runs
WHEN NEW.state IN ('completed', 'failed', 'stopped') AND NEW.pid != 0
BEGIN
  SELECT RAISE(ABORT, 'an ended run has no process');
END;

-- A run only ever uses a worktree of its own project, and a live run is never
-- attached to one that is being, or has been, removed.
CREATE TRIGGER runs_worktree_is_usable_on_insert BEFORE INSERT ON runs
WHEN NEW.worktree_id IS NOT NULL
BEGIN
  SELECT RAISE(ABORT, 'run and worktree belong to different projects')
  WHERE (SELECT project_id FROM worktrees WHERE id = NEW.worktree_id) != NEW.project_id;
  SELECT RAISE(ABORT, 'worktree is removed or being removed')
  WHERE NEW.state IN ('starting', 'running', 'waiting_for_user', 'blocked')
    AND (SELECT state = 'removed' OR removing_since IS NOT NULL FROM worktrees WHERE id = NEW.worktree_id);
END;

CREATE TRIGGER runs_worktree_is_usable_on_update BEFORE UPDATE OF worktree_id, project_id ON runs
WHEN NEW.worktree_id IS NOT NULL AND (NEW.worktree_id IS NOT OLD.worktree_id OR NEW.project_id != OLD.project_id)
BEGIN
  SELECT RAISE(ABORT, 'run and worktree belong to different projects')
  WHERE (SELECT project_id FROM worktrees WHERE id = NEW.worktree_id) != NEW.project_id;
  SELECT RAISE(ABORT, 'worktree is removed or being removed')
  WHERE NEW.state IN ('starting', 'running', 'waiting_for_user', 'blocked')
    AND (SELECT state = 'removed' OR removing_since IS NOT NULL FROM worktrees WHERE id = NEW.worktree_id);
END;

-- Beginning removal under a live run would destroy that run's uncommitted work.
CREATE TRIGGER worktrees_not_removed_while_in_use BEFORE UPDATE OF removing_since ON worktrees
WHEN OLD.removing_since IS NULL AND NEW.removing_since IS NOT NULL
  AND EXISTS (SELECT 1 FROM runs WHERE worktree_id = OLD.id AND state IN ('starting', 'running', 'waiting_for_user', 'blocked'))
BEGIN
  SELECT RAISE(ABORT, 'worktree is in use by an active run');
END;
