# Agent visibility, Team execution awareness and guided plans: implementation plan

Status: **plan only, nothing implemented.** Written 2 November 2026 against local `main` `a90422d` (one commit ahead of
`origin/main` `cb8d161`; clean tree). Re-run `git status` and `git log -3` at the start of every session and say which
commit you built on.

This is a working plan. Once the work ships, delete it: `docs/README.md` says completed plans live in Git history, and the
current guides (`DESIGN.md`, `WORK.md`, `AGENT_SESSIONS.md`, `TEAM.md`, `MOTION.md`) take over.

Reference mockups (direction and style, not pixel specifications) are in
[`agent-visibility/`](agent-visibility/): `mockup-01.png` to `mockup-09.png`. Section 3 says how each one is used and where it
must be corrected.

## 1. Goal and rules

Make Werkbord more informative and compact, with recognisable coding-agent identities, clear human ownership, awareness of
which agent is executing shared work, guided plan execution, and (later) safe provider continuity. Keep the Classic design,
the 224px sidebar, the project selector, Salie's place, and the existing destinations (Work, Calendar, Git, Agents,
Analytics, Settings, Help).

Hard rules, which override any mockup:

1. **No human presence.** No online or offline, no green availability dots, no last seen, no "working on…" statements about
   people, anywhere. People are shown by role and by what is assigned to them.
2. **Execution awareness is read-only.** A teammate's agent is shown, never controlled. Team never gains execution
   authority (`AGENTS.md`, `internal/archtest`).
3. **Only a confirmed, fresh running state animates.** A rotating ring surrounds the agent logo. Stale or unknown never
   animates and never silently becomes "done" or "offline".
4. **Private stays private.** No prompts, transcripts, questions, terminal output, environment, credentials, paths, usage or
   spending in anything Team shares.
5. **Reuse, do not duplicate.** No new monitoring server, no second execution-state store, no peer-to-peer runner access, no
   new polling of teammates' machines, no avatar upload.
6. **Honest wording.** Say only what the observed state proves ("last reported 6m ago", "agent-reported").

## 2. Decisions

Resolved on 2 November 2026:

| # | Decision |
|---|---|
| 1 | Plan branch chips from the mockups become **task branches and pull requests, de-duplicated**. Never invent a plan-level branch. |
| 2 | The "Tests · 2/2 passing" row is **removed**. It returns only if a reliable, controller-verified source for "the tests that should pass" exists. Today none does. (Session-stream lines such as "Agent-reported: 48 passed, 2 failed" stay, labelled as agent-reported.) |
| 3 | Show commit subjects where the data exists (ticket-linked commits, and members with GitHub access). Elsewhere show branch, short SHA and time, with provenance. |
| 4 | Agents page keeps the attention-first groups (Needs you, Needs a look, Running, Recently finished) and adds **plan sub-headers inside them**. Standalone sessions stay first-class. |
| 5 | Plan detail has **Tasks** and **Execution** tabs. The **Schedule** tab is left out for now (it would duplicate the Calendar link). Revisit if wanted. |
| 6 | The shell breadcrumb shows depth (All Projects › Work › Plans › *plan title*). Frames report their title to the shell through the existing `werkbord.frame` message. Drop the duplicate "Work / Plans" line above page titles. |
| 7 | The Git sidebar entry stays. The mockups omit it; they are wrong about that. |

Also resolved (2 November 2026, all recommendations accepted):

| # | Decision | Phase |
|---|---|---|
| D1 | A sender-side opt-out: a member can stop their runner sharing which agent they use (the connector omits `agentId`; peers then see a neutral mark with a genuine running state). | 4b |
| D2 | The run ID is **not** shared with peers. | 4b |
| D3 | Plan-run state is a small column on the work plan (one migration), not derived from task schedule keys. | 8 |
| D4 | For a Team plan, "Run plan" means "review and start *my* steps". Other members' steps show who they wait for. | 8 |
| D5 | Existing device and runner connection displays (`MemberConnections`, `Device.OnlineAt`, the Devices and People pages) are left alone but never reused in the new surfaces. A guard test enforces this. | 4b |

One reading still unconfirmed: decision 5 above (Schedule tab left out) rests on my interpretation of a bare "yes".

## 3. Reference mockups

| File | Shows | Adopt | Correct |
|---|---|---|---|
| `mockup-01` | Plan detail, Personal | Header summary and cluster; ordered tasks with agent, state, holder; right-hand context column (progress, agents, Git) | "Who's working on it" has green and hollow dots, so show **role and assigned tasks, no dots**. Photo avatars become initials. Roles ("Engineer", "Tech Lead") must be real project roles. The plan branch and PR chips become task branches (decision 1). Tabs: Tasks and Execution only. The grouped-by-state layout is not the default (see section 4.5). |
| `mockup-02` | Team Work overview | Agent marks with rings, holder avatar and "Held by", plan cards with per-step holder and agent | "6 online", dots, and "Team at a glance" become **Participants** (role, assignments). The decision card must show **only the viewer's own questions, approvals and reviews**; a peer's agent question (text, "needs approval…") is private. For peers show only "Waiting for Maya". Drop the "Shared" chip (a ticket has one holder). The sidebar shows "Team" where Salie belongs (error). Small spinner beside the label becomes the ring around the logo. |
| `mockup-03` | Queue & Plans with a plan run | Step list with `Step n of N · After step n-1`; Sequential selector; "dependencies enforced · reviews required"; Running now panel | "Team presence … online / Available / Working on review" is removed. `Run plan` and `Pause` shown together become **one state-driven control**. "Step 2 of 5 · 2 running" on a sequential plan is wrong ("1 running"). Remove the Tests row (decision 2). Keep the tab order Overview, then Queue & Plans. |
| `mockup-04` | Continue with another agent? | The whole of Phase 9a | The Approve button states the cost basis and is disabled unless the previous run is confirmed stopped. "Still to verify" and "Completed by" are agent-written handoff text, so label them **agent-reported**. |
| `mockup-05` | Agent session with Work context | Work context panel (plan progress, "Next after this"); grouped activity rows; Execution details | Keep Back trail and the existing beside-the-list layout. The right panel appears only when the window is wide enough. |
| `mockup-06` | Agents & connections popover | Layout, and the usage block (reported API charges, estimated API equivalent, "balances and reset times are not available") | "Available" chips become **Signed in / Installed**, and only if the app detects them. "Limit unknown" is fine. |
| `mockup-07` | Overview | No stat cards; decision, Running now, Git activity, Plans | Rows must show the ringed mark, not a static one. |
| `mockup-08` | Queue & Plans | Flag chips; Now, Next, Later with counts; Plans with progress and agent marks; Provider activity | Plan agents and counts come only from confirmed state. |
| `mockup-09` | Agents overview | Runner panel; counts; plan context on sessions | Attention-first groups with plan sub-headers (decision 4). |

## 4. Visual specification

### 4.1 Shared primitives (Classic baseline; all seven styles in light and dark)

- **Agent mark.** The logo on a white circular tile (as `BrandMark` does, so dark marks stay legible), 18px compact or 24 to
  28px large. Unknown providers get a neutral letter tile. The logo never moves.
- **Ring.** A reserved box of 26 to 28px compact, 32 to 36px large, so state changes cause no layout shift. A 2px border in
  `--accent` over a faint full-circle track. The rotation is one `transform` on a pseudo-element, 1.8s linear, continuous,
  compositor-only. It runs only under `prefers-reduced-motion: no-preference` and pauses when the document is hidden
  (MOTION.md).
- **States**

| State | Treatment |
|---|---|
| Running (confirmed, fresh) | Rotating accent ring |
| Starting | Static dashed accent ring |
| Waiting for you | Static amber ring and dot |
| Blocked | Static purple dashed ring (`--block`) |
| Completed | Static green tick badge |
| Failed, stopped | Static red or neutral badge |
| Stale, unknown | Neutral ring at reduced opacity, no animation, "last reported 6m ago" |

  Reduced motion shows a static 270° arc. Every state also has text or an `aria-label` ("Codex, running").
- **Avatar.** A circular initials tile: 20px compact, 24px default, 32 to 36px in expanded summaries; the colour comes from a
  hash of the member ID using theme tokens; the viewer's own avatar keeps the cobalt tint. No upload.
- **Assignment group.** `[ringed mark] Codex running` and `[avatar] Held by Maya` stay as two separate groups, agent first.
  The human group is always visible when a holder exists. Labels: Held by, Assigned to, Review · Name.

One pure resolver decides the state for all three renderers. `animate` is true only for confirmed running that is verified:
for Personal, the controller is live and (for a remote run) the runner is online (`runReportStale`); for Team peers, the
server-computed `fresh` flag, never the client clock alone.

### 4.2 Global header (`ContextStatus.svelte`, 40px, one row, hairline bottom)

- **Left:** breadcrumb with depth (decision 6): `All Projects › Work › Plans › Authentication overhaul`. The workspace tag
  appears only when more than one workspace exists. Existing unavailable-workspace notices stay.
- **Right:** `● 2 running · ● 1 needs you · ⎇ main · 7m ago`, then an agent-mark cluster (up to 4 marks, `+N`) with a chevron.
  When the context includes Team projects, a people cluster (up to 4 initials avatars, `+N`) follows.
  - "N running" counts confirmed fresh running work (own, plus fresh peer reports in a Team context), via `executionCounts`,
    which already treats stale as unknown. "N needs you" counts waiting decisions and attention for the viewer.
  - Git text appears only when one repository is unambiguous. With several repositories it is omitted or reads
    "3 repositories". Team Git keeps its provenance ("Reported by Maya 3m ago · unverified", "From your GitHub sign-in").
  - Each group is a button opening an anchored popover (`aria-expanded`, Esc closes, focus returns, not modal). The agents
    popover lists `[ringed mark] Codex · task · [avatar] holder · state · age`, each row opening the canonical session (own)
    or the permitted Team task (peers). The people popover lists name, role and assigned tasks; there is no presence anywhere.
- **Responsive:** at 1280px and above everything shows; 900 to 1279px drops the SHA and age; 640 to 899px (the 64px rail)
  turns Git into an icon button; below 640px, including 390px, one "Context" button holds all sections.

### 4.3 Work, Overview (`Hub.svelte`; mockups 07 and 02)

- Remove the five stat cards (`stats`, lines 72 to 78; tray, lines 146 to 151; their CSS). Fold counts into section headers:
  "Waiting for you 2", "Running now 3", "Up next · 5 ready". The `flags` chips keep their navigation behaviour.
- **Your next decision** stays the strongest element, oldest first. Add the agent mark, the holder avatar (Personal and
  Team-own), time waiting, and the existing primary action. In Team context it covers only the viewer's own items.
- **Right column** (at least 1100px, else stacked): Running now, Up next, Plans (compact, with agent marks), Git activity (the
  existing Repository card, compact), and in Team context a Participants summary (role and assignments, never presence).
  "Compact" mode (a pane is open) stays one column.
- **Rows** are 44 to 56px: 14px/500 title, 12px context line (project › plan), then agent group, state text, holder avatar.
- Catch Up, the brief and "Questions and workspace notices" stay in the main column, collapsed where they are today.

### 4.4 Queue & Plans (mockups 08, 03)

- Row grid: `[grip 22] [title + context 1fr] [agent 28] [state auto] [avatar 24] [open 22]`. Drag, Alt+Arrow, keyboard, review
  behaviour and ordering are unchanged. A row someone else holds keeps its lock and reason.
- Plan summary rows: title, `3 / 8`, 5px progress bar, active agent marks (max 3), responsible avatars (max 3), amber dot
  for attention.
- For a selected project this page is rendered by the Personal frame (`TaskCard.svelte`, `WorkPlans.svelte`) or the Team
  console (`console.js`); under All Projects it is `Hub.svelte`. All three use the shared identity layer.

### 4.5 Plan detail (mockups 01, 03)

- Tabs: **Tasks** and **Execution**. Layout at 1100px and above: main column plus a 280px context column; below that the
  context column stacks.
- **Tasks:** plan title, description, progress, then **ordered steps** (the plan's identity is its order):
  `n  Title · Step n of N · After step n-1`, agent mark with ring when running, state text, holder avatar with a `Review` chip
  where a reviewer is named, dependencies and blockers. The existing "not in order" notice stays for unchained plans.
  A "Parallel" label appears only when two running steps have no dependency path between them.
- **Execution:** the run review (section 6, Phase 8), run state, and provider status.
- **Context column:** task branches and pull requests (decision 1), participants with roles, agents executing now, recent
  commits (decision 3).
- Plan progress is `done / total` tasks. A successful session exit never counts as completion.

### 4.6 Team Work and Team plans (mockup 02, console.js)

- Rows: holder avatar with "Held by Maya", and (when a fresh report exists) `[ringed mark] Codex running`. An unclaimed row
  shows "Available". "Assigned to Jamie" appears where an assignee has no claim.
- A stale report shows the provider mark neutral: "Codex · last reported 6m ago". It is not marked done or offline.
- Clicking a peer's agent opens the ticket (permitted Team context). It never opens or attaches to a runner.
- Team plan detail adds participants with roles, and a holder and agent per step.
- "Team at a glance" becomes: Participants (avatar, name, role, assigned count), Agents (marks with rings and counts from
  fresh reports), Git (reported, with provenance).

### 4.7 Agents (mockups 09, 05)

- Keep the attention-first groups (decision 4), with plan sub-headers ("Authentication overhaul · 2 of 5 tasks complete").
  Replace the dot and spinner on `SessionCard` with the ringed mark; add the task › plan line.
- "Right now" counts and the runner list stay (own runners only).
- The Team strip in `Agents.svelte` gets marks and the existing `reportedLine` text, and stays view only.
- **Session:** keep the beside-the-list layout and Back trail. At wide widths add a Work context panel (plan progress,
  "Next after this") and Execution details (runner, branch, session key, usage if known).
- **Stream:** collapsible groups (reads, edits, tests, Git, errors, approvals), raw output and order preserved, approvals and
  questions never collapsed. Group only what the adapter reports as a tool kind; otherwise leave it chronological. Label
  outcomes **Agent reported** or **Verified by Werkbord** (for example a controller-verified merge).

### 4.8 Connected tools popover (mockup 06; Personal context only)

Header-right popover "Agents & connections" with Settings link. Sections: Coding agents (mark, name, "Signed in" or
"Installed" only if detected, own active sessions, "Limit unknown" where no notice is known), Connected tools (`BrandMark`,
verified state from `testedAt`), and Usage this period from the existing analytics (reported API charges and estimated API
equivalent kept separate, with the caveat line). Show configured entries only. No remaining allowance, reset dates or costs
are invented. Nothing here enters a Team report.

### 4.9 Handoff dialog (mockup 04; Phase 9a)

Title "Continue with another agent?", a "Needs review" chip, an explanation that work is retained and nothing has been
dispatched, a fact table (Completed by, Still to verify, Current branch, Proposed provider; the first two marked
agent-reported), a caution about confirming the previous run has stopped and reviewing external effects, and three actions:
Wait for *Provider*, Review handoff, Approve *Provider* (primary; states cost basis).

## 5. Architecture

1. **Shared identity layer.** New `internal/uipresentation/static/identity.css` and `identity.js` (+ `.d.ts`): agent mark,
   ring, avatar, assignment group, and the pure state resolver. Svelte wrappers `AgentMark.svelte`, `Avatar.svelte`,
   `AssigneeChip.svelte`; `web/src/lib/agent-marks.ts` maps a catalog ID to `{path, colour}` (Simple Icons CC0 data, same
   source and trademark note as `brand-marks.ts`; letter tile otherwise). Agent IDs: `claude-code`, `codex`, `gemini-cli`,
   `opencode`, plus those in `internal/agentcatalog/agents.json` (for example `cursor`, `github-copilot-cli`). Verify each
   mark exists before promising it. The Team console loads the same CSS and JS.
2. **Neutral summary contract** (`internal/workspace/workspace.go`, `werkbord.workspace/v1`). Add: an optional agent reference
   on `Item` and `QueueItem` (`id`, `state`, `observedAt`, `fresh`), `Question.agentId`, and `Project.people[]`
   (`id`, `name`, role; at most 12; no email). Validate in `Clean()` and `valid()`. `Decode` rejects unknown fields
   (`workspace.go:720`); that is acceptable because the app and its services ship together. The header derives everything
   from the existing scoped `contextOverview`: no new polling, no new endpoint.
3. **Git in the header.** Team: `Summary.Pushes`, ticket-reported branches and commits, and Team GitHub G1 for members who
   have it. Personal: `GET /api/projects/{pid}/git` fetched once per project selection and on focus, never on an interval.
   Confirm that `GitLocal` carries the current branch and HEAD; if not, extend that payload, not add an endpoint.
4. **Team execution awareness without remote-runner access.** The owner's Individual runner already produces snapshots and the
   owner's connector already reports them with ordering, claim fencing, device authentication, digests and freshness
   (`ReportProgress`, `team/service/progress.go`). The smallest extension:
   - **Sender.** Add `AgentID string \`json:"agentId,omitempty"\`` to `integration.Execution` (`internal/integration/contract.go`),
     set from `Run.AgentID` in `executionMetadata` (`internal/service/integration.go:325`). Validate as a short lowercase
     slug (not against the catalog, which changes). Update the comment at `integration.go:359` that says snapshots expose
     no agents. Report `starting` distinctly (today `RunStarting` maps to `"queued"`). It is stored in the existing
     `ticket_progress.document` JSON, so **no migration**.
   - **Server projection.** A new `Board.Agents map[ticketID]PeerExecution` with only
     `{ agentId, state, observedAt (server clock), fresh }`. Holder, project and ticket are already on the ticket.
     `fresh` is true only if: the report belongs to the current claim (assignment and `ClaimAt`), the device is unrevoked and
     the holder's, `now − reportedAt ≤ DeviceOnlineWindow` (2 minutes), and `Execution.RunnerOnline` is true. This also fixes
     today's inconsistency, where `executionReportOf` honours `RunnerOnline` and `activeAgents` does not.
   - **Selection.** The newest fresh record for the current claim. A provider switch is a new run, so the per-device row
     is replaced and the old agent disappears; a replay of an older sequence cannot revert it.
   - **Compatibility.** `Board.active` stays, derived from the same projection. Old connectors send no `agentId`, so peers
     see a neutral mark with a true running state.
   - **Never shared with peers:** run ID (D2), branch and commit beyond what ticket Git metadata already shares,
     `runnerOnline`, handoff, summary, questions, prompts, paths, usage. The owner-only `/progress` endpoint is unchanged.
   - **Performance.** `activeAgents` queries once per in-progress ticket. Batch it by project in Board and Overview.
   - **UI states:** requested (schedule `awaiting_approval`), queued, starting, running, needs input, blocked, completed,
     failed, stopped, stale or unknown (derived from `fresh = false`; Team never invents "offline").
5. **Plan execution is a thin coordinator over the existing scheduler.** Plans own no runs (`domain/work_plan.go`);
   dispatch is per task through `Orchestration` and `Scheduler.Plan`. "Run plan" arms eligible tasks one-shot in one
   transaction, guarded by plan and task versions plus a preview digest (the pattern in `EXECUTION_COORDINATION.md`).
   `ScheduleLoop`, `Start` and the unique `(task_id, schedule_key)` index keep dispatch durable and duplicate-free.
6. **Where Team UI lives.** Team Work and plans are vanilla JS in `internal/team/console/static/console.js` (the card at
   about line 1421, `ownerChip` at about 1457, `presence()` at about 1407). Rename `presence` (it is about an agent, but the
   name invites confusion with human presence).

## 6. Phases

Each phase leaves the app working and ends with `make check` and its browser scripts. Commit locally; no version bump,
tag or push unless asked (`AGENTS.md`). Use British English in new copy and "Salie".

### Phase 1: Shared components (built inside R1; do not ship alone)

- Files: `identity.css`, `identity.js`, `identity.d.ts`; `AgentMark.svelte`, `Avatar.svelte`, `AssigneeChip.svelte`,
  `agent-marks.ts`; `DESIGN.md`, `MOTION.md` sections.
- Tests: vitest truth table (state × fresh × reduced motion); extend `contrast.test.ts` over every palette for ring, arc and
  tile; a `browser-motion.cjs` scenario (animation on only for running, off under reduced motion, no layout shift).
- Accepts scenarios 1 to 4, 19, 20.

### Phase 2: Header and Work overview

- Files: `ContextStatus.svelte` (container); new `ContextGit`, `ContextAgents`, `ContextPeople` and `context-header.ts`;
  `Hub.svelte`; `shell/types.ts`; frame title message; `internal/workspace/workspace.go` (+ tests);
  `internal/service/workspace.go` (Personal sets agent from the latest run); `daemon_summary.go` (pass-through).
- Tests: `workspace_test` for `Clean()`; `context-header.test.ts` (All Projects with two repositories gives no branch; Team
  Git keeps "reported · unverified"); extend `browser-project-context.cjs`, `browser-work-layout.cjs`,
  `measure-legibility.cjs` at 1440, 1280, 900, 640 and 390.
- Accepts 9, 14, 18, 19; stat cards gone.

### Phase 3: Task and plan interfaces (Personal and shell)

- Files: `TaskCard.svelte`, `WorkPlans.svelte`, `work-steps.ts` (agent id, parallel flag), `work-cards.css`, `work.css`,
  `Hub.svelte` rows. Plan detail gets Tasks and Execution tabs (Execution is a placeholder until Phase 8).
- Tests: `work-steps.test`; `browser-work.cjs`, `browser-work-agents-ux.cjs`, `make test-work-order-browser`.
- Accepts 1 to 3, 16, 17.

### Phase 4a: Team contract, dark launch (built inside R1)

- Files: `integration/contract.go` (`AgentID`, `starting`, validation); `service/integration.go`; a tolerant `domain.Progress`
  decode on the host. The connector sends nothing new yet. See risk 1: this must reach every host first.

### Phase 4b: Team execution awareness (backend)

- Files: `team/service/progress.go` (peer projection, batched query), `tickets.go` (`Board.Agents`), `work.go` (Overview
  queue agent), `daemon_summary.go`, `team/connector/connector.go` (send `agentId`, version-gated), `workspace.go`.
- Backend tests, by what they prove:
  - Report authentication: no device, revoked device, or wrong member returns 401 or 403.
  - Replay: same sequence and digest changes nothing; same sequence with a different digest returns 409; an older sequence
    is ignored.
  - Claim fence: a report from before a re-claim never appears.
  - Freshness boundary at exactly 2 minutes and 2 minutes plus 1ms; `RunnerOnline=false` is not fresh.
  - Provider switch A to B shows B, and an old replay cannot show A again.
  - Authorisation: a non-member, a member without `tickets.view`, an archived project and another workspace get no
    `Agents` and no member details.
  - Minimisation: the peer JSON key set equals the allowlist exactly, using the field-name walker pattern from
    `device_test.go`; prompt, path, environment, run ID and `runnerOnline` never appear.
  - Schema: a report without `agentId` still decodes; a malformed slug returns 400.
  - Presence guard (D5): the new types contain no `online`, `lastSeen`, `presence` or `available` fields.
  - Parity: `Board.active` equals a live state in `Agents[id]` with `fresh`.
  - Connector: a snapshot includes `agentId`; with an older host, `agentId` is withheld and reporting continues.
- Accepts 7, 8, 10, 12, 13.

### Phase 5: Team Work and plan UI

- Files: `console.js` (card, plan detail, `ownerChip`, rename `presence`), `console.css`, `workspace.css`, shared identity
  assets served to the console.
- Tests: `browser-team.cjs`, `browser-team-work-order.cjs`, `make test-team-desktop-browser`, and a two-member scenario
  (Maya's fake runner reports Codex running; Lucas sees the ring; a stale report removes it).
- Accepts 5, 6, 7, 11, 12 and the Team half of 9. Includes the decision-privacy rule (section 3, mockup 02).

### Phases 6 and 7: Agents, stream and connected tools

- Files: `SessionCard.svelte`, `AgentSessions.svelte`, `agent-overview.ts` (plan context), `FeedView.svelte`, `feed.ts`
  (grouping), `Agents.svelte`, new `ConnectedTools.svelte`.
- First audit which tool-kind events the adapters actually emit (`feed.ts`); group only those.
- Tests: `browser-agent-sessions.cjs`, `browser-shell-agents.cjs` (no duplicate session keys, Back trail intact); an
  analytics-fixture test that no remaining-allowance field renders.
- Accepts 15, and Personal usage never reaches Team reports.

### Phase 8: Guided plan execution

- Backend: a new `PlanRun` service with `Preview`, `Run` and `Hold`.
  - **Preview** lists ordered tasks, their `SchedulingDecision`, dependencies, eligible agents, execution policy, review
    gates, runner and capacity constraints, billing note, and interruption behaviour (from `EXECUTION_RECOVERY.md`).
    A mode selector chooses "in order" (chain dependencies) or "as each becomes ready".
  - **Run** arms eligible tasks one-shot, guarded by plan and task versions and the preview digest; dispatch stays with the
    existing loop.
  - **Hold** ("Pause after current task") disarms undispatched tasks and never touches a running process (adapters support
    stop and restart only). One control cycles Run plan, Pause after current task, Resume.
- Files: `internal/service` (new), `internal/api` (three routes), `WorkPlans.svelte` (Execution tab), plan-run state (D3).
- Tests: reconnect and restart create no duplicate runs; a stale digest is refused; chained and unchained ordering; a Team
  plan can never dispatch another member's step; extend `browser-work.cjs` and the plan browser suite.
- Accepts 16, and no duplicate runs.

### Phase 9a: Provider continuity (assessment only)

- Show Interrupted or Blocked with provider and `ProviderKind`; stop the ring only when the stop is confirmed (`Recovery =
  intervention` means unknown, not stopped). Offer Review handoff, Choose another provider (existing continuation, which never
  replays), or Wait. Implements section 4.9.
- **Phase 9b** (bounded automatic fallback) is deferred until 9a is proven: it needs per-project opt-in, a verified handoff, a
  budget cap and an idempotency key.

## 7. Risks and tradeoffs

1. **Version skew (high).** The Team API decodes with `DisallowUnknownFields` (`internal/httpkit/httpkit.go:49`). A newer
   connector that sends `agentId` to an older Workspace Host gets a 400. The connector only suspends on 401, 403, 404 and 409,
   so it retries forever, and **all** progress reporting stops, not just the new field. A host can be another member's
   machine, so skew is realistic. Ship the tolerant decode first (4a), and have the connector send `agentId` only when the
   host is known to accept it, with a "withhold the field and retry on 400" fallback.
2. **Renderer drift.** Three renderers need one resolver and shared CSS. Add a golden-fixture test that all three produce the
   same state and `aria-label`.
3. **Honest flicker.** Rings vanish when a laptop sleeps or the Team service stops. That is intended; the label says
   "last reported".
4. **Sharing which agent someone uses** is mildly sensitive. D1 makes the sender decide.
5. **Presence-like signals already exist** (`MemberConnections`, `Device.OnlineAt`). Out of scope, never reused (D5).
6. **Performance.** Batch the per-ticket progress queries; rings are compositor-only but test about 200 rows; never poll
   `GitOverview`.
7. **Unverified assumptions to check first:** Simple Icons coverage for each provider; the shape of `GitLocal`; how the
   console loads shared `uipresentation` assets; the adapters' tool-kind taxonomy.
8. **Simplifications taken:** no run ID to peers, no new endpoint, table or poller for awareness, no avatar upload, People
   reuses `Board.People`, one shared popover, Schedule tab and Phase 9b deferred.

## 8. Releases

| Release | Contents | Risk |
|---|---|---|
| R1 Identity and overview | Phases 1, 2, 3, 4a | Low: Personal UI plus an invisible contract |
| R2 Team execution awareness | Phases 4b, 5 | Medium: new shared data, version skew |
| R3 Agents and tools | Phases 6, 7 | Low to medium: stream taxonomy |
| R4 Guided plan run | Phase 8 | High: dispatch semantics |
| R5 Provider continuity | Phase 9a (9b later) | Medium to high |

## 9. Acceptance scenarios

1. A locally running Codex task shows Codex's mark with a rotating ring.
2. A locally running Claude Code task shows the same treatment.
3. The ring disappears when execution completes or becomes unverified.
4. Reduced motion gives a static, accessible alternative.
5. A Team task shows its human holder.
6. A Team task shows holder and agent together.
7. Another authorised member sees the right agent and running state when a fresh report exists.
8. A provider change is reflected without confusing old and new runs.
9. No teammate online or offline status appears anywhere.
10. No prompts, transcripts, files or credentials are in Team reports.
11. Team users cannot control another member's runner through these surfaces.
12. A stale observation no longer animates.
13. Unauthorised projects expose no reports or member details.
14. All Projects with several repositories invents no global branch.
15. Sessions keep canonical identity and navigation.
16. Plan progress reflects real task completion.
17. Reordering and approvals still work.
18. Readable and functional at 1440, 1280, 900, 640 and 390px.
19. All styles and dark modes stay consistent.
20. No extra polling or reflow from animation.

## 10. Implementation checklist

- [ ] 0. Decide on `a90422d` (push or not). Branch from it. Record the commit in each session's notes.
- [ ] 1. Add `identity.css`, `identity.js`, `.d.ts`, `AgentMark`, `Avatar`, `AssigneeChip`, `agent-marks.ts`. Run vitest and the contrast test.
- [ ] 2. Document ring and avatars in `DESIGN.md` and `MOTION.md`; add the motion browser scenario.
- [ ] 3. Extend `workspace.go` (agent ref, `Project.people`, `Question.agentId`) in `Clean()` and `valid()`, with tests.
- [ ] 4. Personal summary: set the agent from the latest run; `starting` as its own state.
- [ ] 5. Build the header (summary text, clusters, popovers, breadcrumb depth, frame title message) and its breakpoint tests.
- [ ] 6. `Hub.svelte`: remove the stat grid and CSS, fold counts into headers, add agent and holder to rows and the decision card, set the right column.
- [x] 7. `TaskCard`, `WorkPlans` (ordered steps, Tasks and Execution tabs), `work-steps.ts`. Run `make test-work-order-browser` and `browser-work*.cjs`.
- [ ] 8. **4a:** `AgentID` and `starting` in `integration.Execution`; tolerant host decode. Nothing visible.
- [ ] 9. **4b** (D1 opt-out setting, D2 no run ID): peer projection and `Board.Agents` with a batched query; version-gated connector send; all Phase 4 tests including the presence guard.
- [ ] 10. **5:** console card, plan detail, Participants panel, own-items-only decision card, rename `presence`; run `browser-team*.cjs` and `make test-team-desktop-browser`.
- [ ] 11. **6 and 7:** `SessionCard`, `AgentSessions` (plan sub-headers), `Agents.svelte` strip, session Work context panel, feed grouping after the taxonomy audit, `ConnectedTools`.
- [ ] 12. **8** (D3 plan-run column and migration, D4 Team "review my steps"): `PlanRun` preview, run, hold; restart and duplicate-run tests; Execution tab.
- [ ] 13. **9a:** interrupted-state UI and the handoff dialog.
- [ ] 14. After each phase: `make check`; `make web web-embed web-shell`; `node scripts/measure-legibility.cjs`; `go test ./internal/team/... ./internal/workspace/... ./internal/archtest/...`; screenshots at 1440, 1280, 900, 640 and 390.
