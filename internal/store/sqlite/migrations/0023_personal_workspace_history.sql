-- Keep every valid definition, including definitions saved before this migration.
CREATE TABLE personal_workspace_history (
    workspace_id TEXT NOT NULL REFERENCES personal_workspaces(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL,
    document TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY(workspace_id, revision)
);
INSERT INTO personal_workspace_history SELECT id, revision, document, created_at, updated_at FROM personal_workspaces;
CREATE TRIGGER personal_workspace_history_create AFTER INSERT ON personal_workspaces BEGIN
    INSERT INTO personal_workspace_history VALUES (NEW.id, NEW.revision, NEW.document, NEW.created_at, NEW.updated_at);
END;
CREATE TRIGGER personal_workspace_history_update AFTER UPDATE ON personal_workspaces BEGIN
    INSERT INTO personal_workspace_history VALUES (NEW.id, NEW.revision, NEW.document, NEW.created_at, NEW.updated_at);
END;
-- Idempotency survives conversation and provider changes; no records live here.
CREATE TABLE personal_workspace_requests (
    request_key TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES personal_workspaces(id) ON DELETE CASCADE
);
