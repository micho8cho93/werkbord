-- Initial schema. Timestamps are Unix milliseconds (UTC). JSON columns hold
-- small arrays that are always read and written whole.

CREATE TABLE projects (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL,
    repo_path  TEXT    NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE git_repositories (
    project_id     TEXT    PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    root_path      TEXT    NOT NULL,
    current_branch TEXT    NOT NULL DEFAULT '',
    head_commit    TEXT    NOT NULL DEFAULT '',
    default_branch TEXT    NOT NULL DEFAULT '',
    remotes        TEXT    NOT NULL DEFAULT '[]',
    inspected_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE tasks (
    id          TEXT    PRIMARY KEY,
    project_id  TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title       TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    state       TEXT    NOT NULL CHECK (state IN ('backlog', 'doing', 'review', 'done')),
    position    REAL    NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX tasks_by_column ON tasks (project_id, state, position);

CREATE TABLE worktrees (
    id         TEXT    PRIMARY KEY,
    project_id TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path       TEXT    NOT NULL UNIQUE,
    branch     TEXT    NOT NULL,
    base_ref   TEXT    NOT NULL,
    state      TEXT    NOT NULL CHECK (state IN ('active', 'removed')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX worktrees_by_project ON worktrees (project_id);

CREATE TABLE runs (
    id          TEXT    PRIMARY KEY,
    task_id     TEXT    NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    project_id  TEXT    NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id    TEXT    NOT NULL,
    state       TEXT    NOT NULL CHECK (state IN ('starting', 'running', 'waiting_for_user', 'completed', 'failed', 'stopped')),
    worktree_id TEXT    REFERENCES worktrees(id) ON DELETE SET NULL,
    session_ref TEXT    NOT NULL DEFAULT '',
    reason      TEXT    NOT NULL DEFAULT '',
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    ended_at    INTEGER
) STRICT;

CREATE INDEX runs_by_task ON runs (task_id, created_at);
CREATE INDEX runs_active ON runs (created_at) WHERE state IN ('starting', 'running', 'waiting_for_user');

CREATE TABLE questions (
    id          TEXT    PRIMARY KEY,
    run_id      TEXT    NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    prompt      TEXT    NOT NULL,
    options     TEXT    NOT NULL DEFAULT '[]',
    status      TEXT    NOT NULL CHECK (status IN ('pending', 'answered', 'cancelled')),
    answer      TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    answered_at INTEGER
) STRICT;

CREATE INDEX questions_by_run ON questions (run_id, created_at);
CREATE INDEX questions_pending ON questions (created_at) WHERE status = 'pending';

-- Events deliberately have no foreign keys: the log outlives the rows it
-- describes.
CREATE TABLE events (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    type       TEXT    NOT NULL,
    project_id TEXT,
    task_id    TEXT,
    run_id     TEXT,
    payload    TEXT,
    created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX events_by_project ON events (project_id, seq);
