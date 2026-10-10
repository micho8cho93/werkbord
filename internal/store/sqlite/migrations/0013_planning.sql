-- Flexible work organization: reusable labels, who does a task, planned dates, and projects that
-- are not backed by a Git repository.
--
-- Nothing here changes how an existing task or run behaves: every existing task becomes
-- work_mode 'agent' (what it always was), with no labels and no planned dates, and every
-- existing project becomes kind 'repository'.

-- Labels are the person's own words and colours, defined once and reused by every project. Their
-- names are unique ignoring case and spacing (name_key is the folded name the application computes).
CREATE TABLE labels (
    id          TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL,
    name_key    TEXT    NOT NULL UNIQUE,
    color       TEXT    NOT NULL CHECK (length(color) = 7 AND substr(color, 1, 1) = '#'),
    description TEXT    NOT NULL DEFAULT '',
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

-- A task carries any number of labels. The rowid keeps the order they were put on in. Deleting a
-- label or a task removes the link, never the other side.
CREATE TABLE task_labels (
    task_id  TEXT NOT NULL REFERENCES tasks(id)  ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, label_id)
) STRICT;
CREATE INDEX task_labels_by_label ON task_labels (label_id);

-- Who is expected to do the task. A fixed classification, not a label. 'agent' is what every
-- existing task was.
ALTER TABLE tasks ADD COLUMN work_mode TEXT NOT NULL DEFAULT 'agent' CHECK (work_mode IN ('human', 'agent', 'hybrid'));

-- Planned days for the timeline ('YYYY-MM-DD', or '' when unset). They are separate from
-- orchestration, the task's schedule for agents, which they never touch.
ALTER TABLE tasks ADD COLUMN plan_start TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN plan_end   TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN milestone  INTEGER NOT NULL DEFAULT 0 CHECK (milestone IN (0, 1));

-- A work project has no repository. repo_path is NOT NULL UNIQUE and SQLite cannot drop that in
-- place (rebuilding `projects` is what migration 0003 explains it avoids), so a work project keeps a
-- placeholder that can never be a real path and is unique by construction: 'work:' and its own ID.
-- The application reads it back as an empty RepoPath; the triggers keep the two kinds apart whoever
-- writes the row.
ALTER TABLE projects ADD COLUMN kind TEXT NOT NULL DEFAULT 'repository' CHECK (kind IN ('repository', 'work'));

CREATE TRIGGER projects_kind_path BEFORE INSERT ON projects
WHEN (NEW.kind = 'work' AND NEW.repo_path != 'work:' || NEW.id)
  OR (NEW.kind = 'repository' AND substr(NEW.repo_path, 1, 5) = 'work:')
BEGIN
  SELECT RAISE(ABORT, 'a work project has no repository path, and a repository project must have one');
END;

-- What a project is cannot change after the fact: a repository cannot become a work project (its
-- worktrees, runs and findings would be left pointing at nothing) or the other way round.
CREATE TRIGGER projects_kind_fixed BEFORE UPDATE OF kind ON projects
WHEN NEW.kind != OLD.kind
BEGIN
  SELECT RAISE(ABORT, 'a project''s kind cannot be changed');
END;
