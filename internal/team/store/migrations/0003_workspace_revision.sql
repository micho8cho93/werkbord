-- Werkbord Team: a revision for the whole workspace.
--
-- Projects already carry a revision that moves whenever their board changes. A
-- member's "My Work" and "Reviews" span every project they are on, and the people,
-- projects and workspace name change too, so a client that wants to stay in step
-- with everything it can see needs one number to watch. This is it: it moves,
-- inside the same transaction, whenever anything in the workspace changes.
--
-- Triggers make that impossible to forget: a new kind of write cannot change what
-- members see and leave the revision where it was.

ALTER TABLE workspaces ADD COLUMN revision INTEGER NOT NULL DEFAULT 0;

CREATE TRIGGER workspace_rev_project_insert AFTER INSERT ON projects
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_project_update AFTER UPDATE ON projects
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_member_insert AFTER INSERT ON members
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_member_delete AFTER DELETE ON members
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = OLD.workspace_id; END;

CREATE TRIGGER workspace_rev_project_member_insert AFTER INSERT ON project_members
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_project_member_update AFTER UPDATE ON project_members
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_project_member_delete AFTER DELETE ON project_members
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = OLD.workspace_id; END;

CREATE TRIGGER workspace_rev_workspace_rename AFTER UPDATE OF name ON workspaces
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.id; END;

-- Ticket writes bump their project's revision (service.mutate), which fires the
-- project trigger above. Activity is read in order by id, so index it by workspace.
CREATE INDEX activity_by_workspace ON activity (workspace_id, id);
