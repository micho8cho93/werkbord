ALTER TABLE tickets ADD COLUMN archived_at INTEGER;
CREATE INDEX tickets_archive ON tickets(workspace_id, project_id, archived_at);
