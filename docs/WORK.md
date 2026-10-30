# Work

Work is the task queue and structured planning destination. It replaces the Kanban presentation in Personal and Team.
Existing lifecycle values, task IDs, dependencies, assignments, execution sessions and Git history remain authoritative.
Older board links redirect to Work; the Team `/board` data endpoint remains compatible with existing clients.

## Queue

- **Now:** active work, review, agent attention and immediate priorities.
- **Next:** ready tasks without future start dates or unfinished dependencies.
- **Later:** future work, lower priorities and tasks waiting on dependencies.

These are presentation groups, not stored statuses. Blockers name their dependencies, including unavailable references.
Review is unfinished work for dependency purposes. Completed tasks remain available in a collapsed section and Archive
keeps the existing restoration behaviour. Task details retain lifecycle changes, assignment, execution, review and Git.

The desktop Work destination uses its shared project selection. All Projects reads permission-filtered queue projections
from Personal and connected teams; it does not infer access from names. Unavailable workspaces and bounded summaries are
identified explicitly. Opening a row uses the owning workspace's task interface.

## Queue cards and ordering

Each queue (Now, Next, Later) is a card with a count. Projects (in All Projects) and plans (a series of tasks) are disclosures inside it,
so a plan's tasks sit together; a plan nests under its project. Rows keep their compact form and gain a drag handle.

Ordering is a stored position, never a status. Dragging a row, or Alt plus Up or Down on it, changes only where it sits among its neighbours
in the same group, project and plan; the groups themselves stay derived, so a row cannot be dropped into another group. A move reports the
ids now above and below it; the product computes the position between them (`WerkbordWork.order.between`) and, when two neighbours have no
room left, numbers the list again in the order chosen. Escape cancels a drag. The gesture transport is `internal/uipresentation/static/work-order.js`
and has no product behaviour.

- **Personal:** a standalone task's position is its existing `position`, set with the ordinary task patch. A plan's tasks are ordered by
  the plan (the existing versioned save), so the same task is never ordered in two places.
- **Team:** each ticket has one `position` shared by every member (migration 0029 keeps today's order, creation order; a new ticket joins the
  end). `PUT /projects/{id}/tickets/{tid}/position` sets it and, unlike every other ticket write, leaves `version` and `updatedAt` alone, so an
  open edit never goes stale. Who may move what: a ticket nobody holds is the project's shared priority, for people who may edit tickets
  (`tickets.edit`); a ticket someone holds is ordered by its holder or by an editor; finished and archived tickets are not in the queue. Rows
  you may not move show a lock and the reason. Claiming moves a ticket from Next into Now, shows its holder as an initials chip and gives
  the holder a handle. Plans reorder through the existing initiative save and its own permissions.
- **All Projects** is the cross-project read view: the same cards, project and plan disclosures and holder chips, but no drag and no claim;
  open a project to order or claim. Team work held by others is hidden by default (**Mine and unclaimed**, with an **Everyone** toggle that
  appears only when a teammate holds work). The queue summary carries `owner`, `mine` and `plan` for this: display and grouping only, never authority.

## Structured plans

Personal stores small, versioned plan documents whose ordered task IDs reference the existing task service. Team reuses
its existing initiative records and permission checks, including cross-project references and dependency validation.
The Team service remains a coordinator and starts no agent. No plan owns a run, copies task status or dispatches work.

A plan has a title, description, ordered tasks, decisions and open questions. Its progress follows referenced task states;
the task details provide dependencies and existing execution sessions. Users can edit and reorder task references in Work. Queue and Plans share compact navigation; opening a plan shows
a dedicated detail page with tasks, sessions, decisions and questions, plus plan information and actions. Task details
opened from a plan return to that plan when closed.
Creating tasks and launching standalone CLI sessions remain independent actions.

Salie's `list_work_plans` and `save_work_plan` operations use the existing task read/update permissions and confirmation
flow. Saves check current versions, task scope and permissions again. The desktop bridge accepts only plan metadata and
verifies an approved mutation against the controller's exact pending request before forwarding it. Cross-project Team
plans can be edited through Work; Salie's plan save adapter handles single-project plans.

## Validation

`make check` covers the domain services, stores, API, assistant operations, desktop bridge, architectural boundaries,
web types, lint, unit tests and builds. The targeted browser acceptance runs against disposable services:

```sh
make web web-embed web-shell
BROWSER_SCRIPT=scripts/browser-work.cjs node scripts/test-browser.cjs
make test-work-order-browser   # cards, drag and keyboard ordering, plan order, Team claims, locks and shared order
```

It exercises Personal and Team queues and plans, ordering, dependencies, persistence, session association, desktop/phone
layouts, All Projects aggregation and switching to a specific project context.
