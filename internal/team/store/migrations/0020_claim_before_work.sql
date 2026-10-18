-- Claiming reserves Available work; only starting work moves it to In Progress.
-- Rebuild the ownership CHECK with foreign keys enabled (including on rqlite).
-- Back up every dependent row before the parent drop cascades, then restore them
-- in this same atomic migration. Existing In Progress tickets stay in progress.
CREATE TABLE tickets_next (
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
    archived_at INTEGER,
    assignment INTEGER NOT NULL DEFAULT 0,
    work_mode TEXT NOT NULL DEFAULT 'agent' CHECK (work_mode IN ('human', 'agent', 'hybrid')),
    plan_start TEXT NOT NULL DEFAULT '',
    plan_end TEXT NOT NULL DEFAULT '',
    milestone INTEGER NOT NULL DEFAULT 0 CHECK (milestone IN (0, 1)),
    source_ref TEXT NOT NULL DEFAULT '',
    UNIQUE (workspace_id, number),
    UNIQUE (id, workspace_id),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE,
    -- Backlog has no owner; Available may be claimed; active and completed work has an owner.
    CHECK ((status != 'backlog' OR assignee_id IS NULL) AND (status NOT IN ('in_progress', 'review', 'done') OR assignee_id IS NOT NULL)),
    CHECK (pr_state IN ('open', 'merged', 'closed')),
    CHECK (pr_mergeable IN ('unknown', 'mergeable', 'conflicting'))
) STRICT;


INSERT INTO tickets_next SELECT * FROM tickets;
CREATE TABLE claim_backup_ticket_commits AS SELECT * FROM ticket_commits;
CREATE TABLE claim_backup_ticket_progress AS SELECT * FROM ticket_progress;
CREATE TABLE claim_backup_ticket_schedules AS SELECT * FROM ticket_schedules;
CREATE TABLE claim_backup_ticket_labels AS SELECT * FROM ticket_labels;
CREATE TABLE claim_backup_ticket_dependencies AS SELECT * FROM ticket_dependencies;
DROP TABLE tickets;
ALTER TABLE tickets_next RENAME TO tickets;
INSERT INTO ticket_commits SELECT * FROM claim_backup_ticket_commits;
DROP TABLE claim_backup_ticket_commits;
INSERT INTO ticket_progress SELECT * FROM claim_backup_ticket_progress;
DROP TABLE claim_backup_ticket_progress;
INSERT INTO ticket_schedules SELECT * FROM claim_backup_ticket_schedules;
DROP TABLE claim_backup_ticket_schedules;
INSERT INTO ticket_labels SELECT * FROM claim_backup_ticket_labels;
DROP TABLE claim_backup_ticket_labels;
INSERT INTO ticket_dependencies SELECT * FROM claim_backup_ticket_dependencies;
DROP TABLE claim_backup_ticket_dependencies;
CREATE INDEX tickets_by_project ON tickets (project_id, status);
CREATE INDEX tickets_by_assignee ON tickets (workspace_id, assignee_id) WHERE assignee_id IS NOT NULL;
CREATE INDEX tickets_archive ON tickets(workspace_id, project_id, archived_at);
CREATE UNIQUE INDEX tickets_source_ref_by_project ON tickets(workspace_id, project_id, source_ref) WHERE source_ref <> '';
CREATE INDEX analytics_tickets ON tickets(workspace_id, project_id, created_at);
-- Starting work preserves the claim interval and its already approved execution.
CREATE TRIGGER ticket_assignment_changed AFTER UPDATE OF assignee_id,status ON tickets
WHEN COALESCE(OLD.assignee_id,'') != COALESCE(NEW.assignee_id,'')
 OR (OLD.status IN ('available','in_progress','review')) != (NEW.status IN ('available','in_progress','review'))
BEGIN UPDATE tickets SET assignment=assignment+1 WHERE id=NEW.id; END;
