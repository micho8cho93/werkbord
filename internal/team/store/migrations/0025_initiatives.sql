CREATE TABLE initiatives (
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 id TEXT NOT NULL,
 version INTEGER NOT NULL,
 document TEXT NOT NULL CHECK(length(document)<=262144),
 PRIMARY KEY(workspace_id,id)
) STRICT;
CREATE TRIGGER workspace_rev_initiative_insert AFTER INSERT ON initiatives
BEGIN UPDATE workspaces SET revision=revision+1 WHERE id=NEW.workspace_id; END;
CREATE TRIGGER workspace_rev_initiative_update AFTER UPDATE ON initiatives
BEGIN UPDATE workspaces SET revision=revision+1 WHERE id=NEW.workspace_id; END;
