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

## The Agents screens

- **Overview** (`#/sessions`, `routes/AgentSessions.svelte`, grouping in `lib/agent-overview.ts`): in the order of how much each
  needs the person: sessions **waiting for you**, then **needs a look** (failed or interrupted), then **running**, then what
  finished recently, with the runners and the filters (kind, provider, project) and *New session*. Only real sessions are listed;
  a task that is merely scheduled has none until it starts, and the Work queue is not repeated. A card (`lib/agents/SessionCard.svelte`)
  carries what the person can do without opening it, when the controller says the session offers it: answer a question (an approval
  with its context in view), reply to an agent that is waiting, **Stop** (asked twice, the question lapses by itself) and **Retry**
  for a task run that failed, was interrupted or was stopped (`canRetry`). Running cards keep Stop out of the way until they are
  pointed at or focused; a question is never hidden.
  Each card starts with the agent's **ringed mark** (the shared identity layer, [`MOTION.md`](MOTION.md)): the ring turns only for a
  live session the controller confirms (and, for a remote run, whose runner is online); with the controller out of reach it reads as
  the last report, and a session cut off by a restart is never shown as stopped (`sessionAgent`). Inside each group, **sessions on a
  plan sit under a sub-header** ("Authentication overhaul · 2 of 5 tasks complete", linking to the plan); a plan's block sits where its
  first session sits, so the urgency order never changes, and standalone sessions stay where they are with no header (`planBlocks`).
  Plans are read once per project that has a planned session, not on the list's four-second refresh.
- **A session** (`#/sessions/<key>`) opens **beside the list**, which keeps its place, filter and scroll; choosing another session
  changes only the address, inside the page, so nothing is rebuilt. On a window too narrow for both the session takes the page and
  closing it brings the list back as it was. Inside Work the page is shown with `?pane=1`: the one session alone, with no list.
  The session has Back and its trail, the title once, provider, repository and state as one line, and, only when it belongs to a
  task, a small panel for the task, its state and its plans. A standalone session has no panel. For each plan the panel shows how many
  of its tasks are complete and, in a window at least 1200px wide, **Next after this** (the first later task that is not done) and
  **Execution details**: the runner, the branch, the session key and, when the run reported it, usage. Narrower windows leave those two out.
- **Terminal first.** A terminal session is the agent's own command line (`TerminalSession.svelte`, xterm over the PTY, unchanged)
  and is what *New session* and a task's **Work on this in a terminal** (`lib/agents/TaskTerminal.svelte`) open; the terminal is
  linked to the task and nothing is typed for the person. A structured **run** (started by the scheduler, a task's *Start agent* or
  Salie) has no PTY, so it keeps its feed, questions and approvals, but its view is drawn like a terminal (`variant="terminal"`):
  dark and monospaced, questions pinned above, a prompt to type at (Enter sends) and the same Finish and Stop. A terminal has no
  protocol, so it can be shown as running or ended but never as "waiting for you". Salie's `start_agent_run` still starts a run.
- **What is offered follows what the controller can do** (`controlsFor`): instructions, answers, Stop and terminal input appear
  only for a live session whose capabilities say so and while the controller is reachable. Otherwise the session is shown with a
  line that says why, and a terminal's input is disabled.
- **Agents & connections** (`shell/ConnectedTools.svelte`, a header popover in a Personal project context only): the coding agents the app
  found (*Signed in* or *Installed*, only as detected, with the person's live sessions and *Limit unknown* unless usage reports a reached
  limit), the tools that are connected (*Verified* only when tested) and the last thirty days of usage as two separate figures, reported
  API charges and the estimated API equivalent, with the line that balances and reset times are not available. It comes from one read-only
  bridge call, `AgentsAndConnections`, which the app reduces before anything reaches the window (no command, environment, secret, path or
  full address; a connection's address without its query). Nothing here enters a Team report.
- **Reported activity** from a Team workspace (the window's strip and page) is labelled view-only, with when it was reported, or
  *Last reported* / *Unavailable · last known*. Each shows the reporting agent's mark with the ring the server's freshness allows. It opens the workspace's own page and has no controls here.
- Switching sessions, or going to a task, a plan, Work or Salie and back, changes only the address. It starts, stops and attaches
  nothing; a session has one key and appears once.

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

## The activity stream

`lib/feed.ts` groups only what the controller can prove. Every adapter (Claude Code, Codex and the ACP agents) reports a tool call as a
line of free text on the `tool` stream, with no tool kind, so the stream is **not** grouped into reads, edits, tests or Git: that would be
guessing from words. Runs of tool lines are one collapsed item, standard error is its own collapsed item, and messages, questions,
approvals and markers stay in order and are never collapsed. Group by kind only once an adapter reports one.

## Continuing with another agent

A run that failed or was stopped on a provider error, is blocked, or was interrupted offers **Continue with another agent…** on its
task. The dialog is an assessment: it shows the agent-reported handoff (completed work, what is still to verify), the branch and a
proposed provider, and dispatches nothing until **Approve**. Approve (which states that the next agent bills to its own account) is
available only when the previous run is confirmed stopped (a finished run on a reachable runner), a handoff has been captured and
another agent is usable. `recovery: intervention`, a blocked session that is still open and an unreachable runner all count as not
stopped. It uses the existing continuation, which starts a new run from the handoff and never replays commands. Automatic fallback
is a separate, opt-in setting (below).

### Automatic fallback (opt-in)

Project settings → **Fallback** lets a project hand a failed run to another agent without asking. It is off by default and bounded:

- **Which runs:** only a run that has already *ended* as failed, on this computer, whose retained provider blocker is a rate limit or
  exhausted allowance, within the last 24 hours. A session that is still open (blocked), an unknown stop (`recovery: intervention`),
  a remote run, authentication, a full context window and unknown errors all stay with you.
- **Which agent:** the first of the project's chosen agents, in its order, that can start now and has not already worked on the task.
- **Verified handoff:** the controller captures the handoff itself and reads the worktree's Git state. It must be a clean reading
  (no conflicts, no Git operation in progress) with recorded work (changed files, or a reported summary) to continue from.
  Otherwise nothing starts and the ledger says why.
- **Budget:** at most 1 to 3 automatic switches per task and 1 to 20 per project per 24 hours. These count starts, not money:
  Werkbord does not know what a provider bills, and each continuation bills to that agent's own account.
- **Idempotency:** the failed run's ID is the key. A claim is written before anything starts, so polls, restarts and crashes cannot
  start a second continuation of it. The cost is that a crash between claim and start leaves that run for you to continue by hand; the
  ledger records it as reserved. A refusal for a passing reason (capacity, an operation in progress) is retried; every other outcome is final.
- The new run is an ordinary continuation (a fresh conversation from the handoff, never a replay of commands) with origin
  `fallback:<failed run>`, and its task says it was started automatically.

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
- The same script checks the ringed marks, the plan sub-header and the session's plan progress and execution details. `node scripts/browser-agent-header.cjs` checks the Agents & connections popover (and that nothing about an allowance or presence is shown).
- `node scripts/browser-shell-agents.cjs` (the same, through the desktop window's shell and frames, and Back along every route:
  [`NAVIGATION.md`](NAVIGATION.md)).
- `node scripts/browser-salie-terminals.cjs` (terminals in Agents, and the phone's sessions in Salie).
