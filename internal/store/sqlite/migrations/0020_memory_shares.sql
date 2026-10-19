-- Immutable approved projections only. Private memories are never queried by a grant.
CREATE TABLE memory_shares (
 owner_id TEXT NOT NULL,
 id TEXT NOT NULL,
 workspace_id TEXT NOT NULL,
 expires_at INTEGER NOT NULL,
 document TEXT NOT NULL CHECK(length(document)<=16384),
 PRIMARY KEY(owner_id,id)
) STRICT;
CREATE INDEX memory_shares_destination ON memory_shares(owner_id,workspace_id,id);
