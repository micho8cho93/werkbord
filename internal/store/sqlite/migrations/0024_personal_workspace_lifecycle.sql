-- Local provenance and monotonic change markers for reviewed deletion plans.
CREATE TABLE personal_collection_state (
 collection_id TEXT PRIMARY KEY REFERENCES personal_collections(id) ON DELETE CASCADE,
 owner_workspace_id TEXT REFERENCES personal_workspaces(id) ON DELETE SET NULL,
 generation INTEGER NOT NULL DEFAULT 0
) STRICT;
INSERT INTO personal_collection_state(collection_id, owner_workspace_id)
 SELECT c.id, CASE WHEN COUNT(w.workspace_id) = 1 THEN MIN(w.workspace_id) END
 FROM personal_collections c LEFT JOIN personal_workspace_collections w ON w.collection_id = c.id GROUP BY c.id;
CREATE TRIGGER personal_collection_state_create AFTER INSERT ON personal_collections BEGIN
 INSERT INTO personal_collection_state(collection_id) VALUES(NEW.id);
END;
CREATE TRIGGER personal_collection_claim AFTER INSERT ON personal_workspace_collections BEGIN
 UPDATE personal_collection_state SET owner_workspace_id = COALESCE(owner_workspace_id, NEW.workspace_id), generation = generation + 1 WHERE collection_id = NEW.collection_id;
END;
CREATE TRIGGER personal_collection_unlink AFTER DELETE ON personal_workspace_collections BEGIN
 UPDATE personal_collection_state SET generation = generation + 1 WHERE collection_id = OLD.collection_id;
END;
CREATE TRIGGER personal_record_created AFTER INSERT ON personal_records BEGIN
 UPDATE personal_collection_state SET generation = generation + 1 WHERE collection_id = NEW.collection_id;
END;
CREATE TRIGGER personal_record_updated AFTER UPDATE ON personal_records BEGIN
 UPDATE personal_collection_state SET generation = generation + 1 WHERE collection_id = NEW.collection_id;
END;
CREATE TRIGGER personal_record_deleted AFTER DELETE ON personal_records BEGIN
 UPDATE personal_collection_state SET generation = generation + 1 WHERE collection_id = OLD.collection_id;
END;
