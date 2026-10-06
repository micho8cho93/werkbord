ALTER TABLE tasks ADD COLUMN archived_at INTEGER;
CREATE INDEX tasks_archive ON tasks(project_id, archived_at);
