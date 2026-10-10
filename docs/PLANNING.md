# Planning: labels, who does the work, and the timeline

Werkbord organizes work in ways that do not depend on what the work is. This page is the model, the rules, the API of both products
and what is deliberately **not** done. It applies to Individual and to Team alike; where they differ, it says so.

## The five ideas

| Idea | What it is | What it is **not** |
| --- | --- | --- |
| **Label** | A name and a #rrggbb colour you chose, kept once and put on any number of tasks (Individual) or tickets (Team), in any project. | Not a status, not a priority, not who does the work. Nothing is built in: there is no "development" or "marketing" label unless you make one. |
| **Work mode** | Who is expected to do a piece of work: `human`, `agent` or `hybrid`. A fixed classification. | Not a label, however you name your labels: a label called "human" does nothing. |
| **Plan** | When the work is planned to happen, in whole days (`YYYY-MM-DD`): a start, an end, or a single milestone date. For people and for the timeline. | Not a schedule. When an agent *starts* is a task's schedule (Calendar), which a plan never touches. |
| **Dependency** | Work that must finish before this work starts. | Not a date. Setting one never moves anything. |
| **Work project** | (Individual) A board and a timeline with no Git repository behind it, for work that is not code. | Not a repository: no folder, no Git, no worktrees, no agent runs. |

`internal/planning` is the one shared package behind them ([STRUCTURE.md](STRUCTURE.md#shared-packages)): label names and colours, work
modes, date ranges and the analysis of dependencies. It is pure functions with no storage, workspace or role in it, so both products
apply the same rules and neither reaches into the other.

## Labels

- A name is trimmed and its white space folded; at most 40 characters, no control characters. Names are unique **ignoring case and
  spacing** ("Design" and " design " are one label). A description (optional, 200 characters) says what it is for.
- A colour is `#rgb` or `#rrggbb`, stored as lower-case `#rrggbb`. Nothing else is accepted, so a colour can never carry markup; the
  interfaces also refuse to use one that is not exactly that shape, and set it through the element's style object.
- A task or ticket carries up to 20 labels, kept in the order they were put on. A workspace defines up to 500.
- **Renaming or recolouring** a label changes it everywhere it is used. **Deleting** one takes it off every task or ticket that carries
  it (their versions move on, so a client that holds one sees it changed) and changes nothing else: nothing moves, nothing starts or stops.
- **Individual**: one list for the controller, shared by all your projects (`GET/POST /api/labels`, `PATCH/DELETE /api/labels/{id}`). The
  controller has one owner, authenticated before any of this is reached, so there is no per-label permission. A program's narrow local
  access token cannot reach these routes (the route list is deny-by-default, and tested).
- **Team**: one list per workspace, shared by all its projects (`GET/POST /api/team/v1/labels`, `PATCH/DELETE /api/team/v1/labels/{id}`).

| | Owner | Admin | Member | Project owner / reviewer |
| --- | :---: | :---: | :---: | :---: |
| see the workspace's labels (and how many open tickets carry each in projects you can see) | ✓ | ✓ | ✓ | ✓ |
| define, rename, recolour, delete (`labels.manage`) | ✓ | ✓ | | |
| put a label on a ticket | the ticket's creator, or anyone with `tickets.edit` (the project owner) |

A project role never confers `labels.manage`: labels belong to the workspace, not to a project. A label of another workspace cannot
be put on a ticket (the database refuses it too: the link is keyed by workspace).

## Work modes

| Mode | Meaning | Effect |
| --- | --- | --- |
| `human` | A person does it. | **No agent is ever started on it**: not by hand, not by schedule, not by a shared request. Setting a schedule on it is refused; switching a scheduled task to human switches its schedule off. |
| `agent` | An agent does it. | Exactly as before: started by hand or on a schedule. This is what every existing task and ticket is, and what an unset mode means. |
| `hybrid` | An agent may start on it and a person finishes or checks it. | Behaves as `agent`; the mode tells people a person is still part of it. |

- **Individual** applies it in one place, `domain.AgentRefusal`, which the manual start, the scheduled start, the scheduler's plan and the
  Calendar all use, so they cannot disagree. A task in a **work project** is refused for the same reason whatever its mode (there is no
  repository for an agent to work in). New tasks in a work project start as `human`; elsewhere as `agent`.
- **Team** never runs anything. The mode decides only whether a shared request for agent work (a *schedule*) may be made for the ticket
  (`PUT …/tickets/{tid}/schedule`): refused with `409` for human work. A new ticket is `agent` unless the creator says otherwise (the
  console starts a project with no repository on "Human"). Team does not refuse a ticket for lacking a repository: that was never a rule,
  and the member's own Werkbord, which alone knows whether it has the code, answers the request.

## Plans, milestones and the timeline

A plan is `{start, end, milestone}`: both dates optional and inclusive. Only a start is a one-day item; only an end is something due that
day; a milestone is a single date (kept in `start`). An end before its start is refused. Plans are **not** an `Orchestration` (Individual)
or a schedule (Team): changing one never schedules, delays or moves a run.

The timeline shows a project's planned work as bars (milestones as diamonds) on a day scale (week, month or quarter zoom), with a
line for today, grouped by nothing, label, status or who does it (a task with several labels is listed under each), filtered by label
and work mode, with dependencies as arrows. Work with no dates is listed under the chart ("Not planned yet") with a way to plan it.

**Dependencies are one list, and it is the one the scheduler waits on.** In Individual that is `Orchestration.Dependencies`: an agent is
not started before they finish, and the timeline draws them. In Team it is the ticket's own list (the `ticket_dependencies` table, set in
the ticket form): the timeline draws it and checks it, and a shared request for agent work waits on it too ([below](#team-one-list-of-dependencies)).
A dependency must be another task or ticket **of the same project**.

### Team: one list of dependencies

Until 4.7.0 a Team ticket had two lists of "what must finish first": the planning one (this page) and a second, independent one on its
shared request for agent work, typed as raw ticket IDs. Only the second made anything wait. There is now **one**, the ticket's.

- **What it gates.** A shared request waits until each ticket on the list is **Done** or has a request that is **completed**: exactly what
  it always meant. Order, priority, the missed policy, the grace window and capacity are untouched. A planning-only dependency, set in the
  ticket form and never on a request, now holds a request back too: that is the visible change.
- **Why one.** Two lists meant the arrows on the timeline could say one thing while the scheduler waited on another. Individual has had
  one list from the start; Team now does as well.
- **Archived work.** A ticket may depend on a ticket that was archived without being finished (the timeline warns). A request for it then
  waits, and says so: `waiting for dependency WB-12, which was archived without being finished; finish it or remove it from this ticket's
  dependencies`. Removing it is a ticket edit (see the stale rule).
- **The request keeps a copy, not a second list.** `Schedule.Dependencies` stays in the request's stored document as a record of what the
  request was proposed with, and in the request's fence. Nothing reads it to decide what to wait for. It was kept rather than dropped
  because a fence identifies an execution that may already be approved on someone's computer, and recomputing it from different input would
  invalidate every one of those; it also has no column of its own to drop (the request is a JSON document), so dropping it would have been
  a rewrite of every stored request for no behavioural gain.
- **`PUT …/schedule` still accepts `dependencies`**, for clients that send it, when the list is the ticket's own list (in any order);
  leaving it out is the same. Any other list is refused with `400`: *set what this request waits for in the ticket's dependencies, not on
  its schedule*. Nothing can be set on a request that the ticket does not say.
- **Stale rule.** A request is bound to the ticket as it was proposed (`ScheduleContext`: the same hash that already covers the title,
  description, requirements, assignee and repository). The ticket's dependencies, sorted, are now part of it. Changing them while a request
  is live (not completed or canceled) makes the request **stale**: it is blocked with the reason *the ticket's dependencies changed after
  this request was proposed; propose it again*, it cannot be dispatched, and an approval already given on a computer no longer matches.
  Proposing it again gives a new execution identity, as for any reschedule. Putting the dependencies back as they were makes the request
  valid again; reordering them changes nothing. A ticket that waits for nothing has exactly the fence it had before, so requests made
  before this release keep working; one proposed before this release whose ticket waits for exactly what the request recorded also keeps
  working (see below), and one whose ticket now waits for something else is stale.
- **Circles.** Writing a dependency refuses a circle, as before, and a request is not proposed for a ticket that sits in one. Only data that
  arrived another way (the migration below) can contain one: the timeline reports `dependency_cycle`, the requests in it wait and say what
  they wait for, and nothing loops (eligibility looks one step along, never recursively). The way out is to remove one dependency.
- **The console** shows a read-only line in the ticket's *Shared schedule* panel, *Waits for WB-12 Build, WB-14 Design (set on the ticket)*,
  with a way to the ticket's edit form; a note when the request is stale; and, on the timeline, heavier arrows where the dependent ticket has
  a live shared request (those gate an agent start).

### What is warned about, and what is never done

`planning.Analyze` reads the tasks or tickets of a project and reports, in a stable order (problems first):

| Code | Severity | Meaning |
| --- | --- | --- |
| `invalid_date`, `invalid_range` | problem | A date that is not `YYYY-MM-DD`, or an end before its start (data that arrived another way: a write is refused). |
| `self_dependency`, `missing_dependency` | problem | Depends on itself; or on work that is not in the project. |
| `dependency_cycle` | problem | Work that waits for each other in a circle, so none can start. Reported once per circle, naming every member. |
| `starts_before_dependency_ends` | conflict | Starts before the work it depends on ends. Starting **on** the day it ends is allowed. A finished dependency no longer constrains anything. |
| `done_before_dependency_done` | conflict | Is done while the work it depends on is not. |
| `archived_dependency` | conflict | Depends on work that was closed without being finished. |
| `duplicate_dependency`, `milestone_without_date` | conflict | Listed twice; a milestone with no date cannot be placed. |
| `unscheduled_dependency` | note | Has dates, but the work it depends on has none, so the order cannot be checked. |

The writes refuse what can never work (a self-dependency, a dependency outside the project, a circle, a bad date), so the analysis
mostly tells you about **conflicts**, and about whatever reached the data another way (a restore, an older build). **Nothing is ever
rescheduled.** The warnings are shown (a panel above the chart, a mark on the bar and the row) and what to do about one, move a date or drop
a dependency, is yours to decide on the task. Reading the timeline changes nothing: its endpoints are reads, and a test asserts it.

| Product | Timeline |
| --- | --- |
| Individual | `GET /api/projects/{pid}/timeline` → `{"warnings": […]}`; the app's **Timeline** section. |
| Team | `GET /api/team/v1/projects/{id}/timeline` → `{"warnings": […]}` (any member who can see the project); the console's **Timeline** tab. |

## Work projects and tickets without a repository

- **Individual**: `POST /api/projects` with `{"kind": "work", "name": "…"}` (the Projects page: **Add a project → Work project**). Nothing is
  read from or written to the disk. It has a Board, a Timeline and an Overview, and **no** Calendar, Git or Runs. Everything that
  reads a repository (inspection, worktrees, health, Git actions, the doctor's folder check, the Team connector's list of places to hand a
  ticket to) skips it or answers `400`. A work project can never become a repository project, or the other way round (the database refuses
  it). Its tasks have no branch or source to track.
- **Team**: a project's repository was always optional, and so is everything the workflow needs: a ticket in a project with none is created,
  claimed, submitted, reviewed and completed with no Git report of any kind (a test runs exactly that).

## What is stored (migrations)

| | Migration | Adds |
| --- | --- | --- |
| Individual | `0013_planning.sql` | `labels`, `task_labels`; `tasks.work_mode`, `plan_start`, `plan_end`, `milestone`; `projects.kind`. Existing tasks become `agent` with no labels and no plan; existing projects become `repository`. |
| Team | `0014_planning.sql` | `labels`, `ticket_labels`, `ticket_dependencies` (all keyed by workspace); `tickets.work_mode`, `plan_start`, `plan_end`, `milestone`. Existing tickets become `agent` with no labels, plan or dependencies. |
| Team | `0015_one_dependency_list.sql` | Copies the dependencies of every existing shared request onto its ticket's `ticket_dependencies`. Nothing is created or dropped. |

**Migration 0015.** For each request it adds a ticket dependency only where both tickets exist, are in the same workspace and project, are
different tickets, and are not already linked; the list in the request must be a JSON array and each entry a string. Any other row is skipped,
so it cannot fail an upgrade; running it again adds nothing; it adds nothing the request did not already have (a ticket's other dependencies
are left as they are). It copies the dependencies of every request, finished or canceled ones included, because a request that is proposed
again would otherwise silently lose what it used to wait for. SQL cannot see circles: a ticket whose request waited for a ticket that, in
its own planning list, waits for it, becomes a circle (see above).

SQLite cannot drop `projects.repo_path`'s `NOT NULL UNIQUE` in place, and the repository avoids rebuilding that table (see migration `0003`),
so a work project keeps the placeholder `work:<its id>` there: unique by construction and never a path. The application reads it back as
an empty `repoPath`; triggers keep the two kinds apart whoever writes the row.

## What does not change

Scheduling, dependencies as the scheduler reads them, runner routing, capacity, the Git gates and every existing endpoint answer as before
for a task or ticket that has no labels and no plan and is agent work, which is every one that existed. (Team's one list of
dependencies, 4.7.0, is the exception for Team tickets that have dependencies: see above. Individual is not affected, and
`internal/runner/planning_test.go` asserts it.) The scheduling and runner test suites
run unchanged against this release, and `internal/runner/planning_test.go` and `internal/service/planning_test.go` assert it directly.
