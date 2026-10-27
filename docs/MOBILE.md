# The phone: the Control Deck and the Assistant

Under 900px, outside the desktop app, Werkbord opens on the **Control Deck** and has two places in a bottom bar: the **Control
Deck** and the **Assistant**. Everything else is reached from the deck and has a Back. It is the same web app, the same data, the
same sign-in and the same permissions as the desktop layout; only the arrangement is different. The desktop layout (900px and wider)
and the desktop app's window (which has its own sidebar) are unchanged.

Code: `web/src/lib/mobile/`. Wired in `web/src/App.svelte`. Tests: `web/src/lib/mobile/*.test.ts` and `scripts/browser-phone.cjs`
(`make test-phone-browser`).

## Signing in, and what a phone may do

A phone is let in by scanning a QR code, once ([PHONE.md](PHONE.md)): it opens the app already signed in, with a credential of its
own, and stays signed in until you revoke it (Settings → Phones, or `werkbord phones revoke <id>`). It is not the desktop's sign-in:
it is not the controller's token, and it works on a fixed list of routes (`internal/api/phone.go`).

| A phone can | A phone cannot |
| --- | --- |
| Read everything the deck and the task pages show | Change general settings, register or change a repository |
| Create and edit tasks, start a run with the task's own settings, stop or finish it, answer an agent, accept or reject its work | Choose general task/run execution settings (workflow buttons may configure their own scoped execution); pair or revoke runners |
| Use the Assistant, all of it (Team workspace tools need the desktop app, which a phone does not have) | |
| Use Git as the Control Center does: fetch, merge, push, delete a branch, clean a worktree, open a pull request | |
| Read, rename and sign out itself | Make codes, list or revoke phones, read or rotate the token |

What a phone cannot do is not shown: no Add a project, no general run settings or agent pickers outside workflow configuration, no runner pairing or management, no label creation,
no project settings, and a Settings with only Phones and display (`lib/phones/capability.ts`: a browser holding a `wbp_` credential). The controller
refuses them with `403` regardless.

## The Control Deck

A header (the mark and controller/runner status), icon-only Edit Deck (pencil), search, help and Settings controls, and nine positions. The Agents key shows the exact running count in a corner badge, hidden at zero; there is no separate agents activity row or deck title row. The first six navigation keys retain their original positions. The last three start empty and can be programmed during first use with navigation buttons, workflows, or left empty. Workflow keys use a cobalt tint and keep their workflow label, while navigation keys retain their neutral material.
All nine positions are configurable, including an entirely empty deck. Navigation buttons keep their existing routes.

The header pencil opens editing and becomes a checkmark to save: tap an empty position for the library, tap a button to replace/remove it or choose Move to position,
and drag to swap two positions (including an empty one). Done saves to the controller. Workflow settings are available from
the editing gear, a hold, the context menu or Shift+F10. Long presses and drags never execute a workflow.

The library offers Reviews, Git, Runners and six workflows: Find Bugs, Improve Tests, Security Audit, Dependency Check,
Refactor Codebase and Sprint Summary. The original six navigation buttons can also be restored from it. Workflow configuration
supports Current project (the last Individual project opened, resolved when tapped), Selected repositories, All accessible
repositories, and Ask every time. The button shows only its icon and name; the chosen scope is in the configuration sheet and is still read out to screen readers. All targets are registered repositories authorised
for the controller owner; a phone's existing owner grant does not give access to other users or Team execution.

The sheet selects a runner, agent/provider profile, model and supported reasoning settings using the existing execution
catalogue. Empty values inherit project/controller defaults. It also selects report, draft-task or approved-change output,
approval before sensitive actions (optionally before investigation too), and an optional maximum repository count. A scope
over the limit is refused rather than silently truncated. Run Once With Different Settings uses an invocation override and
leaves the saved slot unchanged. New workflow keys ask for targets on tap by default. Asking for targets opens a bottom sheet with repository names and paths and an explicit Run action; opening it never starts work. Holding the key opens its saved settings.

Find Bugs verifies credible findings and avoids existing tasks; its default output creates human-owned Backlog drafts with
file references. Improve Tests finds coverage gaps and proposes tests. Security Audit uses available scanners and classifies
findings. Dependency Check inspects manifests/lockfiles and distinguishes verified advisories from unknowns. Refactor Codebase
proposes targeted changes; Improve Tests and Refactor Codebase can implement changes after approval and validate them with
relevant tests. Sprint Summary includes captured tasks, blockers and agent activity as well as 14 days of Git activity.
Reports and proposals do not authorise edits. Sensitive output requires explicit invocation approval server-side; concrete
changes and subsequent commits, pushes or destructive operations still require human approval through the agent's existing
question/permission flow. Workflow briefs do not add a sandbox or override provider runtime permissions.

**Suggested next steps.** Below the keys the deck shows up to two suggestions for the current project (the same project a workflow
runs against): a finished branch that should be reviewed, an agent waiting for an answer, a Find Bugs scan that is due. Each
says what it rests on and which rule or policy shaped it, and the nine keys, their labels and their layout do not change. A
suggestion that points at a workflow only **marks** that workflow's key with a dashed ring: the key is pressed by the person,
with the approval sheet it already has. If the workflow is not on the deck, the ordinary Edit Deck library opens to add it. On
a phone, *Useful / Not useful / Dismiss* is remembered on the phone only. See [RECOMMENDATIONS.md](RECOMMENDATIONS.md).

Preferences are versioned JSON records in the existing controller settings store, scoped to the authenticated phone ID (or
the local owner for a browser using the owner token). `GET/PUT /api/deck` reads/saves nine explicit positions, checks versions
and rejects unknown fields and buttons. Phone callers cannot select another owner's record. Browser storage is not the layout's
source of truth. An empty saved deck is distinct from a new deck.

`POST /api/deck/workflows` accepts a saved slot ID, an invocation request ID, the current project ID, picker targets for
Ask every time, a validated one-off configuration, and explicit invocation approval when required. It never accepts commands,
policy flags, general task execution changes or resource registration. The server atomically captures exact targets,
execution defaults and a bounded task/run context before creating tasks. Each repository uses an ordinary task and run.
Retries reuse the snapshot, skip existing runs (including completed ones), and continue a partially started batch. Stage 2
accepted requests retain their original source references across upgrades.

`GET /api/deck/history` returns up to 100 task references and their captured configuration; the deck shows the latest eight
with existing session rows. Open a task for output, approval questions, review and Stop. Draft output uses bounded structured
`WERKBORD_DRAFT` findings in assistant output; the run service creates human-owned Backlog tasks with source-reference
deduplication by project, workflow and finding key. Tool output cannot create drafts, nor can model output choose their
project, execution settings or state. Unstructured findings remain available in the report.

The normal task/run services, worktrees, authorised runner routing, capacity, model resolution, permission prompts and
cancellation remain authoritative. Starts continue after a phone disconnects; controller restarts use existing run recovery
and do not automatically rerun interrupted work. A lost start response keeps a tab-persistent guard with Retry same start /
Check saved task. Progress reports observed state and activity, never an invented percentage.

The relay connector honours explicit Individual workflow result links while retaining normal remembered-scope navigation.

The All clear/attention strip is removed. Needs attention retains its route and live count. Search and Help are header icons beside
Settings. The table below lists the navigation catalogue, rather than the initial saved layout.

| Key | Page | Reuses | Counts |
| --- | --- | --- | --- |
| Needs attention | `#/control` | the Needs attention page (`routes/ControlCenter.svelte`) | what needs you (the app's own count) |
| Calendar | `#/schedule` | `lib/mobile/Schedule.svelte`: Month, Week, Timeline | |
| Projects | `#/projects` | the Projects page, each project with what it asks of you | |
| My Work | `#/mywork` | `lib/mobile/MyWork.svelte`, the board's status words | |
| Agents | `#/agents` | `lib/mobile/Agents.svelte`: a card for each agent (working, waiting for you, idle, not available) with its sessions, `RunBadge`, Reply and a Stop that asks twice; the task page for output and questions | runs working |
| Analytics | `#/analytics` | the Analytics page, unchanged | |
| Reviews | `#/reviews` | `lib/mobile/Reviews.svelte`, the task page for the review itself | tasks ready for review |
| Git | `#/repositories` | `lib/mobile/Repositories.svelte` over the Git Control Center's store, four ways: Overview, Pull requests, Branches, Commits; every row opens the Control Center | repository findings that need attention |
| Runners | `#/runners` | a card for each runner (state, run slots, CPU, free memory, as it reports them) over the Runners panel Settings already has, for managing them | |

The icons are the ones the desktop navigation already draws for each place (`lib/Icon.svelte`); the Assistant's is the Assistant's.
Pages have a subtitle that says how their subject stands (`subtitle.ts`: "4 running · 1 waiting for you · 2 idle", "3 online · 1 offline"). Filters are segmented controls with counts. Counts are amber when they are work waiting for you, cobalt when they are work going on, and absent when zero.

### The look

The deck is the one deliberate departure from the desktop's flat paper: a matte housing in the tray's paper with a little grain, a lit
top edge and a dark lower one, and nine sculpted keycaps in dark wells. It is made only from the design system's tokens (`--surface-2`,
`--ground`, `--key-bg`, `--border`, `--text`, mixed with `color-mix()` and `light-dark()`), so it follows light and dark with the rest of
the app. Keys do not stretch: the housing is at most 440px wide and the material around the keys is part of it. A held key drops onto
its well (`transform` and shadow, 50ms down, 160ms back up). With `prefers-reduced-motion` nothing moves; the cap is shaded instead.
Every key is at least 44px square (82px on a 320px screen), and a key is an ordinary link, so it works from a keyboard and a screen reader.

## Navigation

- The bottom bar (`PhoneNav.svelte`) is on every phone page and over the Assistant. The Control Deck tab is current anywhere under
  the deck; the Assistant tab is current while the Assistant is open and opens it out of the tab.
- **Back** (`PhoneBack.svelte`, `trail.svelte.ts`, `nav.ts`) goes where the person came from (My Work, into a task, and back to My
  Work), and to the page above when they did not come from anywhere in the app (a link, a reload): a key's page under the deck, a
  project's page under Projects, a task on its board, Git's screens on Git.
- A project's sections (Overview, Board, Calendar, Git, Runs, Settings) are a compact row of tabs under its name instead of the old
  bottom bar. On the board, one column is shown at a time behind a segmented control.
- Deep links are the existing ones (`#/p/<id>/task/<id>`, `#/p/<id>/git/…`) plus `#/deck`, `#/mywork`, `#/schedule`, `#/agents`,
  `#/reviews`, `#/repositories` and `#/runners`. On a wide window the pages only the phone has are settled to Needs attention.
- The state of the Calendar (view, day, project, zoom) is kept when the person leaves it and comes back.

## Calendar

`calendar.ts` turns every project's tasks into two kinds of item and never mixes them (DESIGN.md: a **plan** is the dates of work, a
**schedule** is when an agent starts):

- a **planned** span (`task.plan`, whole days, or a milestone) is cobalt and drawn as a bar;
- a **scheduled** start (`task.orchestration.scheduledAt`, when the schedule is on) is amber and drawn at its hour.

Month has marks per day and the day's items underneath; Week has the seven days across and the chosen day's hours scrolling, with whole-day
work above them; Timeline reuses `lib/timeline.ts` (range, zoom, bars, milestones and dependency arrows) at Week, Month and Quarter zoom and
scrolls sideways. An item opens a sheet with the task, its plan or schedule, and the scheduler's own decision for it; it links to the task and
opens the same editor the board uses for the dates. Nothing is moved by dragging on the phone.

## The Assistant

The Assistant retains its conversation engine, providers, model picker, tools, dictation, streaming and confirmation cards. Its compact Session selector switches between Salie conversations, opened task sessions and explicitly authorised native CLI terminals. A phone that
has been enrolled may use it through the relay (an enrolled phone is the only thing that can), through every assistant route the desktop uses. On a phone
its surface stops where the bar begins (and takes the room when the on-screen keyboard is up), its Close button gives way to the bar, and the
bar's two places are part of its focus order. Native terminal control requires a separate local-owner grant for each phone and session; it grants no administrative permissions. See [Native terminal sessions](NATIVE_TERMINALS.md#mobile-session-access-phase-4) for controls, reconnect behaviour and execution limits.

## Individual and Team

The relay connector keeps **Control Deck** and **Assistant** as the two main destinations. The deck has no workspace selector.
Inside each section, a labelled **Individual | Team** selector switches that section's scope. Choosing Team in Git keeps Git
open; returning to the deck and opening Projects then opens Team Projects. Choosing Individual reverses this for subsequent
sections. The preference is stored on this phone and survives reloads and visits to the Assistant. It does not change either
lane's pairing, credentials or permissions. Direct workspace links still open the workspace they name.

This navigation belongs to `relay/connector/static/connector/lanes.js`, injected by the connector into the two separately served
applications. Individual's PWA does not import or render Team. A direct private-network Individual connection retains its own
Control Deck and Assistant; the cross-product selector is available through the paired relay connector.

Team phone pages (`internal/team/phone/static/`) show one section at a time from the existing read-only summary: Projects,
Needs attention, My Work, Calendar, Reviews, reported execution under Agents, and reported conflicts under Git. Git does not expose
branches, commits or repository actions. Analytics, Runners and Settings explain the phone summary's limits and keep the
selector available. Failed refreshes clear the displayed summary. Missing Team pairing retains the selector and main
navigation so the person can return to Individual.

Team and Individual retain independent encrypted relay connections and grants. Team phone access is pinned to the member
and workspace that issued it; it reaches only the Team phone static files and summary endpoint. It does not expose the
workspace console, device API or execution routes. The Assistant runs through Individual's existing phone access.

## What the desktop has that the phone does not

- **Analytics** keeps the desktop's own sections (Activity, Execution, Usage, Completion) and filters. It does not have Overview, Work and Agents or
  Runners sections, because the desktop does not.
- **Reviews** is Individual's: tasks ready for review, opened on the task, where its Git review and the one action that settles it already are.
  Team's reviews are Team's.
- **Calendar and Timeline** show; they do not drag. Dates are changed in the same editor the board uses.
- **Provider** for an agent is its name (Claude Code, Codex): the controller reports no separate provider field.
- **Progress** is shown only for what an agent reports: tokens used. No percentage is shown because none is reported.

## Tests

- `web/src/lib/mobile/*.test.ts`: the deck's keys and counts, Back, the calendar's two kinds of time, My Work, agents, Git digests (`npx vitest run`).
- `scripts/browser-phone.cjs` (`make test-phone-browser`, and part of `make test-browser`): in a real browser against the disposable fixture, a phone that scans a
  code is signed in with a credential of its own, a second scan of the same code is refused, its own screens meet no refusal, the owner's things are refused, and a revoked phone is
  signed out while its page is open; Settings → Phones lists, adds (with the countdown) and revokes with a confirm. And the nine positions
  are equal and in three columns with material round them, each opens its page and Back returns, a held key drops (and does not move with reduced motion),
  Month, Week and Timeline, the Assistant's place in the bar, 320 to 430px and landscape in both themes without horizontal overflow, one board column at a time,
  and a wide window that stays the desktop.
- `scripts/measure-legibility.cjs --check` measures the phone-only pages too (the deck, My Work, Agents, Reviews, Git, Runners and the Calendar's three views): no text
  under 11.5px and none under its contrast floor. (It reports one finding that is not on these pages and was there before: dark-theme "Run settings" in the
  desktop task panel.)

- `relay/connector/test/navigation.test.mjs` exercises section-preserving scope changes, remembered scope across deck keys,
  reloads and Assistant visits, revoked-summary clearing, and the real Team phone views at 320, 430 and 844px in both themes.

- `scripts/browser-deck.cjs` uses an enrolled phone and the real disposable runner process: configuring without executing,
  hold versus tap, Shift+F10, drag and keyboard-compatible moves, saved empty slots, execution after refresh, output and stop,
  a completely empty deck, and workflow targets in light/dark at phone sizes. `BROWSER_DECK_ONLY=1 node scripts/test-browser.cjs`
  runs this check alone after the web build. Service tests reopen SQLite to verify preference persistence.

## Interactive first-use Control Deck onboarding (4.49)

Connection, phone enrolment/authentication, computer/agent sign-in, repository setup and the introductory task remain in the
existing setup path. Once an enrolled phone (or narrow browser) opens its deck, the former tooltip tour is replaced by six
inline steps: introduce the actual six-key/three-empty deck, choose up to three extra buttons, hold a highlighted workflow to
configure it, tap to run, open its progress indicator, and finish back on the deck.

The onboarding library separates page navigation (Reviews, Git, Runners) from the six executable workflows. Navigation-only choices and empty positions are allowed; Continue goes straight to the final step when no workflow was chosen. The first six positions are preserved during this stage. New workflow buttons ask for an authorised registered repository on tap; a phone cannot register one. Holding a button can change this to a saved repository selection. If none is registered, the configuration sheet offers a direct interactive preview. The concise configuration sheet teaches repository selection and inherited agent/model
defaults. Find Bugs, Security Audit and Dependency Check produce reports with human-owned draft findings by default. Runner, output, approval and batch-limit controls remain available in full
configuration after onboarding. Saving never executes. Find Bugs is recommended; selecting it uses an empty socket or replaces
the highlighted workflow if all three additions are occupied.

After the repository is selected, Run uses the real workflow API and ordinary authorised runner. Existing progress, stage evidence, agent output and Stop
controls are reused. Missing output never implies a successful or clean scan. The Find Bugs brief asks for “No bugs found in
this scan.” only after a completed scan with no credible findings; unavailable or incomplete investigation must be reported as
such. Opening and closing execution details returns to the finish instruction. Inspection can be deferred and execution can be
skipped without claiming it happened; an active job continues unless explicitly stopped.

If readiness is unavailable, or a start is refused, the guide offers **Try interactive preview**. A preview tap opens a labelled
preview progress control, then a details sheet with the selected repository, waiting stage, zero completed stages, no agent
activity and no scan results. Stop preview/Return to Control Deck ends it. No preview action calls the workflow start API. With no repository yet, the saved button asks for its repository when run later.
An uncertain start retains the existing retry/check-task guard instead of offering a preview that could obscure a real job.

Choices and configuration save through the existing versioned controller deck record. Guide position, highlighted choice and
preview identity resume from browser storage scoped to the phone ID (or owner browser). Finish saves the deck before marking
the existing `werkbord-tour-phone-deck-v2` completion key. Returning users with that key keep their layout and are not forced
through the new guide. Editing or emptying the deck does not reset completion. Help's existing replay action can reset it.
Clearing browser data resets local tour completion, while the controller still retains the authenticated phone's saved deck.

`scripts/browser-deck-onboarding.cjs`, included in the phone/deck browser suites, covers first use, navigation-only completion,
optional empty slots, three additions, setup repository/default configuration, live scripted-runner execution and Stop,
refused execution plus preview, skipping, reconnection/reload, persisted layout/settings and returning users. These fixtures
exercise the real process and API without paid model credentials; they do not establish provider sign-in, finding quality or
physical-phone usability.

### Workflow next actions (4.50)

Find Bugs, Security Audit and Dependency Check buttons start with draft output. Saved findings appear in execution details under Next actions, with Review finding links to the human-owned tasks. Each execution retains its links even when a repeated scan deduplicates a finding to an existing task. Reviewing never starts a run. Report-only output remains available in settings.

### Native mobile sessions (Phase 4)

`make test-phone-browser` and `make test-browser` include `scripts/browser-salie-terminals.cjs`: independent phone enrolment,
per-session grants, real PTY input/control bytes, desktop/phone UI closure, session switching, network loss/recovery,
uncertain-input blocking, task sessions, Salie conversations, termination and revocation. It checks 320px and 430px portrait
and 844px landscape in both themes. Physical phone keyboards remain a manual validation step.
