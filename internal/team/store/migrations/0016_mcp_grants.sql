-- MCP credentials are distinct from member and device credentials. The token is
-- shown once; only its SHA-256 digest is stored. The selected projects and
-- operation names narrow the member's existing service permissions.
CREATE TABLE mcp_grants (
    id            TEXT    PRIMARY KEY,
    workspace_id  TEXT    NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    member_id     TEXT    NOT NULL,
    name          TEXT    NOT NULL,
    projects_json TEXT    NOT NULL CHECK (json_valid(projects_json) AND json_type(projects_json) = 'array'),
    operations_json TEXT  NOT NULL CHECK (json_valid(operations_json) AND json_type(operations_json) = 'array'),
    token_hash    TEXT    NOT NULL UNIQUE,
    created_at    INTEGER NOT NULL,
    revoked_at    INTEGER,
    FOREIGN KEY (member_id, workspace_id) REFERENCES members(id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX mcp_grants_by_member ON mcp_grants (workspace_id, member_id, created_at);
