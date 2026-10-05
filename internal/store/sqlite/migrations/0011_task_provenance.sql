ALTER TABLE tasks ADD COLUMN source_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN work_branch TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN base_branch TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX tasks_source_ref ON tasks(project_id,source_ref) WHERE source_ref != '';
CREATE UNIQUE INDEX tasks_work_branch ON tasks(project_id,work_branch) WHERE work_branch != '';
CREATE INDEX events_retention ON events(created_at,seq);
