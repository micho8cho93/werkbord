# The assistant: a conversation with your own coding agent, about your board

The assistant lets you talk to Claude Code, Codex, Gemini CLI or OpenCode, signed in the way you already signed them in, about your board:
what is blocked, what an agent is asking, what is planned for next week. It can **look** at the board and **propose**
changes; you confirm each change before anything happens.

It has a screen ([The screen](#the-screen)): a full-window conversation that opens out of the Assistant button in the sidebar.
Everything the screen does is the HTTP API on this computer ([the API](#the-api)), so it, and later an MCP server, have one foundation.

```
 you ──▶ API (loopback, your token only)
            │
            ▼
        Engine ──────────────▶ Provider ──▶ your assistant CLI   (board tools only, private directory, your sign-in)
   (conversation, streaming,        ▲                 │
    retries, recovery)              │   text          │ text (the reply, with any calls in it)
            │                       └─────────────────┘
            │ a call the reply asked for
            ▼
        Operations (internal/appops)  ──▶ domain services ──▶ the board
   (permission, validation, proposal,         (the same ones the app uses)
    confirmation, audit)
            ▲
            └── you: confirm / decline  (POST …/actions/{id})
```

## What it will and will not do

It can: list projects, tickets, schedules and blockers; show what an agent is waiting on and what a run is doing; create
a ticket; change a ticket's title, description, column, work mode, labels or planned dates; and answer an agent's
question.

It cannot, by construction, not by instruction: start, stop or configure a run, change Git or a repository, change a
setting, archive or delete core board/project/Team data, or give an agent permission to run a command. Personal workspace deletion has the narrow owner-only, human-confirmed path described under Phase 5 below. (`recommend_next_actions` reads a bounded
projection of repository state to work out suggestions: branch names and counts, never a path or a diff, and no operation
changes anything.) There is no operation for any of it
(`TestTheAssistantOffersNoRouteToRunsGitOrSettings`).

| Operation | Does | Needs | Changes something |
| --- | --- | --- | --- |
| `get_overview` | counts per project of what is running, waiting, blocked, failed, in review | `projects:read` | no |
| `list_projects` | the projects (no paths on your disk) | `projects:read` | no |
| `get_project_metrics` | current counts in Backlog, Doing, Review and Done; a board snapshot, not a historical completion rate | `tickets:read` | no |
| `get_analytics` | 7, 30 or 90 days of recorded task state changes and run outcomes, with history coverage; provider results describe runs and do not assign task completions to a provider | `analytics:read` | no |
| `list_labels` | the reusable labels | `tickets:read` | no |
| `list_tickets` | a project's tickets, filtered | `tickets:read` | no |
| `get_ticket` | one ticket in full, with its latest runs | `tickets:read` | no |
| `get_schedule` | planned dates, scheduled starts, deadlines, dependencies, and the problems found in them | `schedule:read` | no |
| `list_blockers` | blocked runs (and why), failed runs, tickets waiting on dependencies | `runs:read` | no |
| `list_agent_questions` | what agents are waiting on | `questions:read` | no |
| `list_runs`, `get_run_status` | runs and how they are doing | `runs:read` | no |
| `recommend_next_actions` | at most two suggested next steps for a project, from fixed rules ([RECOMMENDATIONS.md](RECOMMENDATIONS.md)) | `memory:read`; evidence follows `tickets:read`, `runs:read`, `projects:read` | no |
| `create_ticket` | adds a ticket to a project's Backlog | `tickets:create` | **proposes** |
| `update_ticket` | changes a ticket | `tickets:update` | **proposes** |
| `answer_question` | gives an agent the answer it asked for | `questions:answer` | **proposes** |
| Connected MCP tools | calls a reviewed tool on a user-configured connection | `connections:use` plus the connection's assistant grant | **proposes by default**; explicit `allow` can run without another approval |

"Ticket" is the board's card, which the rest of Werkbord calls a task. In the desktop app, project lookup and confirmed ticket
creation also reach connected workspaces through a narrow desktop bridge (see [Team](#team-and-the-coordination-only-boundary)).

The conversational assistant's catalog is `appops.Service.Catalog`. Inbound MCP clients have a separate, MCP-only
`tasks:propose_plan` permission and `propose_task_plan` operation: the assistant does not receive this permission, and
the operation is not listed to conversational sessions. It creates one editable batch proposal that still requires a
person's approval (see [MCP.md](MCP.md)).

The assistant operations share one catalog, one input schema each (JSON Schema, enforced exactly as advertised), one
permission each, and one audit.

MCP tools are injected into this catalog only for a writable Individual assistant session and only when the person enabled
that connection for the assistant. A tool's schema and fingerprint are rechecked before it runs. Unknown tools start at
`confirm`; schema changes invalidate `allow`. Tool annotations are advisory, so every external tool is classified as a
possible mutation. Each call is brokered by the controller, and upstream credentials are never passed to the model.
`allow` is an explicit preauthorization and can run a tool immediately; its call is still mutation-audited. Read-only
assistant sessions and Team principals do not receive upstream connection tools.

## How a change happens

1. The assistant's reply asks for `update_ticket`. The operation checks the permission, the arguments, that the ticket
   exists in that project, and its version; it works out **exactly** what would change and writes it in plain words
   (`Change ticket "Fix login" (tsk_…): moved backlog → doing; planned nothing → 2026-03-02 to 2026-03-04`). That text is made
   by Werkbord from the arguments, not by the model.
2. It is stored as a *proposal*, bound to the conversation that made it, with a fifteen-minute life, and the person is told
   (`confirmation_required`). **Nothing has changed.**
3. One press of **Confirm** sends `POST /api/assistant/sessions/{id}/actions/{action}` with `{"decision":"approve"}`.
   The stored proposal is the approval surface; no second dialog follows. The controller checks its digest and current
   permissions, then carries the change out once through the same domain service the app calls. A ticket changed by
   someone else in between is a conflict, not an overwrite.
4. The card reports the acknowledged result immediately. A created task says **Added to Backlog** and links to that task;
   a failure stays beside its proposal with the reason.

A model cannot approve its own proposal: there is no call that approves, the only way to approve is the API with your
token, and a proposal is found only by the conversation and principal that made it. A conversation that is deleted or
restarted drops what it left waiting. A proposal that is already waiting is not made twice, and at most ten wait at once.

**Requests for permission are yours.** An agent asking to run a command or change files is a question of kind *approval*.
The assistant lists it ("answerableHere": false) and cannot answer it: `answer_question` refuses, at proposal and again at
execution. Policies never answer approvals either (`domain.Question.AcceptFromPolicy`); this follows the same rule.

## What a model that has been talked into misbehaving can do

Everything the assistant reads (titles, descriptions, an agent's question) was written by someone else, and may say "ignore
your instructions and …". The assistant is told that what it reads is data, but that is a request, not a guarantee. The
guarantees are elsewhere:

- Runtime command, file, browser and agent tools are disabled and guarded. Codex can call only registered board
  functions through an engine-owned callback. Claude asks for the same operations through text calls.
- Board changes through built-in operations only propose. External MCP tools follow their own per-tool policy: `confirm`
  puts a call in front of you, and `allow` can perform a change without another prompt because you explicitly preauthorized it.
  Every connection starts disabled, tool descriptions/results are untrusted, and upstream credentials remain in the controller.
- Tool results are given back to it with `<`, `>`, `&` and backticks escaped, so data cannot close the wrapper or open a
  call block (`TestResultsCannotBreakOutOfTheirWrapperOrOpenACall`).
- A call block in its reply is parsed strictly, at the start of a line, one JSON object, at most six per message; anything
  else is reported back as an error rather than guessed.
- `TestAModelTalkedIntoMisbehavingStillCannotChangeTheBoard` plays this out end to end: a ticket whose title and description
  are attacks, a model that obeys them completely, and a board that does not change.

## Permissions and isolation

- A **principal** is one conversation (`assistant:<session>`), with a set of permissions and, optionally, a set of projects.
  The set comes from your configuration, not from the model; narrowing it applies to conversations already under way.
- A project the principal may not see is "not found", the same answer as one that does not exist, for every operation,
  including lists that span projects.
- One conversation cannot confirm, see or affect another's proposals. A local access token (`wba_…`) cannot reach the
  assistant at all, whatever its scope. The private network serves it to an enrolled phone ([PHONE.md](PHONE.md)) and to nobody else: not to the
  controller's own token there. Its routes are all open to a phone, as on the desktop.
- The operations reach the board **only through the domain services** (`internal/service`), never a table, Git, a runner
  or a process; the engine reaches it only through the operations; and `internal/archtest` fails the build if either imports
  more.

## Team and the coordination-only boundary

The assistant belongs to the Individual controller, on your computer, and runs *your* agent runtime with *your* sign-in.

- Team's build links none of it (`TestTeamLinksNeitherTheAssistantNorItsOperations`), so a Team workspace remains a place
  that coordinates and never runs a model or an agent. The controller still holds no Team credential and makes no call to Team.
- In the desktop app, the assistant's dedicated surface connects the available workspace identities to its conversation. A generic
  metadata transport (`appops.WorkspaceCall`) appends their projects to `list_projects`, with the workspace name and namespaced IDs
  (`workspace:<workspace>/<project>`). It supports overview counts, project lookup, listing tickets and creating a ticket in Backlog.
  Current metrics and planned-date timelines are calculated from those approved ticket reads. Historical analytics is unavailable for connected
  workspaces because that transport does not expose an analytics read; the assistant must identify that gap instead of querying its local store.
  It exposes no run, repository, settings, deletion, or arbitrary HTTP operation. Project creation and editing connected tickets
  remain actions in the workspace itself. Browser/PWA sessions see their own Individual board.
- The engine emits a `workspace_request`; only the dedicated desktop assistant frame forwards it to the native shell. The shell
  rechecks that the workspace is connected, validates a fixed semantic operation and calls Team's existing API through `teamlink`.
  The desktop credential stays there. Only project and ticket metadata returns; no raw API objects, credentials, repository paths,
  branches or execution settings cross. Team's current member/project permissions apply to every read and write.
- A connected workspace that cannot be read just now (its service stopped, the window slow to answer: a read waits 25 seconds) does not
  take the person's own board away: `list_projects` and `get_overview` still answer for everything else and add
  `unavailableWorkspaces`, so that the assistant says the workspace could not be read rather than that it has no projects.
- Creation is shown in the stored proposal with its task title, Backlog destination, project and proposed details. After one
  **Confirm**, the desktop checks a controller-issued authorisation bound to the session, proposal, workspace and exact
  arguments before calling Team. An arbitrary page bridge call cannot create a ticket. Request IDs are cached so a frame
  reconnect or replay cannot create it twice. The card acknowledges success and opens the exact ticket in Team; unavailable
  or failed requests keep their reason beside the proposal. **Confirm all N** takes the proposals waiting when pressed,
  continues after individual failures, and reports the added and failed counts. After a controller restart the frame reconnects
  before sending or confirming.
- Imported Team tasks still use the Individual integration as before. Creating a connected ticket changes only its own workspace;
  it does not create a local task or start an agent.
- Execution stays with your runner. The assistant never starts a run. Answering a question goes through the runner the
  run is on (`Runner.Answer`), exactly as answering in the app does.

## Providers

The assistant borrows what is already on this computer. It has no account, key or model of its own, and asks for none.
Each provider says whether it is installed and available, how to sign in, and what to do if not (`GET /api/assistant/providers`).
Where the CLI can identify that an environment key is in use, `usesApiKey` reports it; Werkbord neither adds nor removes keys.

#### Claude Code and Codex

What was found by trying the real command lines (Claude Code 2.1.285, Codex 0.155.1, macOS), and what is **not** supported:

| | Claude Code | Codex |
| --- | --- | --- |
| Sign-in reused | `claude auth login` (Claude plan); read with `claude auth status` | `codex login` (ChatGPT plan); read with `codex login status` |
| How a turn runs | `claude -p --output-format stream-json --include-partial-messages`, prompt on stdin | `codex app-server` (JSON-RPC over stdio): `thread/start` or `thread/resume`, then `turn/start` |
| Streaming | **Tokens**: `text_delta` events | **Tokens**: `item/agentMessage/delta`. (`codex exec --json` delivers a message whole, so it is not used) |
| Continuing a conversation | **Yes**: `--session-id` (chosen by Werkbord, so it is known before the first word) then `--resume` | **Yes**: the thread id from `thread/start`, with `thread/resume` |
| A conversation the provider no longer has | `No conversation found with session ID …` → `session_lost`; a fresh one is started and the assistant told | `no rollout found for thread id …` → `session_lost`, the same |
| Cancelling | SIGTERM to the process group, SIGKILL after 2 s; measured about 0.6 s, children included | `turn/interrupt`, then the process is ended; measured about 0.4 s |
| Model choice | **Yes**, `--model` (an alias or a full name). There is **no way to list models**: aliases `sonnet`, `opus`, `haiku`, or the list in `config.json`. An unknown one is an API 404 → `model_unavailable` | **Yes**, `model/list` gives names, the default, and the reasoning levels of each. A model the account cannot use → `model_unavailable` |
| Reasoning level | `--effort` (levels read from the CLI's own help) | `effort` on the turn, from the model's own levels |
| Tools turned off | `--tools ""`, `--safe-mode`, `--strict-mcp-config`, `--disable-slash-commands`, `--setting-sources ""`. The `init` event lists `tools: []` and `mcp_servers: []`; if it lists any, the turn is ended | Runtime execution features disabled, read-only sandbox, approval `never`. The isolated code-mode dispatcher remains available for registered board functions. Item and request guards reject unregistered tools, commands, approvals and other agent actions |
| Native tool calling | Supported by the CLI (MCP servers). **Not used** | Client-registered board functions call back into the engine's `runCalls`, with the same permissions, validation, audit and confirmation as text calls |
| Usage | tokens, reported per turn (not stored, not priced) | tokens, reported per turn (not stored, not priced) |
| Voice | **Not supported**: Claude Code has no voice input or output when run headless | **Experimental**: the protocol has realtime audio (`thread/realtime/*`), gated by account support. **Not used.** Dictation on the screen ([Dictation](#dictation)) is the browser's own, and is not either provider's voice |
| Images and files | **Not used.** Text files are shown to it inside the message itself ([Attachments](#attachments)); a PDF or a picture is refused, because with no tools there is nothing to read it with | **Not used**, the same |
| Deleting the provider's copy of a conversation | **Not possible** (no command); Werkbord forgets its handle | Possible in the protocol (`thread/delete`); **not used** |
| Where the conversation lives | Claude Code's own storage, under the assistant's private directory | Codex's own storage (`~/.codex/sessions`) |

#### Gemini CLI and OpenCode

Both use ACP over stdio, the structured Agent Client Protocol. Gemini CLI starts with `gemini --acp`; OpenCode starts with
`opencode --pure acp`. ACP notifications stream reply text, session handles support continuation, and cancelling the
turn stops its CLI process tree. OpenCode lists configured models with `opencode models`, keeping provider-qualified
IDs such as `openrouter/anthropic/model`. Gemini accepts `--model`; its model list can be supplied in Werkbord config,
and `auto` leaves selection to Gemini CLI. OpenCode session options carry its model and effort selection.

Assistant isolation is enforced by each CLI's configuration: Gemini gets a highest-priority system settings file that
disables built-in tools, MCP servers, extensions and skills; OpenCode gets inline settings that deny tools and clear
MCP configuration, and runs with external plugins disabled. The assistant advertises no ACP filesystem or terminal
services. If an ACP agent still requests a permission or tool, Werkbord refuses the request. These adapters use only
the text operation protocol, so the same operation checks, proposal confirmation and audit remain in force.

**Four transports, one operation boundary.** Claude uses text calls. Codex registers the catalog as application functions
so the model can request real tools instead of interpreting disabled runtime tools as unavailable board access. The
providers have no board imports or credentials: the callback belongs to the engine and calls the same operation service.
Calls are bounded to 36 per attempt; audit failures stop the turn with their original reason. A tool request must belong
to the active thread and turn and name a registered operation. Command and approval requests still end the turn.

**The text protocol.** The system prompt lists the operations and their input schemas. To use one, the assistant writes a
fenced block in its reply:

````
```werkbord-call
{"id":"c1","name":"list_tickets","arguments":{"projectId":"prj_…"}}
```
````

The engine finds these as the reply streams, **without showing them**, runs each as the conversation's principal, and sends the
results back inside `<werkbord-results>` for the assistant to answer from. At most six calls a message and six trips a
turn.

**A call that was not written as a call block.** Only the exact block runs, so text that merely quotes a call can never run one. But
the real models do not always write it exactly (Claude Sonnet and Haiku, checked live): the JSON alone with no opening fence and a
stray closing one after it, the call in a `json` block, several calls in one block, Claude's own `<invoke>` syntax written into the
reply. A reply ends the turn when it holds no call, so such a reply used to leave a line of raw JSON on the screen and the turn over
with nothing done ("the assistant went round in circles"). Now `repairReply` finds what looks like a call in the text that was not
written as one (a JSON object that names a real operation and has `"arguments"`, alone on its line or in a fence of its own), takes
it out of what the person sees (the screen drops the attempt's text, as for a retry, and shows the rest), and tells the assistant, in
the result for that call, that it was not run and how to write it, quoting what it wrote. Its second try is run like any other.
A reply that ends on its closing fence with no newline after it, which is how both CLIs end a reply that asks for something, is a
proper call: it was once reported as "never closed". Several calls in one block, with or without commas, or as an array, are each a
call, up to the same limit. Checked against the real command lines by `TestLiveTheAssistantAsksForAnOperationInTheWayTheEngineReads`
and, for the whole conversation (lookup, four proposals, confirmation, no duplicates on the next turn), `TestLiveClaudeCreatesPortfolioTasksAfterConfirmation`
and `TestLiveCodexCreatesPortfolioTasksAfterConfirmation` (`WERKBORD_LIVE_ASSISTANT=1`; the model of each can be chosen).

Codex message boundaries are preserved in that text: a commentary message followed by a call message has a blank line
between them, even when the provider sends neither a trailing nor a leading newline. Each message can stream or arrive
whole independently; completed messages are not repeated and no earlier message is discarded. The live
`TestLiveCodexCreatesPortfolioTasksAfterConfirmation` exercises project lookup, four task proposals, confirmation and the
persisted Backlog cards in a disposable board. Set `WERKBORD_LIVE_ASSISTANT_MODEL` to check a specific Codex model.

The Codex adapter replaces the default coding-agent base instructions with Werkbord's board instructions on both new
and resumed threads. Existing text-only Codex chats reconnect once to register the functions, with the engine's explicit
conversation-recovery notice. New handles carry a `werkbord-tools:` prefix; the adapter strips it before addressing
Codex. The live check uses the spoken numbered portfolio request with the current date, confirms four cards, then
resumes the conversation and checks that an existing task list produces no duplicate proposals.

### OpenUI Lang responses

The assistant can also answer a request to create an interface with OpenUI Lang. The Svelte 5 registry in
`web/src/lib/assistant/openui/library.ts` defines Werkbord components for metrics, progress, status summaries, bar, line,
pie and comparison charts, sortable/filterable task and project tables, planned timelines, and task workflows with dependencies
and blockers, alongside `Text`, `Heading`, `Card`, `List`, `Table`, `Disclosure`, `Grid` and `Stack`. Zod 4 schemas bound
each component's data and arguments. `library.prompt()` generates the component signatures and inline-response rules from
those same definitions; the browser sends that bounded formatting prompt with the message. The controller adds it to the
provider's normal system prompt, without changing the session, provider sign-in, operation catalog or confirmation policy.
The implementation uses the installed `@openuidev/svelte-lang` 0.4.1 API (`defineComponent`, `createLibrary`, `Renderer`,
`library.prompt({ inlineMode: true })`) and Zod 4. See the [Svelte Lang API](https://www.openui.com/docs/api-reference/svelte-lang)
and [Svelte integration guide](https://www.openui.com/integrations/svelte) when updating the dependency. The live
`TestLiveOpenUIResponsesUseRegisteredComponents` uses the generated registry prompt with each available CLI, then validates
the fenced response with the installed parser. Run it with `WERKBORD_LIVE_ASSISTANT=1 go test ./internal/assistant
-run TestLiveOpenUIResponsesUseRegisteredComponents -v`.

The assistant may mix ordinary prose with a fenced `openui-lang` block. OpenUI code streams in the existing `delta` events,
and `ReplyView.svelte` splits it from prose and passes each block to the registered Svelte renderer. Tables sort and filter
their already-returned rows locally; chart category selection, comparison filters, series toggles, date ranges and disclosures
also update locally. These controls change no board state and make no request. Each data visualization includes the exact
operation snapshot timestamp and is not live.

The registry also supports native Svelte forms with text, select, date and checkbox fields, and a visible conversation action.
OpenUI's Svelte form state and validation APIs keep edits scoped to that form. Validated field values are kept beside the reply
in the existing tab-only transcript cache, so a reload in the same tab restores the draft. A form has no operation or API
callback: pressing its action forwards only that user-selected label and field values as a message to the current conversation.
The callback accepts only the registered continue-conversation action, rejects unexpected parameters and malformed values,
and cannot approve a proposal. The assistant must still request an existing operation; `internal/appops` checks its grants,
project scope, input schema and optimistic version, then records the proposal in the audit. Werkbord's ordinary confirmation
endpoint remains the only way to apply a proposed change. Read-only sessions cannot create a proposal, expired proposals
cannot be applied, and repeated button presses cannot submit twice.

The assistant uses prose for simple questions and retrieves data through its existing permission-scoped operations before
generating an interface. OpenUI cannot add operations or call an API. Generated UI cannot run HTML, JavaScript, CSS, OpenUI
queries or mutations, or unregistered components. The operation filter and repair pass treat the UI fence as opaque;
`werkbord-call` blocks remain a separate protocol outside it. Retries still discard the failed attempt, while SSE replay and
transcript recovery carry the same streamed reply text.

An invalid block keeps any valid rendered portions and shows a plain fallback with an expandable, text-only source preview.
An unfinished block is labelled incomplete after the turn ends. OpenUI runs locally in the web app: Werkbord does not use
OpenUI Gateway, ask for an OpenUI key or send prompts to an OpenUI service. The upstream core package has optional
pseudonymous install-time telemetry; `make` disables it with `OPENUI_TELEMETRY_DISABLED=1`. Use the same environment variable
for a direct `npm ci` or `npm install`.

The conversation keeps a working indicator until the turn ends and marks completed replies explicitly. Operation errors
show their reason in the conversation. After a reconnect, partial text alone never implies success: an unrecovered final
result is shown as stopped or failed, with a note to check the board before retrying changes.

**Both CLIs change often.** `go test ./internal/assistant/provider/live -v` with
`WERKBORD_LIVE_ASSISTANT=1` repeats these checks against the real command lines (a handful of one-word turns).

## The screen

The assistant is one page of Individual, not a second app. It is a button, a surface and a box.

**The way in.** An *Assistant* button sits at the top of the sidebar (on a phone, an icon in the page heading; in the desktop
window, the window's own sidebar, [below](#in-the-desktop-window)). It is quiet until something needs you: it shows a count when a
change is waiting for your confirmation and a pulse while a reply is on its way, so you know without opening it.

**Opening and closing.** The surface grows from the button's centre until it covers the window, while its background moves from the
sidebar's colour to its own paper (light and dark each have their own). It takes 950ms, with a fading light along the edge and the
content arriving from 540ms. Closing runs the same movement backwards into the button, 30% faster,
and focus goes back to it. It can reverse in either direction halfway through without jumping. With *reduce motion* on, nothing
moves: it is there, or it is gone. The workspace underneath is only covered (and made inert, so focus and screen readers stay in the
assistant): the same page elements, the same place, the same half-typed text are there when you come back
(`scripts/browser-assistant.cjs` checks this by identity, not by eye).

**Closing is not stopping.** The conversation lives in the page's memory, not in the surface. Close it, go to the board, come back:
a reply that was streaming has gone on streaming and is complete; a change that was waiting is still waiting, and says so on the
button. Only the *Stop* button stops a reply, and only your answer carries out a change.

**What is on it.** A heading with *which assistant answers* (provider, model, how hard it thinks) and *New*/*Close*; the
conversation; a box. Nothing else: there is no history list, no sidebar of past chats and no way to browse them, because the server
keeps none ([What is kept](#what-is-kept-and-what-is-not)).

- **The box** takes words, files (the paperclip) and speech (the microphone). Enter sends, Shift+Enter makes a new line; on a phone
  Enter is a new line and the arrow sends. It grows to a few lines.
- **A reply** streams in a few words at a time and is drawn from a small, safe reader (paragraphs, lists, bold, italic, `code`, code
  blocks) that builds elements, never HTML: markup in a reply, or in anything it read, is shown as text, and links are not made
  clickable. While it works there is one quiet line (*Looking for what is blocked…*, *Thinking…*); afterwards, a collapsed *What I
  looked at*. Operation names never appear.
- **A change** is a card: what Werkbord says will happen, *Confirm* and *Not now*, and *Nothing changes until you confirm*.
  One press carries it out. A created task shows a green check, **Added to Backlog** and an *Open task* link. When several are
  waiting, *Confirm all N* answers the proposals that were waiting when pressed. Each has its own audit record; individual
  failures do not stop the rest, and one short result reports the counts.
- **Choosing the assistant.** The heading's *Claude Code · Sonnet* opens the provider, model and thinking-level chooser, which can be
  used at any time. A model your plan cannot use ends the turn with *That model is not available to your account* and offers *Choose
  another model* and *Try again*. Moving to the other provider starts a fresh conversation with it, and the screen says so. Files you
  had agreed to share with the first are taken back, because the agreement was for that provider.
- **Starting over** (*New*) ends the conversation. If a reply is running or a change is waiting, it asks twice and says what it will
  drop.

**When there is nothing to talk to**, it says why, in words, and offers what can be done:

| State | Shown | Offered |
| --- | --- | --- |
| Getting ready | *Getting ready…* | |
| No provider ready | *Set up a supported CLI first*, with each one's own reason and command | *Check again* |
| Controller not answering | *Cannot reach Werkbord* | *Try again*; the screen keeps trying by itself |
| Connection dropped mid-reply | *Reconnecting… your reply keeps going meanwhile* | none needed |
| Turned off in settings | *The assistant is turned off* (`assistant.disabled`) | |
| Not signed in to Werkbord | *This device is not signed in* | open Werkbord from its link |
| A turn failed | what happened, in words, from the error code | *Try again*, *Choose another model*, *Check again*, as it fits |
| Empty | *What can I help with?* and three questions to start from | |

**Coming back.** The page keeps what is on screen in the tab (`sessionStorage`, which goes with the tab, is never sent anywhere, and is
cleared by *New*): capped to the last 40 items, without file contents. After a reload the conversation is back; the reply that was
streaming goes on from the last event the page saw (the server remembers 1024 recent events and says when it cannot); a change that
is still waiting is found from the server even if the tab forgot it; one that was answered or timed out meanwhile is shown as no
longer waiting. If the controller was restarted its events count from 1 again, and the page counts with it (it compares the
server's last event number on every connection, so the next reply is not thrown away as "already seen"). If the controller no
longer has the conversation, the screen says so and the next message starts a new one. The stream is read with `fetch`, because
`EventSource` cannot send the token and the server refuses it in a URL.

**For people who cannot see it.** The surface is a dialog with a name, Tab stays inside it, Esc closes it (the chooser first), every
control has a name, text is at least 16 px in the box on a phone (so it does not zoom), tap targets are at least 44 px, and what
happens is said in words in a status line (*The assistant is working*, *…has finished. A change is waiting for your
confirmation*). The reply text itself is not read out word by word. Nothing relies on colour alone.

### In the desktop window

The assistant has its own Individual page (`?surface=assistant`), holding the controller token, in a dedicated transparent frame
over the entire viewport, including the sidebar. The frame is created on first use and kept alive while closed.

- The page is told *where the button is*, in window coordinates (the frame covers that same window), so the
  surface still grows out of the sidebar's button and collapses back into it.
- Every workspace and window page stays in place beneath it. Closing restores access and focus to the sidebar button.
- The page's sidebar state report carries only whether it is open, how many changes wait, and whether a reply is running.
  The separate workspace bridge carries only bounded semantic requests and metadata; no transcript is handed to a workspace page,
  and a Team frame cannot report assistant state or call the privileged bridge
  (`shell/frames.ts`, tested; `scripts/browser-shell-assistant.cjs` checks the whole path in a real frame).

## Attachments

You can show the assistant a text file with a message: notes, a spec, a CSV, a log. A file is held only under these rules.

- **Explicit, per file, before anything leaves the page.** Picking (or dropping) a file asks *Share this file with Claude Code?* and
  says what happens next. The page holds the file and sends nothing until you say yes. The request carries your yes
  (`X-Attachment-Authorized: true`); without it the server refuses, and the authorization is recorded in the audit (name, size, how
  long, which provider; never the content).
- **Short-lived, in memory only.** Held for up to `assistant.attachmentRetentionMinutes` (default 30, 1 to 1440) unless you send it
  first, then dropped. Never written to disk; a restart forgets them. They go too when you remove one, when the conversation is
  deleted or the provider is changed, and at shutdown. The chip says until when.
- **Sent once.** A message names the files it carries; they are put in that message and forgotten here. The next message does not
  carry them, and the audit records what was shown to which provider. The provider keeps its own copy as part of the conversation,
  in its own storage, and **Werkbord cannot delete that**; the question says so before you agree.
- **Bounded.** 256 KB a file, 4 files and 512 KB a message, 8 waiting in a conversation and 8 MB waiting in all. Plain text only (UTF-8, no control
  characters): a PDF or a picture is refused, saying so. The assistant runs with no tools, so there would be nothing to read one with;
  PDFs, Markdown libraries and images are the resources library's work (stage 4).
- **Data, not instructions.** The content is escaped (`<`, `>`, `&` and backticks) inside `<werkbord-attachment>`, so a file cannot
  close its wrapper, forge a notice or write a tool call, and the assistant is told the file is yours to read, not to obey
  (`TestAFileCannotBreakOutOfItsWrapperOrOpenACall`). Names are cleaned the same way.
- **Off if you like.** `assistant.attachmentsDisabled` removes the paperclip and refuses the route.
- **Choosing on Mac.** The paperclip uses the native app's file dialog, avoiding WebKit's framed file-input sheet dismissal.
  Only chosen filenames and bounded contents return, never disk paths. Browsers retain their ordinary file picker.

Checked against the real command lines (`TestLiveAttachmentTravelsInTheMessage`, Claude Code 2.1.285 and Codex 0.155.1): both read the
file and answered from it, and neither obeyed the "reply only PWNED" planted inside it. The provider's answer to such a file is still
a request kept, not a guarantee; what a manipulated model can do is limited as described under [What a model that has been talked
into misbehaving can do](#what-a-model-that-has-been-talked-into-misbehaving-can-do).

## Dictation

The microphone types what you say into the box. It is the browser's (or the desktop app's web view's) own speech recognition, and
only that: **it is not Claude's or ChatGPT's voice.** Neither provider offers voice input to a program that runs it this way (the
table above), and the screen says "not Claude or ChatGPT" in the notice and never claims otherwise.

- **Where the audio goes is disclosed before the first use.** Chrome sends it to Google, Edge to Microsoft, Safari to Apple, unless
  the browser reports that it can recognise speech on the device (`SpeechRecognition.available` with `processLocally`), in which
  case it is asked to, and the notice says nothing leaves the device. Anything that is not known to be local is treated as sending.
  You agree once per device and service; the agreement is kept in that browser only.
- **When it is not available** (no recogniser in this browser, or the page is not a secure context) the button says so and points to
  the microphone key on your phone's keyboard, which types locally.
- Speech is typed into the box, where you can read and change it before sending, and mixed freely with typing. A blocked
  microphone, a missing microphone and a speech service that cannot be reached each say so in words; silence is not an error.
- During dictation, X (or Escape) cancels and restores the earlier draft, the tick finishes into an editable draft, and Send
  queues sending until the final words arrive. Recognition failure retains what was heard for review and does not send it.
  A continuous engine that stops during silence is restarted with a bounded limit. Finishing has a timeout so the box cannot hang.
- The Mac bundle declares microphone/speech usage descriptions and the audio-input entitlement. Denied permission is explained
  using macOS settings; the microphone is never opened before the person starts dictation.
- macOS Dictation must also be enabled in **System Settings → Keyboard → Dictation**. Apple's speech service can end immediately
  when it is off, even after microphone and Speech Recognition permissions are granted. The app stops that failed startup without
  retrying it and points to the relevant settings, preserving the typed draft.
- Whether on-device recognition is possible is asked only when you press the microphone, never when the assistant opens. (Asking in
  headless Chromium crashed the page, and opening the assistant should not wake a speech service anyway.)

## Choosing the provider, model and reasoning level

The screen offers all three, and can change them at any point, so that someone whose plan cannot use a model can move to one it can.
`GET /api/assistant/providers` returns, for each provider: whether it is ready (and what to do if not), its models (with the
reasoning levels each takes, and which is the default), `custom` (a name not in the list may still be typed) and its
capabilities. `POST /sessions` takes `{provider, model, reasoning}`; `PATCH /sessions/{id}` changes any of them between messages.

- **Same provider, another model or level:** the conversation continues where it was.
- **Another provider:** a fresh conversation with it begins (each provider keeps its own), the model and level are chosen again
  (they belong to the provider), the assistant is told it remembers nothing from before, and any change already waiting for
  confirmation is untouched and can still be confirmed.
- **A level the model does not take** is refused at once, with the ones it does take, before anything is spent. A model the
  provider does not list is let through; the provider has the last word, and a model the account cannot use ends the turn with
  `model_unavailable`, after which the choice can simply be changed.
- **Nothing chosen** means the provider's own default, which can itself be a model your plan does not offer; the same error applies.
- Claude Code cannot list its models (aliases, or the list in `config.json`); Codex lists them itself.

## Streaming, cancelling, timing out, reconnecting

- **Streaming.** A turn's reply is published as `delta` events as it is generated. Events have a per-conversation sequence
  number that only grows.
- **Presentation timing.** Provider delta arrival, controller publication, browser receipt and visible update are measured
  separately. The browser combines text changes at animation-frame boundaries, while approval, error and completion events
  are handled immediately. Completed Markdown and OpenUI sections are reused as later text arrives. Controlled short, long
  and mixed-output fixtures report provider delay separately from browser receipt-to-visible latency; the target for the latter
  is below 100 ms at p95.
- **Reconnecting.** A turn belongs to the engine, not to the request that started it: closing the page does not stop it.
  `GET …/events` with `Last-Event-ID` (or `?after=`) replays what a returning client missed from the last 1024 events, then
  goes live; if more than that was missed it says `gap` and the client reloads the session. A client that cannot keep up is
  dropped rather than allowed to slow the turn, and resumes the same way.
- **Cancelling.** `POST …/cancel` stops the provider and everything it started. Changes already proposed keep waiting.
- **Timeouts.** One message has 5 minutes (`assistant.turnTimeoutSeconds`). A provider that says nothing for 90 seconds is
  given up on (`assistant.idleTimeoutSeconds`); thinking counts as saying something.
- **Retrying.** A failure a retry can fix (`transient`: a passing service problem, a crash, a stall, an empty reply) is
  retried up to twice, with a short wait, and the client is told (`notice`, then `retry`, after which the earlier attempt's
  text is to be discarded). A provider that is not signed in, a plan that is used up, an unavailable model, a protocol
  surprise and a tool being used are **not** retried.
- **Lost conversation.** If the provider no longer has the conversation, a new one is started, the assistant is told it
  remembers nothing from before, and the person gets a `notice`.
- **Clear errors.** A failed turn ends with `turn_failed` and `{code, message, retryable}`:

| Code | Means | What to do |
| --- | --- | --- |
| `not_installed`, `not_signed_in` | the command is missing, or signed out | the message says which command to run |
| `rate_limited` | the plan has no allowance left for now | wait |
| `model_unavailable` | the account cannot use that model | choose another (`PATCH …/sessions/{id}`) |
| `session_lost` | (handled: a new conversation is started) | |
| `transient` | a passing problem, a crash or a provider that said nothing, still there after the retries | try again |
| `timeout` | the whole message took too long | try again, or ask for less |
| `policy` | the provider tried to use a tool of its own; the turn was ended | report it; nothing was changed |
| `protocol` | the provider's output was not understood | update Werkbord or the provider |
| `too_many_steps` | the assistant kept asking for information | ask something more specific |
| `audit_unavailable` | the record could not be written, so it stopped | check the disk; nothing was changed |

## What is kept, and what is not

Per conversation (`assistant_sessions`): the provider and model chosen, the provider's **handle** for the conversation, how
many turns it has had, the state (`idle`, `running`, `awaiting_confirmation`) and the last failure's code. **Not**: the
messages, the replies, or any history; and there is no way to browse one. The conversation itself is kept by the provider,
in its own storage, under the assistant's private directory (`<data dir>/assistant`, mode 0700, empty).

Per file waiting to be sent: its name, size, content and time limit, **in memory only** (never in the database or on disk), for at
most the retention period. The audit records that it was authorized, shown or dropped, without its content.

In the browser tab: what is on screen, so a reload comes back to it (`sessionStorage`: the last 40 items, no file contents, gone with
the tab or with *New*), and, in `localStorage`, which provider/model/level you chose last and whether you have read the dictation
notice. Nothing else of a conversation is kept anywhere by Werkbord.

Per proposed change (`assistant_actions`): the operation, the exact arguments and their digest, the summary shown, who
proposed it, its state, and what came of it. Deleted with the conversation.

The **audit** (`assistant_audit`) is kept: every operation attempted, by whom (`assistant:claude-code`), in which
conversation, with its outcome (`ok`, `proposed`, `confirmed`, `rejected`, `expired`, `denied`, `invalid`, `failed`) and
the argument digest. Reads record identifiers and filters, never prose; a change records the summary you were shown. It is
append-only (the database refuses an update or a delete), chained (each line carries the hash of the one before:
`GET /api/assistant/audit/verify`), and fail-closed: an operation whose record cannot be written is not done, and a
confirmation is recorded **before** the change is carried out. It outlives the conversations it describes.

**Retention.** Entries are a few hundred bytes each; a busy day is tens of kilobytes. They are kept for 400 days
(`assistant.auditRetentionDays`: at least 30, or `-1` for ever), pruned at start and once a day. Pruning removes only the
oldest entries as one unbroken run and leaves a checkpoint holding the hash of the last one removed; the entries that remain
still verify against it, a line taken from the middle or the end is still caught, and the pruning writes its own line
(`audit_pruned`). The database refuses any other delete.

## Recovery

At start, `Recover` settles what a restart left in doubt, and nothing else: a turn that was running is over (the session
becomes `idle`, with `interrupted`); a change that was being carried out is marked **failed**, with a note that it may or may
not have been applied, because that cannot be known (check the board before asking again); a proposal past its time is
expired. A proposal still within its time **survives** the restart and can be confirmed. Recovery never carries anything out
and writes what it did in the audit. Shutting down cancels turns and records where they ended.

## The API

All of it needs the controller's own token (header only, never in the URL), is served on the loopback listener only, and
answers `503` if `assistant.disabled` is set.

| | |
| --- | --- |
| `GET /api/assistant/providers` | what is installed, signed in, and which models and reasoning levels it offers; capabilities; and the attachment limits (`attachments`) |
| `POST /api/assistant/sessions` `{provider, model?, reasoning?}` | start a conversation (fails at once, with what to do, if the provider is not ready) |
| `GET /api/assistant/sessions`, `GET …/{id}` | conversations, with the changes waiting, the files waiting (never their content) and the last event number |
| `PATCH …/{id}` `{provider?, model?, reasoning?}` | change provider, model or reasoning level for the next turn (see below) |
| `DELETE …/{id}` | stop it, drop what it left waiting, forget it (the audit stays) |
| `POST …/{id}/messages` `{text, attachments?: [id], openuiPrompt?: string}` → `202 {turnId}` | send a message (words, files, or both); `openuiPrompt` is bounded to 24,000 characters and supplies the registered component instructions for inline UI; read the reply from the events |
| `POST …/{id}/attachments` (body: the file; `X-Attachment-Name`, percent-encoded; `X-Attachment-Authorized: true`) → `201 {id, name, bytes, expiresAt}` | hold a text file for the next message ([Attachments](#attachments)); `413` over the limit, `400` without the yes or if not text |
| `DELETE …/{id}/attachments/{attachment}` | drop a file that has not been sent |
| `GET …/{id}/events` (SSE) | `turn_started`, `delta`, `tool_call`, `tool_result`, `confirmation_required`, `confirmation_resolved`, `notice`, `retry`, `turn_completed`, `turn_failed`, `turn_cancelled`, `gap` |
| `POST …/{id}/cancel` | stop the turn in progress |
| `POST …/{id}/actions/{action}` `{decision: "approve"\|"decline"}` | the person's answer to a proposal |
| `GET /api/assistant/audit?session=&before=&limit=`, `GET …/audit/verify` | the trail, and a check of its chain |

## Configuration

`config.json`:

```json
{
  "assistant": {
    "disabled": false,
    "readOnly": false,
    "projects": ["prj_…"],
    "turnTimeoutSeconds": 300,
    "idleTimeoutSeconds": 90,
    "auditRetentionDays": 400,
    "attachmentRetentionMinutes": 30,
    "attachmentsDisabled": false
  }
}
```

`readOnly` removes the permissions to propose changes (the assistant is not even told those operations exist). It grants the
read-only `analytics:read` permission so the assistant can answer historical project questions from recorded task transitions
and run outcomes. This permission exposes neither model usage/cost analytics nor any write operation. `projects` limits what it can see. The commands, models and reasoning levels it offers are the ones set for the agents under `"agents"`
(`agents.claude-code.command`, `.model`, `.models`, `.reasoning`; likewise for `codex`, `gemini-cli` and `opencode` where
supported); `model` is what a conversation that chose none gets. Gemini CLI does not expose a reasoning selector through
this adapter.

## Testing

| What | Where |
| --- | --- |
| Operations: permissions, scope, validation, proposals, confirmation, expiry, double confirmation, tampering, audit chain, fail-closed, recovery, injected MCP tools | `internal/appops`, `internal/connections`, `internal/controller/connection_operations_test.go` |
| Engine: streaming, tool loop, injection, retries, stalls, timeouts, cancel, lost conversation, reconnection, isolation, recovery | `internal/assistant` |
| The tool-call protocol, cut at every possible point | `internal/assistant/protocol_test.go` |
| Providers against faithful fake command lines (flags, environment, stdin, classification, process-group kill) | `internal/assistant/provider/claude`, `…/codex` |
| Providers against the real command lines (opt-in) | `internal/assistant/provider/live` |
| HTTP: end to end, SSE resume, status codes, **no local-access token and no private network can reach it** | `internal/api`, `internal/controller` |
| The store: compare-and-swap, one claim per action, append-only audit | `internal/store/sqlite` |
| The boundaries | `internal/archtest/assistant_test.go` |
| Attachments: authorization, once-only, limits, expiry, isolation, escaping, fail-closed | `internal/assistant/attachments_test.go`, `internal/api` |
| The screen's logic: the transcript from events (retries, rounds, replays, gaps), the stream (cut anywhere, reconnects, restarts), dictation, the animation's plan, the safe reader, OpenUI schema/prompt/reply parsing, visualization data, empty states and mobile breakpoints, choices, words | `web/src/lib/assistant/*.test.ts` |
| The screen against a stand-in controller: opening/closing, sending, confirming, choosing, files, reload, restart, no conversation, no provider | `web/src/lib/assistant/store.test.ts` |
| The screen in a real browser, with the real engine and API and a scripted provider: the animation frame by frame (light, dark, reduced motion), the workspace left untouched, streaming, generated OpenUI local controls and forms, invalid form values, duplicate submissions, task proposal approval/rejection/expiry, denied writes, malformed-output recovery, tool progress, confirmations that survive closing/reload/restart, files, dictation, keyboard and screen-reader behaviour, a phone | `make test-assistant-browser` (`scripts/browser-assistant.cjs`) |
| The same from the desktop window's sidebar, in a real frame | `make test-assistant-browser` (`scripts/browser-shell-assistant.cjs`) |

## Not in this stage

PDFs, images and other resources, and reading them (the resources library, stage 4); analytics on model usage and cost (5); the MCP
server and external clients (6; it will use `appops.Service` with a principal of its own); native voice and communication
integrations (7). The screen has no keyboard shortcut of its own yet, and keeps no history on purpose.

## Phase 3A: Salie's memory context

Salie uses the existing provider engine and operation boundary. `retrieve_memory` is a read-only,
`memory:read` operation. It never proposes, edits, verifies or publishes a memory. Read-only assistant
configurations receive this read permission too; Git, execution, settings and mutation restrictions stay in force.
The operation is refused to non-assistant principals, even if they have a memory grant.

For architecture decisions, recent changes, previous problems, merge policies and next-work questions,
Salie identifies relevant projects through the permitted project catalog, then requests context for each
relevant project. Inputs carry concise search keywords, an optional task ID and a request type
(`architecture`, `changes`, `lessons`, `policy`, `next`, `general`). These are search hints, never authorisation.
The server binds the actual principal and project; repository identities come from checked record scopes,
not filesystem paths. Local memory stays bound to the controller's Individual owner. Restricted sessions
cannot retrieve another project's memory, and do not receive personal memory. Unrestricted sessions can
also include useful personal context on a keyword search or a next-work request. No transcript extraction
or separate model engine is introduced.

Individual retrieval reuses Knowledge's project, repository identity, run and evidence checks. Verified
current policies and explicit decisions rank before facts and preferences, before the bounded result is
selected. Superseded, duplicate, sensitive and unverified claims are excluded. Historical lessons are
available only for a `lessons` request, must have verification evidence, and are labelled historical with
low confidence. Each item carries its record ID, source reference, scope, verification, freshness and dates.

Connected workspaces use the reviewed desktop `retrieve_memory` semantic operation. The controller
holds no Team credential or Team API route. The native bridge calls the existing authenticated project
memory search and exact workspace-scope published-memory read. Team rechecks membership, project
membership, classification and every constituent project of cross-repository records on each read.
The controller also checks every constituent project against the assistant session's own project allow-list. The projection contains no credentials, raw API objects or arbitrary data fields. Superseded, stale,
conflicting, duplicate and unverified records are excluded; policies require approval. Verified records
whose freshness is uncertain are explicitly low-confidence suggestions, never authoritative context.
Disconnected or unreadable memory is reported as unavailable rather than as proof of no prior decision.

The default context budget is 1,500, configurable per read from 512 to 6,000. This is a deliberately
conservative upper bound of one token per serialised JSON byte, including provenance and framing,
rather than a provider-specific tokenizer estimate. Oversized items are omitted and truncation is reported.
The final combined projection is bounded again after personal context and request metadata are added.
Empty, unavailable and truncated results remain distinguishable.

Memory enters the model only through escaped operation results, never the system prompt. The shared
provider guidance requires citations by record ID/source reference, separates documented evidence from
suggestions, and asks for fresh retrieval each turn. These are response guidance; permissions, audit,
confirmation and the provider runtime restrictions remain enforced independently of model behaviour.
The same operation and result wrapper work with native Codex tools and Claude/Gemini/OpenCode text calls.

Every item carries an `origin`, set by the layer that fetched it and never by the stored record: `individual`
(shown as **Your private memory**) or `team` (**Team memory**). A Team read is bounded to 10 seconds in total, so a
Workspace Host that accepts the request and does not answer degrades that project's memory to "unavailable"
instead of holding the turn for the transport's 20-second timeout per call. The memory and suggestion
paragraphs of the system prompt (about 700 tokens) are sent only when the principal's catalog contains
`retrieve_memory` or `recommend_next_actions`.

Replies expose a collapsed **Memory sources** disclosure with the exact retrieved summaries, IDs,
verification, freshness and source references. It uses escaped UI text and does not add a permanent panel.
These sources travel only with the session's ordinary tool-result events and existing bounded tab transcript;
no new global source browser or controller transcript store is introduced.

## Phase 3B: suggested next steps

Salie can recommend what to do next from policies, permissions, the repository, pull requests, tasks and saved preferences.
`recommend_next_actions` is a read operation that needs only `memory:read`; the evidence it gathers is limited by the principal's
other grants (`tickets:read`, `runs:read`, `projects:read`), and a session limited to some projects gets no personal memory.
The decision is made by fixed rules, not by the model, in a strict order: application restrictions, then Team policies and
repository protection, then the person's explicit decisions, then verified Git and task evidence, then habits and feedback.
Lower tiers can order what higher tiers allow and can never add what they removed. See [RECOMMENDATIONS.md](RECOMMENDATIONS.md).

* The operation returns the rules' own answer. The engine carries it to the screen as `tool.recommendations`, and the reply
  shows it as cards (what, why, the screen it opens, *Why this*, and *Useful / Not useful / Dismiss*), so the actions a person
  sees are never the model's words. The model is told to offer only what the result contains, to explain it in a sentence or two,
  to mention a standing restriction when it matters and to say when a source was unavailable.
* A suggestion starts nothing: every action opens an existing screen (a task, a branch, the changes, the Control Deck) where the
  existing confirmation applies. In the desktop app the assistant is a frame of its own, so a card asks the window to show the
  place (`werkbord.frame` `open` with target `personal` and a `#` place that passes the same validation as every other place).
* Team policies for a Team-linked project are read through the existing `retrieve_memory` desktop bridge, within the session's
  project allow-list. Without the bridge, the pull request route is assumed, never a direct merge.
* Feedback is recorded by the person pressing a button on a card (a REST call with the controller token), not by the model:
  there is no assistant operation that writes memory.

## Personal generative workspaces — Phase 3

Salie can create and revise persistent personal interfaces with three application
operations: `list_personal_workspaces`, `get_personal_workspace` and
`propose_personal_workspace`. The additional permissions are `personal:read` and
`personal:propose`: the first reads private definitions and schemas; the second
only proposes a definition and compatible schema changes for human confirmation.
Neither grants agent, Git, execution, Team or arbitrary network access. These
operations are available only on authenticated controllers to unrestricted owner
assistant sessions, never MCP or project-restricted sessions. Tokenless controllers
do not expose personal tools, matching the personal owner API. Read-only assistants receive only the read grant.

Ask “create a weekly to-do list with deadlines and priorities”, then choose
Universal, Individual, selected Team context IDs, or a combination. Salie proposes
a short name, an existing registry icon, registered layout/view components and
empty personal collection schemas. The fixed confirmation card previews the exact
interface and locations, exposes its fields and record controls, and saves the
workspace and all collections atomically only after **Confirm**. Preview controls
cannot write or approve. Generated OpenUI conversation forms still have no
approval capability. Creation uses a durable normalised-name key and stable
collection alias IDs; retries share pending proposals, and subsequent creation
attempts direct Salie to reopen the saved workspace rather than duplicate it.
Choose another name to deliberately create a distinct workspace.

Use **Edit with Salie** on a saved workspace, or name its exact `pws_…` ID in the
conversation. Salie fetches its current definition and collection schemas from
SQLite each time. An edit carries the exact workspace ID, current workspace
revision and every bound collection revision. Its preview reads existing records
locally for display; records are never sent to the model. Edits preserve collection
IDs, order, field keys/types, records and bindings. Name, icon, layout, visible
fields, saved equality filters, compatible schema additions and display locations
can change. In the desktop shell these buttons open the existing Salie frame using
only a bounded workspace ID, and the saved-workspace link returns to the local
workspace frame. No conversation or record text crosses this handoff. Removing fields/options, changing field types or making fields newly
required is refused with an explicit migration-review explanation. Phase 3 does
not implement destructive migrations.

Saved names, definitions and schemas are untrusted operation-result data, never
system instructions, approvals or permissions. Collection records have no assistant
read/write tool in this phase. The model cannot submit records or executable
source. Closed grammar validation runs before proposal storage and again inside
the atomic save transaction. Existing appops expiry, grant rechecks, proposal
ownership/digests and fail-closed audit apply unchanged; private proposal prose
is omitted from the audit detail.

Every valid workspace revision is retained in SQLite, including definitions saved
before this phase's migration. `get_personal_workspace` with `revision` retrieves a
historical definition with current collection schemas. Salie can propose that
definition as a rollback using the current workspace/schema revisions; it requires
the same preview and confirmation. Failed or stale updates retain the previous
valid definition and records. Memory may supply useful context, but never replaces
SQLite. Changing models, providers or conversations does not change saved data.

Verification: `TestSaliePersonalWorkspaceConversationCreateReopenModify` exercises
creation, human approval, record entry, model switching, reopening, a filtered
urgent section and moving progress to the top, another approval and unchanged task
IDs/values. `internal/appops/personal_test.go` covers durable deduplication,
rollback, invalid output, stale revisions, transaction failure, permissions and
audit/approval isolation. `node scripts/browser-personal-assistant.cjs` demonstrates
both complete flows with the real API, SQLite, engine and renderer using a scripted
provider, and captures desktop/mobile previews. No external model account is
required for these deterministic tests.


### Personal workspace lifecycle — Phase 5

Salie can propose `propose_delete_personal_workspace` with `keep_data` (delete the
interface, retain all personal collections) or `delete_data` (also delete only
exclusively owned personal collections). This narrowly extends the existing
owner-only `personal:propose` authority; it adds no core-task/project/Git/Team or
memory deletion permission. Model text cannot confirm anything. The fixed human
card loads the full authoritative deletion preview and verifies its fingerprint;
the UI and Salie execute the same `Personal.DeleteWorkspaceReviewed` operation.
Both recheck live revisions, record changes, ownership and workspace dependencies
inside the deletion transaction. Shared/unowned collections are explicitly kept.
A changed plan requires a fresh review and confirmation.

Retained data stays discoverable in the Personal data library and through
`list_personal_collections`. A future creation proposal may reuse a retained
collection by its exact saved ID, revision and unchanged schema. No records are
copied, reset or generated. Definition rollback keeps current collection schemas
and records. See [Personal workspaces](PERSONAL_WORKSPACES.md#phase-5--management-deletion-and-recovery)
for ownership, retention, backup limitations and the full endpoint contract.
