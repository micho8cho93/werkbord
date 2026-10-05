-- Werkbord Team: the collaborative workflow. Project roles, invites, the shared
-- board (tickets), the Git metadata developers report about them, and the
-- project's activity. Timestamps are Unix milliseconds (UTC).
--
-- Everything here is coordination metadata. There is no column for a path on
-- anyone's computer, a credential, or an environment variable.

-- A member's role on one project (see internal/team/domain/project_roles.go). Free
-- text for the same reason workspace roles are: what a role may do lives in code.
ALTER TABLE project_members ADD COLUMN role TEXT NOT NULL DEFAULT 'member';

-- Bumped, in the same transaction, by every change to a project's board or to the
-- repository state reported for it. Clients ask "has the revision moved?" to keep
-- their view in step with everyone else's.
ALTER TABLE projects ADD COLUMN revision INTEGER NOT NULL DEFAULT 0;

CREATE TABLE tickets (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL,
    project_id   TEXT    NOT NULL,
    -- Numbered across the whole workspace, so a branch name such as
    -- wb-142-authentication-error is unambiguous even when two projects share a repository.
    number       INTEGER NOT NULL,
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    requirements TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL CHECK (status IN ('backlog', 'available', 'in_progress', 'review', 'done')),
    -- Who holds the ticket. A plain id, not a foreign key: the history must outlive
    -- a member who is removed from the workspace.
    assignee_id  TEXT,
    creator_id   TEXT    NOT NULL,
    reviewer_id  TEXT,
    branch       TEXT    NOT NULL DEFAULT '',
    -- The pull request, as reported by a developer's own Werkbord.
    pr_url         TEXT    NOT NULL DEFAULT '',
    pr_number      INTEGER NOT NULL DEFAULT 0,
    pr_state       TEXT    NOT NULL DEFAULT 'open',
    pr_draft       INTEGER NOT NULL DEFAULT 0,
    pr_mergeable   TEXT    NOT NULL DEFAULT 'unknown',
    pr_base        TEXT    NOT NULL DEFAULT '',
    pr_behind      INTEGER NOT NULL DEFAULT -1,
    pr_ahead       INTEGER NOT NULL DEFAULT 0,
    pr_reported_by TEXT    NOT NULL DEFAULT '',
    pr_reported_at INTEGER NOT NULL DEFAULT 0,
    pr_created_at  INTEGER NOT NULL DEFAULT 0,
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    claimed_at   INTEGER,
    submitted_at INTEGER,
    completed_at INTEGER,
    UNIQUE (workspace_id, number),
    UNIQUE (id, workspace_id),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE,
    -- A ticket nobody is working on has nobody on it, and one being worked on has someone.
    CHECK ((status IN ('backlog', 'available')) = (assignee_id IS NULL)),
    CHECK (pr_state IN ('open', 'merged', 'closed')),
    CHECK (pr_mergeable IN ('unknown', 'mergeable', 'conflicting'))
) STRICT;

CREATE INDEX tickets_by_project ON tickets (project_id, status);
CREATE INDEX tickets_by_assignee ON tickets (workspace_id, assignee_id) WHERE assignee_id IS NOT NULL;

CREATE TABLE ticket_commits (
    ticket_id    TEXT    NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    sha          TEXT    NOT NULL,
    subject      TEXT    NOT NULL,
    author       TEXT    NOT NULL DEFAULT '',
    committed_at INTEGER NOT NULL,
    PRIMARY KEY (ticket_id, sha)
) STRICT;

-- Branches developers report having. Team never lists a repository: each member's
-- Werkbord reports what it sees, and this is the union.
CREATE TABLE project_branches (
    project_id     TEXT    NOT NULL,
    workspace_id   TEXT    NOT NULL,
    name           TEXT    NOT NULL,
    head_sha       TEXT    NOT NULL DEFAULT '',
    base_branch    TEXT    NOT NULL DEFAULT '',
    ahead          INTEGER NOT NULL DEFAULT 0,
    behind         INTEGER NOT NULL DEFAULT -1,
    last_commit_at INTEGER NOT NULL DEFAULT 0,
    files          TEXT    NOT NULL DEFAULT '[]', -- JSON array of changed repository-relative paths
    reported_by    TEXT    NOT NULL,
    reported_at    INTEGER NOT NULL,
    PRIMARY KEY (project_id, name),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE TABLE activity (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT    NOT NULL,
    project_id   TEXT    NOT NULL,
    ticket_id    TEXT    NOT NULL DEFAULT '',
    ticket_key   TEXT    NOT NULL DEFAULT '',
    actor_id     TEXT    NOT NULL,
    kind         TEXT    NOT NULL,
    detail       TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX activity_by_project ON activity (project_id, id DESC);

CREATE TABLE project_invites (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL,
    project_id   TEXT    NOT NULL,
    -- SHA-256 of the invite code; the code itself is shown once and never kept.
    code_hash    TEXT    NOT NULL UNIQUE,
    role         TEXT    NOT NULL,
    created_by   TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    max_uses     INTEGER NOT NULL CHECK (max_uses >= 1),
    uses         INTEGER NOT NULL DEFAULT 0 CHECK (uses >= 0),
    revoked_at   INTEGER,
    CHECK (uses <= max_uses),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX project_invites_by_project ON project_invites (project_id);
