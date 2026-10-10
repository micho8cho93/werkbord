# The assistant: a conversation with your own coding agent, about your board

The assistant lets you talk to Claude Code or Codex, signed in the way you already signed them in, about your board:
what is blocked, what an agent is asking, what is planned for next week. It can **look** at the board and **propose**
changes; you confirm each change before anything happens.

This is the backend. There is no screen for it yet (the conversation view is a later stage). Everything here is reachable
through an HTTP API on this computer ([the API](#the-api)), so the screen, and later an MCP server, have one foundation.

```
 you ──▶ API (loopback, your token only)
            │
            ▼
        Engine ──────────────▶ Provider ──▶ claude / codex   (no tools, empty directory, your sign-in)
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

It cannot, by construction, not by instruction: start, stop or configure a run, touch Git or a repository, change a
setting, archive or delete anything, or give an agent permission to run a command. There is no operation for any of it
(`TestTheAssistantOffersNoRouteToRunsGitOrSettings`).

| Operation | Does | Needs | Changes something |
| --- | --- | --- | --- |
| `get_overview` | counts per project of what is running, waiting, blocked, failed, in review | `projects:read` | no |
| `list_projects` | the projects (no paths on your disk) | `projects:read` | no |
| `list_labels` | the reusable labels | `tickets:read` | no |
| `list_tickets` | a project's tickets, filtered | `tickets:read` | no |
| `get_ticket` | one ticket in full, with its latest runs | `tickets:read` | no |
| `get_schedule` | planned dates, scheduled starts, deadlines, dependencies, and the problems found in them | `schedule:read` | no |
| `list_blockers` | blocked runs (and why), failed runs, tickets waiting on dependencies | `runs:read` | no |
| `list_agent_questions` | what agents are waiting on | `questions:read` | no |
| `list_runs`, `get_run_status` | runs and how they are doing | `runs:read` | no |
| `create_ticket` | adds a ticket to a project's Backlog | `tickets:create` | **proposes** |
| `update_ticket` | changes a ticket | `tickets:update` | **proposes** |
| `answer_question` | gives an agent the answer it asked for | `questions:answer` | **proposes** |

"Ticket" is the board's card, which the rest of Werkbord calls a task. A Team ticket is reached only as the task it was
imported as (see [Team](#team-and-the-coordination-only-boundary)).

These are the same operations an MCP server will offer: one catalog (`appops.Service.Catalog`), one input schema each
(JSON Schema, enforced exactly as advertised), one permission each, one audit.

## How a change happens

1. The assistant's reply asks for `update_ticket`. The operation checks the permission, the arguments, that the ticket
   exists in that project, and its version; it works out **exactly** what would change and writes it in plain words
   (`Change ticket "Fix login" (tsk_…): moved backlog → doing; planned nothing → 2026-03-02 to 2026-03-04`). That text is made
   by Werkbord from the arguments, not by the model.
2. It is stored as a *proposal*, bound to the conversation that made it, with a fifteen-minute life, and the person is told
   (`confirmation_required`). **Nothing has changed.**
3. You confirm or decline: `POST /api/assistant/sessions/{id}/actions/{action}` with `{"decision":"approve"}`. Only this
   carries the change out, once, with the arguments that were shown (their digest is checked), as the assistant's own
   principal with its permissions as they are *now*, through the same domain service the app calls. A ticket changed by
   someone else in between is a conflict, not an overwrite.
4. The assistant is told what became of it with your next message (confirmed and done, failed and why, declined, expired).

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

- It has no tools. Both providers are run with theirs off (below), and a turn in which one is announced is ended.
- Its only effects are the operations above, and those only propose. The worst a fully manipulated model can do is put a
  harmless-looking proposal in front of you, which you read before confirming, in words Werkbord wrote.
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
  assistant at all, whatever its scope. The private network does not serve it.
- The operations reach the board **only through the domain services** (`internal/service`), never a table, Git, a runner
  or a process; the engine reaches it only through the operations; and `internal/archtest` fails the build if either imports
  more.

## Team and the coordination-only boundary

The assistant belongs to the Individual controller, on your computer, and runs *your* agent runtime with *your* sign-in.

- Team's build links none of it (`TestTeamLinksNeitherTheAssistantNorItsOperations`), so a Team workspace remains a place
  that coordinates and never runs a model or an agent. The controller still holds no Team credential and makes no call to Team.
- A Team ticket is visible to the assistant only as the task your own Werkbord imported it as (its `source` says where it
  came from), through the same integration as before. Anything the assistant changes, it changes on your board; Team learns
  of it the way it learns of any change you make: from your own Werkbord, if you have it reporting.
- Execution stays with your runner. The assistant never starts a run. Answering a question goes through the runner the
  run is on (`Runner.Answer`), exactly as answering in the app does.

## Providers

The assistant borrows what is already on this computer. It has no account, key or model of its own, and asks for none.
Each provider says whether it is installed and signed in, how, and what to do if not (`GET /api/assistant/providers`).
If a key in *your* environment makes the provider bill an API key instead of your plan, `usesApiKey` says so; Werkbord
neither adds nor removes it.

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
| Tools turned off | `--tools ""`, `--safe-mode`, `--strict-mcp-config`, `--disable-slash-commands`, `--setting-sources ""`. The `init` event lists `tools: []` and `mcp_servers: []`; if it lists any, the turn is ended | `--disable` of the tool features this Codex has, read-only sandbox, approval `never`. **Some tools cannot be removed** by any setting (`functions.exec`, `collaboration.*`, `request_user_input`); so every item of a turn is checked against an allow-list (message, reasoning, plan, compaction) and anything else interrupts and ends the turn |
| Native tool calling | Supported by the CLI (MCP servers). **Not used** | Supported (MCP servers, and "dynamic tools" the client registers). **Not used** |
| Usage | tokens, reported per turn (not stored, not priced) | tokens, reported per turn (not stored, not priced) |
| Voice | **Not supported**: Claude Code has no voice input or output when run headless | **Experimental**: the protocol has realtime audio (`thread/realtime/*`), gated by account support. **Not used** |
| Images and files | **Not used** in this stage | **Not used** in this stage |
| Deleting the provider's copy of a conversation | **Not possible** (no command); Werkbord forgets its handle | Possible in the protocol (`thread/delete`); **not used** |
| Where the conversation lives | Claude Code's own storage, under the assistant's private directory | Codex's own storage (`~/.codex/sessions`) |

**Why the assistant does not use native tool calling.** It could: both CLIs can call tools through MCP. It does not, for
three reasons: it behaves differently in each; it would hand the model a way to act that skips the engine; and the engine's
own protocol (below) makes the operations the same for every provider and, later, for MCP clients. Native tools through
the MCP server belong to the stage that builds it.

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

**Both CLIs change often.** `go test ./internal/assistant/provider/live -v` with
`WERKBORD_LIVE_ASSISTANT=1` repeats these checks against the real command lines (a handful of one-word turns).

## Choosing the provider, model and reasoning level

A screen offers all three, and can change them at any point, so that someone whose plan cannot use a model can move to one it can.
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
| `GET /api/assistant/providers` | what is installed, signed in, and which models and reasoning levels it offers; capabilities |
| `POST /api/assistant/sessions` `{provider, model?, reasoning?}` | start a conversation (fails at once, with what to do, if the provider is not ready) |
| `GET /api/assistant/sessions`, `GET …/{id}` | conversations, with the changes waiting and the last event number |
| `PATCH …/{id}` `{provider?, model?, reasoning?}` | change provider, model or reasoning level for the next turn (see below) |
| `DELETE …/{id}` | stop it, drop what it left waiting, forget it (the audit stays) |
| `POST …/{id}/messages` `{text}` → `202 {turnId}` | send a message; read the reply from the events |
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
    "auditRetentionDays": 400
  }
}
```

`readOnly` removes the permissions to propose changes (the assistant is not even told those operations exist). `projects`
limits what it can see. The commands, models and reasoning levels it offers are the ones set for the agents under `"agents"`
(`agents.claude-code.command`, `.model`, `.models`, `.reasoning`; the same for `codex`); `model` is what a conversation that
chose none gets.

## Testing

| What | Where |
| --- | --- |
| Operations: permissions, scope, validation, proposals, confirmation, expiry, double confirmation, tampering, audit chain, fail-closed, recovery | `internal/appops` |
| Engine: streaming, tool loop, injection, retries, stalls, timeouts, cancel, lost conversation, reconnection, isolation, recovery | `internal/assistant` |
| The tool-call protocol, cut at every possible point | `internal/assistant/protocol_test.go` |
| Providers against faithful fake command lines (flags, environment, stdin, classification, process-group kill) | `internal/assistant/provider/claude`, `…/codex` |
| Providers against the real command lines (opt-in) | `internal/assistant/provider/live` |
| HTTP: end to end, SSE resume, status codes, **no local-access token and no private network can reach it** | `internal/api`, `internal/controller` |
| The store: compare-and-swap, one claim per action, append-only audit | `internal/store/sqlite` |
| The boundaries | `internal/archtest/assistant_test.go` |

## Not in this stage

The conversation view, attachments, dictation and voice (stage 3); the resources library (4); analytics on model usage and
cost (5); the MCP server and external clients (6; it will use `appops.Service` with a principal of its own); native voice
and communication integrations (7).
