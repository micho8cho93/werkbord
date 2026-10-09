CREATE TABLE ticket_schedules (
 workspace_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 ticket_id TEXT NOT NULL,
 version INTEGER NOT NULL,
 document TEXT NOT NULL,
 PRIMARY KEY(workspace_id, project_id, ticket_id),
 FOREIGN KEY(ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
);
