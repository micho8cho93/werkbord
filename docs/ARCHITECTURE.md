# Werkbord architecture

Werkbord is a **local-first control plane for coding agents**. One program, the
controller, runs on your computer. It owns SQLite, the board, calendar, scheduler and
user-facing application. Local or paired runners own their agent processes, Git clones,
worktrees and credentials. Phones, tablets and browsers
are remote controls for it. There is no hosted backend, no account system and no cloud
database.

The repository also builds **Werkbord Team**, a separate product that coordinates a team and never executes anything;
this document is about the individual product, and [PRODUCTS.md](PRODUCTS.md) explains how the two relate. Nothing here
depends on Team, and a test keeps it so.

This document describes the system as built: the foundation, the agent runtime (§8, §14),
questions (§15), projects as the scope of the application (§16), per-task execution policies (§17), the Git Control Center (§18) and repository health (§19) and installation, the private network and execution defaults (§20). Sections marked *Deferred* name things that are intentionally not implemented yet.

---

## 1. Shape of the system

```
 Phone / tablet / desktop browser (installable PWA, Svelte + TypeScript)
        │  HTTP JSON  +  Server-Sent Events (/api/events)
        ▼
┌──────────────────────── werkbord controller (one Go process) ───────────────────────┐
│                                                                                       │
│  api ──────────▶ service ──────────▶ store (interfaces) ──▶ store/sqlite ──▶ devboard.db
│  (HTTP, SSE,      (use cases,          ▲                                              │
│   auth, PWA)       domain rules)        │ same transaction                            │
│      ▲               │   │              │                                             │
│      │               │   └──▶ events.Publisher ──▶ events.Broker ──┐                  │
│      └───────────────┼──────────────────────────────────────────────┘ (live signal)   │
│                      ├──▶ gitrepo.Inspector ──▶ git CLI ──▶ your existing checkouts   │
│                      └──▶ runner.Manager ──▶ agent.Adapter ──▶ claude / codex process │
│                                                                                       │
│  domain: Project · Task · Run · Agent · Question · Worktree · GitRepository · Event  │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

The controller is a **modular monolith**: one binary (`bin/werkbord`), one SQLite file,
with package boundaries doing the job that service boundaries would do in a distributed
system. No Docker, no Electron, no second language on the backend.

## 2. Component boundaries

| Package | Owns | May depend on | Must not |
| --- | --- | --- | --- |
| `internal/domain` | Entity types, state enums, validation, the run state machine, sentinel errors, and the pure rules (Git state words, repository-health findings and their lifecycle) | stdlib only | Know about SQL, HTTP, Git or agents |
| `internal/store` | Persistence **interfaces** (`Store`, `Tx`, one repo per aggregate) | `domain` | Contain an implementation |
| `internal/store/sqlite` | The SQLite schema, migrations and repositories; opening, migrating and backing up the file is `internal/sqlitekit` | `domain`, `store`, `sqlitekit` | Contain business rules beyond integrity constraints |
| `internal/sqlitekit` | **Shared with Werkbord Team.** Opens a SQLite database (one writer, readers, WAL), runs versioned migrations, backs up before an upgrade | stdlib, the SQLite driver | Know any product's schema |
| `internal/httpkit` | **Shared with Werkbord Team.** JSON responses and error envelope, strict body decoding, request logging, panic recovery, security headers | stdlib | Hold a route or a rule |
| `internal/events` | Live fan-out of committed events (`Publisher`, `Subscriber`, `Broker`) | `domain` | Be relied on for durability |
| `internal/gitrepo` | The Git boundary: `Inspector`; `Worktrees` (makes and removes linked worktrees: all the agent runner may use); `Reader` and `Operator` (the Git Control Center's reads and guarded writes); one CLI implementation | `domain` | Run a repository's hooks, force anything, or decide whether an action *should* happen |
| `internal/github` | The GitHub boundary: the user's own `gh` CLI, for pull requests | `domain` | Hold a GitHub credential, account or token |
| `internal/agent` | The agent boundary (`Adapter`, `Session`, `Registry`), process groups, the event queue | `domain` | Leak protocol details of a specific agent |
| `internal/agent/claude`, `internal/agent/codex` | One adapter each: the agent's protocol → normalised events | `agent`, `domain` | Know about runs, tasks or the database |
| `internal/agent/fake` | A scriptable in-memory adapter for tests | `agent`, `domain` | Be used outside tests |
| `internal/runner` | Agent processes: starting runs, the live-session goroutines, input, stop, recovery, shutdown | `service`, `agent`, `gitrepo` | Write the store except through `service` |
| `internal/service` | Use cases: register project, create/move task, recover runs, `GitControl` (what to show of Git, and every safety check before an action) and `GitHealth` (what Git state needs attention: gathers facts, keeps findings' lives, watches events) | all of the above via interfaces | Speak HTTP |
| `internal/api` | HTTP routing, JSON, SSE and auth; the response and security helpers are `internal/httpkit` | `service`, `store`, `events`, `agent`, `runner`, `httpkit` | Contain business rules |
| `internal/webui` | Serving the embedded PWA build | stdlib | — |
| `internal/controller` | Wiring and lifecycle | everything | — |
| `internal/config`, `internal/logging` | Settings and the slog logger | stdlib | — |
| `cmd/werkbord` | CLI entry point (`serve`, `migrate`, `project add/list`, `version`) | `controller`, `config` | Write the database while a controller runs |
| `web/` | The PWA | the HTTP API only | — |

### The four replaceable interfaces

- **Persistence** — `store.Store` with `View` (read-only snapshot) and `Update`
  (serialised read-write transaction). Services never see `database/sql`.
- **Event delivery** — `events.Publisher` / `events.Subscriber`. The in-process broker
  could be swapped (e.g. for a fan-out to a desktop shell) without touching services.
- **Git operations** — `gitrepo.Inspector`. Implemented by shelling out to `git`, because
  that respects the user's own Git config, credentials helpers and `safe.directory`
  rules. Services are tested with a fake.
- **Agent execution** — `agent.Adapter` → `agent.Session`, driven by `runner.Manager`. See §8 and §14.

## 3. Domain model

```
Project 1───* Task 1───* Run *───1 Agent (runtime only, by ID)
   │1            │          │1
   │             │          ├──* Question
   │1            │          └──? Worktree *───1 Project
GitRepository    │
   (snapshot)    └─ events reference project/task/run IDs, no foreign keys
```

- **Project**: a registered local Git repository. `RepoPath` is the repository's
  top-level directory, absolute and symlink-resolved. The repository is never copied.
- **GitRepository**: the last inspection snapshot of a project's repository (branch, HEAD,
  origin/HEAD, remotes with any embedded credentials redacted). Refreshed on demand. It
  records the repository's **common directory** (the shared `.git`), which is the
  repository's identity: a linked worktree has its own top-level path but the same common
  directory, and `git_repositories.common_dir` is unique.
- **Task**: a card on the board. Its `State` is one of exactly four workflow states:
  `backlog`, `doing`, `review`, `done`. Ordered within a column by `Position`. It carries an
  execution `Policy` (§17): how its runs are carried out, interactive by default.
- **Run**: one interactive agent session working on a task. Its `State` is one of
  `starting`, `running`, `waiting_for_user`, `blocked`, `completed`, `failed`, `stopped`. While it
  waits, `Waiting` says what for: `question` (blocked on an answer) or `idle` (the agent finished a
  turn and awaits the next message). `blocked` is different: the run's policy forbids guessing and
  the agent stopped at a decision it could not safely make; `Blocker` says what (§17). A run keeps a
  copy of the `Policy` it started with. It also records the prompt, the agent's resumable `SessionRef`,
  its latest one-line `Activity`, the process exit code and, internally, the process's PID and
  identity.
- **Question**: something an agent asked during a run: a `clarification`, `decision`, `approval`,
  `selection` or `instruction`. It is self-contained (run, task and project IDs, prompt, optional
  `context`, optional `options`, `allowFreeText`) and has a state: `pending`, `answered` or
  `cancelled` (see §15).
- **Worktree**: a Git worktree created for a run so concurrent runs never share a
  working directory. Its row is the controller's only evidence that it owns a directory, so
  it is constrained like a deletion target (see "Worktree safety" below).
- **Agent**: a coding agent the controller can drive. Discovered from adapters at
  runtime, not persisted; runs store its ID.
- **Event**: an append-only record of a change, with a strictly increasing `Seq`.

### Workflow state vs. runtime state

A task's workflow state and a run's runtime state are **separate on purpose**. Moving a
card never starts, stops or changes a run, and a run ending never moves a card. A task
can sit in *Doing* while its latest run failed; a run can complete while the task waits
in *Review* for a human. Policies that connect the two (e.g. "a completed run moves the
task to Review") will be explicit service code, not an implicit coupling of enums.

A run is a **session**, not a job. The agent works a turn, then waits for the next message
(`waiting_for_user`/`idle`); it asks questions (`waiting_for_user`/`question`) when it needs a
decision or permission. The session ends when the process exits, the user finishes it, or the user
stops it.

Run transitions are enforced by `domain.Run.Transition` and `WaitFor`:

```
starting ──▶ running ◀──────────────▶ waiting_for_user ──▶ completed
   │         ▲  │ ╲                       │       (finishing an idle session)
   │         │  │  ╲─▶ completed          │
   │         │  ▼                         │
   │         └─ blocked ─▶ completed      │
   └────────────┴──────────────▶ failed | stopped   (terminal states have no exits;
                                                       a retry is a new Run)
```

The database enforces the pairing too: `waiting` is set exactly while the state is
`waiting_for_user`, and an ended run has no process.

A **blocked run is execution state, never a Kanban column**: the task stays in *Doing* (or wherever the
user put it) while its run is blocked, exactly as it does while the run fails. There is no fifth column;
the board distinguishes *Running*, *Needs input*, *Blocked* and *Failed* on the card.

**Policy that connects the two** (explicit in `service`, not implicit in the enums): a task moves
to *Doing* in the same transaction that marks its run *running* (and again when the user resumes
work on it from Review); a task already in Doing or Done is left alone. Nothing moves a card when
a run ends: that is the user's call.

## 4. Data flow

**A write** (e.g. moving a task from a phone):

1. PWA sends `PATCH /api/projects/{pid}/tasks/{id}` with `{state, version}`.
2. `api` decodes and validates the shape, then calls `service.Tasks.Update`.
3. The service opens one `store.Update` transaction: reads the task, checks the
   version, applies domain validation, writes the task (compare-and-swap on `version`)
   and appends a `task.updated` event to the event log.
4. The transaction commits. Only then does the service publish the event to the broker.
5. Every connected client's `/api/events` stream delivers it; each PWA updates its cache.
6. The HTTP response carries the new task, so the acting device updates without waiting.

A stale `version` returns **409 conflict**; the PWA reloads the board and tells the user.
Two devices (and, later, an agent) can never silently overwrite each other.

**A read** goes `api → service → store.View`, a read-only transaction on a separate
connection pool, so reads never wait for writes.

**Project registration**: `POST /api/projects {path}` → `gitrepo.CLI.Inspect` checks the
path exists, is a directory, is inside a non-bare Git repository and can be inspected
(`rev-parse`, `symbolic-ref`, `config --get-regexp`). The top level is stored; registering
the same repository twice (even via a subdirectory, a symlink or a linked worktree) returns
409. A repository whose `HEAD` names a missing object is rejected as damaged rather than
reported as empty; a branch with no commits yet is still accepted.

**Project refresh**: `POST /api/projects/{id}/refresh` re-inspects the stored path. If that
path no longer resolves to the repository registered there (for example the directory was
deleted and recreated inside another repository) the refresh returns 409 and the stored
snapshot is left untouched, so a project never silently starts describing another repository.

## 5. Controller lifecycle

`werkbord serve` → `controller.Run`:

1. **Load config**: defaults → `<dataDir>/config.json` → `WERKBORD_*` env (or the older `DEVBOARD_*`) → flags.
2. **Lock the data directory** with an exclusive `flock` on `controller.lock`. A second
   controller on the same data dir fails fast. The OS releases the lock if the process dies.
3. **Open SQLite and migrate** (see §6). A database from a newer build is refused.
4. **Recover** state left by the previous process (`runner.Manager.Recover`). First, any agent
   process still running for a recorded run is stopped, but only if the PID still has the
   recorded start-time identity and leads its own process group, so a PID reused by an unrelated
   program is never signalled. Then `service.Runs.RecoverAfterRestart` settles the runs: those that
   were `starting`/`running` are marked `failed` (`interrupted: controller restarted`); a run that
   was waiting is kept if it has a `SessionRef` (its pending questions are cancelled, since the
   requests died with the process, and it becomes `idle`: a message resumes it) and failed
   otherwise. Each change emits `run.state_changed` and an `agent.*` event.
5. **Resolve auth**: a token is required unless `requireToken` is off and the address is
   loopback (§10); it is read from config or `<dataDir>/token`, generating one (mode 0600) on
   first use. If auth is off the controller logs a warning.
6. **Wire** broker, services, API and the embedded PWA; **listen**; serve.
7. **On SIGINT/SIGTERM**: `http.Server.Shutdown` stops accepting connections and waits
   for in-flight requests (up to `shutdownTimeout`, default 10s). A shutdown hook closes
   the event broker first, which ends every SSE stream, so long-lived connections do not
   hold shutdown hostage. Then `runner.Manager.Shutdown` stops every agent process (SIGTERM to
   its process group, SIGKILL after 5 s) and records the outcome while the database is still
   open: working runs fail with `interrupted: controller shut down`, waiting runs are kept
   resumable. Only then are the database closed and the lock released.

If any startup step fails, everything already started is torn down in reverse order.

## 6. Database ownership

- **The controller is the only writer.** CLI commands such as `werkbord project add`
  are HTTP clients of the running controller, so every change goes through services,
  validation and the event log. `werkbord migrate` is the one offline command, and it
  is guarded by the same migration code.
- Location: `<dataDir>/devboard.db` (default `~/Library/Application Support/werkbord` on
  macOS, `~/.config/werkbord` on Linux; an install from before the rename keeps `…/devboard`). The file keeps
  its name. Directory 0700, file 0600.
- Driver: `modernc.org/sqlite` (pure Go, so builds need no C toolchain). WAL mode,
  `synchronous=FULL` on the writer (a committed transaction survives a power cut; with
  `NORMAL` it could roll back, and a record written before a directory is created or after
  one is removed would then disagree with the disk), foreign keys on, `STRICT` tables,
  `CHECK` constraints on every state column so the database itself rejects a fifth column
  or an unknown run state.
- **Migrations** are embedded SQL files `internal/store/sqlite/migrations/NNNN_name.sql`,
  contiguous from 1, each applied in its own transaction and recorded in
  `schema_migrations`. Migrations are forward-only; to change the schema, add a file.
- Timestamps are Unix milliseconds (UTC). Small list fields (remotes, question options)
  are JSON columns because they are always read and written whole. So is the execution policy
  (`tasks.policy`, `runs.policy`, `{"interaction": …}`), with only the interaction mode constrained
  by a `CHECK`, so that finer-grained permissions can become further fields without a migration; and a
  run's `blocker`.
- A migration that must rebuild a table other tables refer to (SQLite cannot change a `CHECK` in place,
  and dropping a parent table with foreign keys on cascades into its children) starts with
  `-- migrate:foreign-keys-off`: the migrator turns them off on its connection, outside the
  transaction, runs the file, runs `PRAGMA foreign_key_check` and refuses to commit if anything
  dangles, then turns them back on. Migration 0006 does this to add `blocked` to `runs.state`, and
  recreates every index and trigger that hung off `runs`, counting `blocked` as an active state (so a
  blocked run still owns its worktree). A test upgrades a populated version-5 database to prove nothing
  is lost.
- Migration 0007 adds `health_findings` (the memory of what repository health found: identity, severity and
  state in columns, the wording and evidence as JSON, with `CHECK`s that keep a finding's state and its
  timestamps consistent) and `health_checks` (when each project was last checked). They are a memory, not the
  source of truth: the findings are recomputed from Git, and these rows give them a life (§19).
- What survives a restart: projects, repository snapshots, tasks (with versions and
  order), runs, questions, worktrees, repository-health findings and the full event log.

## 7. Event architecture

Events are **durable first, live second**.

- Every state change appends to the `events` table **in the same transaction** as the
  change. The log cannot disagree with the tables.
- After commit, the service publishes the events to `events.Broker`, an in-memory,
  non-blocking fan-out. A subscriber whose buffer fills is disconnected rather than
  allowed to slow down writers.
- `GET /api/events` is a Server-Sent Events stream. Each message has `id: <seq>`,
  `event: <type>` and the event as JSON. Clients resume with `Last-Event-ID` (EventSource
  sends it automatically on reconnect) or `?after=<seq>`; without either, the stream
  starts at the end of the log.
- The SSE handler treats the broker as a **wake-up signal, not the source of truth**. If a
  live event is not exactly `lastSeq + 1` (publishes from concurrent writers can arrive
  out of order), it re-reads the gap from the database. The order clients see is always
  the commit order.
- SSE (not WebSocket) because the traffic is server→client; commands go over ordinary
  HTTP requests, which are easier to authenticate, retry and test. A WebSocket can be
  added later for interactive agent I/O if SSE + POST proves insufficient.
- Event types: `project.registered`, `project.inspected`, `task.created`, `task.updated`,
  `worktree.created|removing|removed`, `run.state_changed`, `question.answered|cancelled`, and the agent
  events below. `GET /api/events?project=<id>` carries only that project's events, filtered before
  anything is written (§16).
- **Agent events** are the activity timeline of a run (they carry `runId`): `agent.started`,
  `agent.output` (`{stream, text}`; streams `assistant`, `tool`, `user`, `system`, `stderr`),
  `agent.question`, `agent.waiting` (the agent finished its turn, or the session was
  interrupted), `agent.blocked` (`{blocker}`: the run stopped rather than guess), `agent.resumed`,
  `agent.completed`, `agent.failed`, `agent.stopped`. Every state
  change emits `run.state_changed` (carrying the run, which is what clients cache) *and* one of
  these, in the same transaction. `agent.waiting` means "idle, awaiting a message";
  `agent.question` means "blocked on an answer".
- Output is written in batches (150 ms or 64 KiB) because each write is a durable transaction,
  each event's text is capped at 16 KiB, and a run keeps at most 16 MiB of output.
- A run's history is `GET /api/projects/{pid}/runs/{id}/events` (paged backwards from the newest), served from an
  index on `events(run_id, seq)`.
- *Deferred*: event-log compaction or retention. The log grows without bound; at the
  expected volume (a person's tasks and runs) this is fine for a long time.

## 8. Agent adapter boundary

Claude Code and Codex expose different protocols. Each has an `agent.Adapter` that translates
its protocol into one vocabulary (`internal/agent/agent.go` has the full contract):

- `Detect` — installed, version, signed in? Cached for 30 s so `GET /api/agents` is cheap.
- `Start(StartRequest{RunID, WorkDir, Prompt, ResumeRef, Policy})` → `Session`. The adapter receives
  the run's normalised execution policy and passes `agent.Instructions(Policy)` through its agent's
  channel for standing instructions (§17); it does nothing else with it. The context bounds
  start-up only; the process belongs to the controller. A process that dies within moments of
  starting fails `Start` instead of looking like a session that ran.
- `Session.Events()` — `output`, `question`, `question_closed`, `turn_end`, `session_ref`. The
  queue behind it never blocks the agent and never drops a control event (output past a backlog of
  4096 is dropped, with a notice).
- `Session.Send` (a message: the next turn if waiting, otherwise queued or steering),
  `Respond` (answer a question), `Close` (graceful end), `Stop` (terminate the process group),
  `Wait` → `Result{State, Reason, ExitCode}`, `Process` (PID and identity).

**Claude Code** (`internal/agent/claude`): `claude -p --input-format stream-json
--output-format stream-json --verbose --permission-prompt-tool stdio`. The session ID is chosen
up front (`--session-id`), so it is stored before the agent says anything; `--resume` continues
one. Permission requests (`control_request`/`can_use_tool`) become approval questions; the
`AskUserQuestion` tool becomes ask questions, put to the user one at a time. Unsupported control
requests are refused rather than ignored, because an unanswered request hangs the session.
Default `--permission-mode acceptEdits`. Policy instructions go in `--append-system-prompt`.

**Codex** (`internal/agent/codex`): `codex app-server`, JSON-RPC over stdio. `thread/start` (or
`thread/resume`) then `turn/start` per message; a message during a turn uses `turn/steer`.
Command and file-change approvals and `item/tool/requestUserInput` become questions; requests the
controller cannot honour get a JSON-RPC error. Secret inputs (`isSecret`) are never collected.
Defaults `approvalPolicy=on-request`, `sandbox=workspace-write`, passed explicitly so they
override the user's own `~/.codex/config.toml`. Policy instructions go in `developerInstructions` of
`thread/start` and `thread/resume`.

Both were checked against the installed CLIs (Claude Code 2.1, Codex 0.155): Codex's generated
protocol schema, and `internal/agent/live`, an opt-in test (`WERKBORD_LIVE_AGENTS=1`) that runs
the real agents through round trips, approvals and resume. The default tests use scripted fakes
that speak the same protocols.

**Process handling** (`internal/agent/process*.go`, `base.go`): each agent runs in its own
process group; stdout and stderr are read by goroutines with line-length limits; stdin writes have
a deadline; when the agent exits, anything it started in its group is killed; a grandchild holding a
pipe open cannot delay the end. `Stop` is SIGTERM, then SIGKILL after a grace period.

## 9. Concurrency approach

- **One writer, many readers.** The SQLite store has a single-connection writer pool
  whose transactions begin with `BEGIN IMMEDIATE`, so `Update` calls queue in Go rather
  than fail with `SQLITE_BUSY` on lock upgrade. Readers use a separate 4-connection pool
  in query-only mode; WAL lets them run alongside the writer on a consistent snapshot.
- **Optimistic concurrency for user-visible entities.** Tasks, runs and worktrees carry a
  `version`; updates are compare-and-swap.
- **Inspections of one project are serialised.** An inspection reads Git and then writes what
  it read; two overlapping could finish in the opposite order and leave the older snapshot
  stored as the latest. `Projects.Refresh` takes a per-project lock (waiting honours the
  caller's context; other projects are unaffected).
- **Git processes are bounded.** At most 4 run at once (further requests queue, and give up
  with their caller), each with a 10 s timeout, 1 MiB of kept output per stream, and a
  1 s grace after a timeout: on timeout only git is killed, and a child it started could
  otherwise hold the pipes open and block the request indefinitely.
- **The broker never blocks.** Publishing is O(subscribers) non-blocking sends under one
  mutex; slow subscribers are dropped and catch up from the log.
- **HTTP handlers are stateless** apart from SSE streams, each of which owns one
  subscription and ends when the client leaves or the broker closes.
- **Runs**: each live run has one goroutine (`runner.live`) that is the only reader of its
  session's events; user actions (message, answer, finish, stop) take the run's lock, which that
  goroutine also holds while recording a question or the end of a turn, so an answer and the next
  question are recorded in the order they happened. State changes still go through `service` →
  `store.Update`, so the single-writer rule holds. Starting a run is serialised per task, so two
  requests cannot launch two agents.

### Worktree safety

Removing a worktree deletes a directory, which cannot be undone, so the records that
authorise it are constrained before any engine exists. Rules are enforced as low as they
can be stated: in the database for anything a single row or two tables can decide, in
`domain` for pure rules, and in `service.Worktrees` for what needs the filesystem or other
projects. The engine must go through `service.Worktrees`; it must not write the table.

- **Placement** (`domain.WorktreePlacement`, checked in the same transaction as the write): a
  worktree must be strictly inside one configured directory (`Worktrees.Root`; nothing is
  placeable if it is empty), must not be, contain or lie inside any registered repository's
  working tree or Git directory (for *every* project), must not overlap another worktree that
  is still on disk, and must be canonical: a path through a symlink is refused, because paths
  are compared as text and a link would let a later deletion leave the directory.
- **Shape** (domain and a database trigger): absolute, clean, not the root, no control
  characters; branch and base ref obey `git check-ref-format` (a test keeps the validator at
  least as strict as the installed Git) and may not start with `-`, so they cannot be taken
  for options.
- **Identity is fixed.** Only the removal state ever changes; id, project, path, branch and
  base ref are immutable, and a removed worktree never becomes active again.
- **Compare-and-swap.** `version` guards every change, as for tasks and runs.
- **Removal is two steps**, because "nothing is using this" must not go stale before the
  deletion: `BeginRemoval` (refused while a run uses the worktree; from then on no run can
  start on it; cannot be cancelled), then the engine deletes the directory, then
  `FinishRemoval`. A record stays `active` until then, so its path and branch stay reserved.
  Creation is the mirror image: record first, directory second. A crash therefore leaves a
  record without a directory (retry or reconcile), never a directory nobody has a record of.
- **A record of a worktree that is still on disk cannot be deleted**, directly or by deleting
  its project (which would cascade); doing so would orphan the directory and the run's work.
- **Concurrent runs never share one:** at most one active run per worktree, a run's worktree
  must belong to the run's project, and a run cannot become active on a removed or removing
  worktree. At most one active worktree per branch per project (Git refuses a second
  checkout of a branch; so does the record).

A path that was used by a removed worktree is not reused: its record is kept as history.

**The engine** (`gitrepo.Worktrees`, driven by `runner`) follows that order. A task keeps one
worktree and one branch (`devboard/<title-slug>-<task-id-tail>`) across its runs, so a retry,
follow-up or resumed session continues from the same files; each worktree gets a fresh
directory name, based on the repository's current HEAD commit. A worktree whose directory
vanished is retired and replaced on the same branch. If starting an agent fails, a worktree made
for that attempt is removed again, with its branch. Git hooks are disabled for these operations
(`core.hooksPath=/dev/null`): `git worktree add` would otherwise run the repository's
`post-checkout` hook unasked. A partial clone's missing objects are not fetched (inspection's
`GIT_NO_LAZY_FETCH` also applies here), so such a repository may fail to check out.

## 10. Security assumptions

The controller will eventually start processes with shell access on this computer, so
the HTTP API is treated as a remote-execution surface from day one.

- **Loopback by default.** The default address is `127.0.0.1:7420`, so the controller is not
  reachable from the network unless you bind it elsewhere.
- **A token is required by default, on loopback too** (`requireToken: true`). A loopback port
  is open to every program on the computer, including other users' and anything running
  inside a browser, and the API can start processes as you; being "local" is not an identity.
  The token lives in `<dataDir>/token` (0600) and `werkbord token` prints it (`--url` prints
  a link that signs a browser in). The PWA accepts it once via `/#token=…` or a prompt and
  keeps it in `localStorage`; `EventSource` cannot send headers, so the token is accepted as
  `?access_token=` on `/api/events` only. Query strings are never logged. `/api/health` and the
  static PWA shell are public; all data is not.
  - Because the token is the defence, the `Host` allowlist below is not applied. A DNS-rebinding
    page can send any `Host` it likes, but it runs on another origin and cannot read this
    origin's token (a test covers it).
- **Opting out** (`requireToken: false`, `WERKBORD_REQUIRE_TOKEN=false`, `--require-token=false`)
  is for people who accept that any local program may use the API. It applies **only** to a
  loopback address: binding to any other address always requires the token, whatever the
  setting says. The controller logs a warning at every start while it is off, and a
  `WERKBORD_REQUIRE_TOKEN` that is not `true` or `false` is an error rather than a guess. With
  the token off, these protections apply instead:
  - requests whose `Host` is not `localhost`, `*.localhost`, a loopback IP or a configured
    `allowedHosts` entry are rejected (DNS-rebinding defence);
  - state-changing requests with an `Origin` that does not match `Host` are rejected
    (cross-site request defence; this check applies in every mode).
  Neither stops another program or user on the same computer.
- **No TLS in the controller.** Plain HTTP on loopback is fine. For remote access, put the
  controller behind something that terminates TLS and authenticates the network (Tailscale
  is the expected setup). Exposing it directly to the internet is unsupported.
- **Repository inspection is read-only and never runs repository-supplied commands.** Only
  plumbing commands that do not run hooks or fsmonitor are used, with `GIT_OPTIONAL_LOCKS=0`
  and `GIT_TERMINAL_PROMPT=0`. A repository's own config is untrusted input: a partial clone
  with a missing object makes even `rev-parse` lazily fetch from its promisor remote, which
  runs `core.sshCommand` or `remote.<name>.uploadpack`. Inspection therefore sets
  `GIT_NO_LAZY_FETCH=1` and `GIT_ALLOW_PROTOCOL=none`, overriding anything inherited from the
  controller's environment. Git's own `safe.directory` protection still applies. Variables that select or
  reconfigure a repository (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_CONFIG_COUNT`, …) are removed from
  the environment of every git process, so inspecting a path always describes that path's
  repository even if the controller was started from a Git hook. Credentials embedded in
  remote URLs are redacted before they are stored or shown: for `http(s)`/`ftp` the whole
  userinfo (a token is often the user name), for other schemes the password. The userinfo
  ends at the last `@` of the authority.
- **Agents run as you.** Claude Code and Codex are started with your environment (minus
  variables that would point Git at another repository), in a worktree, with your own
  credentials and configuration, because that is what makes them useful. What they may do
  without asking is their own permission system's (Claude Code's `permissionMode`, default
  `acceptEdits`; Codex's `approvalPolicy`/`sandbox`, default `on-request`/`workspace-write`).
  Whatever they ask goes to the user as a question, so the API token is what authorises running
  code on this computer. **An execution policy never changes any of this** (§17): "autonomous" decides who
  answers conversational questions, not what an agent may do, and a permission request is put to the user
  under every policy. Settings that remove the asking (`bypassPermissions`, `never`,
  `danger-full-access`) are allowed and logged as warnings at every start. Process IDs are never
  sent to clients, answers to agents' questions are stored and shown in the activity feed (so
  agents' requests for secrets are refused), and recovery signals a recorded process only after
  checking its identity.
- **Untrusted text is data.** Task text and agent output are rendered as text by
  Svelte, never as HTML. A strict CSP (`script-src 'self'`, `frame-ancestors 'none'`, …),
  `nosniff`, `no-referrer` and `X-Frame-Options: DENY` are set on every response.
- **Errors do not leak internals.** Unexpected errors are logged in full and returned as a
  generic 500.
- **Assumption: one user, one machine.** Anyone who can run code as your user can read the
  database and token; that is out of scope, as it is for your Git credentials.

## 11. Frontend

- Svelte 5 (runes) + TypeScript + Vite, built to static files and embedded into the Go
  binary with `go:embed`. ~41 KB gzipped JS.
- **Project is the scope** (§16). Two kinds of page, hash-routed: global (`#/control` the Control Center,
  `#/projects` the list of repositories and registering one) and in a project (`#/p/<id>/board`, `git`,
  `activity`, and `#/p/<id>/task/<id>`, which belongs to the board). The list of a project's sections lives in
  one place (`PROJECT_SECTIONS` in `src/lib/location.ts`); the side rail, the tab bar and the switcher are
  built from it, so Calendar (V1) is one more entry, not a navigation rewrite.
- **Phone first.** Under 900 px: sticky top bar with the project's name as its heading (tap to switch), bottom
  tab bar within thumb reach (Control Center, then the project's sections, with safe-area insets), and the board
  shows one column at a time behind a four-way segmented control. From 900 px: a side rail replaces the tab
  bar, with the project switcher at its head, the global pages, and the current project's sections under its
  name, and all four columns sit side by side. Inputs use 16 px text on touch devices to stop iOS zooming.
- **PWA**: web manifest, PNG + SVG icons, and a service worker that caches the shell
  (network-first for navigation, cache-first for content-hashed assets) and never caches
  `/api/*`. State always comes from the controller.
- `src/lib/state.svelte.ts` is the global cache: the connection, the projects, the agents, the Control Center's
  overview and the questions waiting anywhere. What belongs to a project is in that project's `ProjectScope`
  (`src/lib/scope.svelte.ts`), one per project visited. Both are filled over HTTP, kept current by the event
  stream, and refetched on every (re)connect.
- **Cards** show, for a task with a run: the agent, a status — *Running*, *Needs input*, *Blocked*, *Failed*
  (and *Waiting for you* once an agent finished a turn) — elapsed time, a live one-line activity or, when it
  asks, the question, or, when blocked, the blocker; a coloured edge when the user is wanted; and the task's
  interaction mode when it is not the default. **The task page** shows the session as an activity feed, not a
  terminal: messages, runs of tool use folded into one line, stderr tucked into a collapsed "diagnostics"
  item, questions with their outcome (and, for one the policy answered, saying so), and a few lifecycle
  markers. A blocked run shows its blocker, with the agent's options as one-tap replies. The reply box, or the
  pending questions with one-tap answers, is docked at the bottom within thumb reach. Actions: send a message,
  finish, stop (asks twice), edit the task (title, description, interaction), and start or re-run with an agent
  choice, an interaction for that run, extra instructions and optionally "continue the previous conversation".
- **The Control Center is for exceptions** (§19): Needs input, Blocked, Failed, Ready for review and Repository
  risk, in that order, with an explicit *All clear* when there are none; successful background activity and the
  agent list are folded away. The badge counts questions, blocked and failed runs, runs waiting for a message and
  repositories at risk, not finished work or ordinary findings.
- `src/lib/health.ts` holds the logic of the Git screen's *Repository health* card (severity wording, grouping, and
  what each recommended action does: only an action Werkbord `canPerform` becomes a button, and the buttons that
  change the repository open the existing confirmation sheets).
- `src/lib/feed.ts`, `format.ts`, `policy.ts`, `projects.ts` and `location.ts` hold the logic that turns events
  into the feed, run data into labels, and addresses into pages; they are framework-free and unit-tested with
  vitest (`npm test`), as is the project scope.
- Pending questions are kept by `QuestionBook` (`src/lib/questions.ts`), which merges fetched lists
  and stream events so a question seen closed never comes back. Types in `src/lib/types.ts` mirror the Go JSON by hand. *Deferred*: generating them from
  the Go structs (e.g. with `tygo`).

## 12. Intentionally deferred

| Area | Not built yet | Hook already in place |
| --- | --- | --- |
| Agent execution | More agents; an "interrupt this turn" action that keeps the session; token/cost reporting; policies such as "completed run → Review" | `agent.Adapter`; the runner and adapters are built (§8, §14) |
| Worktrees | Garbage-collecting worktrees of finished tasks automatically | `service.Worktrees` records and `gitrepo.Worktrees` engine; a worktree is kept until a person cleans it in the Git Control Center (§18) |
| Git | Committing, stashing or discarding for the user; pulling or fast-forwarding the target; merging a pull request on GitHub; rebasing; conflict resolution; force pushes of any kind; tags; submodules (see `docs/GIT.md` §7) | `gitrepo.Operator` is the place a new guarded operation goes, and `service.GitControl` the place its checks go |
| Board interactions | Drag and drop, reordering within a column, delete | `Position` and `PATCH /api/projects/{pid}/tasks/{id}` already accept title, description, state, position and policy; the task page edits title, description and interaction |
| Projects | Unregistering, renaming | — |
| Calendar | Everything | `PROJECT_SECTIONS` in the web app: one more section of a project |
| Task permissions | What an agent may do to the filesystem, Git or network, per task | `ExecutionPolicy` is a struct stored as JSON on tasks and runs, so fields can be added without a migration (§17) |
| Interrupting an agent | Forcing a turn to stop (Claude `interrupt`, Codex `turn/interrupt`); today a blocked run's agent is *told* to stop and the controller records and shows the block either way | `agent.Session` |
| Notifications | Web Push for "needs you" | Event log + SSE |
| Pairing UX | QR code for phones | Token file, `werkbord token [--url]`, `/#token=` adoption |
| Multi-user / accounts | None in this product, by design. Teams use **Werkbord Team**, a separate product that coordinates people and never executes anything ([TEAM.md](TEAM.md)) | — |
| Windows | Data-dir locking is a no-op on non-Unix | `lock_other.go` |
| Event log retention | Compaction or pruning | `seq`-based resume makes it safe to add |
| Type generation | Go → TS types | Single source in `internal/domain` |

## 13. Relationship to the earlier hosted plan

> **Superseded; kept as history.** The hosted design below (an API server that sends signed jobs to a runner) was never
> built, and it is not the design of Werkbord Team either. Team, the multi-person product, coordinates people and
> executes nothing: no server hands work to anyone's runner, no machine is reachable by another member, and no credential
> is shared (see [TEAM.md](TEAM.md) and [TEAM_SECURITY.md](TEAM_SECURITY.md)). Do not revive this plan for a team.

An earlier plan (`PLAN.md`, since removed) described a **hosted** product (Postgres, GitHub App sign-in, a
cloud API server that sends signed jobs to a runner). This foundation follows the newer,
local-first brief instead, and diverges deliberately:

| Earlier plan | This foundation | Why |
| --- | --- | --- |
| Hosted Go API + Postgres | The controller on your machine + SQLite | The user's computer owns all state; no SaaS |
| GitHub App sign-in, devices, signed jobs | No accounts; loopback + bearer token | Single user, single machine |
| Columns: Backlog, Up next, Working, Review (+ Shipped) | Backlog, Doing, Review, Done | Exactly the four states required by the brief |
| Runner as a separate process talking to a server | Agent adapters inside the controller | Modular monolith; no network hop to start a run |
| Tauri desktop shell | PWA served by the controller | No Electron; a desktop shell is not needed yet |

Ideas from the earlier plan that carry over unchanged: compare-and-swap writes, a worktree per
run, trust rules (agents never move tasks to Done or merge), untrusted text treated as
data, the phone layout (one column at a time, bottom navigation), and Svelte 5 for the UI.


## 14. Running a task

`POST /api/projects/{pid}/tasks/{id}/runs {agentId, instructions?, resume?, policy?}` → `runner.Manager.Start`:

1. **Validate**: the task exists and is not in Done, the agent exists and `Detect` says it is
   usable (not installed / not signed in → 409 with the reason), and the task has no active run.
2. **Prepare the worktree**: re-inspect the repository (it must still be the registered one and
   have a commit), then reuse the task's worktree or make one (§9, "The engine").
3. **Create the run** in `starting`, with a copy of its execution policy (the task's, unless the request gave one). At this point nothing says an agent is running.
4. **Launch** the agent in the worktree with the task as its first message (title, description,
   then any instructions). On failure: the run is marked `failed` with the agent's own message
   (502 `agent_failed`), the card does not move, and a worktree made for this attempt is removed.
5. **Mark running and move the task to Doing**, in one transaction, with `agent.started`.
6. **Stream and persist**: the live-run goroutine turns the agent's events into state and
   `agent.*` events, recording each in the database before the browser can see it.

Setup is carried through even if the caller disconnects; after step 5 nothing depends on the
HTTP request. Resuming (`resume: true`) passes the previous session's `SessionRef`; so does
sending a message to a run that is waiting but has no process (after a restart), which starts a
new process for the *same run* (`MarkStarted` with `resumed`).

Other endpoints (all under `/api/projects/{pid}`, see §16): `GET …/tasks/{id}/runs`, `GET …/runs` (latest run per
task, for the board), `GET …/activity`, `GET …/runs/{id}`, `GET …/runs/{id}/events?before&limit`,
`POST …/runs/{id}/input|finish|stop`, `POST …/questions/{id}/answer`,
`GET /api/worktrees/{id}`.

**Configuration** (`config.json`): `worktreesDir`, and per agent (`claude-code`, `codex`):
`command`, `model`, plus `permissionMode` (Claude Code) or `approvalPolicy` and `sandbox` (Codex).
Unknown agents, settings on the wrong agent and invalid values are errors at start-up.


## 15. Questions: the agent asks, the user answers

An agent may pause and ask for **clarification, a decision, approval, a choice between
alternatives, or more instructions**. Adapters report each as one normalised `agent.Question`
(`Ref`, `Kind`, `Prompt`, `Context`, `Options`, `AllowFreeText`); the controller never sees the
agent's protocol.

```
agent asks ─▶ adapter emits Question ─▶ RecordQuestion: persist + run → waiting_for_user/question
          + agent.question event ─▶ every client (SSE) and GET /api/projects/{pid}/questions show "Needs input"
user answers ─▶ POST /api/projects/{pid}/questions/{id}/answer
   1. AcceptAnswer     the answer is persisted (state answered); nothing else moves yet
   2. Session.Respond  delivered to the SAME session that asked
   3. ConfirmDelivery  deliveredAt set; when nothing else holds the agent up → run running
```

The task stays in **Doing**: there is no "Needs input" column. Only the run waits.

**Persisted fields**: id, run, task, project, kind, prompt, context, options, allowFreeText, state,
answer, answeredBy (`user`, or `policy` when the run's execution policy replied for the user, §17), cancelReason, askedAt, answeredAt, deliveredAt, closedAt. The database rejects a question
whose fields contradict its state (migration 0005).

**Validation**: a question without options always takes free text; an approval always has options.
An answer is checked against the question *before* anything is stored: it must be non-blank, and one
of the options unless free text is allowed (a choice is recorded as spelled).

**Surviving disconnection.** The controller, not a browser, holds the state. A client that opens,
refreshes or reconnects reads `GET /api/control-center` (every pending question, oldest first) and, in a project, `GET /api/projects/{pid}/questions` and resumes the event
stream from its last `seq`; every question event carries the whole question. Nothing depends on a
client having seen the event.

**Races** (all decided by the run's lock, so they are deterministic):

| Situation | Outcome |
| --- | --- |
| Same answer sent twice (lost reply, double tap, two devices) | Both succeed; delivered once; one `question.answered` |
| Different answers at once | One wins; the others get 409 `question_answered` with the question (and winner's answer) |
| Agent withdrew it / no longer has it | `cancelled` (`withdrawn`); 409 `question_closed`; the run resumes if nothing else is open |
| Agent exits, fails or is stopped while waiting | Every open question `cancelled` (`run_ended`) with an event each |
| Answer arrives after the process ended | 409 `question_closed`, saying the agent stopped |
| Process dies between steps 1 and 2 | Answer kept on a `cancelled` question, `deliveredAt` empty |
| Controller restarts | Open questions `cancelled` (`interrupted`); the run is idle and a message resumes the session |
| Delivery fails on a live agent that is now stuck | The run is failed (it would wait forever); the answer is kept |
| Several questions open | Independent; the run resumes when the last is delivered |

**Events** (durable, in the log): `agent.question` (asked), then exactly one of `question.answered`
or `question.cancelled`; payload `{question}`; envelope carries project, task and run. A future
notifier needs nothing else.

**UI**: a banner under the top bar on every page, a count in the tab title and (for an installed
app) the icon badge, Control Center leading with pending questions, the question on its Board card,
and on the task page a docked alert with the prompt, folded context, big choices and a text box only
where free text is allowed. Failures are explained, not shown as 409s.

*Deferred*: push notifications (the events above are their source); multi-select answers.


## 16. Projects are the scope

A user may register many repositories, but when they enter one they see that project's information and
nothing else. There is no global board with filters: each project has its own board, Git view, activity
(run history), tasks, runs, questions and repository state, and the boundary is drawn in the API and the
domain, not left to the frontend.

```
Global                      Control Center (aggregates actionable state across every project)
                            Projects (every repository; registering one)
Project A                   Board · Git · Activity       (Calendar will be a further section)
Project B                   Board · Git · Activity
```

**API.** Everything that belongs to a project is reached *through* it:

```
GET|POST   /api/projects/{pid}/tasks            PATCH /api/projects/{pid}/tasks/{id}
GET|POST   /api/projects/{pid}/tasks/{id}/runs  GET   /api/projects/{pid}/runs        (latest run per task)
GET        /api/projects/{pid}/activity         (run history, newest first)
GET        /api/projects/{pid}/runs/{id}[/events]   POST …/runs/{id}/input|finish|stop
GET        /api/projects/{pid}/questions[/{id}]     POST …/questions/{id}/answer
GET        /api/projects/{pid}/worktrees/{id}       GET  /api/events?project={pid}
```

The routes that took a bare task, run, question or worktree ID (`/api/tasks/{id}`, `/api/runs/{id}`,
`/api/questions`, …) no longer exist. The project in the path is checked against the thing asked for
(`Tasks.GetIn`, `Runs.GetIn`, `Runs.GetQuestionIn`, `Worktrees.GetIn`), and a mismatch is a **404 identical
to the one for an ID that does not exist**, so a request scoped to one project can neither read nor act on
another's, and cannot even learn that it exists. Lists are filtered by the database (`WHERE project_id = ?`,
with indexes), not by the caller, and the event stream can be narrowed to one project before anything is
written. Tests cover each route with another project's IDs, the removed routes, the lists and the stream.

The only reads that cross projects are global on purpose: `GET /api/projects`, `GET /api/agents`, the
unscoped event stream and **`GET /api/control-center`**. The latter is one consistent snapshot of what needs
the user anywhere: per-project counts (needs input, blocked, idle, running, failed, to review, repository
attention and risk), every pending question and every active run, the **exceptions** (runs that failed on a task
still waiting on them, tasks in Review with nothing running, and open repository-health findings that are a risk
or worse), each naming its project and task so a client needs nothing else to show it. It reads stored findings,
so drawing it runs no Git. Answering from the
Control Center goes through the question's own project.

**Frontend.** A project's data lives in its own `ProjectScope`, filled only from the scoped endpoints, and the
scope refuses events and entities that name another project. Switching is therefore looking at another scope:
no page reload, nothing filtered, and a project already visited is there at once (the switcher fetches the
others ahead of time). The switcher (the project's name in the top bar on a phone; the head of the rail, or
Ctrl/⌘ K, on a computer) lists projects with what each asks of the user and filters as you type; it keeps the
section you are in (Board, Git or Activity), leaves a task page for the new project's board, and from a global
page enters the new project at its board. The active project is unmistakable: its name is the page heading, the
top of the page is its colour (derived from its ID), and its section links hang off a bar in that colour. What
needs the user in *other* projects is never hidden: the banner, the tab title and badge, and the Control Center
all count every project, and a badge on the switcher says how much waits elsewhere.


## 17. Execution policy

Each task has an **execution policy**, the default for its runs, and each run keeps a copy of the one it
started with (a run can be started with another, for that run only; editing a task never changes a run that is
working). It is a struct, `{"interaction": …}`, stored as JSON, so that finer-grained permissions can be added
as fields in V1 without a migration. Today it has one field:

| Interaction | What the user asked for | What happens |
| --- | --- | --- |
| `interactive` (default; "Ask me when needed") | The agent may ask. | Unchanged: asks → the run is `waiting_for_user` → the question is persisted → the user answers → the same session continues. No instructions are added. |
| `autonomous` ("Work autonomously") | No routine questions. | The agent is told to investigate the repository itself, decide, and carry on until it considers the task done. If it asks an ordinary question anyway, the controller **answers for the user** with a reply telling it to decide for itself, records the question as answered by the policy, and the run keeps working. |
| `autonomous_stop_if_blocked` | As above, but never guess. | As above, plus the agent is told that when it reaches a decision it cannot safely infer it must stop and end its turn with a one-line `WERKBORD_BLOCKED {…}` report. If it asks a question instead, the controller does not put it to the user: the run becomes **`blocked`** with a persisted, structured `Blocker`, and the agent is told to stop. |

**One place.** `internal/agent/policy.go` is the only code that knows what a policy means: `Instructions(policy)`
(the text), `HandleQuestion` (what the controller does with a question) and `ParseBlocker` (reading a report).
The runner and the adapters call it; nothing else appends policy text to a prompt. Adapters receive the
normalised policy in `StartRequest` and translate it to their agent's channel (§8), on resumed sessions too.

**The controller is authoritative, not the prompt.** Instructions are a request. What the controller *does*
does not depend on the agent complying: a question that a policy does not allow is intercepted in the
runner (`live.onQuestion`) whatever the agent was told, and a blocker is recorded the moment it is asked or
reported. An agent that keeps asking after being told to decide gets at most 8 answers per session, after which
the question goes to the user. *Deferred*: forcibly interrupting a turn; today a blocked run's agent is told
to stop and, if it does not, the run still shows as blocked and the user can stop it.

**Autonomous does not mean unrestricted.** A policy changes who answers *conversational* questions. It grants
and removes no capability: Claude's `permissionMode` and Codex's `approvalPolicy`/`sandbox` stay as configured,
and the instructions say so and forbid destructive Git and filesystem changes the task does not call for.
**A permission request (an approval) is never answered, swallowed or blocked by any policy**: `HandleQuestion`
returns "ask the user" for an approval whatever the policy and however many replies were given, the domain
refuses to record an approval as answered by a policy (`Question.AcceptFromPolicy`), and the tests assert both
at every layer. Under `autonomous_stop_if_blocked` an approval is still an approval, not a blocker: the run
waits for the user, as it always has.

**Blocked.** A blocked run has a session and no question. `Run.Blocker` (`summary`, `detail`, `options`,
`source` = `question` or `report`, the question's `kind`, `raisedAt`) is persisted on the run, announced by
`agent.blocked` and shown on the card, the task page and the Control Center. The user settles it by sending a
message (the same session continues and the blocker is cleared; the log keeps it), or finishes or stops the
run. It survives a controller restart like an idle run (its question, if any, was answered by the policy
before the process went), and a message resumes the session under the same policy. A blocked run is *active*:
it holds its worktree, and the database enforces that.

**Audit.** A question a policy answered is recorded as asked and answered (`answeredBy: "policy"`, answer = the
reply the agent was given) in the same transaction, so the activity feed shows what was asked and how it was
dealt with, and "who decided" is never ambiguous.

## 18. The Git Control Center

A project's Git section lets someone orchestrating many agents see and control the Git state those agents
produce, from a phone, without GitHub or a terminal. It is not a clone of GitHub and does not reimplement
Git: every operation runs the installed `git`, and GitHub is reached only through the user's own `gh`.
**The full description and the safety model are in [GIT.md](GIT.md); this is the shape.**

```
web (Git screens)  ──▶  api/git.go  ──▶  service.GitControl  ──▶  gitrepo.Control (Inspector + Reader + Operator) ──▶ git
                                              │  └──▶ github.Client ──▶ gh (the user's own sign-in; optional)
                                              └──▶ store (tasks, runs, worktree records, the event log)
```

- **Local, remote and GitHub are three sources, never mixed.** The overview is local and never touches the
  network; what it says of a remote comes from remote-tracking refs and is labelled "as of the last fetch";
  pull requests are a separate call that can fail without affecting the rest. An action's result reports its
  *local effect* and the *remote's confirmation* separately, and a push, a pull request or a remote deletion is
  only reported done after the remote itself was asked and agreed.
- **Ownership.** A branch is Werkbord's only if its name is under `devboard/` *and* a worktree record of the
  project names exactly that branch. Only such branches are ever deleted, and only after proving nothing
  unmerged would be lost; only directories Werkbord's records name, inside its own worktree directory, that
  Git lists as worktrees and that hold nothing uncommitted, are ever removed.
- **Actions** (fetch, push, merge, delete, clean a worktree, open a pull request) each run under one lock per
  project and in four steps: look again, compare with the commit IDs the user was looking at, check every
  condition (refusing with the reasons), then do one guarded operation. Merging, deleting and cleaning also
  have a *plan* call that changes nothing, which the phone shows before asking to confirm.
- **No agent merges.** The runner's Git interface has no merge or push (a test pins its method set), and a run
  finishing changes nothing in Git. Only a person's confirmed tap merges.
- **The engine cannot do more than it says**: no force, reset, checkout, clean or rebase; no hooks for writes;
  no repository-configured programs for reads; no `ext::` transport; revisions are only full refs or commit
  IDs; deletion is conditional on the expected commit; every output and run time is bounded.
- **Audit**: `git.fetched`, `git.pushed`, `git.merged`, `git.branch_deleted`, `git.worktree_cleaned` and
  `git.pull_request_created` are events like any other, written only for what happened; the web app refetches
  Git when they, or a task, run or worktree event, arrive. `git.health_changed` (§19) is the one Git event that is
  not an action: it announces that a project's open health findings changed.
- **Phone first.** The screens drill down: overview (repository summary, branches that need you with Werkbord's
  on top, pull requests, recent commits, working changes, worktrees, all branches) → a branch (state, actions,
  commits, changed files) → a file's diff, a window at a time. The address says which screen
  (`#/p/<id>/git/branch/local/<name>/file?path=…`), so Back, reload and shared links work. Review, Merge, Push
  and Delete are one tap from the list: they open a bottom sheet that shows the controller's own check.

## 19. Repository health

*"I have had several coding agents working all day. What Git state now needs my attention?"* Repository health
answers it with **findings**, not a score. **The full description, including every rule and whether its signal is
deterministic or heuristic, is [HEALTH.md](HEALTH.md); this is the shape.**

```
events (run/task/worktree/git.*) ─▶ GitHealth.Watch (debounce 3 s) ─┐
web "Check now" / old report ─▶ api/githealth.go ─▶ GitHealth.Refresh ◀┘
                                         │ gather: GitControl.overview (Git metadata) + records (tasks, runs, worktrees)
                                         │         + in-memory merges (merge-tree) + index.lock stat
                                         ▼
                              domain.EvaluateHealth  (pure rules)  ─▶ []HealthFinding
                                         ▼
                              domain.ReconcileHealth (pure) ─▶ store: health_findings, health_checks ─▶ git.health_changed (only on change)
```

- **Pure rules over gathered facts.** `domain.EvaluateHealth(HealthInput)` runs no Git and reads no file, so each rule is
  tested with a hand-built input. `service.GitHealth` gathers the facts: the Git overview (which already joins
  branches with tasks, runs and worktree records), the records, an in-memory merge per unmerged Werkbord branch (to
  recognise a squash merge by content), file lists and in-memory merges for overlapping branches, and the age of
  `index.lock`.
- **Deterministic vs heuristic is a field**, not a vibe: each finding carries its `basis`, the screen shows it, and
  a test fails a heuristic finding that says "will conflict". Only Git's own in-memory merge may say two branches
  conflict.
- **Findings have a life.** Identity is derived from project + rule + subject, so recalculating the same problem
  is the same finding: it keeps `detectedAt`, resolves when it stops being true, and can be dismissed until it gets
  worse. If the repository cannot be read, that is one finding and the others are left alone rather than reported
  as fixed.
- **Cheap, no model, no network, no writes to the repository.** A recalculation is tens of milliseconds. It runs
  when an event that can change the answer arrives (after a quiet period), when asked, when a screen asks for a
  report older than two minutes, and once at start: never on a timer. It publishes `git.health_changed` only when
  the set of open findings changed. Tests check the repository is byte-for-byte unchanged and that the network
  and any model are never used.
- **Never acts.** Each finding recommends an action and says whether Werkbord `canPerform` it. A button opens the
  existing confirmation sheet; destructive steps are flagged; steps Werkbord refuses to do (pull, rebase,
  resolving conflicts) say so and say what to do. "Create task" and "Ask an agent to investigate" only add a
  prefilled card to the board.
- **Quiet by design.** An agent with a live session is never flagged; finished, pushed work waiting for review is
  normal; housekeeping (`info`) is folded away and never counted; your own branches are yours. HEALTH.md §7 lists
  what is deliberately not reported, and a test pins each.
- **In the Control Center**, only a *risk* or *critical* finding is listed (*Repository risk*), alongside Needs input,
  Blocked, Failed and Ready for review; attention-level findings are counted on the project's strip and live on
  its Git screen.
- **Lifecycle of the process**: the controller starts the watcher after wiring the services and, on shutdown,
  cancels it and waits for any recalculation to finish before the broker and database close.

## 20. Install, private network and execution defaults

**One command to a working system.** `scripts/install.sh` downloads a release, verifies its checksum and runs
`werkbord setup`, which prepares the data directory, token and database, installs the controller as a *user*
service (`internal/daemon`: launchd, a systemd user unit, a scheduled task, or a detached process where none
exists), starts it, and shows that this computer is registered as the first runner. `start`, `stop`, `restart`,
`status` and `open` manage the one controller the service owns: before starting anything they ask the controller
whether it is already answering, so a controller started by hand is never duplicated. The service is given the
`PATH` setup ran with, because agents are found through it. The database is copied before any migration that has
something to change (`<data dir>/backups`, newest five kept), and `werkbord update` verifies a download, runs it
once to confirm its version, replaces the executable atomically, restarts the controller, and restores the old
executable if the new one does not come up.

**Runner.** `runners` records computers that can run agents. The controller registers *this* computer, as the one
`local` runner, every time it starts (`Settings.RegisterRunner`), with nothing to configure; a unique index keeps it
one. Runs do not yet name a runner: multi-runner execution is deferred.

**Private network** (`internal/netprivate`). The controller embeds a Tailscale node (`tsnet`, userspace: no root, no
TUN, no system Tailscale) when the user has turned phone access on (`settings.network`, or `config.network.enabled`
which overrides it). A `Manager` brings the node up in the background and keeps a `Status` (`off`, `starting`,
`needs_login` with the sign-in link, `needs_approval`, `connected` with the address, `error`); once connected it
serves the app on the tailnet only (443 with Tailscale-issued certificates when the tailnet has HTTPS, else 80).
The tailnet listeners use a *second* `api.Server` built with `AuthRequired: true`, so the access token is demanded
there even when loopback was opened with `requireToken=false`. Funnel (public ingress) is never used, Tailscale's
log upload is switched off, and the sign-in link is never logged. The node sits behind a small `Backend`
interface, so the manager is tested with a fake and, separately, against a real node talking to Tailscale's own
in-process test control server and DERP relay (sign-in link, completing it, serving, a second node reaching it,
identity across a restart). `GET /api/network/phone` returns the address and a link and QR code (SVG) that carry
the token in the URL fragment.

**GitHub** (`service.GitHubSetup`, `internal/github`) is optional and goes through the user's own `gh`: status
(`gh api user`), sign-in (`gh auth login --web`, whose one-time code the app shows), the repositories the user can
reach, and which of them already exist on this computer. The local search reads `.git/config` of repositories under
the usual code directories (no git, no network); *on this computer* (access to a clone) and *GitHub only* (metadata)
are separate fields. Adding a GitHub-only repository clones it with git using `gh auth git-credential` as a
credential helper for that command only, and sets the same helper on that clone only. Werkbord stores nothing
about itself in GitHub and never holds a GitHub credential.

**Execution defaults** (`domain.ExecutionConfig`, `ResolveExecution`). Agent, model, reasoning, interaction and
priority are set at three levels (global, project, task) and optionally for one run; the first level that sets a
field wins. Model and reasoning belong to an agent: a level that sets either must set the agent, and a level's
model only applies when its agent is the one that won. `agent.Optioner` lets an adapter report models and reasoning
levels (Codex asks its own app-server, Claude Code its `--help` for efforts, plus aliases), with "Agent default"
always first and unlisted names allowed. The runner resolves at start, records the resolved agent, model, reasoning
and policy on the run, and passes them to the adapter. See [EXECUTION.md](EXECUTION.md).

**Doctor** (`internal/doctor`). Checks run inside the controller (`GET /api/doctor`) so they see its PATH, agents
and network; the CLI adds the service, version and PATH-parity checks, and runs what it can when the controller is
down. Checks report states, never values: the token, the sign-in link and GitHub's code cannot reach a report.

## 21. V1 orchestration

Task.Orchestration is the shared source of truth for Board and Calendar, stored
as JSON in the existing SQLite tasks table. The controller starts ScheduleLoop
only after recovering runs, and cancels/joins it before shutting down the runner.
Scheduler.Plan applies deterministic ordering and conservative Git gates. The
runner shares GitControl's repository-action lock; Runs.Create atomically checks
and consumes a schedule key with its new run. A unique index prevents duplicate
dispatch. Dependencies are checked against a project-scoped graph, with cycle
validation inside task edits. Project concurrency is persisted in settings.
Runs snapshot their objective and store a structured handoff, parent run, purpose,
and monotonically increasing attempt. Continuation reuses the worktree with a
fresh agent conversation and bounded selected evidence. See [ORCHESTRATION.md](ORCHESTRATION.md)
for API contracts, limits and the exact recovery/completion semantics.

*Deferred:* multi-runner execution, recurring schedules, economics, and any Brain.

## Multi-runner control plane

`internal/service.Runners` pairs identities, applies owner-defined project permissions, chooses
machines deterministically and claims capacity in the same serialized SQLite transaction that
creates the Run and scheduled claim. Each Run records its owning runner, branch, base/head commits
and normalized usage. Capacity counts starting, running, blocked and idle sessions; lost heartbeats
change availability without releasing ownership. Existing local runs without a runner ID count toward
the controller machine's capacity.

`internal/runnerwire` defines only signed pairing and heartbeat/report messages. `internal/remote`
is the separate runner process, backed by private atomic JSON identity and run journals rather than
a replica of controller SQLite. It fetches authorized repository origins, creates unique worktrees,
starts local adapters and journals observations before retransmission. Ordered observations and
commands survive network interruptions. A new job cannot relaunch a journaled run after restart.

`werkbord join` embeds a separate private-network node and installs a separately named runner login
service. Agent and Git authentication stay on the machine that executes. Runner keys have no access
to board CRUD, project administration or any owner endpoint. These machines belong to one user;
this is not a team or multi-user service. See [RUNNERS.md](RUNNERS.md) for the operational and security contract.
