-- Private local memory, in the controller's existing database. No existing data changes.
CREATE TABLE memories (
 -- An explicit integer primary key keeps FTS row IDs stable through VACUUM INTO backups.
 rowid INTEGER PRIMARY KEY,
 id TEXT NOT NULL UNIQUE CHECK(length(id) BETWEEN 1 AND 128),
 owner_id TEXT NOT NULL CHECK(length(owner_id) BETWEEN 1 AND 128),
 project_id TEXT NOT NULL DEFAULT '',
 repository TEXT NOT NULL DEFAULT '' CHECK(length(repository)<=2000),
 type TEXT NOT NULL CHECK(length(type) BETWEEN 1 AND 128),
 authority TEXT NOT NULL CHECK(authority IN ('factual_observation','inferred_preference','explicit_decision','approved_policy')),
 source_type TEXT NOT NULL CHECK(source_type IN ('user','git','run','handoff','import')),
 provenance TEXT NOT NULL CHECK(json_valid(provenance) AND json_type(provenance)='object' AND length(CAST(provenance AS BLOB))<=32768),
 summary TEXT NOT NULL CHECK(length(CAST(summary AS BLOB)) BETWEEN 1 AND 4096),
 details TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(details) AND json_type(details)='object' AND length(CAST(details AS BLOB))<=16384),
 state TEXT NOT NULL CHECK(state IN ('current','needs_verification','historical','superseded')),
 sensitivity TEXT NOT NULL DEFAULT 'private' CHECK(sensitivity IN ('private','sensitive','secret')),
 sharing_eligible INTEGER NOT NULL DEFAULT 0 CHECK(sharing_eligible IN (0,1) AND (sharing_eligible=0 OR sensitivity='private')),
 superseded_by TEXT,
 duplicate_of TEXT,
 fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL CHECK(updated_at>=created_at),
 verified_at INTEGER CHECK(verified_at IS NULL OR verified_at<=updated_at),
 record_bytes INTEGER NOT NULL CHECK(record_bytes BETWEEN 1 AND 32768),
 UNIQUE(owner_id,project_id,repository,id),
 FOREIGN KEY(owner_id,project_id,repository,superseded_by) REFERENCES memories(owner_id,project_id,repository,id) ON DELETE RESTRICT,
 FOREIGN KEY(owner_id,project_id,repository,duplicate_of) REFERENCES memories(owner_id,project_id,repository,id) ON DELETE RESTRICT,
 CHECK((state='superseded')=(superseded_by IS NOT NULL)),
 CHECK(superseded_by IS NULL OR (superseded_by<>id AND duplicate_of IS NULL)),
 CHECK(duplicate_of IS NULL OR duplicate_of<>id),
 CHECK(coalesce(json_extract(provenance,'$.type'),'')=source_type),
 CHECK(authority NOT IN ('explicit_decision','approved_policy') OR (source_type='user' AND length(coalesce(json_extract(provenance,'$.approvedBy'),''))>0 AND length(coalesce(json_extract(provenance,'$.approvalRef'),''))>0))
) STRICT;
CREATE UNIQUE INDEX memories_dedup ON memories(owner_id,project_id,repository,fingerprint) WHERE duplicate_of IS NULL;
CREATE INDEX memories_scope ON memories(owner_id,project_id,repository,created_at DESC,id DESC);
CREATE INDEX memories_filter ON memories(owner_id,project_id,repository,state,type,authority,created_at DESC,id DESC);
CREATE INDEX memories_retention ON memories(owner_id,state,updated_at,id);
CREATE INDEX memories_supersession ON memories(owner_id,project_id,repository,superseded_by);
CREATE INDEX memories_duplicates ON memories(owner_id,project_id,repository,duplicate_of);

-- Fixed hard ceilings protect against accidental unbounded ingestion across owners.
CREATE TABLE memory_usage (singleton INTEGER PRIMARY KEY CHECK(singleton=1), records INTEGER NOT NULL CHECK(records BETWEEN 0 AND 100000), bytes INTEGER NOT NULL CHECK(bytes BETWEEN 0 AND 268435456)) STRICT;
INSERT INTO memory_usage VALUES(1,0,0);
CREATE TRIGGER memories_capacity_insert BEFORE INSERT ON memories BEGIN
 SELECT CASE WHEN (SELECT records FROM memory_usage)>=100000 OR (SELECT bytes FROM memory_usage)+new.record_bytes>268435456 THEN RAISE(ABORT,'memory capacity exceeded') END;
END;
CREATE TRIGGER memories_capacity_update BEFORE UPDATE ON memories BEGIN
 SELECT CASE WHEN (SELECT bytes FROM memory_usage)-old.record_bytes+new.record_bytes>268435456 THEN RAISE(ABORT,'memory capacity exceeded') END;
END;
CREATE TRIGGER memories_usage_insert AFTER INSERT ON memories BEGIN
 UPDATE memory_usage SET records=records+1, bytes=bytes+new.record_bytes;
END;
CREATE TRIGGER memories_usage_update AFTER UPDATE ON memories BEGIN
 UPDATE memory_usage SET bytes=bytes-old.record_bytes+new.record_bytes;
END;
CREATE TRIGGER memories_usage_delete AFTER DELETE ON memories BEGIN
 UPDATE memory_usage SET records=records-1, bytes=bytes-old.record_bytes;
END;

-- modernc.org/sqlite includes FTS5. Only summaries are indexed, never raw details.
CREATE VIRTUAL TABLE memories_fts USING fts5(summary, content='memories', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2');
CREATE TRIGGER memories_fts_insert AFTER INSERT ON memories BEGIN
 INSERT INTO memories_fts(rowid,summary) VALUES(new.rowid,new.summary);
END;
CREATE TRIGGER memories_fts_delete AFTER DELETE ON memories BEGIN
 INSERT INTO memories_fts(memories_fts,rowid,summary) VALUES('delete',old.rowid,old.summary);
END;
CREATE TRIGGER memories_fts_update AFTER UPDATE OF summary ON memories BEGIN
 INSERT INTO memories_fts(memories_fts,rowid,summary) VALUES('delete',old.rowid,old.summary);
 INSERT INTO memories_fts(rowid,summary) VALUES(new.rowid,new.summary);
END;
