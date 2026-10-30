-- Plans reference tasks; they neither copy task state nor dispatch execution.
CREATE TABLE work_plans (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 version INTEGER NOT NULL,
 document TEXT NOT NULL
) STRICT;
CREATE INDEX work_plans_project ON work_plans(project_id);
