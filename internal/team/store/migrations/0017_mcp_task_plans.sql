-- MCP task plans are proposals: the external client can create and inspect
-- them, while a member edits or approves them in one guarded transaction.
ALTER TABLE tickets ADD COLUMN source_ref TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX tickets_source_ref_by_project
    ON tickets (workspace_id, project_id, source_ref)
    WHERE source_ref <> '';

CREATE TABLE mcp_task_plan_proposals (
    id               TEXT    PRIMARY KEY,
    workspace_id     TEXT    NOT NULL,
    member_id        TEXT    NOT NULL,
    grant_id         TEXT    NOT NULL,
    project_id       TEXT    NOT NULL,
    idempotency_key  TEXT    NOT NULL,
    request_digest   TEXT    NOT NULL,
    plan_digest      TEXT    NOT NULL,
    plan_json        TEXT    NOT NULL CHECK (json_valid(plan_json) AND json_type(plan_json) = 'object'),
    state            TEXT    NOT NULL CHECK (state IN ('pending', 'approved', 'declined')),
    result_json      TEXT    NOT NULL DEFAULT '{}',
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    resolved_at      INTEGER,
    UNIQUE (workspace_id, grant_id, project_id, idempotency_key),
    FOREIGN KEY (member_id, workspace_id) REFERENCES members (id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (grant_id) REFERENCES mcp_grants (id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX mcp_task_plans_by_project ON mcp_task_plan_proposals (workspace_id, project_id, state, created_at);
CREATE INDEX mcp_task_plans_by_grant ON mcp_task_plan_proposals (workspace_id, grant_id, created_at);
