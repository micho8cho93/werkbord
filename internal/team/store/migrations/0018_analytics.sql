-- Coordination analytics uses existing records, with no telemetry writes.
CREATE INDEX analytics_tickets ON tickets(workspace_id, project_id, created_at);
CREATE INDEX analytics_completions ON activity(workspace_id, project_id, created_at)
WHERE kind='ticket.completed';
