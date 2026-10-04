ALTER TABLE tasks ADD COLUMN orchestration TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(orchestration));
ALTER TABLE runs ADD COLUMN parent_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN purpose TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN schedule_key TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN handoff TEXT NOT NULL DEFAULT 'null' CHECK(json_valid(handoff));
CREATE UNIQUE INDEX runs_schedule_attempt ON runs(task_id, schedule_key) WHERE schedule_key <> '';
CREATE TRIGGER run_parent_scope BEFORE INSERT ON runs WHEN NEW.parent_run_id <> '' BEGIN
 SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM runs WHERE id = NEW.parent_run_id AND task_id = NEW.task_id AND project_id = NEW.project_id AND state IN ('completed','failed','stopped')) THEN RAISE(ABORT, 'continuation parent must be a terminal run of the same task') END;
END;

ALTER TABLE runs ADD COLUMN attempt INTEGER NOT NULL DEFAULT 0;
UPDATE runs SET attempt = (SELECT COUNT(*) FROM runs prior WHERE prior.task_id = runs.task_id AND (prior.created_at < runs.created_at OR (prior.created_at = runs.created_at AND prior.rowid <= runs.rowid)));
CREATE UNIQUE INDEX runs_task_attempt ON runs(task_id, attempt) WHERE attempt > 0;
