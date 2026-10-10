-- A ticket has one list of what must finish first: ticket_dependencies. Until now a shared request for agent
-- work (ticket_schedules) kept a second, independent list inside its document, and only that one made the request
-- wait. From here the request waits for the ticket's own list, so every dependency a request already had is
-- copied onto its ticket.
--
-- Only what the request already had is added, and only where it is certain to be a valid row: both tickets exist,
-- are in the same workspace and project, are different, and are not already linked. A row that is not (an id that
-- is gone, another project, a document that is not a list of ids) is skipped, so this never fails an upgrade.
-- Running it again adds nothing. It cannot see circles. A circle made by this copy is reported by the timeline
-- and its requests wait (nothing loops), and the way out is to remove one dependency on a ticket.
--
-- The request's own list stays in its document as a record of what it was proposed with. Nothing reads it to
-- decide what to wait for any more, and the request's fence still includes it.
INSERT OR IGNORE INTO ticket_dependencies (ticket_id, depends_on_id, workspace_id)
SELECT s.ticket_id, j.value, s.workspace_id
  FROM ticket_schedules s
  JOIN json_each(
         CASE WHEN json_valid(s.document)
              THEN CASE WHEN json_type(s.document, '$.dependencies') = 'array' THEN json_extract(s.document, '$.dependencies') ELSE '[]' END
              ELSE '[]' END) j ON j.type = 'text'
  JOIN tickets a ON a.id = s.ticket_id AND a.workspace_id = s.workspace_id AND a.project_id = s.project_id
  JOIN tickets b ON b.id = j.value AND b.workspace_id = a.workspace_id AND b.project_id = a.project_id
 WHERE a.id != b.id
   AND NOT EXISTS (SELECT 1 FROM ticket_dependencies d WHERE d.ticket_id = a.id AND d.depends_on_id = b.id)
 ORDER BY s.ticket_id, j.key;
