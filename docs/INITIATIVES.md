# Team Initiatives (Phase 4B)

An Initiative links existing Team projects and tickets around an objective. Projects keep their boards,
repositories, ticket ownership, claims, reviews and runner assignments. A ticket may appear in several
Initiatives. Branches and PRs come from those tickets' existing Git reports; architectural decisions and
repository relationships are references to published shared memory, with its existing approval rules.

Open **Initiatives** in the Team console to create an objective, choose its owner, link existing projects,
tickets and published context, and add prerequisite/dependent ticket pairs. The overview shows ticket states,
reported PR states, blockers, next actions and activity. Edit the same record to change links, owner or status;
`archived` retires the coordination record without archiving any project or ticket. This is not another board.

## Permissions and evidence

Every operation rechecks the authenticated member inside its database transaction. Scoped MCP credentials
cannot use these endpoints. Creation requires access to all linked projects; only the Initiative owner or a
workspace project manager can edit it. Its owner must belong to every linked project or have project-management
permission. Updates require the current `version`; a stale write fails with a conflict and must be reloaded.

The title, objective and history can describe several repositories. For that reason **the entire Initiative is
omitted unless the reader has ticket, repository and activity access to every linked, active project**. A direct
lookup then returns not found. There are no hidden-project counts, placeholder names or hidden-resource cursors.
This conservative full-scope rule prevents indirect disclosure through free text. Archived projects also hide
an Initiative until its coordinator removes the link. Published memory references undergo separate full-scope
and classification checks on every read; inaccessible records and their saved IDs are omitted. Initiative
ownership grants no extra project or memory permission.

Completion counts come from current Team tickets marked Done (including archived completed tickets). An Initiative's `completed` status
never completes tickets. PR states are member-reported observations, not new queries to a Git host. Reports older
than 24 hours, unknown PR report times, conflicting PRs, overlapping reported branch files and changed upstream
work prompt revalidation. File overlap is a possible conflict, not proof of incompatible changes. Dependencies
are coordination advice between linked tickets, with cycle checks including existing dependencies among linked tickets; they do not change the existing project
execution gates or schedule work. Activity includes the latest 100 project events filtered to linked tickets;
coordination edits record actor, kind and timestamp. Missing reports never imply completed work.

## Sally and execution

The desktop's approved Team bridge includes fresh Initiative context in `retrieve_memory` when `requestType`
is `initiative`, `changes` or `next`. The projection contains only coordination metadata, is bounded by the
existing context budget, and labels suggestions as advisory. Every constituent project is attached as a related
scope so the assistant's own project selection can only narrow the member's access. Sally retrieves project
memory separately for policies, decisions and approved repository relationships. Sources and timestamps should
be cited; stale observations call for revalidation.

Sally can suggest missing tickets through the existing ticket proposal and human confirmation. Coordinated
review uses each project's existing review workflow. Team never executes an Initiative, contacts an agent,
collects Git/agent credentials, pushes or merges. Each owner still claims work and uses an explicitly approved
Individual runner and local execution policy. An agent's completion does not complete the Team ticket.

## API

All routes are under `/api/team/v1` and the caller's authenticated workspace:

- `GET /initiatives` lists full-scope-visible projections.
- `GET /initiatives/{iid}` returns a one-element projection list, or not found.
- `GET /projects/{id}/initiatives` limits that list to Initiatives linked to the visible project.
- `POST /initiatives` creates a record (`version: 0`).
- `PUT /initiatives/{iid}` replaces editable fields using the current `version`.

Writes contain `title`, `objective`, `status` (`planned`, `active`, `paused`, `completed`, `archived`), `ownerId`,
`projectIds`, `tickets: [{projectId,ticketId}]`, `dependencies: [{before:{projectId,ticketId},after:{projectId,ticketId}}]`,
and `memories: [{projectId,memoryId}]`. IDs, timestamps, versions and history are assigned by Team.
At most 30 projects, 200 tickets, 200 dependencies and 100 memory references may be linked to one Initiative.
The workspace supports 1,000 Initiatives; each has a bounded 500-entry coordination history.

Reads return `initiative`, linked `projects`, current `tickets`, permitted `memories`, recent `activity`, `done`,
`openPRs`, `blockers`, `nextActions` and `generatedAt`. Repository URLs, branches and PR references come from the
linked project/ticket records rather than copied execution environments.

## Validation

`make check` covers Team/Individual regressions, architecture boundaries, desktop bridge tests and builds.
`TestInitiativeAcrossRepositoriesAndIndependentMemberRunners` uses real Git repositories, independent Individual
APIs/databases/runners and two signed member connectors. It checks isolated handoffs, no implicit launches,
completion/review separation and access revocation. Service tests cover cycle rejection, optimistic writes,
restricted memory omission and credential revocation. The console regression runs with:

```sh
BROWSER_INITIATIVES_ONLY=1 node scripts/test-browser.cjs
```

It covers three repositories, creation/editing, dependencies, reload, a restricted member and desktop/phone layout.
All fixtures use disposable data and credentials.
