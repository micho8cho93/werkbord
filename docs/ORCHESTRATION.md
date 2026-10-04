# V1: Calendar, scheduling, dependencies and handoffs

All execution remains on this controller. Calendar is a view of the same tasks
as Board, scoped to a project. It has day/week views, an upcoming queue, hourly
drag targets, and a schedule form that also works on a phone. Open a task to
reschedule or inspect its execution policy, dependencies, not-before time,
execution order, priority and deadline. Unschedule turns automatic execution off
and removes its exact scheduled time; the task stays on the Board.

## One-shot execution and recovery

Enable **Run automatically from the controller** to arm one attempt. An exact
time is optional: without one, this is a queue entry. Saving scheduling settings
explicitly rearms the attempt; edits to task text or execution options do not.
The SQLite task row stores `orchestration`, including the schedule's unique key,
UTC timestamps, IANA timezone, dependency IDs and the dispatch result. Inputs
must carry an RFC3339 offset. The UI converts wall times with the chosen timezone;
it rejects nonexistent DST hours and offers the first/second occurrence of a
repeated hour. Timezone data is embedded in the controller for portable installs.

The controller evaluates pending work every second after run recovery. No page,
phone, frontend event stream, or remote worker is needed. Starting a run commits
its durable schedule claim, run and task event in one SQLite transaction. A unique
index prevents reusing `(task, schedule key)`. A manual Start also consumes an
armed pending schedule. Interrupted attempts retain their claim and surface as
failed; they are never automatically rerun. This is **at-most-once dispatch per
armed attempt**, not a promise of exactly-once agent side effects. Rearm after
inspecting an interrupted or failed run. Worktree setup before the claim can be
recovered and reused without having launched an agent.

Choose an explicit missed-time policy:

- `run_late` (default): run when dependencies, capacity and repository checks
  permit, including after a controller restart.
- `skip`: mark missed if the scheduled time plus `graceSeconds` has passed.
  The window applies while waiting for dependencies or capacity too. A missed
  attempt stays visible and requires an explicit rearm.

A passed deadline blocks execution. Not-before is a gate even for manual Start;
a manual Start may otherwise intentionally run before the scheduled time.
A scheduled session finishes after an agent turn that ends without a question
or blocker. Completed scheduled work moves to Review, releases capacity, and
can unblock dependencies. An interactive question waits for the user as usual;
autonomous questions use the existing autonomy rules; stop-if-blocked remains
blocked. Approvals always go to the user. The schedule never invents an answer.

## Dependencies, order and deterministic decisions

Dependencies must be distinct other tasks in the same project. The service
checks the complete proposed graph in the write transaction and rejects direct
and indirect cycles. Completion means a latest completed run or a task explicitly
marked Done, with no active run. A failed, stopped or blocked dependency blocks
its dependents with a named reason. A new active run means its earlier success
no longer satisfies completion. Dependencies do not transfer or merge changes:
separate tasks use their own worktrees. If downstream work needs upstream code,
review and integrate it in Git first; use another run under the same task when
continuing implementation on the same files.

Eligible tasks sort by scheduled instant (entries without an exact instant sort
first), execution order (unset sorts last), resolved priority (high, normal, low),
creation timestamp, then task ID. Order controls launch precedence; use
**dependencies** when one task must finish before another starts. Board position
is independent. Agent/model/interaction/priority retain the existing hierarchy.

`GET /api/projects/{pid}/schedule` returns decisions with explicit reasons:
`runnable`, `queued`, `waiting_dependency`, `waiting_schedule`, `waiting_capacity`,
`potentially_conflicting`, or `blocked`. The Control Center keeps Needs Input,
Blocked, Failed and Review prominent, with Scheduled/Queued/Waiting on Dependency
below. Board cards and Calendar show the same decisions.

Project Settings controls concurrent sessions (1 by default, 1–16 allowed),
persisted through `/api/projects/{pid}/orchestration`. Starting, running, waiting
and blocked sessions all occupy slots. Launch and Git Control writes share a
per-project lock, and launch rechecks durable gates before claiming a run.
Repository reads are local and do not fetch or merge.

## Conservative conflict checks

Open critical repository-health findings, unresolved conflicts and in-progress
repository operations block new work. An expected full target commit can pin the
state a task was planned against. A root checkout or previously used task branch
behind the current target must be reconciled before starting or continuing work.
Expected relative paths/directories
are optional hints. Unknown scope or hints that overlap serialize active work,
even when capacity is greater than one. The scheduler also checks actual staged,
unstaged, untracked and committed changes on active worktrees for overlap with
the next task's expected scope. Missing or truncated evidence is conservative.

This does not predict future edits: an agent can later leave its declared scope.
The default concurrency of one is the safest choice for tightly coupled work.
No automatic rebase, force push, merge, fetch or file-conflict resolution is added.

## Run handoffs and continuation

Each run snapshots its original task objective. On completion/failure/stop it
stores a structured `handoff` with summary, changed files, decisions, tests,
results, Git state, known issues, blockers, unanswered questions and next action.
Agents are asked for a compact tagged JSON report; reported decisions/tests/results
remain agent evidence, not controller-verified claims. If no report is provided,
only the last assistant report is retained, and missing evidence is stated as
unknown. The controller captures changed paths and Git state independently.
The artifact can be edited with compare-and-swap version protection. Git evidence
can be refreshed without discarding edits. Terminal runs permit these actions:

- `POST /api/projects/{pid}/runs/{id}/handoff`: capture/refresh the artifact.
- `PUT /api/projects/{pid}/runs/{id}/handoff`: save `{version, handoff}`.
- `POST /api/projects/{pid}/runs/{id}/continue`: create another run under the same
  task, naming `agentId`, optional `model`/`reasoning`/`policy`, `purpose`
  (`continue`, `review`, `fix`), `instructions`, and `selectedContext`.

Continuation requires a terminal parent of the same task and an available task
worktree. It starts a fresh conversation with task instructions, the edited
handoff, current repository evidence, bounded committed/working diffs (including
up to five untracked files), and selected context. Diff content is capped at
20 KB, selected context at 16 KB, and the handoff at 32 KB. It never replays the
prior transcript. Changes remain in the task's existing worktree across agents.
The new run persists its parent ID, purpose and attempt number, making an
implement → review → fix chain clear in run history and Activity, including
agent/model changes and equal-time attempts. An ordinary session resume remains
available separately for users who deliberately want their earlier conversation.

Tests cover time/offset/DST behavior, deadlines, missed windows, cycle and scope
rejection, dependency failure/success, priority/order, capacity, observed and
hinted overlap, stale branches, atomic claims, controller and scheduler recovery,
policy handling, handoff evidence/CAS, selected context, agent/model switches,
review/fix chains, and multiple attempts of one task. Multi-machine workers,
recurring schedules and workflow graphs remain deferred.
