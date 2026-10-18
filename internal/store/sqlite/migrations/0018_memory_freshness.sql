-- Private checkpoints, never exposed through settings or the event stream.
CREATE TABLE memory_freshness (
 owner_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 repository TEXT NOT NULL,
 checkpoint TEXT NOT NULL CHECK(json_valid(checkpoint) AND length(CAST(checkpoint AS BLOB))<=1048576),
 version INTEGER NOT NULL CHECK(version>=1),
 PRIMARY KEY(owner_id,project_id,repository)
) STRICT;
-- Checkpoints cannot accumulate indefinitely after partitions are deleted/pruned.
CREATE TRIGGER memories_freshness_cleanup AFTER DELETE ON memories BEGIN
 DELETE FROM memory_freshness WHERE owner_id=old.owner_id AND project_id=old.project_id AND repository=old.repository
 AND NOT EXISTS(SELECT 1 FROM memories WHERE owner_id=old.owner_id AND project_id=old.project_id AND repository=old.repository);
END;

CREATE INDEX memories_freshness_project ON memories(project_id,owner_id,repository);
