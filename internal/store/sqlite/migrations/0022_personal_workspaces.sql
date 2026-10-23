-- Private local definitions and generic collections; no existing product rows
-- or Team database is referenced. Whole-database backups include all four tables.
CREATE TABLE personal_workspaces (
 id TEXT PRIMARY KEY CHECK (length(id) = 20 AND substr(id, 1, 4) = 'pws_' AND substr(id, 5) NOT GLOB '*[^a-z2-7]*'),
 revision INTEGER NOT NULL CHECK (revision > 0),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL CHECK (updated_at >= created_at),
 document TEXT NOT NULL CHECK (json_valid(document) AND json_type(document) = 'object' AND length(CAST(document AS BLOB)) <= 32768)
) STRICT;
CREATE TABLE personal_collections (
 id TEXT PRIMARY KEY CHECK (length(id) = 20 AND substr(id, 1, 4) = 'pcl_' AND substr(id, 5) NOT GLOB '*[^a-z2-7]*'),
 revision INTEGER NOT NULL CHECK (revision > 0),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL CHECK (updated_at >= created_at),
 document TEXT NOT NULL CHECK (json_valid(document) AND json_type(document) = 'object' AND length(CAST(document AS BLOB)) <= 32768)
) STRICT;
CREATE TABLE personal_records (
 id TEXT PRIMARY KEY CHECK (length(id) = 21 AND substr(id, 1, 5) = 'prec_' AND substr(id, 6) NOT GLOB '*[^a-z2-7]*'),
 collection_id TEXT NOT NULL REFERENCES personal_collections(id) ON DELETE RESTRICT,
 revision INTEGER NOT NULL CHECK (revision > 0),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL CHECK (updated_at >= created_at),
 document TEXT NOT NULL CHECK (json_valid(document) AND json_type(document) = 'object' AND length(CAST(document AS BLOB)) <= 32768)
) STRICT;
CREATE INDEX personal_records_collection ON personal_records(collection_id, id);
CREATE TABLE personal_workspace_collections (
 workspace_id TEXT NOT NULL REFERENCES personal_workspaces(id) ON DELETE CASCADE,
 collection_id TEXT NOT NULL REFERENCES personal_collections(id) ON DELETE RESTRICT,
 PRIMARY KEY (workspace_id, collection_id)
) STRICT;
CREATE INDEX personal_collection_workspaces ON personal_workspace_collections(collection_id, workspace_id);
