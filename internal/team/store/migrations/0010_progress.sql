ALTER TABLE tickets ADD COLUMN assignment INTEGER NOT NULL DEFAULT 0;
UPDATE tickets SET assignment=1 WHERE status IN ('in_progress','review');
CREATE TRIGGER ticket_assignment_changed AFTER UPDATE OF assignee_id,status ON tickets
WHEN COALESCE(OLD.assignee_id,'') != COALESCE(NEW.assignee_id,'')
 OR (OLD.status IN ('in_progress','review')) != (NEW.status IN ('in_progress','review'))
BEGIN UPDATE tickets SET assignment=assignment+1 WHERE id=NEW.id; END;

CREATE TABLE ticket_progress (
 workspace_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 ticket_id TEXT NOT NULL,
 device_id TEXT NOT NULL,
 member_id TEXT NOT NULL,
 sequence INTEGER NOT NULL CHECK(sequence > 0),
 claim_at INTEGER NOT NULL,
 digest TEXT NOT NULL,
 document TEXT NOT NULL,
 reported_at INTEGER NOT NULL,
 PRIMARY KEY(workspace_id,project_id,ticket_id,device_id),
 FOREIGN KEY(ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
 FOREIGN KEY(device_id) REFERENCES devices(id) ON DELETE CASCADE,
 FOREIGN KEY(member_id) REFERENCES members(id) ON DELETE CASCADE
);
