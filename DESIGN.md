# Werkbord design

The visual system of the individual app (`web/`) and, from Werkbord Team 2.2, the Team console. It is the
identity sheet "Werkbord · Identity — Quiet technical · v1" made into code. The landing site
(`werkbord-site`, `assets/css/site.css` and `app.css`) copies the same tokens; when they disagree, the sheet wins.

## Character

Quiet technical. Paper and ink, one action colour, one attention colour. The tool disappears into the work:
density over decoration, every state readable at a glance, nothing hidden that you need weekly.

## Voice and language

Write calmly, directly and in active voice. Name the action and its object, then explain the consequence or next step where
it matters. Keep useful technical terms when the nearby copy explains them. A message must describe only what the observed
state proves; saved is not delivered, unreachable is not stopped, and a failed or timed-out mutation may have an unknown
outcome. Tell people to check uncertain outcomes before repeating an action.

Use British English in new Werkbord prose: *colour*, *organise*, *authorised*, *synchronise*, *cancelled* and
*acknowledgement*. Preserve brand and provider names, commands, API and domain identifiers, provider output, and text
entered by people. Individual calls its board item a **task**; Team calls its coordination record a **ticket**. Explain that
opening a Team ticket in a person's Werkbord creates a local task for the handoff. Keep **plan** for work dates shown in Calendar or Timeline,
**schedule** for agent starts, **Review** for inspection, **Done** for completed work, and **Archive** for work removed
from the active board.

## Tokens (`web/src/app.css`)

| Role | Light | Dark |
| --- | --- | --- |
| Paper (`--bg`) | `#F5F5F2` | `#0B0B0C` |
| Surface (`--surface`) | `#FFFFFF` | `#141415` |
| Tray (`--surface-2`) | `#EDEDE8` | `#1D1D1F` |
| Line (`--border`) | `#DAD9D3` | `#2A2A2D` |
| Ink (`--text`) | `#0A0A0A` | `#F0F0EC` |
| Secondary text (`--text-2`) | `#55554F` | `#A3A39D` |
| Cobalt (`--accent`): actions, running work | `#2447FF` | `#7C93FF` |
| Amber (`--amber`): needs you, pending | `#F2A93B` | `#E0A84A` |

Status: running = cobalt, needs you = amber (`--warn` `#B7791F` as a dot), blocked = purple `#7B4FC0`,
ready/done = green `#2F8F55`, failed = red `#C2413B`. Use the `*-text` variants for status words on paper.

Theme follows the OS unless the person picks one (sun/moon in the rail, stored per device as
`werkbord.theme`; `public/theme.js` applies it before first paint because the CSP forbids inline scripts).

## Type

Geist for the interface (400/500/600), Geist Mono for machine text: paths, branches, IDs, counts, times,
commands, keys, and the uppercase section labels (`.lab`, 11.5px, `letter-spacing: .14em`). Both are bundled
(`@fontsource-variable/*`), never loaded from a CDN. Page titles 20px/600; card titles 14px/500; body 13–14px.
The wordmark is `werkbord`, lowercase, Geist Mono 600, tracking −6%.

**The legibility floor:** no text is smaller than 11.5px, and all text is at least 4.5:1 against what is behind it (3:1
for large text) in both themes. Status words on paper use the `*-text` tokens; the bare `--ok`, `--warn` and `--danger`
are for dots, borders and fills. A dimmed state (a zero count) lowers the dot, not the words. `web/src/lib/contrast.test.ts`
checks the tokens and `scripts/measure-legibility.cjs` every rendered screen ([docs/LEGIBILITY.md](docs/LEGIBILITY.md)).

## Material

- Raised: buttons (`.btn`, `.btn.primary`), cards (`.card`), keys (`.key`). Gradient face, light top edge, soft drop.
- Inset: trays (`.tray`: board columns, wells), form fields (`.input`, `.select`), the current nav item.
- Corners stay at 4–8px (trays 9px). Counts are pills: cobalt `.num`, amber raised `.num.pend` for what waits.
- Machine text sits in a `.well` (tray, mono 11.5px).

## Mark and icons

The pixel mark (`lib/Mark.svelte`): three slanted bars, eight rows, one flat shade per row; ink, cobalt, amber.
It uses the light ramps on paper and the dark ramps on ink (`light-dark()`). The app icon and favicon are
always on ink with the dark ramps (`public/icon.svg`, `favicon.svg`, PNGs generated from them). Icons
(`lib/Icon.svelte`) are drawn on a 16px grid, 1.4px stroke, round caps. No emoji, no glyph icons.

## Layout

- Standalone browser desktop (≥900px): a 220px rail (Needs you, projects with live counts, runners, Analytics, Settings, theme)
  and a main column. Inside the Werkbord desktop app the window's one sidebar replaces this rail
  ([ARCHITECTURE.md §22](docs/ARCHITECTURE.md#the-window-and-its-workspaces)). Inside a project: header (path · branch, name, Jump to ⌘K, project
  settings, New task) and the section tabs. The window is the app: views fit the viewport and scroll inside
  themselves (board columns, panel feed), not the page.
- Phone (under 900px, outside the desktop app): the configurable Control Deck, nine positions in a matte housing, is home, and the bottom bar has two
  places, the deck and the Assistant ([docs/MOBILE.md](docs/MOBILE.md)). The deck is the one place the flat paper gives way to material,
  built from the same tokens; every other phone page is the desktop's page in a phone layout.
- A task opens as a panel over its board (`#/p/<id>/task/<id>`); Esc closes it.
- Project tabs in both Individual and Team: Overview · Board · Calendar · Git · Runs. Timeline is a presentation
  within Calendar, selected beside the Calendar presentation; it is not a sixth project tab. Calendar opens on a week.
  Individual work projects keep the same five tabs: Git explains that there is no repository, and Runs explains that
  there is no execution history. Their Calendar shows planned work. Pages use `.split` (main + side column, each
  scrolling inside the window) and `.pn` panels with a `.ph` header; groups that want an action sit in an
  inset `.slot`. Filters are `.pick` (a raised button that is a select); few-way choices are `.seg`.
- Settings show one section at a time beside a section list; each section has an address (`#/settings?runners`).
- A narrow window of the desktop app (<900px; a phone in a browser has the Control Deck instead): sticky heading, the page scrolls, bottom tab bar (Needs you · Board · Calendar · Git · Runs · More;
  one equal column per tab, including for work projects), floating New task button,
  horizontally scrolling Kanban columns and tables as two-line rows. Git and Settings use a compact, labeled native
  section chooser. Its selected option states the current subsection;
  choosing one keeps the existing hash address, direct links and browser Back behavior. The desktop section navigation
  remains visible at wide widths. In the desktop shell's intermediate-width icon rail, an accessible expand control
  reveals the section names and project list without changing workspace or project routes.
  The installed app starts on the Control Deck (`start_url` `/#/deck`; on a wide window that address is settled to Needs you).
  In the desktop shell's narrow window the bottom bar above stays as it was; outside it the phone has the two-place bar and the
  Control Deck described under Layout. The board opens on the first column holding something that needs you (a question,
  a reply, blocked work, or work in Review). Buttons that answer from a card are at least 44px tall, and a task
  panel's actions wrap onto their own row rather than clip. A loaded task detail has one visible close control,
  with a 44px target on phones.

### The window's workspace selector

Universal is a peer choice beside Individual and each Team workspace. Its sidebar destinations are My Work,
Calendar and Needs you, spanning all workspaces. Individual's workspace destinations are Needs you and Projects,
with Analytics immediately below Add project. Each Team workspace lists Needs you, Projects, Reviews, Members and Analytics.
The shell keeps Assistant, Workspaces and devices, relevant workspace Settings and theme controls separate from
these destinations. Universal has no workspace-specific Settings or project list. Project sections belong above
the project's content, not in the workspace sidebar. The window uses its own 208px sidebar, collapsing to 48px;
the intermediate-width label control reveals the names without changing the route.

### Team console

Team uses the same paper, ink, Geist, icon strokes and action colours, with its own coordination sections:
Needs you · Projects · Reviews · Members · Analytics. Desktop has a 232px rail (248px above
1800px), project shortcuts and Settings at the foot. Changing projects keeps the current project section. Inside the
desktop app the window's sidebar replaces the rail. A project's Overview, Board, Calendar, Git and Runs are tabs above
its content; Project members appear in the project header; membership and invitation management live under Project settings. Calendar shows planned dates and shared schedule requests;
Timeline is its alternate presentation. Settings contains This computer, MCP clients, Labels, Devices, Workspace Hosts,
Connectivity, Backups and License, subject to the person's permissions;
the existing `?tab=…` addresses still work. Host resilience lives under Settings → Workspace Hosts.

The workspace's Needs you view shows Working now beside Needs you and Projects. Lists use spacing and dividers;
cards are reserved for movable board tickets and focused ticket details. Team page titles are 24px,
ticket titles 15px. The board fills the available width and height, with each column scrolling internally.
The five Kanban columns stay side by side at every width, scrolling horizontally when they no longer fit. Below 900px, primary navigation
becomes a sticky scrolling row with an always-visible Settings button; the document scrolls, and actionable
attention appears before the working list. N opens a new ticket and Escape closes ticket details.

⌘/Ctrl K and the heading's Search button open a focused jump list of sections, projects and tickets. Its input keeps
focus through live updates; arrows select and Enter opens. Escape restores the original focus and unsent edits.
Members is an organization directory: initials, names, email and roles, with filtering and management in row menus.
Light/dark sits beside Settings (an icon on phones), with Team's own per-device preference, independent of the
individual app. `theme.js` resolves it before styles load and keeps the mark and browser chrome in step.

Team Runs shows the latest member-supplied execution report per ticket/device for at most 32 assigned tickets per project read. These are
reported states and timestamps, including stale/unknown runner availability, rather than a complete run-history
browser. Raw output and full history stay with the owning member's Werkbord. Team Git presents reported repository
metadata; it does not inspect or change a member's repository, execute agents or verify their work.

## The assistant surface (4.11)

A full-window conversation, opened from the Assistant button (the top of the rail; the sidebar in the desktop window; an icon in the
heading on a phone). It is the quietest screen in the product: paper, one column of at most 46rem, no history list, no chrome beyond a
heading (the provider chooser, New, Close) and the box.

- **Movement.** A circle grows from the button's centre until it covers the window (950ms, `cubic-bezier(.72, 0, .18, 1)`), with a cobalt
  light along its edge that fades as it arrives,
  while the background moves from `--surface` (the sidebar) to `--bg` (paper): `#FFFFFF → #F5F5F2`, `#141415 → #0B0B0C`. The content
  arrives from 540ms in, in a short sequence from the heading to the composer. Closing runs it backwards, 30% faster, into the button.
  It can be turned round in either direction half way. In the desktop app a dedicated transparent frame covers the entire viewport,
  including the sidebar, while the workspace beneath keeps its state. Under *reduce motion* it
  is simply there or gone: the movement is a Web Animation, so the page checks the setting itself and the global CSS rule that
  switches off transitions is not what it relies on.
- **Reading.** A reply is plain text on paper, 15px/1.55, no bubble; what you said is a tray (`--surface-2`) at the right. Progress is
  one quiet line, never operation names. A change is a raised card with a status dot and word on its label: amber for "Needs you", green for "Done", and red for "Failed".
- **The box** is raised, 14px corners, with attach, dictate and send/stop at its edge; 16px on a phone so the page does not zoom, 44px
  targets. The send arrow is cobalt only when there is something to send.
- **Dictation** shows the words in the box while listening. X cancels and preserves the earlier typed draft; the tick finishes into
  an editable draft; Send waits for transcription to finish before sending. Failed recognition keeps the draft for review.
- **Not a place to browse.** No history, no list of conversations, no search of them. *New* starts again.
- **Counts on the button.** Amber `.num.pend` for changes waiting; a cobalt pulse while a reply is on its way.

## Interaction rules

- **Vocabulary.** Needs you names attention; Questions is its question filter. Calendar contains Calendar and Timeline
  presentations. A repository project's Calendar schedules agent execution ("Not scheduled to run"); work projects
  show planned work. Timeline plans work dates and milestones ("Not planned yet"). Team Calendar combines planned
  dates with shared schedule requests that wait for the owner's runner and approval. Review means work is ready
  for a person's inspection, Done means the work was completed or merged, and Archive keeps a task out of the active
  board. Accept work / Reject work record a verdict; Merge… changes the repository after confirmation.
- **Attention counts.** The Needs you and project interruption badges exclude work ready for review; the Needs you
  page's All list includes it. Keep that scope visible beside the counts and in their accessible names. Project summaries
  call pending questions "Needs you".
- **Planning dates and execution schedules.** Date fields plan work; they do not start an agent. Repository Calendar
  schedules when the owning Werkbord starts agent work. Explain this at both sets of date controls. Work projects keep
  planning fields and have no execution controls. Individual's New task shows planning fields at the bottom of the
  form, outside the collapsed run-settings disclosure.
- **Touch and keyboard.** Answer choices and card actions are at least 44px tall on phones and coarse pointers, with
  spacing between choices. Coarse-pointer form fields use 16px text to prevent zoom. A visible-on-focus skip link moves
  to the main work region in both the Individual app and the desktop shell. A task panel receives focus on its heading
  and returns focus to its opener on close. Task detail view controls are a labelled button group, not a navigation
  landmark; links remain the navigation pattern. These rules complement the 11.5px type floor and shared disclosure
  chevron above; compact secondary controls still need a separate touch-target audit.
- **Navigation levels stay named.** The phone's primary destinations remain visible in the bottom bar; More retains
  Overview, Project settings, Projects, Settings and Help, with Overview and Project settings titled on the page. Help
  is also linked from Settings and the desktop rail, and is a global destination in Jump to. Its search covers only
  bundled Help articles and shortcut guidance, never assistant conversation history. Git and
  Settings show a visible level label beside the selected
  section chooser on phones. In the desktop shell, the narrow rail's expand/collapse control has a matching accessible
  name and tooltip so icon-only navigation can reveal its labels and project names.
- **Shortcuts respect editing.** Jump to and New task are available from the page, but app shortcuts do not intercept
  keystrokes while an input, select, textarea or editable region has focus.
- **A confirmation keeps focus.** Only the topmost Sheet handles its keyboard events. Tab and Shift+Tab cycle through
  visible enabled controls, background focus returns to the Sheet, Escape closes it, and closing restores a connected
  opener. Collapsed content and disabled actions never enter that cycle.

- **One clock.** Every time of day is written by `lib/clock.svelte.ts`: 12-hour or 24-hour as the device's locale writes it, unless
  the person picks one under Settings → Alerts and display (kept per device). The Calendar's hour column, its events, the run
  history, the feed and the desktop Calendar all follow it; nothing hard-codes `HH:MM`.
- **One disclosure.** Every `<details>` shows the same chevron, which turns down when it opens (`app.css`); the browser's own
  triangle is not used, because it disappears on a flex summary.
- **An empty view says so.** A Calendar week with nothing scheduled says "Nothing is scheduled to run this week" above the grid.
- **Individual speaks of the work.** Its surfaces omit label controls and Human/Agent/Hybrid work-mode vocabulary;
  existing label and work-mode metadata is preserved. Repository execution controls still name agents and runners
  where they are relevant. A work project's board has no Agent or Runner filter or agents legend, and its task panel
  has no Runs tab. Its project-level Git and Runs tabs remain visible with truthful empty states.
- **Recovery keeps uncertainty visible.** Git health recognizes disabling only from the controller's explicit
  disabled response. Failed reads say current health is unknown; retained findings and scores are last known evidence,
  never a new healthy result. Check now reads Git metadata and leaves repository confirmations under controller checks.
- **Drafts stay with their source.** New task briefs, execution/planning choices, quick-add titles and typed answers
  remain in origin-scoped, tab-session storage, keyed by project and question. Closing or reloading keeps an unsent
  draft; Discard draft clears an unsent New task brief. Storage failure explains that reload recovery is unavailable.
- **A lost response is not a refusal.** Persist an uncertainty guard before sending a task save or start; network,
  server and malformed-response failures keep it through reload. Check Board or View saved task is the next step,
  without replaying the mutation. Opening a known saved task clears its creation draft. A controller refusal permits
  correction/retry. An unconfirmed answer offers only Retry same answer, using the controller's one-answer guarantee;
  stale/offline shell guards and runner acknowledgement still govern delivery.
- **First run is quiet.** Failing to look up an optional connection (GitHub, phone access) is a muted line with *Try again*;
  red is for something the person asked for that failed.
- **A blocked permission is an explanation, not a button.** Alerts blocked by the browser say where to allow them.
  The browser-permission state and local opt-in are rechecked when the window regains focus, so Off does not imply
  that pressing a button can override denied browser permission. Alerts require this browser page to stay open.

- First-run setup leads with three local requirements: an enabled online runner with capacity configured, a usable coding
  agent and a registered Git repository. A planning project does not count as a repository. One primary action advances
  the missing requirement or opens task creation; planning remains available separately. Phone access and GitHub are
  optional connections, collapsed initially, and skipped connections stay labelled as skipped.
- The window's My Work view separates reported running, queued and starting states. Idle turns and unanswered questions
  have different labels. Source timestamps say when the state was recorded; they do not claim agent activity. Offline
  or stale reports retain their last state with availability unknown. Pending requests remain awaiting acknowledgement.
- The window's Needs you view orders urgent problems, actions and operational warnings. Each row preserves its workspace,
  project and detail, and names its next action. Individual questions expand in place with context before choices; answering uses the owning Werkbord and stays disabled for stale or offline reports. Team rows open the owning workspace. Saved answers remain unconfirmed until the runner confirms delivery. Only the first urgent or actionable item has primary emphasis.
- In the window's Needs you view, a collapsed question row says what the agent asked. When expanded, its row changes to
  "Answering this question" while workspace, project and task provenance stay visible; the answer card states the
  question once. Stale/offline refusal and pending acknowledgement remain unchanged.

- What needs you is never more than one click away and always says what it wants (the question itself, on the card).
- Inline actions on cards: Start, Approve/Deny, Reply, Merge…, Move to Review, Run again. A Runs row whose run waits on a
  question has an Answer button that opens the same question card beneath the row.
- **One name: "Needs you".** Individual actionable attention uses it in the rail and tab, the card's eyebrow ("Needs you · Decision"), the page title, the banner ("1 question needs you") and the notification. A row's kind in the list is its plain type (Question, Blocked, Failed). A shared execution report may say "Waiting for an answer" when it does not establish that the current viewer can answer it.
- **A question is stated once per screen.** The banner stays out of the way where the question is already asked with its
  answer: the task panel that has it open, and the project's Overview, Board and Runs. In the task panel the dock below
  the feed is the one statement; the feed shows the question again once it is answered, and the status line does not.
- **Archive, then undo.** A task panel's "Archive task" moves the task to the board's Archive and says so in a toast with
  Undo (12 seconds), which puts it back in the column it left. Nothing is called "closing a task".
- **Review starts at the task title.** A task in Review leads with its title and Review changes in the task-panel header.
  Merge… sits beside it only when current local Git evidence says the run's own branch is ahead and unmerged; it opens
  the same confirmation Sheet as the board, and a successful merge moves the task to Done. The header summarizes branch
  or pull-request provenance and whether tests were reported, never as verified success. Full run and Git evidence stays
  in a clear disclosure. Accept work / Reject work record a verdict (counted as Kept in usage by runner and agent); they
  never merge, move or archive the task.
- **Assessment stays visible.** Accept work / Reject work update the owning project scope's run cache and the displayed
  run together. Accepted, Rejected or Not recorded appears beside the choices; a saved assessment can be changed later.
  An assessment remains separate from confirmed repository actions and board movement.
- **An answer's delivery is confirmed separately.** Answering a question, on a card, in the task panel, on Runs or in the
  window's Inbox, shows whether it was saved or the agent's receipt was confirmed. Receipt does not claim the agent has
  resumed work.
- **Only a refusal of a permission is red.** An approval's Deny or Reject is drawn as a refusal; a "No" to a decision is an
  answer like Yes, in the neutral button.
- What needs a person is one list (`lib/attention.ts`): Needs you shows it for every project in
  segments, a project's Overview for that project. Each item is answerable where it is shown.
- Numbers are only what was measured: the run history strip colours half hours by real runs; usage says
  which runs reported tokens or cost.
- **One board presentation, separate owners.** Both boards use the product-neutral static card styles and pointer/keyboard
  transport in `internal/uipresentation`. Each product supplies its own data, permissions and transition handlers;
  shared presentation imports no product behaviour. Individual cards show the brief (clipped to 180 characters) and
  planned dates when present. Card-body drag, pointer-handle drag and Left/Right arrows on the move handle reach the same
  owning board's transition checks. Coarse-pointer handles have a 44px target.
- Individual board drops follow the work: Backlog → Doing starts an agent where execution is available; Review → Done
  opens the merge confirmation when Git merge is applicable and moves the card once merged; other permitted drops
  move the card. Team drops follow Team's role and ticket-action permissions, never local execution or Git operations.
- **Dates move where they are shown.** Individual, Team and Universal calendars and timelines use the shared pointer/keyboard date transport in `internal/uipresentation`. Dragging a planned card moves its inclusive date range; timeline edges resize it. Scheduled events reschedule agent starts and preserve time of day. Alt + Left/Right moves a day; Alt + Shift + Left/Right resizes. Controller and Team validation remain authoritative, and a refused drop preserves the original position with actionable feedback. Clicking opens an editor beside the current calendar or timeline, with Save changes. Universal sends bounded requests to each authenticated workspace frame, without changing the visible workspace or gaining execution authority.
- **Timeline zoom.** Day, Week, Month and Quarter provide progressively broader scales. Ctrl/⌘ + wheel zooms; ordinary scrolling pans the chart.
- **Timeline keeps names quiet.** The name column shows task or ticket titles without repeating dates. A custom tooltip
  reveals the title and planned span immediately on hover or focus. Week is the default scale; Today recentres in the
  available chart width after resize, accounting for the fixed name column.
- Anything that changes the repository is confirmed in a `Sheet` with the controller's own checks.
- Keyboard: ⌘/Ctrl K Jump to (projects, sections, tasks, actions), N new task, Esc closes.
- Motion is state, not decoration: 150–250ms, `cubic-bezier(.16, 1, .3, 1)`, and none under reduced motion.

### Final verification refinements (4.19.1)

Needs you's All, Questions and Review choices are native filter buttons in a labelled group, with pressed state.
Tab and Space/Enter activate them; they do not announce a tab widget or imply arrow-key tab navigation.
Count scope is unchanged. The attention-card Review action follows the same 44px phone/coarse-pointer height
floor as its answer actions. In the desktop shell, reported execution text wraps within the My Work status cell;
the dot remains compact and supplementary. Reported state, timestamps, stale availability and acknowledgement
remain visible. These refinements do not establish physical-device or screen-reader usability; see the final audit
and the unconducted five-developer formative test guide for evidence limits.
Git branch headlines that call work ready for review use `--warn-text` for readable status words; `--warn` remains
appropriate for the accompanying border/dot. Final legibility checks include Git subsections and branch, commit and
file drill-downs rather than treating the overview as evidence for text hidden elsewhere.

### Review flow refinement (4.19.2-preview.1)

- A task in Review keeps Accept work / Reject work with its branch/result summary. The nearby explanation says this
  records an assessment and does not merge or move the task. Saving is announced while awaiting acknowledgement;
  on phone, the status sits above equal-sized verdict choices. Repository actions remain separate and confirmed.
- Review changes carries its task/run origin into the branch and changed-file views. Git repeats a compact result and
  reported-test status only while current task, run and branch records agree; it labels tests as reported, not verified,
  and links back to the task's full evidence. The return and Open run links select that exact run, including when it is
  not the task's latest. Direct Git routes carry no task evidence.
- A branch detail view leads with Review changes. Branch actions is a keyboard-operable native disclosure for merge,
  push, cleanup and less frequent operations; task and run destinations appear once as working links. Repository
  confirmations and cancellation remain available inside it.

### Analytics views

Individual analytics uses Activity, Execution, Usage and Completion tabs. Activity compares created tasks and moves to
Done over UTC days; Execution compares run outcomes; Usage compares measured tokens or API costs with reporting coverage;
Completion keeps recorded assessments and separate completion signals visible. Each tab retains its own project and
period filters. Provider filters belong to execution, usage and completion; runner filters apply to execution and usage.
Activity's series choice and Usage's measure choice change the chart, rather than the underlying report scope. Exact daily
values, execution breakdowns and complementary evidence remain in disclosures. Unknown measurements remain distinct
from reported zero, and cost estimates are separate from reported costs.

Team analytics uses Activity, Categories and Completion tabs, each with its own project and period. Category comparisons
use current labels and explain that tickets may appear in multiple categories. Team exposes coordination evidence only.
Project and period filters keep the console's explicit Apply / refresh snapshot behaviour. Both tab bars support arrows,
Home and End, show selected state and retain visible labels at narrow widths.

Every desktop window opens on Universal → My Work. Stored workspace locations remain available when a person explicitly
opens that workspace; they do not choose the startup surface.

### Configurable phone Control Deck (4.46)

The initial deck keeps Needs you, Calendar, Projects, My Work, Agents and Analytics, with three empty sockets. An icon-only pencil in the header enters editing; its checkmark saves the deck. Every position can be replaced, removed or moved, including by keyboard through Move to
position. Empty sockets remain empty. Preserve the keycap materials, themes and reduced-motion behaviour.

Workflow keys show their name, icon and repository target (or reported run state). Tap executes a configured workflow; hold,
Shift+F10, context menu and the editing settings control configure it. Configuration never starts work. Workflows target
Individual repositories; ordinary task views provide output, questions, review and cancellation. Phone preferences live in the
controller per enrolled phone. The redundant attention strip is removed; Needs you retains its live count and destination.
Edit Deck, Search and Help sit beside Settings in the header with 44px targets. The per-phone tour explains editing and execution.

### Persistent phone workflow execution (4.48)

A workflow tap keeps the Control Deck open. The execution banner sits below the header and stays accessible while scrolling
and navigating phone screens. Each repository run has its own labelled Stop action and stage count; terminal results remain
available in history. Opening the banner shows a vertical, expandable sequence with controller-observed preparation and
explicitly agent-reported investigation stages. Missing stage reports stay pending; no elapsed-time percentage is shown.
The monitor uses durable task/run history and the existing event stream, catches up on reconnect, and treats an unacknowledged
Stop as uncertain. Completed evidence stays available after cancellation or partial failure. Use the existing bottom sheet,
status text colours, 44px actions and reduced-motion behaviour.

### Interactive phone onboarding (4.49)

The phone's first-use deck guide is inline above the existing tactile housing. It uses the actual button library, hold/keyboard
configuration and execution monitor. Keep one short instruction beside the action, a six-step count, and 44px controls. Require
one workflow among up to three additions; leave unused sockets empty. The introductory configuration emphasises the setup
repository and inherited agent/model defaults. Unavailable execution offers an explicitly labelled interactive preview with
no simulated findings or completed stages. Finish remembers completion independently of subsequent deck edits. Existing setup
and desktop tours remain unchanged.

### Compact phone deck (4.49.1)

The Agents key carries the exact live running count in its corner badge, hidden at zero. The separate agents activity row and
Control Deck title/edit row are removed. The header pencil edits the deck and changes to a checkmark to save, with accessible
labels and pressed state. Onboarding explains the pencil, checkmark and Agents badge.

### Phone workflow and setup refinement (4.50)

The first six Control Deck keys stay ready and the bottom three start empty. First use accepts navigation, workflows or empty positions. Workflow keys carry a cobalt-tinted face and a Workflow accessible label. New workflows ask for repositories on tap in a bottom sheet; hold opens saved settings. Find Bugs, Security Audit and Dependency Check default to human-owned draft findings. Execution details link each saved finding to its task under Next actions, including deduplicated findings from repeated scans.

Onboarding checks readiness before presenting a setup action. Verified local runner, agent and repository steps become disabled status controls and leave the active path automatically. Team's connected runner has one disabled Runner connected control; configured but unavailable runners can reconnect without repeating installation. The members directory follows the demo's compact rows with two-letter initials, a tinted self avatar, role and reported device presence. Recent device presence is labelled as such and does not claim that an execution runner is available.
