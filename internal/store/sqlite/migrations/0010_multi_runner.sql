DROP INDEX runners_one_local;
ALTER TABLE runners RENAME TO runners_v0;
CREATE TABLE runners (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('local','remote')),
 hostname TEXT NOT NULL DEFAULT '', os TEXT NOT NULL DEFAULT '', arch TEXT NOT NULL DEFAULT '',
 version TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL,
 metadata TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(metadata))
) STRICT;
INSERT INTO runners(id,name,kind,hostname,os,arch,version,created_at,last_seen_at) SELECT * FROM runners_v0;
DROP TABLE runners_v0;
CREATE UNIQUE INDEX runners_one_local ON runners(kind) WHERE kind='local';
ALTER TABLE runs ADD COLUMN distribution TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(distribution));
