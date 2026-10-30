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

## Structured plans

Personal stores small, versioned plan documents whose ordered task IDs reference the existing task service. Team reuses
its existing initiative records and permission checks, including cross-project references and dependency validation.
The Team service remains a coordinator and starts no agent. No plan owns a run, copies task status or dispatches work.

A plan has a title, description, ordered tasks, decisions and open questions. Its progress follows referenced task states;
the task details provide dependencies and existing execution sessions. Users can edit and reorder task references in Work.
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
```

It exercises Personal and Team queues and plans, ordering, dependencies, persistence, session association, desktop/phone
layouts, All Projects aggregation and switching to a specific project context.
