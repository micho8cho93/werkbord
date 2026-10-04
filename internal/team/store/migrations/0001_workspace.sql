-- Werkbord Team: workspaces, members, projects and who is on which project.
-- Timestamps are Unix milliseconds (UTC).
--
-- A role is free text on purpose: what a role may do lives in code
-- (internal/team/domain/roles.go), so a new role needs no migration.

CREATE TABLE workspaces (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE members (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    email        TEXT    NOT NULL DEFAULT '',
    role         TEXT    NOT NULL,
    -- SHA-256 of the member's token; the token itself is shown once and never kept.
    token_hash   TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    UNIQUE (id, workspace_id)
) STRICT;

-- A workspace has exactly one owner.
CREATE UNIQUE INDEX members_one_owner ON members (workspace_id) WHERE role = 'owner';
-- Two people in a workspace cannot share a name (ignoring case): it would make
-- "who is this?" unanswerable.
CREATE UNIQUE INDEX members_name ON members (workspace_id, lower(name));

CREATE TABLE projects (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    repository   TEXT    NOT NULL DEFAULT '',
    archived     INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (id, workspace_id)
) STRICT;

CREATE UNIQUE INDEX projects_name ON projects (workspace_id, lower(name));

-- The composite foreign keys make it impossible, in the database and not only in
-- the service, to put a member on a project of another workspace.
CREATE TABLE project_members (
    project_id   TEXT    NOT NULL,
    member_id    TEXT    NOT NULL,
    workspace_id TEXT    NOT NULL,
    added_by     TEXT    NOT NULL,
    added_at     INTEGER NOT NULL,
    PRIMARY KEY (project_id, member_id),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (member_id, workspace_id)  REFERENCES members (id, workspace_id)  ON DELETE CASCADE
) STRICT;

CREATE INDEX project_members_by_member ON project_members (member_id);
