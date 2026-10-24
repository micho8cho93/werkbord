-- A record may hold a long document (a note, a page, a snippet), so its bound rises from 32 KiB to 64 KiB. SQLite cannot
-- change a CHECK in place: the table is rebuilt with every row, index and trigger it had. Definitions and collection
-- schemas keep their 32 KiB bound.
CREATE TABLE personal_records_wide (
 id TEXT PRIMARY KEY CHECK (length(id) = 21 AND substr(id, 1, 5) = 'prec_' AND substr(id, 6) NOT GLOB '*[^a-z2-7]*'),
 collection_id TEXT NOT NULL REFERENCES personal_collections(id) ON DELETE RESTRICT,
 revision INTEGER NOT NULL CHECK (revision > 0),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL CHECK (updated_at >= created_at),
 document TEXT NOT NULL CHECK (json_valid(document) AND json_type(document) = 'object' AND length(CAST(document AS BLOB)) <= 65536)
) STRICT;
INSERT INTO personal_records_wide(id, collection_id, revision, created_at, updated_at, document)
 SELECT id, collection_id, revision, created_at, updated_at, document FROM personal_records;
DROP TABLE personal_records;
ALTER TABLE personal_records_wide RENAME TO personal_records;
CREATE INDEX personal_records_collection ON personal_records(collection_id, id);
CREATE TRIGGER personal_record_created AFTER INSERT ON personal_records BEGIN
 UPDATE personal_collection_state SET generation = generation + 1 WHERE collection_id = NEW.collection_id;
END;
CREATE TRIGGER personal_record_updated AFTER UPDATE ON personal_records BEGIN
 UPDATE personal_collection_state SET generation = generation + 1 WHERE collection_id = NEW.collection_id;
END;
CREATE TRIGGER personal_record_deleted AFTER DELETE ON personal_records BEGIN
 UPDATE personal_collection_state SET generation = generation + 1 WHERE collection_id = OLD.collection_id;
END;
