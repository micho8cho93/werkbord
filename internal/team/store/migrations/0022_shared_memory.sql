-- Shared memory is separate from local Individual memory and execution metadata.
CREATE TABLE shared_memories (
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 id TEXT NOT NULL,
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 scope_key TEXT NOT NULL,
 author_id TEXT NOT NULL,
 client_id TEXT NOT NULL,
 proposal_digest TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>0),
 document TEXT NOT NULL CHECK(length(document)<=16384),
 PRIMARY KEY(workspace_id,id),
 UNIQUE(workspace_id,author_id,client_id)
) STRICT;
CREATE INDEX shared_memories_scope ON shared_memories(workspace_id,scope_key,id);
CREATE TABLE shared_memory_revisions (
 workspace_id TEXT NOT NULL,
 memory_id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version>0),
 document TEXT NOT NULL CHECK(length(document)<=16384),
 PRIMARY KEY(workspace_id,memory_id,version),
 FOREIGN KEY(workspace_id,memory_id) REFERENCES shared_memories(workspace_id,id) ON DELETE CASCADE
) STRICT;
CREATE TRIGGER workspace_rev_memory_insert AFTER INSERT ON shared_memories
BEGIN UPDATE workspaces SET revision=revision+1 WHERE id=NEW.workspace_id; END;
CREATE TRIGGER workspace_rev_memory_update AFTER UPDATE ON shared_memories
BEGIN UPDATE workspaces SET revision=revision+1 WHERE id=NEW.workspace_id; END;
