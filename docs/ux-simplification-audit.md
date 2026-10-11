# UX simplification audit

October 2026, against `werkbord-v4.12.3-preview.1` (commit `c96aa96`). Scope: the Individual app (`web/src`), the desktop
window's shell (`web/src/shell`), and, from source only, the Team console. The goal is a simpler, more truthful product
that keeps every capability, not a redesign.

## How this was done

- **Rendered:** the Individual app from source (`npm run dev`) against the disposable browser fixture
  (`scripts/browser-fixture`, real controller code, a scripted agent and a throwaway Git repository). It had one finished
  run with a branch, one run waiting on a question, four backlog tasks, and a work project. Screens inspected at
  1440×900 in dark and light, and at 375×812: Board, task panel (finished, waiting, not started), New task, Control Center,
  project Overview, Calendar, Timeline, Git, Runs, Projects, Settings (Task defaults, Runners, Routing rules, This
  computer) and the assistant.
- **Read only:** the desktop shell (Sidebar, Inbox, My Work, Workspaces, Add a Team), the Team console
  (`internal/team/console/static`), and the assistant conversation. The fixture switches the assistant off, and the
  shell needs the native window, so none of these were rendered here.
- **Not present yet:** Documents (the resources library, roadmap stage 4) and Analytics (stage 5). The only reporting
  today is the Usage panel in Control Center and each run's usage line, and that is what was assessed.
- **Method:** Impeccable's *critique* heuristics, *distill*, *quieter*, *clarify* and *layout* (organize) guidance,
  applied in one context (no sub-agents were requested). The deterministic detector (`impeccable detect`) ran over
  `web/src`: 4 findings. Two are false positives (a QR `<img>` with a dynamic `src`, and a test string); two are real
  (3px coloured left borders on two assistant cards, see A3).
- Friction that only users can confirm is marked **hypothesis**. Everything else was seen on screen or in the code.

## What already works and must be kept

- **Honest status.** Run state, runner online/last seen, "Last reported:" when live updates drop, "awaiting
  acknowledgment" for remote runners, and "Recorded test notes — not independently verified" all come from real data.
  The connection banner says that agents may still be running while the page is disconnected.
- **Answer where it is shown.** A question can be answered on its board card, in Control Center, in the banner's target
  and in the task panel. Answer failures say whether it is safe to try again.
- **Guarded repository actions.** The merge sheet shows Git's own simulation, re-checks at confirm time and refuses if
  anything moved. Push and merge results keep "on this computer" apart from "confirmed by the remote".
- **Recoverable creation.** New task keeps a saved task when starting the agent fails, and says when it cannot tell
  whether the save happened.
- **Requirements-first onboarding** (since `4.12.3`): three verified requirements, one next action, optional
  connections folded away. This resolves the first-run finding of the earlier journey audit.
- **Progressive execution settings.** Every picker says what leaving it alone means ("Same as the project"), and
  advanced choices sit behind "Change execution settings".
- **One visual vocabulary.** Paper/ink, one action colour and one attention colour are used consistently. The amber
  "needs you" treatment is the only loud thing on most screens, as it should be.

## Findings by screen

Priority: **Critical** (blocks or misleads on an essential task), **High**, **Medium**, **Low**.
Principles: *Critique* (Nielsen heuristic named), *Distill*, *Quieter*, *Clarify*, *Organize*.

### Navigation (rail, phone tabs, desktop sidebar)

**N1. Phone tab bar hides Runs** · Critical · *Critique: visibility, consistency; Organize* · **implemented**
- *Problem:* the phone tab bar is a fixed 5-column grid, but it now carries six tabs (Needs you, Board, Calendar,
  Timeline, Git, Runs). Runs wraps to a second row underneath the bar and the safe area, with only its icon peeking
  out (seen at 375×812).
- *Why it matters:* a primary section is unreachable on the phone, and the bar looks broken.
- *Proposal:* size the grid from the number of tabs, so every tab gets an equal column at every width.
- *Benefit:* all sections are reachable again, with no change to the other layouts.

**N2. One place, four names** · Medium · *Clarify: consistent terminology* · **partly implemented**
- *Problem:* the cross-project attention page is "Control Center" in the rail and its title, "Needs you" on the phone
  tab and in the desktop sidebar, "Needs input" as its filter, and the desktop shell's version is headed "Needs
  attention".
- *Why it matters:* a person cannot tell whether these are the same place. The heading should repeat the word they
  clicked.
- *Proposal:* use "Needs you" for the attention list everywhere a person navigates to it. The desktop shell's page
  heading should match its sidebar label. Renaming Control Center itself is a product decision (it also holds
  projects, runners and usage), so it is left as a recommendation.
- *Benefit:* recognition rather than recall.

**N3. "Everywhere" duplicates the workspace for a person with one workspace** · Medium · *Distill; Organize* ·
**hypothesis**
- *Problem:* in the desktop window the sidebar always shows "Everywhere: My Work, Calendar, Needs you" above the
  workspace's own Control Center and Projects. With only the Individual workspace (most people), Needs you and
  Control Center show the same items, and Everywhere's Calendar overlaps the project calendars.
- *Proposal:* show the Everywhere group only once a second workspace exists, or fold it into the switcher.
- *Recommendation only:* it changes navigation people may already use.

**N4. Six project tabs** · Low · *Distill; Organize* · **hypothesis**
- *Problem:* Overview · Board · Calendar · Timeline · Git · Runs. Calendar and Timeline both start from the same list
  of "unscheduled"/"not planned" tasks (see C1).
- *Proposal:* keep all six for now. Revisit after C1, because the overlap is in naming more than in function.

### Board and tickets

**B1. Card noise on every backlog card** · Medium · *Distill; Quieter* · **implemented**
- *Problem:* each backlog card repeats "Not started", a Start agent button and an "Any agent" chip. Four cards means
  twelve identical lines. "Any agent" says that nothing was chosen, which is the default, so it carries no
  information.
- *Proposal:* show the agent chip only when it says something: a chosen agent, or the agent of an existing run. Keep
  "Not started" (it carries the hollow dot that distinguishes the card's state) and keep the Start button (the weekly
  action).
- *Benefit:* the cards that differ stand out (Von Restorff), with no information lost.

**B2. Finished work needs a manual "Move to Review"** · High · *Critique: user control vs efficiency* ·
**recommendation**
- *Problem:* a completed session stays in Doing with "Session finished · ready to review" and a Move to Review button.
  Every finished run costs an extra click before review can start.
- *Proposal:* move a task to Review automatically when its run completes with commits, and keep Doing for runs that
  finished without changes or that the person wants to continue.
- *Recommendation only:* it changes task-state rules shared with the Team bridge and the scheduler, so it needs a
  product decision.

**B3. Merging outside the Board leaves the task open** · High · *Critique: consistency; Peak-end* · **implemented**
- *Problem:* merging from the board card moves the task to Done. Merging the same branch from the task's "Review
  changes" path (branch page → Merge…), which is the path the run result leads to, does not. The task then stays in
  Review with its work already merged.
- *Proposal:* whichever path a merge comes from, move the branch's own task to Done after a successful local merge.
  Say so in the result, and offer to put it back.
- *Benefit:* the loop closes the same way every time, and the board stays true.

**B4. Starting an agent when every runner is busy** · High · *Clarify: errors; Critique: match system/real world* ·
**implemented**
- *Problem:* when the only runner is at capacity, the task panel says "No coding agent is ready. Codex: unavailable ·
  Disposable process fixture: unavailable" in red, although the agent is installed and working on another task.
  Pressing Start agent on a card shows the raw server text "conflict: waiting_capacity: Project concurrency limit
  (1) is occupied by run run_… on runner rnr_…".
- *Why it matters:* the person is sent to Settings to fix an agent that is not broken.
- *Proposal:* tell "busy" apart from "not set up". When agents exist on runners that are only full, say "Every runner
  that can take this task is busy" with what to do: wait, or stop a run. Show the capacity refusal in plain words.
- *Benefit:* a truthful state and the correct next action.

### Task panel and run review

**T1. The question shows twice in the panel** · Low · *Distill* · **kept**
- *Observation:* the feed shows "Decision for you … Waiting for your answer" and the dock below shows the answerable
  card.
- *Why kept:* the feed entry is the transcript (it later becomes "You answered …"). The duplication is brief and
  truthful.

**T2. Run result** · resolved in `4.12.2` · *Clarify*
- "Review changes" is now the one primary action, with handoff editing secondary. The earlier journey audit's
  remaining gap was the merge path in B3.

### New task

**F1. The execution summary is dense** · Medium · *Distill; Clarify* · **hypothesis**
- *Problem:* before typing a title, the person reads four lines at once:
  - "Automatic agent · Automatic runner"
  - "Ask me when needed · from the built-in default"
  - two sentences of policy
  - a capacity warning
- *Proposal:* collapse to one line: "{agent} on {runner} · asks you when needed". Show the explanation on demand, and
  keep the capacity line, which is actionable. Verify with a five-person comprehension check before changing it,
  because the permissions sentence is a deliberate safety statement.
- *Recommendation only.*

**F2. Three ways to create a task** · Low · *Critique: flexibility* · **kept**
- Quick add (Backlog, Enter), the New task sheet (N, header button, phone FAB) and the assistant all exist. Each serves
  a different context: fast capture, a full brief, and conversation or phone use. They share one API and one confirm
  rule, so this is intentional redundancy.

### Needs you, Control Center, Overview and the global banner

**A1. The banner repeats what the page already shows** · Medium · *Distill; Quieter* · **implemented**
- *Problem:* on a project's Overview, the amber banner "1 question needs your input" sits directly above the "Needs
  you" panel showing the same question with its answer buttons. Control Center already suppresses the banner for this
  reason; Overview does not.
- *Proposal:* hide the banner on a project's Overview for questions from that project. Questions from other projects
  still show.
- *Benefit:* one amber signal per question on screen.

**A2. The desktop Inbox cannot answer** · High · *Critique: efficiency; product principle "answerable where it is
shown"* · **recommendation**
- *Problem:* (from the journey audit) the shell's Inbox rows carry no question options, so every answer means opening
  the workspace frame and the task.
- *Proposal:* inline answering for the person's own workspace, with the context visible before choices are enabled.
- *Recommendation only:* needs a summary payload change. Team rows must stay open-only, because a Team workspace
  coordinates and never executes.

**A3. Coloured side stripes in assistant cards** · Low · *Quieter (detector: side-tab)* · **recommendation**
- `ConfirmCard` (3px state stripe) and `ReplyView` (3px danger stripe) use thick coloured left borders. The rest of the
  product uses status dots and status words.
- *Proposal:* move the state onto the card's label (dot and word).
- *Recommendation only:* not rendered here, because the fixture disables the assistant.

### Runners and agents

**R1. A full runner reads "Online"** · Medium · *Clarify; Critique: visibility of system status* · **implemented**
- *Problem:* Settings → Runners shows "Online · 1/1" in green for a runner whose only slot is in use, and the rail
  shows a bare "1/1". "Idle" appears only at 0 runs, so a full runner and a half-full one look the same.
- *Proposal:* use the words people need: Idle, Working (some slots in use), Busy (all slots in use), Offline,
  Disabled. Keep the n/m count, and give the rail's count an accessible name and tooltip ("1 of 1 run slots in use").
- *Benefit:* the runner screen answers "can it take work now?" without arithmetic.

**R2. Settings has nine flat sections** · Medium · *Organize* · **recommendation**
- *Problem:* Task defaults, Labels, Runners, Routing rules, Phone access, GitHub, Agents, Alerts and theme, This
  computer. Three are about machines (Runners, Agents, This computer) and two about connections, but nothing groups
  them, and "This computer" is also listed as a runner.
- *Proposal:* group them under four short labels: Work (Task defaults, Labels, Routing rules), Machines (Runners,
  Agents, This computer), Connections (Phone access, GitHub), Preferences (Alerts and theme). Addresses stay the same.

### Calendar, scheduling and timeline

**C1. "Unscheduled" and "Not planned" are two lists of the same tasks** · Medium · *Clarify; Distill* ·
**recommendation**
- *Problem:* Calendar schedules *when an agent runs*; Timeline plans *when work should happen* (ranges, milestones,
  dependencies). Both open with a list of the same tasks, "Unscheduled" or "Not planned yet", and the tab names do not
  say which is which.
- *Proposal:*
  - Keep both features; they answer different questions.
  - Make the difference explicit: the Calendar's side list becomes "Not scheduled to run" and the Timeline's stays
    "Not planned yet".
  - Each empty state names the other view: "To plan dates and milestones, use Timeline."
  - Consider renaming the tabs to "Schedule" and "Plan", which is a product decision.

### Runs and usage

**U1. A brand illustration fills the Runs page** · Low · *Quieter (Operate mode)* · **recommendation**
- *Problem:* under a short run list, the Runs page centres the mark, the wordmark and "Werk local, ship global."
- *Proposal:* use that space for the selected run's details, or leave it empty. Brand belongs in precise details on an
  Operate surface.
- *Recommendation only:* it is a deliberate brand choice and harmless.

**U2. Usage is the only reporting** · Info
- Control Center's Usage panel is careful ("Only what agents reported. An estimate is not a bill."). Analytics
  (roadmap stage 5) should keep that honesty and live as one view, not as panels spread over several pages.

### Team console and desktop shell (source only)

**S1. Team console** · Info
- The Team console uses its own vocabulary: Workspace, Reviews, Members, and a "Needs attention" section inside a
  repository. Aligning "Needs attention" with the product-wide "Needs you" belongs with N2, but the Team console's
  code is behind the Team boundary.
- *Recommendation only:* not changed here.

## Implemented in this pass

All changes are in `web/src`. There are no API, execution, orchestration, networking, authentication, licensing or Team
changes, and no design tokens were changed.

| Finding | Change | Files |
| --- | --- | --- |
| N1 | The phone tab bar gives each tab an equal column, whatever the number of sections. Labels never wrap, and drop to 10px below 340px rather than clip. | `App.svelte` |
| B4 | `onlyBusy()` tells "every eligible runner is full" apart from "no agent is set up". The task panel and New task now say *Every runner that can take this task is busy. It can start when a run finishes, or after you stop one.*, with a link to the runners instead of Settings. The task panel keeps its start form, so another runner can still be chosen. | `lib/execution.ts`, `routes/TaskPanel.svelte`, `lib/NewTaskDialog.svelte` |
| B4 | A start the scheduler refuses ("conflict: waiting_capacity: …") is turned into plain words at the one place runs are started (`api.startRun`), so the Board, Control Center, Git findings and New task all say the same. Only the scheduler's own states are translated; any other conflict message is left as it is. | `lib/scheduling.ts`, `lib/api.ts` |
| B3 | A successful local merge moves the branch's own task to Done from any page. The Board still does this itself, so it is not done twice. The sheet says *"{task}" moved to Done.* and offers *Move it back to {column}*. If the move fails, it says that the merge happened and the task could not be moved. | `lib/git/MergeSheet.svelte` |
| R1 | `runnerState()` gives Idle, Working, Busy, Offline or Disabled. Settings → Runners shows *Busy · 1/1*. The rail's count has an accessible name and tooltip (*1 of 1 run slot in use*), and its dot uses the running colour while a runner works. | `lib/format.ts`, `lib/RunnersPanel.svelte`, `lib/shell/Rail.svelte` |
| A1 | On a project's Overview, the banner leaves out that project's questions, because the Needs you panel right below shows them, with a link to any beyond the first three. Other projects' questions still show. | `lib/NeedsInputBanner.svelte` |
| B1 | The agent chip shows only when an agent was chosen or has run, and the empty chip row is no longer drawn. Mode and priority chips are unchanged. | `lib/board/TaskCard.svelte` |
| N2 (part) | The desktop shell's attention page is headed "Needs you", the label of the sidebar item that opens it. | `shell/Inbox.svelte` |

Also corrected: DESIGN.md's phone tab list (it said five tabs).

## Validation

- `npm run check` (svelte-check, fail on warnings): 0 errors, 0 warnings. `npm run lint`: clean.
- `vitest`: 40 files, 465 tests, all passing. New tests cover `onlyBusy`, `startRefusal` and `runnerState`/`runnerSlots`.
- `make web web-embed web-shell`: production builds succeed.
- `scripts/browser-individual.cjs` against a fresh fixture with the rebuilt, embedded UI: all 14 flows pass. They
  include onboarding, Kanban at four widths, task creation, run review, settings, every route without overflow on
  desktop and phone, and no JavaScript errors.
- **Seen in a browser** (fixture, dev server):
  - phone tabs measured at 375px and 320px: six tabs on one row, no clipped label;
  - the busy message in the task panel and New task, and the plain-words toast from a card's Start agent;
  - the Overview without the duplicate banner (the Board keeps it);
  - Settings → Runners and the rail reading Busy;
  - a merge from the Git page moving its task to Done, and *Move it back* returning it to Doing (confirmed through the
    API).
- Screenshots in dark and light, at 1440×900 and 375×812, of Board, task panel, Overview, Control Center and Runners
  were taken with headless Chromium, because the app's own browser pane was hidden.
- **Not run:** `scripts/test-unified-desktop-browser.cjs` (needs the Team service, rqlite and the native shell). The
  only shell change is the Inbox heading text, and no script asserts the old text. `make check` was not run in full:
  no Go code changed.

## Remaining recommendations

In order of expected value. Each needs a product decision or a larger change than this pass allowed.

1. **B2 · Move finished work to Review automatically** (High) · **implemented**. `internal/service/runs.go` checks local branch divergence before completion; remote completion uses valid differing base/HEAD commits. Only Doing moves, with one normal versioned task event. Scheduled and remote paths use the same rule (`runner_events.go`). Tests cover no evidence, failure, stop, prior moves, scheduling, stale versions and replay. Team progress deliberately leaves ticket status alone; submission remains explicit (see INTEGRATION.md).
2. **A2 · Answer from the desktop Inbox** (High) · **implemented**. Optional question context in `internal/workspace` and `service/workspace.go` is Individual needs_input only; cleaning strips it elsewhere. `shell/Inbox.svelte` expands the reused `QuestionCard`, shows full context before choices, focuses no choice, and disables stale/offline reports (30 seconds without refresh), with last-report time. The existing native relay and Individual credential call the existing answer endpoint; controller conflicts and saved-but-unacknowledged replies are retained. Team rows say Open. Tests cover summary boundaries, response semantics and the real shell/controller answer/reconnect flow.
3. **F1 · A one-line execution summary in New task** (Medium). Verify with a short comprehension test first, because
   the permissions sentence is deliberate.
4. **C1 · Name the two lists apart.** "Not scheduled to run" (Calendar) and "Not planned yet" (Timeline), with each
   empty state pointing to the other. Possibly rename the tabs to Schedule and Plan.
5. **R2 · Group the nine Settings sections** under Work, Machines, Connections and Preferences. Addresses stay the same.
6. **N2 · One name for the attention list.** Decide whether Control Center becomes "Needs you" everywhere, or keeps
   its name with "Needs you" as its first section. The Team console's "Needs attention" should follow.
7. **N3 · Hide "Everywhere" with a single workspace** in the desktop sidebar (hypothesis: check with people who use
   only Individual).
8. **Phone board filters** take three rows above the columns at 375px. Fold Agent, Priority, Who and Archive into one
   Filters control on phones (hypothesis).
9. **A3 · Assistant cards:** replace the 3px coloured side stripes with a status dot and word on the card label, after
   rendering the assistant.
10. **U1 · Runs page brand fill:** use the space for run details, or leave it empty.
