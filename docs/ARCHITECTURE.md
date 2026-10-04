# Devboard architecture

Devboard is a **local-first control plane for coding agents**. One program, the
controller, runs on your computer. It owns the repositories, the credentials, the
database, the agent sessions and all development state. Phones, tablets and browsers
are remote controls for it. There is no hosted backend, no account system and no cloud
database.

This document describes the foundation as built. Sections marked *Deferred* name
things that are intentionally not implemented yet.

---

## 1. Shape of the system

```
 Phone / tablet / desktop browser (installable PWA, Svelte + TypeScript)
        │  HTTP JSON  +  Server-Sent Events (/api/events)
        ▼
┌──────────────────────── devboard controller (one Go process) ───────────────────────┐
│                                                                                       │
│  api ──────────▶ service ──────────▶ store (interfaces) ──▶ store/sqlite ──▶ devboard.db
│  (HTTP, SSE,      (use cases,          ▲                                              │
│   auth, PWA)       domain rules)        │ same transaction                            │
│      ▲               │   │              │                                             │
│      │               │   └──▶ events.Publisher ──▶ events.Broker ──┐                  │
│      └───────────────┼──────────────────────────────────────────────┘ (live signal)   │
│                      ├──▶ gitrepo.Inspector ──▶ git CLI ──▶ your existing checkouts   │
│                      └──▶ agent.Adapter (interface only, no adapters yet)             │
│                                                                                       │
│  domain: Project · Task · Run · Agent · Question · Worktree · GitRepository · Event  │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

It is a **modular monolith**: one binary (`bin/devboard`), one process, one SQLite file,
with package boundaries doing the job that service boundaries would do in a distributed
system. No Docker, no Electron, no second language on the backend.

## 2. Component boundaries

| Package | Owns | May depend on | Must not |
| --- | --- | --- | --- |
| `internal/domain` | Entity types, state enums, validation, the run state machine, sentinel errors | stdlib only | Know about SQL, HTTP, Git or agents |
| `internal/store` | Persistence **interfaces** (`Store`, `Tx`, one repo per aggregate) | `domain` | Contain an implementation |
| `internal/store/sqlite` | The SQLite implementation and versioned migrations | `domain`, `store` | Contain business rules beyond integrity constraints |
| `internal/events` | Live fan-out of committed events (`Publisher`, `Subscriber`, `Broker`) | `domain` | Be relied on for durability |
| `internal/gitrepo` | The Git boundary (`Inspector`, CLI implementation) | `domain` | Write to repositories (today) |
| `internal/agent` | The agent boundary (`Adapter`, `Session`, `Registry`) | `domain` | Leak protocol details of a specific agent |
| `internal/service` | Use cases: register project, create/move task, recover runs | all of the above via interfaces | Speak HTTP |
| `internal/api` | HTTP routing, JSON, SSE, auth and security middleware | `service`, `store`, `events`, `agent` | Contain business rules |
| `internal/webui` | Serving the embedded PWA build | stdlib | — |
| `internal/controller` | Wiring and lifecycle | everything | — |
| `internal/config`, `internal/logging` | Settings and the slog logger | stdlib | — |
| `cmd/devboard` | CLI entry point (`serve`, `migrate`, `project add/list`, `version`) | `controller`, `config` | Write the database while a controller runs |
| `web/` | The PWA | the HTTP API only | — |

### The four replaceable interfaces

- **Persistence** — `store.Store` with `View` (read-only snapshot) and `Update`
  (serialised read-write transaction). Services never see `database/sql`.
- **Event delivery** — `events.Publisher` / `events.Subscriber`. The in-process broker
  could be swapped (e.g. for a fan-out to a desktop shell) without touching services.
- **Git operations** — `gitrepo.Inspector`. Implemented by shelling out to `git`, because
  that respects the user's own Git config, credentials helpers and `safe.directory`
  rules. Services are tested with a fake.
- **Agent execution** — `agent.Adapter` → `agent.Session`. See §8.

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
  `backlog`, `doing`, `review`, `done`. Ordered within a column by `Position`.
- **Run**: one execution attempt by an agent against a task. Its `State` is one of
  `starting`, `running`, `waiting_for_user`, `completed`, `failed`, `stopped`.
- **Question**: something an agent asked during a run; `pending` until answered or
  cancelled.
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

Run transitions are enforced by `domain.Run.Transition`:

```
starting ──▶ running ──▶ completed
   │            │  ▲
   │            ▼  │
   │     waiting_for_user
   │            │
   └────────────┴──────▶ failed | stopped     (terminal states have no exits;
                                                a retry is a new Run)
```

## 4. Data flow

**A write** (e.g. moving a task from a phone):

1. PWA sends `PATCH /api/tasks/{id}` with `{state, version}`.
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

`devboard serve` → `controller.Run`:

1. **Load config**: defaults → `<dataDir>/config.json` → `DEVBOARD_*` env → flags.
2. **Lock the data directory** with an exclusive `flock` on `controller.lock`. A second
   controller on the same data dir fails fast. The OS releases the lock if the process dies.
3. **Open SQLite and migrate** (see §6). A database from a newer build is refused.
4. **Recover** state left by the previous process (`service.Runs.RecoverAfterRestart`):
   runs that were `starting`/`running` have lost their process and are marked `failed`
   with reason `interrupted: controller restarted`; `waiting_for_user` runs are kept,
   because the question is still answerable and a future adapter can resume the session
   from the stored `SessionRef`. Each change emits a `run.state_changed` event.
5. **Resolve auth**: a token is required unless `requireToken` is off and the address is
   loopback (§10); it is read from config or `<dataDir>/token`, generating one (mode 0600) on
   first use. If auth is off the controller logs a warning.
6. **Wire** broker, services, API and the embedded PWA; **listen**; serve.
7. **On SIGINT/SIGTERM**: `http.Server.Shutdown` stops accepting connections and waits
   for in-flight requests (up to `shutdownTimeout`, default 10s). A shutdown hook closes
   the event broker first, which ends every SSE stream, so long-lived connections do not
   hold shutdown hostage. Then the database is closed and the lock released.

If any startup step fails, everything already started is torn down in reverse order.

## 6. Database ownership

- **The controller is the only writer.** CLI commands such as `devboard project add`
  are HTTP clients of the running controller, so every change goes through services,
  validation and the event log. `devboard migrate` is the one offline command, and it
  is guarded by the same migration code.
- Location: `<dataDir>/devboard.db` (default `~/Library/Application Support/devboard` on
  macOS, `~/.config/devboard` on Linux). Directory 0700, file 0600.
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
  are JSON columns because they are always read and written whole.
- What survives a restart: projects, repository snapshots, tasks (with versions and
  order), runs, questions, worktrees and the full event log.

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
- Event types today: `project.registered`, `project.inspected`, `task.created`,
  `task.updated`, `run.state_changed` (recovery), and reserved `question.created` /
  `question.answered`.
- *Deferred*: event-log compaction or retention. The log grows without bound; at the
  expected volume (a person's tasks and runs) this is fine for a long time.

## 8. Agent adapter boundary

Claude Code, Codex and future agents expose different protocols. Each will get an
`agent.Adapter` that translates its protocol into a small, shared vocabulary:

- `Detect` — is the agent installed and signed in? (No session started.)
- `Start(StartRequest{RunID, WorkDir, Prompt, ResumeRef})` → `Session`
- `Session.Updates()` — `output` (activity), `question` (blocks on the user),
  `session_ref` (a resumable handle became known)
- `Session.Answer`, `Session.Stop`, `Session.Wait() → Result{State, Reason}`

The interface is deliberately limited to what both Claude Code and Codex support today:
start in a directory with a prompt, stream output, ask a question, resume a session, stop.
It does not model tools, models, token accounting or permissions; those get added when a
real adapter needs them, not speculatively. The registry currently holds **no adapters**,
so `GET /api/agents` returns an empty list.

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
- **Future runs**: each run will be a goroutine owning one `agent.Session` and one
  worktree; state changes still go through `service` → `store.Update`, so the single-writer
  rule holds. Agent processes are children of the controller, which is why restart
  recovery (§5) marks in-flight runs failed.

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

## 10. Security assumptions

The controller will eventually start processes with shell access on this computer, so
the HTTP API is treated as a remote-execution surface from day one.

- **Loopback by default.** The default address is `127.0.0.1:7420`, so the controller is not
  reachable from the network unless you bind it elsewhere.
- **A token is required by default, on loopback too** (`requireToken: true`). A loopback port
  is open to every program on the computer, including other users' and anything running
  inside a browser, and the API can start processes as you; being "local" is not an identity.
  The token lives in `<dataDir>/token` (0600) and `devboard token` prints it (`--url` prints
  a link that signs a browser in). The PWA accepts it once via `/#token=…` or a prompt and
  keeps it in `localStorage`; `EventSource` cannot send headers, so the token is accepted as
  `?access_token=` on `/api/events` only. Query strings are never logged. `/api/health` and the
  static PWA shell are public; all data is not.
  - Because the token is the defence, the `Host` allowlist below is not applied. A DNS-rebinding
    page can send any `Host` it likes, but it runs on another origin and cannot read this
    origin's token (a test covers it).
- **Opting out** (`requireToken: false`, `DEVBOARD_REQUIRE_TOKEN=false`, `--require-token=false`)
  is for people who accept that any local program may use the API. It applies **only** to a
  loopback address: binding to any other address always requires the token, whatever the
  setting says. The controller logs a warning at every start while it is off, and a
  `DEVBOARD_REQUIRE_TOKEN` that is not `true` or `false` is an error rather than a guess. With
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
- **Untrusted text is data.** Task text and (later) agent output are rendered as text by
  Svelte, never as HTML. A strict CSP (`script-src 'self'`, `frame-ancestors 'none'`, …),
  `nosniff`, `no-referrer` and `X-Frame-Options: DENY` are set on every response.
- **Errors do not leak internals.** Unexpected errors are logged in full and returned as a
  generic 500.
- **Assumption: one user, one machine.** Anyone who can run code as your user can read the
  database and token; that is out of scope, as it is for your Git credentials.

## 11. Frontend

- Svelte 5 (runes) + TypeScript + Vite, built to static files and embedded into the Go
  binary with `go:embed`. ~21 KB gzipped JS.
- Three primary surfaces, hash-routed: **Board**, **Control Center**, **Git**.
- **Phone first.** Under 900 px: sticky top bar, bottom tab bar within thumb reach (with
  safe-area insets), and the board shows one column at a time behind a four-way segmented
  control. From 900 px: a side rail replaces the tab bar and all four columns sit side by
  side. Inputs use 16 px text on touch devices to stop iOS zooming.
- **PWA**: web manifest, PNG + SVG icons, and a service worker that caches the shell
  (network-first for navigation, cache-first for content-hashed assets) and never caches
  `/api/*`. State always comes from the controller.
- `src/lib/state.svelte.ts` is a cache of controller state: filled over HTTP, kept current
  by the event stream, refetched on every (re)connect.
- Types in `src/lib/types.ts` mirror the Go JSON by hand. *Deferred*: generating them from
  the Go structs (e.g. with `tygo`).

## 12. Intentionally deferred

| Area | Not built yet | Hook already in place |
| --- | --- | --- |
| Agent execution | Claude Code / Codex adapters, starting/stopping runs, streaming activity, answering questions | `agent.Adapter`, `Session`, `Registry`; `runs`/`questions` tables; run state machine; restart recovery |
| Worktrees | Running `git worktree add/remove`, creating and deleting directories, garbage-collecting | `service.Worktrees` (validated, crash-ordered records), database rules (see "Worktree safety"); `Run.WorktreeID`. Not yet wired into the controller or exposed over HTTP |
| Advanced Git | Branches, diffs, commits, push, PRs, GitHub integration | `gitrepo` package boundary |
| Board interactions | Drag and drop, reordering within a column, task detail/editing UI, delete | `Position` and `PATCH /api/tasks/{id}` already accept title, description, state and position |
| Projects | Unregistering, renaming | — |
| Notifications | Web Push for "needs you" | Event log + SSE |
| Pairing UX | QR code for phones | Token file, `devboard token [--url]`, `/#token=` adoption |
| Multi-user / accounts | None, by design | — |
| Windows | Data-dir locking is a no-op on non-Unix | `lock_other.go` |
| Event log retention | Compaction or pruning | `seq`-based resume makes it safe to add |
| Type generation | Go → TS types | Single source in `internal/domain` |

## 13. Relationship to the earlier hosted plan

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
