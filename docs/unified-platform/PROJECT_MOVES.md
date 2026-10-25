# Project moves (stage 5a)

Implemented in 5.4.0-preview.1. In the app's sidebar, the arrow beside a project opens **Move project**. Choose a connected
Team for a Personal project, or Personal for a Team project. Moving out of Team requires the workspace's project-management
permission, the same permission used to archive a project. Team's existing Premium/read-only gate still applies.

The review displays every field that crosses the boundary: project name, credential-free remote repository reference, and
unarchived task/ticket titles and descriptions. Team ticket requirements are appended to the description under
“Requirements”. Project descriptions, archived tickets, comments, labels, planned dates, dependencies, assignments, schedules,
runs, history, agent output, memory, files, execution settings and local paths stay at the source. No checkout is copied or
attached. Text itself may be private; the person reviews it and confirms in the native app before it is sent.

Destination tasks start in Backlog (Personal) or Available (Team), as human work, without assignments or automation.
Team → Personal creates a work project, with its remote reference retained as inert metadata for a later move; it never
clones or registers a repository. Personal projects with active runs or automatic schedules must stop/disable them first.
There is a limit of 500 unarchived tickets, 700 KB of text and 900 KB of encoded JSON per move. Names must fit both boards (80 characters and 120 bytes); titles must be on one line. Longer descriptions are refused, never truncated.

The shell retains the reviewed snapshot for 15 minutes; the page supplies only its opaque token at confirmation. Workspace
and assistant frames have no relay for project moves. Both engines offer the same neutral text contract, with each engine's
own credentials, permissions and transactions. Neither engine calls the other.

Each destination import is one transaction. Its project ID is derived from provenance and the reviewed text, so a lost
response can be retried against the same import, even after restarting the app. An existing destination is reused only when
its unarchived text still matches and it remains active. Nothing overwrites later edits. Names and ticket text are never
silently truncated or normalised.

Only after the destination confirms its commit does the shell ask the source to archive. The source compares its revision
and reviewed text inside that archive transaction; intervening edits refuse the archive. Original records remain recoverable,
with all their history. If import has an unknown outcome, the source stays active and the same review can be retried. If the
archive fails or has an unknown outcome, the app names the saved destination and asks the person to check both projects.
There is no distributed transaction between computers, and an uncertain archive is never presented as a completed move.

Stage 5's Salie read-only Team policy and remaining Home polish are separate follow-up work; this slice implements project
moves only.
