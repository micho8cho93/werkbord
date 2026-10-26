# UX excellence plan: 30/40 → 40/40

11 October 2026. Baseline: `werkbord-v4.18.1-preview.1`, implementation commit `e5cb15e`.
Scope: the Individual web app and `web/src/shell`, in Operate mode.
Evidence: [final audit](ux-simplification-audit.md#critique-of-4141--final-pass-11-october-2026) and the locally saved
`.impeccable/critique/2026-10-11T14-06-23Z__web-src.md`.

The goal is excellent task completion across the reviewed surfaces. 40/40 is an assessment target, not a promised
result of completing a checklist. Fixes must earn their scores in a fresh independent review. Missing observations
remain unknown; passing automated checks does not establish complete accessibility or user comprehension.

## Boundaries

- Read `AGENTS.md`, `PRODUCT.md` and `DESIGN.md` before each implementation pass. Preserve quiet technical identity,
  existing capabilities, compatibility names, guarded repository actions and truthful execution state.
- Make no Go or Team source changes. Use existing APIs and the Individual shell's existing controller relay.
  If evidence or functionality requires an API change, record the dependency rather than inventing data or crossing
  the boundary. The unified `cmd/werkbord/VERSION` update is the required metadata exception.
- Use disposable browser fixtures only. Never start, stop, install, seed or reconfigure the real controller.
  Never push commits or tags. Full checks may exercise disposable Team services without changing Team source.
- Preserve weekly navigation, deep links, keyboard shortcuts, answering where shown, archive Undo, review/merge
  confirmations, assistant behavior and reduced motion. Do not hide essential work merely to reduce visible choices.
- Verify each defect before fixing it; add behavior-based regression tests. Each implementation commit includes its
  semantic version increment, `make tag` and `make verify-tag`. New Help functionality warrants a minor bump;
  fixes usually warrant a patch. Determine actual versions from the repository at execution time.

## Score and evidence map

The gaps total ten points. Workstreams overlap; their contributions must not be added or awarded in advance.

| Heuristic | Baseline → target | Evidence needed to justify 4/4 | Workstreams |
| --- | --- | --- | --- |
| System status | 3 → 4 | People understand interruption versus review counts, stale reports and answer acknowledgment without guessing. | 1, 5, 7 |
| Real-world language | 3 → 4 | People distinguish planning dates, execution schedules, finished sessions, verdicts and merging. | 1, 3, 6, 7 |
| User control | 3 → 4 | Keyboard, touch and assistive-technology journeys preserve focus, drafts, cancellation and recovery. | 2, 5, 7 |
| Consistency | 3 → 4 | One attention vocabulary and one prompt per answered question; navigation and disclosures behave consistently. | 1, 2, 4, 7 |
| Error prevention | 3 → 4 | No accidental confirmation or duplicate save; checks remain authoritative through changing/stale evidence. | 2, 3, 5, 7 |
| Recognition | 3 → 4 | Weekly sections, review actions and recovery paths are discoverable without remembering hidden locations. | 3, 4, 6, 7 |
| Efficiency | 3 → 4 | Common answer/review/plan tasks complete directly with keyboard and phone, without unnecessary reading or navigation. | 2, 3, 4, 7 |
| Minimalist design | 4 → 4 | Preserve the authored hierarchy while adding accessibility and help; no new dashboard clutter. | All |
| Error recovery | 3 → 4 | Distinct unavailable, disabled, failed and uncertain states explain the safe next step and preserve work. | 5, 6, 7 |
| Help | 2 → 4 | Task-focused local help is easy to find, accurate, contextual and useful during an observed task. | 6, 7 |

## Ordered workstreams

### 1. Make attention and work vocabulary exact

Start with `lib/projects.ts`, `lib/shell/Rail.svelte`, `shell/Sidebar.svelte`, `shell/Inbox.svelte`,
`routes/ControlCenter.svelte`, Calendar and Timeline. Inspect actual rendering and existing count derivation first.

- Replace remaining UI "needs input" summaries with "Needs you". Preserve internal state names and API contracts.
- Explain the existing badge's scope: interruptions exclude ready-to-review work, while the list's All includes it.
  Give counts accessible names and visible contextual explanation; do not silently change what they count.
- On shell question expansion, keep task/project/workspace provenance and show the actual question only in its
  answer card. The collapsed row must still explain what opening it will reveal.
- Put short, specific Calendar/Timeline explanations where dates are chosen. Keep work projects free of execution
  controls and give their planning rows clear task state and project context where needed.

**Acceptance:** mixed question/review counts are understandable; each expanded screen states each question once;
answer options and context remain present; stale/offline disabling and pending acknowledgment remain unchanged.
Tests cover zero/multiple projects, mixed attention, expanded/collapsed states and work projects.

### 2. Complete keyboard, touch and assistive-technology behavior

Start with `lib/LabelManager.svelte`, `lib/Sheet.svelte`, shared controls, project navigation and shell navigation.

- Give color swatches and compact coarse-pointer controls at least 44×44px hit areas without enlarging every visual
  dot. Measure target geometry and spacing; keep desktop density where appropriate.
- Use native radios or correct roving tabindex/arrow navigation for label colors. Expose human color names with
  hex values as supplementary detail; selection must remain visible without relying on color alone.
- Add a visible-on-focus skip link to the main work region and verify focus order through Board, task details,
  Settings and shell. Use link semantics for navigation and tab semantics only for actual tab interactions.
- Extend Sheet regression coverage to nested sheets, disabled fieldsets, collapsed content, dynamic actions and
  disconnected openers. Check Escape, focus restoration and shortcuts while typing and using menus.
- Test at 200% zoom and with VoiceOver, light/dark, reduced motion and real coarse-pointer behavior. Viewport
  resizing alone does not simulate a touch device or prove screen-reader behavior.

**Acceptance:** complete the core answer/review/create flows without a pointer; radio arrows work with one tab stop;
no focus loss or hidden focused controls; no clipped essential action at zoom; names, errors and state changes are
announced clearly. Record real assistive-technology observations separately from automated evidence.

### 3. Put review before ticket administration

Start with `routes/TaskPanel.svelte`, the run result/handoff components and `lib/git/MergeSheet.svelte`.

- For a task in Review, lead with task title and Review changes; place Merge… alongside it when existing evidence
  allows. Keep Archive and Edit available with secondary emphasis. Avoid duplicating the same primary actions in
  several competing blocks.
- Summarize branch/PR provenance and reported test confidence once. Keep full handoff and Git evidence accessible
  through clear disclosure. Never turn recorded test notes into independently verified success.
- Explain Accept work / Reject work beside those controls: they record a verdict and do not merge or move the task.
- Keep local/remote evidence, open-PR restrictions, stale-check refusal, merge confirmation, Done transition and
  Undo semantics intact. Preserve continuation and reply flows for Doing tasks.

**Acceptance:** at 375×812, entering a representative Review task reveals the inspection action immediately and
without expanding details. Test local review, open PR, unverified tests, no changes, failed evidence lookup and
stale merge checks. A user can explain the difference between review, verdict and merge before acting.

### 4. Give phone navigation one clear hierarchy

Start with `App.svelte`, Git section navigation, `routes/Settings.svelte` and `shell/Sidebar.svelte`.

- Prototype a compact secondary section chooser for Git and Settings, compared with the current wrapped/scrolling
  sections. Keep primary project destinations stable and weekly work directly reachable.
- Give every navigation level an explicit current-location label. Make overflow discoverable without relying on
  a clipped next item. The shell's narrow icon rail needs accessible names and an obvious way to reveal labels.
- Preserve More, project switching, browser Back, section-specific addresses and task-panel return location.
  Do not choose a five-tab layout simply because it is conventional.
- Test navigation with long project names, a work project, multiple workspaces, large text and safe-area insets.

**Acceptance:** people can find Runs, Overview, project settings and a named Git subsection unassisted. No hidden
weekly capability, overlapping bottom bar or clipped label. Choose the prototype using observed task completion,
then implement one layout and verify its route/focus behavior.

### 5. Make failure and uncertainty actionable

Start with `lib/git/HealthPanel.svelte`, route error boundaries, New task, question cards and connection banners.

- Reproduce Git health's enabled-success, enabled-failure, explicitly disabled and unknown/unreachable cases with
  controlled fixture responses. The disabled fixture alone is not evidence of a production defect.
- For an explicitly disabled feature, explain what is off and link to a real existing enabling path if available.
  If no enabling path exists in the authorized UI/API, state the limitation and record that dependency. Do not
  invent an enable action or label every 503 response as disabled.
- Keep recovery adjacent to the error. Preserve unsent answers and briefs through disconnect/reload; distinguish
  retry-safe failure from uncertain save/answer delivery. Refuse duplicate or unsafe retries.
- Check long, localized error text, offline entry, lost responses, reconnect and changing Git evidence. Keep
  optional setup failures quiet and actual user-requested failures explicit.

**Acceptance:** each failure explains the safe next step; retry recovers without duplicate task/run/answer or lost
draft; unknown state is not presented as certainty; repository confirmation still uses the controller's checks.

### 6. Add useful local Help without adding noise

Use a bundled frontend Help surface, linked consistently from More/Settings and Jump to where supported. Confirm
its route within the existing frontend router; do not add Go endpoints or remote resources.

- Organize around tasks: answer an agent, inspect and merge work, continue a run, plan dates, schedule execution,
  choose a runner, recover from disconnection and navigate with keyboard.
- Give each entry a short worked example, a link to the relevant existing view and common recovery advice.
  Explain attention-count scope, verdicts versus merging and reported versus verified tests.
- Add contextual Help links only where the evidence shows confusion. Keep the app usable without opening Help;
  avoid repeating long explanations above every primary action.
- Keep terminology and routes aligned with DESIGN.md and actual behavior. Help search, if included, searches
  these bundled articles and shortcuts, not assistant conversation history.

**Acceptance:** help is findable from phone, desktop and keyboard; a person can resolve a realistic confusion with
it; examples make no unsupported promises; links work for repository/work projects; text reads at the same type
and contrast floors. Cover navigation and search behavior with tests, rather than snapshotting every paragraph.

### 7. Prove the journeys, then assess again

Use the [documented fixture startup and seed steps](ux-simplification-audit.md#reproduce-the-isolated-fixture-and-seeded-state).
Keep the waiting question, committed Review task and work project as baseline states; add controlled sparse,
long-content, offline, stale, failed and busy scenarios without touching production data.

1. Capture baseline task success, wrong turns, assistance, critical errors and time for the core journeys. Prepare
   a neutral guide; the user recruits participants. Do not send invitations or messages without authorization.
2. Run a formative pilot with five representative developers, including phone-heavy and keyboard users, plus an
   assistive-technology check. Ask them to answer a question, review/merge fixture work, distinguish verdict from
   merge, plan versus schedule, find a Git subsection and recover a draft after disconnection.
3. Require no critical errors in observed repository/answer actions. Aim for at least four of five participants
   completing each other core task unassisted, with no repeated material misunderstanding. Report actual counts;
   this small pilot is diagnostic evidence, not a population-level claim or proof of 40/40.
4. Run full `make check`, production web/embed/shell builds, the disposable browser/integration suite, unified shell
   and both assistant suites. Extend only checks justified by the changes. Render all final surfaces at 1440×900
   and 375×812, both themes; include focus, reduced motion and failure variants, not just happy paths.
5. Run a fresh independent two-assessment critique under Impeccable's authorization requirements. Keep the design
   assessor blind to the old scores and detector until scoring is complete. Then compare all ten heuristics,
   not just the total, and distinguish observed fixes, hypotheses and untested cases.
6. Update the audit, DESIGN.md rules and local critique snapshot. Only claim 40/40 when the fresh review justifies
   ten 4s with evidence; otherwise publish the honest result and a bounded follow-up for the remaining gaps.
7. Finish each implementation release with the unified semantic version, commit, `make tag` and `make verify-tag`.
   Stop disposable services; preserve review artifacts; never push.

## Delivery sequence and decision gates

Execute workstreams 1–2 first, then 3–5, then 6, followed by the final evidence pass. Help content depends on settled
terminology and navigation. Basic baseline user observation can happen before implementation; the final pilot happens
after the new flows are stable. Keep implementation chunks reviewable and independently versioned.

- **Gate A — trustworthy interaction:** vocabulary/counts, one-question rule and accessibility behaviors pass.
- **Gate B — direct task completion:** review and phone navigation pass their discovery/efficiency tests, and recovery
  is clear. No capability, source evidence or repository guard has been lost.
- **Gate C — understandable product:** Help resolves real task confusion without adding visual clutter.
- **Gate D — independently assessed excellence:** full checks, final render and human observations support a fresh
  heuristic assessment. No points are awarded merely for finishing workstreams. An honest score below 40 remains
  a valid outcome and identifies the next evidence-backed gap.

This file is a plan only. It starts no implementation, changes no product version and authorizes no user outreach,
controller operation, Go/Team change or push.
