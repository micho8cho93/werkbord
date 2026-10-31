# Agent sessions: one execution, one identity

A coding agent can be reached from Salie, from Agents, from a task in Work and from the Calendar. Whichever way a person comes
to it, it is the same running thing. This page says how that is kept true without turning three different concepts into one.

## The rule

**One real agent execution has one canonical session identity.** Starting an agent through Salie must not create a
disconnected or duplicated session; the session must be reachable from Agents with its own history, conversation and state.

## Three concepts, deliberately kept apart

| Concept | What it is | Owner | Identity |
| --- | --- | --- | --- |
| **Run** | A structured, task-bound execution: a feed, questions and approvals, a worktree, a runner. | the runner (`internal/runner`, `domain.Run`) | `run_…`, with the provider's own handle in `Run.SessionRef` |
| **Terminal** | The agent's own interactive command line in a repository, on a PTY. May or may not be for a task. | the terminal service (`internal/terminal`) | a random 192-bit ID |
| **Salie conversation** | Salie talking to the person (`domain.AssistantSession`). It is **not** an agent execution. | the assistant engine | `ast_…`, with the provider's handle in `ProviderRef` |

They are not merged into one table. A run has a state machine and a worktree; a terminal has a process group and no
protocol; a conversation has proposals and an audit trail. Forcing them together would lose what each is for.

## What is shared

`internal/domain/agentsession.go` adds no table. It adds:

- **`SessionRef`**, written `run:<id>` or `terminal:<id>`: the kind of record that owns an execution and that record's own ID.
  The key is derived from the owner's ID, never minted separately, so two screens cannot disagree about which execution
  they mean. `ParseSessionRef` refuses anything that is not exactly a kind, a colon and an ID, so a key from an address
  or a message is safe to pass on.
- **`AgentSession`**, the read model every screen lists: key, kind, project, task (and, through the task, plans), agent,
  state, whether it is live or waiting, where it runs, which Salie conversation started it, and **capabilities**
  (`feed`, `message`, `terminal`, `questions`, `stop`). Capabilities are how provider-specific behaviour survives: a run offers
  its feed, instructions and questions; a terminal offers the agent's own UI and nothing Werkbord invented.
- **`GET /api/agent-sessions`** (`?project=`, `?task=`, `?live=1`), assembled on every read from the runs and the terminal
  service. Nothing is stored, so nothing can drift or appear twice; a key appears at most once however the list is built.

Two small facts are stored where they belong, and they are provenance and association only, never authority:

- `Run.Origin` (`salie:<conversation id>`, migration `0034_run_origin.sql`) records which conversation started a run.
- `Terminal.TaskID` associates a terminal with a task of its project. `PUT /api/projects/{pid}/terminals/{id}/task`
  changes it (the local owner's alone), and `taskId` is accepted when a terminal is created. The task must belong to the
  project and is checked before any process starts. A terminal with no task is a **standalone** session, which stays a
  first-class thing.

Plans reference tasks, never sessions, so a session's plans are the plans of its task.

## Salie launches; Agents holds

Salie has one new operation, **`start_agent_run`**. It is a confirmed proposal like every change Salie makes: nothing starts
until the person presses *Confirm and start* on the exact task and agent shown. It is the owner's assistant's alone (not
offered to MCP clients, not held by a read-only assistant), and it goes through the runner's own start, so one active run per
task, the execution policy and the worktree rules apply unchanged. A task that already has a live session is refused with
that session's key instead of being given a second one, and a repeated confirmation cannot start two (the proposal is claimed
once, and the runner refuses a second active run). `docs/ASSISTANT.md` lists it, and
`TestTheAssistantOffersNoRouteToRunsGitOrSettings` is where this decision is reviewed.

The conversation keeps only the reference: the confirmed card shows **Open in Agents**, and the run records
`Origin`. Opening it does not copy anything; the session shows its own persisted feed.

**Agents** (`#/sessions[/<key>]` in the person's own Werkbord, shown in the window's *Agents* destination) is where a session
is read and continued, and where direct CLI terminals are opened and managed (*New terminal*: agent, repository, optionally a
task). It lists runs and terminals together, Needs you first, with runner status beside them. Opening a session attaches to
the same process: a run through its existing feed and run controls, a terminal through the one terminal renderer
(`TerminalSession.svelte`, xterm over the PTY, unchanged). There is no second terminal implementation.

**Back** on a session goes to the page it was opened from (a trail in its address; see [`NAVIGATION.md`](NAVIGATION.md)):
Salie, a task, a plan, Work, the Calendar or the list. **Back to Salie** (the trail for a session Salie started, *Ask Salie* otherwise) opens Salie, on that conversation when
this window has it. Salie's frame is kept alive while it is out of sight and a session does not depend on any screen, so going
back and forth interrupts nothing.

**Work** opens a task's session by the same key: *Open Session* on a task card, a task page or a plan goes to Agents instead of
a second view in Salie. So does the **Calendar** (and Scheduling): a task's sheet offers *Open session* for its latest run.

## What Salie is not

Salie is not a replacement for any provider's CLI. It does not render a terminal on a computer any more (Agents does), it does
not convert a provider protocol into something else, and it does not mirror a session's content into the conversation. The
phone, which has the Control Deck and Salie but no Agents, keeps its session picker inside Salie for the runs and the granted
terminals it may use, exactly as before.

## Restarts and reconnecting

The list is read from the owners' records, so there is nothing to reconcile in the browser. After the controller or the app
restarts, runs are recovered by the runner as always, terminals are reloaded from `terminals.json` (a terminal that was
running is marked **interrupted** and its old process reaped; nothing is relaunched), and the same keys appear. An address
that names a session (`#/sessions/run%3A…`) is remembered by the window and opens it again. A key that no longer exists
says so and offers the list; it never creates anything.

## Limits

- A terminal's association with a task is kept by the controller that owns the terminal. On another execution device, a
  task can be named when the terminal is created (it travels with the device operation) but an existing remote terminal
  cannot be re-linked from here.
- `/api/agent-sessions` is for the local owner (and enrolled desktop clients). Phones and local programs are refused; a
  connection that may not list terminals still gets its runs, and `terminals: false` says terminals were not listed.
- Team never executes. Team sessions run on their members' computers and are not listed here; what Teams report remains in
  the activity strip above the sessions (the person's own reports are the sessions listed below it, so they are not repeated).

## Validation

- Go: `internal/domain` (keys, origins, capabilities), `internal/appops` (`start_agent_run`: assistant-only, confirmation,
  origin, no duplicates, refusals), `internal/api` (`TestEachExecutionHasOneIdentityAcrossRunsAndTerminals` and friends,
  authorisation), `internal/terminal` (association survives a restart without relaunching), `internal/archtest`.
- `node scripts/browser-agent-sessions.cjs` (real controller, runner, scripted Salie, PTYs, Chromium): launch through Salie;
  open the same session in Agents; continue it; return to Salie; open it from a Work task; terminals standalone and for a
  task; restart the controller and reconnect, with no duplicates.
- `node scripts/browser-shell-agents.cjs` (the same, through the desktop window's shell and frames, and Back along every route:
  [`NAVIGATION.md`](NAVIGATION.md)).
- `node scripts/browser-salie-terminals.cjs` (terminals in Agents, and the phone's sessions in Salie).
