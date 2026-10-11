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

**T1. The question shows twice in the panel** · Low · *Distill* · **implemented in `4.16.0`** (was kept)
- *Observation:* the feed shows "Decision for you … Waiting for your answer" and the dock below shows the answerable
  card.
- *Resolved:* it showed three times (status line, feed entry, dock). The dock is the one statement while the question is
  open; the feed shows it once it is answered, so the transcript still reads in order. The banner also stays out of the
  project's Overview, Board and Runs, which ask the question themselves, and Runs rows have an Answer button.
- *Also in `4.16.0`:* "Needs you" is the one name (was Needs input / Needs answer), and only an approval's Deny or Reject
  is red, not every "No".

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
3. **F1 · A one-line execution summary in New task** (Medium) · **implemented**. `NewTaskDialog.svelte` shows "{agent} on {runner} · {interaction}" and one line, "Permission requests still come to you." The longer interaction explanation, the permissions note and the source of the setting moved inside "Change execution settings". The busy and unavailable lines stay visible because they are actionable. The comprehension test was not run; the permissions sentence was kept rather than removed.
4. **C1 · Name the two lists apart** (Medium) · **implemented**. Tab names unchanged. Calendar's side list is "Not scheduled to run"; Timeline keeps "Not planned yet". The empty states each name the other view (`Calendar.svelte`, `Timeline.svelte`).
5. **R2 · Group the nine Settings sections** (Medium) · **implemented**. `Settings.svelte` groups them under Work, Machines, Connections and Preferences. Section ids and addresses (`#/settings?runners` and the rest) are unchanged. On a phone the groups sit in one scrolling row, each with its heading.
6. **N2 · One name for the attention list** (Medium) · **implemented**. The Individual rail, the page, the desktop shell's workspace entry (`shell/nav.ts`), the New-task and overview links, the docs and the Team console's repository section (`console.js`, copy only) say "Needs you". The "Needs input" filter segment is "Questions". Route ids and addresses are unchanged. Run-status labels such as "Needs input" on a run describe the run, and stay.
7. **N3 · Hide "Everywhere" with a single workspace** (Low) · **implemented**. `shell/Sidebar.svelte` shows the group only with two or more workspaces. With one, the count moves to the workspace's own "Needs you" entry, and when that workspace is unusable a "Needs you" entry opens the Inbox. My Work, Calendar and Inbox stay routable through `shell:go` and the native menu (covered by `test-unified-desktop-browser`).
8. **Phone board filters** (Low) · **implemented**. Below 900px, `Board.svelte` folds Agent, Runner, Priority, labels, Archive and Clear Done into one "Filters" button with an active-filter count. The status legend, now including zero counts, stays visible. Desktop is unchanged.
9. **A3 · Assistant cards** (Low) · **implemented**. `ConfirmCard.svelte` and `ReplyView.svelte` lose the 3px side stripes. The label carries a status dot and word (Needs you, Working, Done, Declined, Expired, Failed) in the existing tone tokens.
10. **U1 · Runs page brand fill** (Low) · **implemented**. The mark, wordmark and tagline under the run table are removed; the space stays empty.

## Validation of the second pass

* `cd web && npm run check && npm run lint && npx vitest run`: no errors or warnings, 465 tests pass.
* `make web web-embed web-shell`, then `make test-browser` (fresh disposable Individual and Team, desktop and phone, light and dark): passes, including the new Filters control, the one-line summary and the Settings groups.
* `make test-unified-desktop-browser`: covers the single-workspace sidebar (no Everywhere, count kept, My Work, Calendar and Inbox reachable through `shell:go`) and its return when a second workspace joins.
* Screenshots at 1440×900 and 375×812, light and dark, from a fresh `scripts/browser-fixture` with `WERKBORD_BROWSER_EXECUTION=1` and `WERKBORD_BROWSER_ASSISTANT=1`: Board (with the phone Filters control open), Settings, Needs you, Runs, Calendar, Timeline, New task, and the assistant's confirmation and failure cards. Taken with headless Chromium, because the app's browser pane was hidden.
* Go: `internal/team/console` and `internal/archtest` pass. Items 1 and 2 shipped earlier with their own tests; this pass touched only Team console copy and web code.
* Test changes: `test-unified-desktop-browser.cjs` expects the renamed entries and covers the single-workspace sidebar; the final unroute no longer waits on in-flight handlers (it hung), and the Calendar width check sets its own 480px viewport. `browser-individual.cjs` opens the phone Filters control before using Archive and Clear Done.
* **Not run:** the full `make check` and the Go suite for `internal/service` (no Go code changed in this pass). The F1 comprehension test and the check with Individual-only users for N3 were not run.

## Critique of 4.14.1 — final pass (11 October 2026)

The original critique scored **26/40** (`.impeccable/critique/2026-10-11T11-04-16Z__web-src.md`).
This pass verifies the resulting Individual UI and shell, rather than changing execution or Team behavior.

**Final score: 30/40 (+4), Good**, for `4.18.1-preview.1`. Method: two isolated assessments (`/root/critique_design`
and `/root/critique_evidence`), with the design agent kept blind to the baseline and detector. The initial independent
score was 29/40; a browser-verified shared-Sheet focus escape was fixed with a failing-before/passing-after regression
test. The design agent's focused source reassessment raised User control from 2 to 3; the parent verified the final
browser behavior. The agent could not reopen the native browser for that focused follow-up and did not claim it did.

| Nielsen heuristic | Before | Final | Remaining limitation |
| --- | ---: | ---: | --- |
| Visibility of system status | 3 | 3 | Interruptions badge excludes Review; All includes it. |
| Match system / real world | 3 | 3 | Calendar versus Timeline still requires learning. |
| User control and freedom | 3 | 3 | Sheet containment is now tested; broader assistive-technology cases remain. |
| Consistency and standards | 2 | 3 | Expanded shell question repeats its prompt; project summaries still say needs input. |
| Error prevention | 3 | 3 | Guarded confirmations are strong; complete screen-reader behavior is untested. |
| Recognition rather than recall | 3 | 3 | Phone Git/Settings have competing navigation levels. |
| Flexibility and efficiency | 3 | 3 | Phone review requires reading repeated technical details. |
| Aesthetic and minimalist design | 2 | 4 | Restrained, product-specific attention and work surfaces. |
| Error recovery | 2 | 3 | Disabled fixture Git health lacks an enabling path. |
| Help and documentation | 2 | 2 | Contextual explanations exist; no obvious general task-focused help. |
| **Total** | **26/40** | **30/40** | All ten apply; this is a heuristic judgment, not a user-study result. |

The detector returned two `broken-image` warnings, both false positives: Svelte's dynamic QR `src` and an intentionally
malformed image string in a transcript test. The final focused Sheet scan returned zero findings. Native browser script
injection failed because its evaluation scope is read-only; no live overlay or overlay server was created. Screenshots,
computed styles, keyboard checks and the fixture browser suites supply the visual/interaction evidence instead.

**Priority residuals (P2):** bring review actions ahead of ticket administration; clarify phone navigation levels;
state the expanded shell question once; enlarge label-color hit areas and add normal radio arrow navigation with
human-readable color names. None was treated as a product blocker or silently fixed in this final pass. Disabled
fixture Git health is separately recorded as contextual evidence, not a claim about enabled production checks.

**Cognitive load and journey:** answering is low-load and reassuring; mobile Git and review carry moderate load.
The merge confirmation and delivery line make the end of work explicit. Keyboard users benefit from the Sheet fix,
phone users still face dense navigation, and screen-reader users still face hex-only label-color names. The palette,
attention hierarchy and planning-specific work vocabulary remain the strongest authored features.

**Resolved since that critique:** the installed app starts on Needs you; the phone board opens on attention;
More reaches Overview and Settings; answer/card actions have 44px targets; the task header wraps and receives focus;
null execution capability arrays are guarded; route failures have a recoverable boundary; the question is stated once
in its task panel and answerable from Runs; attention vocabulary is consistent; Archive task offers Undo;
the run result offers the guarded Merge… path and explains verdicts; answering reports delivery; clocks use the locale
helper; disclosures show chevrons; Calendar states when no execution is scheduled; work projects omit agent controls;
optional connection failures are quiet and blocked alerts explain where to allow them.

**Persists:** keyboard navigation still crosses substantial chrome before work; generic nested panels remain in
Overview and Settings; historical `devboard/` branch names still appear under Werkbord branches (compatibility names
must remain valid); the Phone access QR is shown by default; assistant suggestions still demonstrate read-only
questions; Timeline's compact Plan… actions provide little context. Compact secondary controls are not covered by
the answer-button target guarantee. Git health is unavailable in the fixture and must not be mistaken for a production
regression. The deliberate 950ms assistant opening is preserved and reduced motion is tested separately.

**Still hypotheses:** frequency of null capability payloads on real runners, whether answering confirmation feels
sufficient, QR exposure during screen sharing, Inbox urgency ordering on real mixed-workspace data, real-device
switcher/splash behavior, and comprehension of verdicts versus merging. Automated fixture tests cannot establish
these; no user comprehension study was run.

### Reproduce the isolated fixture and seeded state

Run from the repository root. The binary below is the browser fixture, not `bin/werkbord`; no setup, login service,
real controller, real repository, agent account or network connection is used. Choose an unused loopback port.

```sh
make web web-embed web-shell
UX_FINAL_DIR=$(mktemp -d /tmp/werkbord-ux-final.XXXXXX)
go build -o "$UX_FINAL_DIR/fixture" ./scripts/browser-fixture
WERKBORD_BROWSER_ADDR=127.0.0.1:17499 \
WERKBORD_BROWSER_EXECUTION=1 WERKBORD_BROWSER_ASSISTANT=1 \
GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
"$UX_FINAL_DIR/fixture" > "$UX_FINAL_DIR/fixture.log" 2>&1 &
UX_FINAL_PID=$!
until curl -fs http://127.0.0.1:17499/api/health > "$UX_FINAL_DIR/health.json"; do
  kill -0 "$UX_FINAL_PID" || break
  sleep 1
done
PERSONAL_BROWSER_URL=http://127.0.0.1:17499 SEED_DIR="$UX_FINAL_DIR/seed" \
node scripts/seed-browser-fixture.cjs > "$UX_FINAL_DIR/projects.json"
PLAYWRIGHT_MODULE="$PWD/web/node_modules/playwright" \
PERSONAL_BROWSER_URL=http://127.0.0.1:17499 \
node scripts/measure-legibility.cjs --check --json "$UX_FINAL_DIR/legibility.json" \
  --screenshots "$UX_FINAL_DIR/surfaces"
kill -TERM "$UX_FINAL_PID"
wait "$UX_FINAL_PID"
```

The seed creates two throwaway Git repositories: one Doing run waiting on "Commit the fixture work?", and one
finished run with a commit and its task in Review. It also creates Venue search, a work project with three tasks.
The seeder checks loopback and `version: browser-fixture` before any mutation; three Node tests verify refusal of
ordinary controllers, missing version metadata and non-loopback destinations before writes or repository deletion.
Run them with `node --test scripts/seed-browser-fixture.test.cjs`.

The screenshot mode covers 28 routed surfaces plus New task and Assistant, at **1440×900 and 375×812 in both
themes** (120 captures). It fails on JavaScript errors or document overflow. Routed text measurements: **5,418 text
nodes, zero contrast failures, zero text below 11.5px**. New task and Assistant captures are visual evidence;
their dialog text is not included in that routed-node count. The shell Inbox and expanded question are captured by
`BROWSER_ARTIFACT_DIR="$UX_FINAL_DIR/shell" node scripts/test-unified-desktop-browser.cjs`, which starts its own
disposable services. Assistant interaction/motion checks run with `scripts/browser-assistant.cjs` and
`scripts/browser-shell-assistant.cjs`. No Go or Team source changed in this final pass.

### Final validation

- `make check`: passes, including Go/service/boundary tests, desktop logic, release/notarization/workflow checks,
  web lint and Svelte checks (zero errors/warnings), 44 Vitest files / 533 tests, and production builds.
- `make web web-embed web-shell` and `make test-browser`: pass. The complete disposable Individual/Team browser
  and real-process integration suite ran; no fallback was needed.
- `scripts/test-unified-desktop-browser.cjs`: passes after correcting its stale task-panel close selector from
  "Close" to "Close panel". Inbox and expanded questions were inspected at the four requested size/theme variants;
  stale disabling, reconnect, delivery, focus, persistent frames and workspace boundaries pass.
- Both assistant browser suites pass, including reduced motion, focus restoration, confirmations and recovery.
- `node --test scripts/seed-browser-fixture.test.cjs`: three tests pass; successful seeding was also repeated on a
  second fresh fixture. These tests are now included in `make check`. Script syntax and `git diff --check` pass.
- The new Sheet regression first failed with focus outside New task. After the fix, forward/reverse Tab containment,
  background-focus redirection, disabled/collapsed controls, both themes/sizes and Escape opener restoration pass in
  `scripts/browser-individual.cjs`. Sheet stops trapping before restoring focus on teardown; only the topmost sheet
  handles keyboard events. All full checks and the browser suite were rerun after the fix.
- Vite still reports its existing main-chunk size advisory (>500 kB); it does not fail the build. User studies and
  real-device observations listed above remain unperformed.

Local, ignored artifacts: `.impeccable/review/final-pass-4.18.1/` contains the final screenshot matrix, shell captures,
legibility JSON and independent assessment reports. The critique snapshot is also kept in `.impeccable/critique/`.
Fixtures are stopped after verification. The unified version is bumped and tagged locally; nothing is pushed.

### Workstream 1 — attention counts and work vocabulary (11 October 2026)

Verified the mixed question/Review seed before editing: the Individual Needs you page showed `All 2`, `Questions 1`, and
`Review 1`; the Fixture repo interruption badge showed `1`, while Second repo had one item ready for review and no
interruption badge. The project summary said “1 needs input.” The shell's collapsed question row named the prompt and
kept workspace, project and task provenance. The answer card then stated the same prompt after expansion.

Changes in this pass:

- Project summaries now say “1 question needs you” / “2 questions need you.” Internal `needsInput` and API states are unchanged.
- Project and Needs you interruption badge accessible names say that work ready for review is excluded. The visible count
  note explains that the All list also includes work ready for review; each segment's accessible name announces its count,
  with All naming the included Review count.
- An expanded shell question row now says “Answering this question.” Workspace, project and task provenance remain on the
  row, and the actual prompt appears once in the answer card. The collapsed row continues to show what the agent asked.
- Timeline date fields explain that they plan work and do not schedule an agent; execution schedule fields explain that
  they start agent work and point to Timeline for dates and milestones. The work-project edit sheet keeps planning fields
  and omits the execution schedule controls.

Evidence and regression coverage:

- The refreshed isolated fixture retained the seeded question, Review task and Venue search work project. Browser assertions
  verified the 2/1/1 mixed counts, count accessible names, visible scope explanation, project vocabulary, one prompt after
  expanding the shell answer, and retained workspace/project/task provenance.
- Browser assertions verified that work projects retain timeline planning without the controller-run schedule control, and
  that the execution schedule sheet explains the other meaning of dates. Existing shell checks still verify stale-answer
  disabling, reconnect, delivery and pending acknowledgment.
- Final Individual screenshots cover 28 routed pages at 1440×900 and 375×812 in light and dark. The measurement reported
  5,422 visible text nodes, zero contrast failures, and zero text below 11.5px. Reduced-motion media was enabled for that
  matrix. A separate touch-emulated run confirmed coarse pointer mode and 46px-high answer targets. Keyboard Tab showed a
  visible focus indicator on the Needs you link.
- The shell answer state was captured at 1440×900 and 375×812 in both themes. Planning and execution date sheets were also
  captured at those four viewport/theme combinations. Baseline and final files are in
  `.impeccable/review/workstream-1/` (`baseline-individual/`, `baseline-shell/`, `final/individual/surfaces/`,
  `final/shell/`, `final/date-controls/`, plus the keyboard and coarse-pointer captures).
- `npm test -- src/lib/projects.test.ts src/lib/attention.test.ts`, `npm run check`, and `npm run lint` passed. `make
  test-browser` passed the Individual, Team and bridge browser flows, including the new date-copy and work-project checks.
  `scripts/test-unified-desktop-browser.cjs` passed the shell question-count/provenance check and its stale/offline and
  delivery coverage. The first test attempts exposed errors in my new test setup (singular wording, sheet close control,
  and viewport restoration); I corrected those assertions/setup issues and reran the browser suite successfully.

No API dependency was needed. The fixture is stopped. No human comprehension study, VoiceOver session, or physical-device
observation was performed; browser and emulated-pointer results do not establish those outcomes. This pass does not claim
an updated heuristic score.

### Workstream 2 — keyboard, touch and assistive technology (11 October 2026)

Reproduced the remaining interaction defects against the freshly built and seeded browser fixture before editing:

- The Labels palette exposed nine button radios with hex-only names, nine Tab stops, no arrow-key selection, and 22×22px
  targets under coarse-pointer emulation.
- Neither the Individual work region nor the desktop shell had a skip link. Task detail view buttons sat inside a
  navigation landmark despite not being links. Jump to intercepted Ctrl/⌘K while an editable field had focus.
- Opening a task focused its title, but closing it left focus on the document body. The cleanup could capture the
  panel's own autofocused heading instead of its opener.
- The existing Sheet trap covered simple forward/reverse Tab, but had no regression coverage for a nested top sheet,
  controls in disabled fieldsets or collapsed details, live-added actions, or a disconnected opener.

Changes in this pass:

- Label palette choices are native radios in a named radiogroup. The fixed palette uses Red, Orange, Gold, Olive, Teal,
  Blue, Violet, Pink and Gray; hex values supplement the names. Native arrow keys move selection, the selected swatch
  has a check mark and outline, desktop dots remain 20px, and coarse-pointer hit areas measure 44×44px.
- Both Individual and desktop shell have a visible-on-focus “Skip to main content” link, with a focusable main work
  region. Task detail view controls are a labelled group, Settings and project navigation remain links, and Jump to/N
  do not take keystrokes from a focused editor.
- Task-panel teardown now restores focus to the original connected opener, or to the matching task link on the board
  when mount-time autofocus displaced or disconnected that opener.
- Browser regression coverage exercises the live Sheet trap with disabled-fieldset and collapsed controls, a
  dynamically added final action, and a nested top-sheet layer. Escape closes only the top layer; focus restoration
  skips a disconnected opener and returns to a connected board/task creation opener. The answer, Review and creation
  browser journeys remain covered.

Evidence and verification:

- The post-build guarded fixture is `.impeccable/review/workstream-2/fixture/`. It contains `health.json`, the seeded
  project IDs, the fixture log, legibility JSON and all 120 routed screenshots (28 pages × two themes × two sizes, plus
  New task and Assistant captures). Legibility measured **5,570 text nodes, zero contrast failures and zero text below
  11.5px**.
- `.impeccable/review/workstream-2/individual/labels/` contains the populated Labels surface at 1440×900 and 375×812
  in both themes, coarse-pointer phone captures, and the 720×450 200%-zoom viewport captures. The keyboard-focus
  screenshots are alongside them. At 200% zoom, the fixed phone tab bar initially covers the lower part of the page;
  the page's bottom padding lets the Add label action scroll fully above it, which the browser assertion measures.
- Coarse-pointer emulation reported `(pointer: coarse) = true`; all nine radio hit areas measured 44×44px. Keyboard
  checks reached the skip link with Tab, entered main content without changing the hash route, moved radio selection
  with ArrowRight/ArrowLeft, and kept the label draft when Ctrl+K was pressed in its input. The selected radio focus
  ring was inspected in both themes.
- `make test-browser` passed after the final test changes: Individual answer/review/create and recovery flows, the
  Individual/Team shell and integration fixtures, the responsive layouts, and the keyboard/Sheet regressions. The
  unified desktop browser suite passed, including the shell skip link. Both `scripts/browser-assistant.cjs` and
  `scripts/browser-shell-assistant.cjs` passed, including reduced motion. The Impeccable detector returned no findings.
- The early browser runs caught test-harness mistakes (programmatic focus instead of Tab for `:focus-visible`, reading
  visually hidden text with `innerText`, a locator tied to text replaced by the edit form, an incorrect assumption
  about the Sheet's first focusable control, a route-cleanup race, and zoom geometry measured before scrolling). Those
  assertions were corrected and the full affected browser suites rerun. The route test also exposed the task-panel
  focus-return defect above, which was fixed and verified before the passing run.

No out-of-scope API dependency was identified; existing API payloads and behavior are unchanged. No Go or Team source
was changed. VoiceOver and a physical touch device were unavailable for this pass, so their behavior remains
unverified; browser accessible-name checks and touch emulation are not presented as manual assistive-technology or
device checks. No human comprehension study was run and no heuristic score change is claimed.

### Workstream 3 — Review before ticket administration (11 October 2026)

Reproduced on the fresh seeded fixture before editing: at 375×812 the Review task title was at y=120, while Review
changes and Merge… were inside the run result lower in the activity feed. They required scrolling past the task
header and activity. The seeded local branch was one commit ahead and unmerged, so the controller's existing Git
evidence allowed both actions. The pre-change capture is
`.impeccable/review/workstream-3/baseline-review-phone.png`.

The Review task panel now places the task title first, followed immediately by Review changes and Merge… when the
run's local branch is authoritatively ahead and unmerged. Archive and Edit stay available in the header with
secondary button styling. Branch or PR provenance and reported test-note count appear once in the summary; the
test line explicitly says “not verified.” Full run and Git evidence, including the full test notes and handoff,
stays inside “Full run and Git evidence.” Failed Git/PR lookup is identified as unknown and offers no invented
review or merge action. The existing merge sheet still checks current Git state, refuses stale branch evidence,
requires confirmation, and moves a merged task to Done. The existing Accept work / Reject work explanation was
verified: it records a verdict and does not merge or move the task. Doing continuation and reply rendering remain
on their original RunHandoff path.

Behavior evidence:

- `scripts/browser-individual.cjs` now exercises local branch review, a controlled open-PR response, no-change
  evidence, failed Git and PR lookup responses, unverified test notes, stale merge refusal, merge confirmation,
  the Done transition, and Archive Undo. It asserts the review action appears in the task header only, the title
  leads at 375×812, the full evidence disclosure starts closed, the verdict stays separate from merge, and Undo
  returns the task to Done. The controlled PR and failure responses are browser mocks; no API or fixture source
  was changed.
- On the final fixture, the action row measured y=153 below the title and both Review changes and Merge… measured
  44px high. At coarse-pointer emulation, Review changes measured 152×44px; the title received focus when the panel
  opened, Tab reached Review changes, and `prefers-reduced-motion: reduce` removed the panel animation.
- The four Review-task screenshots at 1440×900 and 375×812 in light and dark themes, plus coarse-pointer phone
  captures, are in `.impeccable/review/workstream-3/individual/`. The complete guarded-fixture matrix, health
  identity, seeded project IDs, log and legibility JSON are in `.impeccable/review/workstream-3/fixture/`.
  Legibility measured **5,554 text nodes**, **zero contrast failures**, and **zero text below 11.5px** across 28
  routes × two themes × two sizes.
- The Impeccable detector returned no findings. `npm run check` found zero Svelte errors and warnings; `npm run lint`
  passed; Vitest passed **44 files / 534 tests**. Production web/embed/shell builds completed.
- `make test-browser` completed the Individual flows (including the Review regression) and Team browser flows, but
  did **not** complete: its separate `scripts/browser-bridge.cjs` integration repeatedly timed out while waiting
  for a disposable run to reach `waiting_for_user` within its 10-second polling window. The failure occurred after
  the Review and Team browser flows had passed. It remains a reported suite failure; no Team or Go source was
  changed, and the wait was not relaxed.
- No developer comprehension session was observed. Whether people can explain review, verdict and merge remains
  unverified; scripted browser assertions do not establish human comprehension. No heuristic score increase is
  claimed.

No out-of-scope API dependency was identified. Review and merge continue to use the existing controller evidence
and confirmation APIs.

### Workstream 4 — phone navigation hierarchy (11 October 2026)

**Baseline, verified on the isolated fixture before editing.** The seeded phone capture at 375×812 showed seven Git
section links wrapping across four rows above the repository content; at least 104px of vertical space went to the
section row before the first Git section. The existing grouped Settings navigation was a horizontally scrolling row;
the current group and selected item could move off-screen. Runs remained a visible primary tab. Overview, Project
settings and global Settings were reachable through More. The project switcher and project header already exposed the
current project, and the seeded work project correctly omitted Calendar, Git and Runs.

Two concrete phone alternatives were compared:

1. **Existing grouped/wrapped links:** each subsection is a pill link in the current row. It preserves one-tap access,
   but Git takes four rows and Settings can hide later groups off-screen. Baseline captures are in
   `.impeccable/review/workstream-4/baseline/`.
2. **Compact labeled chooser:** a visible level label (`Git section` or `Settings section`) and a native select show
   the active location continuously and expose every destination through the platform's normal menu. The existing
   desktop link lists stay in place. This is implemented and captured in
   `.impeccable/review/workstream-4/individual/` and `final/`.

There were no participant observations to choose between these by task completion. I asked for a preference while
continuing route and focus checks; the user chose the compact chooser. This is a provisional preference-based choice,
not evidence of human discoverability or task-completion improvement.

**Implemented:** the Individual app replaces Git's wrapped phone links and Settings' horizontally scrolling group
row with the compact chooser at widths below 900px. Selecting an item retains the existing `?section` address, direct
links select the matching item, and browser Back restores it. Runs and the other weekly destinations remain in the
bottom bar; More still contains Overview, Project settings, Projects and Settings, and the first two now show their
current location as a phone page heading. The bottom bar reserves its own
height plus the CSS safe-area inset. The desktop shell's icon rail now has an accessible expand/collapse control at
intermediate widths; expanding it reveals section and project labels, including long project names.

**Behavior and rendered evidence:** `scripts/browser-navigation-workstream-4.cjs` covers the seeded repository and
work project, Runs, More destinations, Git/Settings deep links, selector changes, browser Back, keyboard focus,
task-panel return to its owning board, no horizontal overflow at a 720×450 zoom-equivalent viewport, coarse-pointer
44px chooser targets, the bottom-bar viewport boundary, and reduced-motion preference exposure. A controlled browser
response supplies a long project display name without changing fixture data. `make test-unified-desktop-browser`
passed with two workspaces; its shell case verifies the 1024px rail labels and long project name by keyboard and
pointer, and its captures are in `.impeccable/review/workstream-4/shell/`. Final light/dark captures for 1440×900
and 375×812 are in `.impeccable/review/workstream-4/final/`; the focused Git subsection and Settings captures are
in `individual/`. The final post-build legibility run measured 5,326 text nodes across 28 routes × two themes × two sizes,
with zero contrast failures and zero text below 11.5px.
The Impeccable detector returned no findings for the changed UI components.
After the version bump, `make check` passed, including Go tests/vet/builds, 44 web test files (534 tests), Svelte check,
lint, desktop checks, release/notarization checks and workflow checks. `make test-assistant-browser` also passed in
the browser and desktop shell, including reduced motion, confirmations, recovery, dictation and phone controls.

**Limits:** Playwright's coarse-pointer emulation and viewport-bound checks passed, and the page declares
`viewport-fit=cover` with top/bottom `env(safe-area-inset-*)` handling. A physical iPhone safe-area inset was not
available in this browser environment. No VoiceOver session or human task-completion observation was conducted.
No score increase is claimed. Existing APIs cover all navigation; no out-of-scope API dependency was identified.
No Go or Team source was changed.

The broader `make test-browser` command did not complete: its 120-second `spawnSync` limit expired while
`scripts/test-browser.cjs` was running `scripts/browser-individual.cjs`, after the suite had rendered its Settings and
Project settings routes. The run exited with `ETIMEDOUT`; its Team and bridge browser sub-suites were not reached.
The timeout was not relaxed; the captured Runs/Settings/Project settings screens are preserved in
`.impeccable/review/workstream-4/browser-suite-timeout/`. The focused navigation browser script and
`make test-unified-desktop-browser` did pass.
Production web/embed/shell builds succeeded; Vite reports the 554.18 KB main chunk above its 500 KB
advisory threshold.

### Workstream 5 — recovery, drafts and uncertain delivery (11 October 2026)

**Baseline verified before fixes.** On the guarded, embedded fixture, New task and a controlled free-text question lost
both drafts on reload. Aborting the answer response produced “Your answer was not sent; it is safe to try again.”
Quick add likewise lost its title on reload and left Add enabled after an aborted save response. Controlled Git health
responses reproduced enabled success, enabled read/request failure, explicit disabling and generic unavailable states;
errors offered Check now but did not explain explicit disabling or identify retained results as last known. A controlled
malformed run response triggered the task-panel boundary; after the mock was corrected, Try again still redrew its cached
bad evidence without refetching it. Baseline screenshots and `baseline.json` are in `.impeccable/review/workstream-5/`.
Early health captures accidentally reused the Git cache; the baseline was rerun with reloads so each response was actually
consumed. The free-text flag is a browser response mock; the seed's real decision only offers Yes/No.

**Changed behavior.** Health now recognizes only the existing explicit `503 / unavailable / repository health is not
enabled` contract as disabled. Generic 503s and unreachable reads remain unknown. Failed reads retain findings and scores
as last known evidence and offer Check now beside the explanation; they no longer proclaim “Nothing needs you.” The
boundary's retry refreshes existing task/run/question read APIs before resetting the panel, and a failed refresh stays
beside the retry. Connection banners give the safe refresh step without promising agents stopped.

New task briefs, execution/planning choices, quick-add titles and typed answers persist in tab-session storage, scoped by
origin/project/question. Closing or reloading keeps drafts. Discard draft explicitly clears an unsent New task brief;
success or opening a known saved task clears its creation draft. Before a save/start/answer request, the frontend persists
an uncertainty marker. Lost, server-error and malformed responses keep that guard through reload. Task saves and starts
are not replayed: Check Board or View saved task is the adjacent next step. Controller refusals remain correctable and
retryable. Uncertain answers offer Retry same answer and disable competing choices, using the controller's existing
one-answer acceptance/conflict guarantee. Saved answers still wait for runner acknowledgment; the shell still uses only
its existing Individual relay and refuses stale/offline answers. Recovery buttons on Health, the boundary and question
cards measure at least 44px high on phones/coarse pointers. Existing optional setup failures keep their quiet treatment.

**API evidence and dependencies.** `internal/api/githealth.go` emits the explicit disabled contract when its health service
is absent. Normal startup in `internal/controller/controller.go` installs that service; the disabled browser fixture alone
is not a production health defect. No authorized UI/API enabling switch exists, so the explanation has no invented Enable
link. An enabling path would require a separate backend/product decision. Task creation/start APIs expose no request
identity for resolving a lost response. This pass cannot prove that an absent Board item means a save never committed, so
it retains the uncertain draft and refuses automatic replay. Automatic reconciliation/release of that guard needs a
controller idempotency or request-status contract, outside this pass. Answer acceptance and repeated/competing-answer
behavior were read in `internal/service/runner_events.go`, `internal/service/runs.go` and `internal/domain/question.go`, and
confirmed through the existing fixture endpoint. No Go or Team source was edited.

**Regression and rendered evidence.** `scripts/browser-recovery-workstream-5.cjs` requires an explicit fixture URL, checks
`version: browser-fixture`, and uses controlled responses for success, read failure, generic failure, disabled, unreachable
and changed Git evidence. It covers offline entry/reconnect, long German/CJK/Arabic errors, corrected boundary evidence,
brief/planning/answer reload recovery, rejected versus committed-but-lost task saves, guarded quick add, unchanged saved
answer/timestamp on repeat, keyboard reachability, and phone/coarse-pointer/reduced-motion behavior. It passes against the
rebuilt/restarted/reseeded embedded UI. `recovery-verified/` contains 54 captures: 13 cases at 1440×900 and 375×812 in both
themes, plus offline entry and a coarse-pointer capture. Phone recovery targets are asserted at 44px and the answer retry
is scrolled fully into view. The original routed matrix measured 5,326 text nodes across 28 pages × two themes × two sizes,
with zero contrast or text-size failures. The final fresh-seed measurement reproduced those results on the final build;
its JSON and 120 captures are in `legibility-final.json` and `surfaces-final/`. Earlier captures remain in `surfaces/`.

**Checks and failures.** Targeted Vitest recovery/question tests passed (35 tests); all web tests passed (45 files, 538
tests), Svelte check reported zero errors/warnings, lint passed, and production web/embed/shell builds completed. Full
`make check` runs passed; logs are retained under `workstream-5/`. The unified desktop browser suite and both assistant
suites passed, including stale/offline refusal, acknowledgment, reduced motion, confirmations and draft behavior. Vite
continues to report its main chunk above the 500 KB advisory threshold; builds succeed. The
Impeccable detector reported two existing false positives: QrImage's dynamic `src` and an intentional malicious-image
string in the assistant transcript test; neither is a broken product image.

The broad browser suite first exposed my failure to clear a known saved task's creation draft on View saved task; that
was fixed and its creation journey passed on rerun. A later wrapper run expired at 120 seconds. Running the Individual
script directly identified a stale capture assertion from workstream 4: it entered Git at phone width and waited for the
desktop navigation. The harness now enters the route at desktop width and separately asserts the phone's Git chooser,
without extending timeouts. In the final `make test-browser` run, **Individual and Team browser flows passed**, but the
separate bridge integration failed with **“Integration state did not settle”** in its existing polling window. The bridge
failure is retained in `browser-suite-verified.log`; the full command is not claimed as passing and its timeout was not
relaxed. Prior failure logs and the direct run are also retained. No Team or Go fix was attempted. A final legibility
rerun initially refused its baseline vocabulary assertion because the recovery test had already answered the seeded
question. The fixture was restarted and freshly seeded for the measurement; no assertion was removed.

**Limits.** Drafts last for this browser tab session, not across closing the tab or switching devices. Denied storage is
reported in the form; reload persistence cannot be guaranteed there. Browser mocks establish frontend handling, not that
production health is disabled or failing. No human comprehension study, physical touch device or VoiceOver session was
conducted, and no new heuristic score is claimed. Fixtures were rebuilt/reseeded after UI changes, and only owned fixture
children are stopped after verification. The release is PATCH `4.18.5-preview.1`, committed and tagged locally; nothing
is pushed.
