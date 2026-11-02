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

### Running a plan (Personal)

A plan's **Execution** tab runs it without owning any run. `GET …/work-plans/{plan}/run` previews what would happen;
`POST …/run` arms the plan's eligible tasks as one-shot schedules in one transaction; `POST …/hold` pauses. Dispatch stays with
the existing scheduler loop, so the unique `(task_id, schedule_key)` claim keeps restarts and reconnects duplicate-free.

- **Preview** lists each task as *Will start*, *Waiting to start* or *Left alone* with the reason (done, archived, human work, already
  running, an earlier session to review), its dependencies, resolved agent and runner, the scheduler's current reading, the project
  concurrency limit, and notes on billing, review and interruption. Its digest covers the plan version, mode and each task's version
  and action, not live capacity.
- **Run** is refused (409) if the plan version or the digest differs from what was reviewed. Asking again when nothing is left to
  arm changes nothing. **In order** adds a dependency on the nearest earlier open task; **as each becomes ready** adds none.
- A step whose last session **failed or was stopped** shows as *Will start again*, with the reason; running the plan you reviewed is
  the request for one new session. It is armed once: an armed retry shows as waiting, and the scheduler's unique claim keeps it to
  one run. A step whose session *completed* is left alone for you to review, and later steps wait for it.
- **Pause after current task** disarms steps this run armed that have not dispatched. A running agent is never touched (the
  adapters stop and restart; they do not pause). **Resume** re-arms after a fresh preview. One control cycles Run plan, Pause after
  current task, Resume.
- A step finishing moves its task to Review; the next step waits for the run before it to *complete*, not for approval.
- The run state lives in the plan document (`run`), writable only by this service; a plan edit keeps it.
- Team plans have no such control: Team never executes. Each member starts their own ticket from its Agent controls.
  `internal/archtest/plan_run_test.go` pins this: Team's build does not link the services or the runner, and no Team source names the
  plan-run or fallback operations or routes.

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
