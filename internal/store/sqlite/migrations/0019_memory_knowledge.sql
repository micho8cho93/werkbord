ALTER TABLE memories ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0 CHECK(pinned IN (0,1));
-- Immutable, concise evidence; a deleted memory leaves a content-free identity tombstone
-- so replay does not recreate something the person deliberately removed.
CREATE TABLE memory_evidence (
 owner_id TEXT NOT NULL,
 source_key TEXT NOT NULL CHECK(length(source_key)=64),
 memory_id TEXT REFERENCES memories(id) ON DELETE SET NULL,
 provenance TEXT NOT NULL CHECK(json_valid(provenance) AND length(CAST(provenance AS BLOB))<=4096),
 excerpt TEXT NOT NULL CHECK(length(CAST(excerpt AS BLOB))<=1024),
 observed_at INTEGER NOT NULL,
 PRIMARY KEY(owner_id,source_key)
) STRICT;
CREATE INDEX memory_evidence_record ON memory_evidence(owner_id,memory_id,observed_at);
CREATE TRIGGER memory_evidence_capacity BEFORE INSERT ON memory_evidence BEGIN
 SELECT CASE WHEN (SELECT count(*) FROM memory_evidence)>=100000
 OR (SELECT count(*) FROM memory_evidence WHERE owner_id=new.owner_id)>=50000
 THEN RAISE(ABORT,'memory capacity exceeded') END;
END;
CREATE TRIGGER memory_evidence_forget AFTER UPDATE OF memory_id ON memory_evidence WHEN new.memory_id IS NULL BEGIN
 UPDATE memory_evidence SET provenance='{}',excerpt='' WHERE owner_id=new.owner_id AND source_key=new.source_key;
END;
CREATE TABLE memory_extractions (
 owner_id TEXT NOT NULL,
 run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
 version INTEGER NOT NULL,
 PRIMARY KEY(owner_id,run_id)
) STRICT;
CREATE INDEX runs_memory_extraction ON runs(updated_at,id) WHERE state IN ('completed','failed','stopped');
-- Constant-time global evidence accounting; also bounds tombstones.
CREATE TABLE memory_evidence_usage(singleton INTEGER PRIMARY KEY CHECK(singleton=1), bytes INTEGER NOT NULL CHECK(bytes BETWEEN 0 AND 67108864)) STRICT;
INSERT INTO memory_evidence_usage VALUES(1,0);
CREATE TRIGGER memory_evidence_byte_capacity BEFORE INSERT ON memory_evidence BEGIN
 SELECT CASE WHEN (SELECT bytes FROM memory_evidence_usage)+length(CAST(new.provenance AS BLOB))+length(CAST(new.excerpt AS BLOB))+128>67108864 THEN RAISE(ABORT,'memory capacity exceeded') END;
END;
CREATE TRIGGER memory_evidence_usage_insert AFTER INSERT ON memory_evidence BEGIN
 UPDATE memory_evidence_usage SET bytes=bytes+length(CAST(new.provenance AS BLOB))+length(CAST(new.excerpt AS BLOB))+128;
END;
CREATE TRIGGER memory_evidence_usage_update AFTER UPDATE ON memory_evidence BEGIN
 UPDATE memory_evidence_usage SET bytes=bytes-length(CAST(old.provenance AS BLOB))-length(CAST(old.excerpt AS BLOB))+length(CAST(new.provenance AS BLOB))+length(CAST(new.excerpt AS BLOB));
END;
CREATE TRIGGER memory_evidence_usage_delete AFTER DELETE ON memory_evidence BEGIN
 UPDATE memory_evidence_usage SET bytes=bytes-length(CAST(old.provenance AS BLOB))-length(CAST(old.excerpt AS BLOB))-128;
END;
ALTER TABLE memory_evidence ADD COLUMN active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1));
CREATE INDEX memory_evidence_run ON memory_evidence(owner_id,json_extract(provenance,'$.runId'),active);
CREATE TRIGGER memory_evidence_owner BEFORE INSERT ON memory_evidence BEGIN
 SELECT CASE WHEN new.memory_id IS NULL OR NOT EXISTS(SELECT 1 FROM memories m WHERE m.id=new.memory_id AND m.owner_id=new.owner_id) THEN RAISE(ABORT,'memory evidence owner mismatch') END;
END;
