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

**Dependencies are the same list the scheduler already waits on** in Individual (`Orchestration.Dependencies`): an agent is not started
before they finish, and the timeline draws them. In Team, a ticket's dependencies are planning only; they are separate from the
dependencies of a shared request for agent work, which keep meaning what they meant. A dependency must be another task or ticket **of the
same project**.

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

SQLite cannot drop `projects.repo_path`'s `NOT NULL UNIQUE` in place, and the repository avoids rebuilding that table (see migration `0003`),
so a work project keeps the placeholder `work:<its id>` there: unique by construction and never a path. The application reads it back as
an empty `repoPath`; triggers keep the two kinds apart whoever writes the row.

## What does not change

Scheduling, dependencies as the scheduler reads them, runner routing, capacity, the Git gates and every existing endpoint answer as before
for a task or ticket that has no labels and no plan and is agent work, which is every one that existed. The scheduling and runner test suites
run unchanged against this release, and `internal/runner/planning_test.go` and `internal/service/planning_test.go` assert it directly.
