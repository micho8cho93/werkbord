-- Deterministic reconciliation metadata, never private memory or execution data.
CREATE TABLE memory_relationships (
 workspace_id TEXT NOT NULL,
 memory_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 revision INTEGER NOT NULL,
 repository TEXT NOT NULL,
 PRIMARY KEY(workspace_id,memory_id,project_id),
 FOREIGN KEY(workspace_id,memory_id) REFERENCES shared_memories(workspace_id,id) ON DELETE CASCADE
) STRICT;
CREATE INDEX memory_relationships_project ON memory_relationships(workspace_id,project_id,memory_id);
CREATE TABLE memory_evidence (
 workspace_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 member_id TEXT NOT NULL,
 device_id TEXT NOT NULL,
 branch TEXT NOT NULL,
 pr INTEGER NOT NULL,
 sequence INTEGER NOT NULL,
 document TEXT NOT NULL CHECK(length(document)<=4096),
 PRIMARY KEY(workspace_id,project_id,member_id,device_id,branch,pr),
 FOREIGN KEY(project_id,workspace_id) REFERENCES projects(id,workspace_id) ON DELETE CASCADE
) STRICT;
CREATE TABLE memory_reconciliation (
 workspace_id TEXT NOT NULL,
 memory_id TEXT NOT NULL,
 checked_at INTEGER NOT NULL,
 document TEXT NOT NULL,
 PRIMARY KEY(workspace_id,memory_id),
 FOREIGN KEY(workspace_id,memory_id) REFERENCES shared_memories(workspace_id,id) ON DELETE CASCADE
) STRICT;
CREATE TABLE memory_freshness_audit (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 workspace_id TEXT NOT NULL,
 memory_id TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 classification TEXT NOT NULL,
 document TEXT NOT NULL,
 FOREIGN KEY(workspace_id,memory_id) REFERENCES shared_memories(workspace_id,id) ON DELETE CASCADE
) STRICT;
CREATE INDEX memory_freshness_audit_record ON memory_freshness_audit(workspace_id,memory_id,id);
-- Existing records get conservative baselines, but still need source evidence.
INSERT INTO memory_relationships SELECT m.workspace_id,m.id,p.id,0,p.repository FROM shared_memories m JOIN projects p ON p.workspace_id=m.workspace_id
 WHERE json_extract(m.document,'$.scope.projectId')=p.id
 OR EXISTS(SELECT 1 FROM json_each(m.document,'$.scope.targets') x WHERE json_extract(x.value,'$.projectId')=p.id);

CREATE INDEX memory_reconciliation_due ON memory_reconciliation(checked_at,workspace_id,memory_id);
CREATE INDEX shared_memories_claim ON shared_memories(workspace_id,scope_key,json_extract(document,'$.content.context.claim'),COALESCE(json_extract(document,'$.content.context.branch'),''));
CREATE TABLE memory_evidence_audit (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 workspace_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 member_id TEXT NOT NULL,
 device_id TEXT NOT NULL,
 branch TEXT NOT NULL,
 pr INTEGER NOT NULL,
 sequence INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 document TEXT NOT NULL,
 FOREIGN KEY(project_id,workspace_id) REFERENCES projects(id,workspace_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX memory_evidence_audit_stream ON memory_evidence_audit(workspace_id,project_id,member_id,device_id,branch,pr,sequence);
-- Aliases preserve idempotency when an identical proposal gets a new client key.
CREATE TABLE memory_proposal_aliases (
 workspace_id TEXT NOT NULL,
 author_id TEXT NOT NULL,
 client_id TEXT NOT NULL,
 memory_id TEXT NOT NULL,
 digest TEXT NOT NULL,
 PRIMARY KEY(workspace_id,author_id,client_id),
 FOREIGN KEY(workspace_id,memory_id) REFERENCES shared_memories(workspace_id,id) ON DELETE CASCADE
) STRICT;
CREATE TABLE memory_project_changes (
 workspace_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 changed_at INTEGER NOT NULL,
 PRIMARY KEY(workspace_id,project_id),
 FOREIGN KEY(project_id,workspace_id) REFERENCES projects(id,workspace_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX memory_delivery_receipts_record ON memory_delivery_receipts(workspace_id,json_extract(document,'$.record.id'));
CREATE INDEX memory_evidence_audit_age ON memory_evidence_audit(created_at,id);
CREATE INDEX memory_evidence_audit_context ON memory_evidence_audit(workspace_id,project_id,branch,pr,id);
