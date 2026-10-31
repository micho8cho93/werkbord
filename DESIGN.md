# Werkbord design

The visual system of the individual app (`web/`) and, from Werkbord Team 2.2, the Team console. It is the
identity sheet "Werkbord · Identity — Quiet technical · v1" made into code. The landing site
(`werkbord-site`, `assets/css/site.css` and `app.css`) copies the same tokens; when they disagree, the sheet wins.

## Character

Seven styles share a flat layout, icon set, spacing and motion: **Paper** (the default, warm white and charcoal with cobalt),
**Graphite** (cool neutrals and teal), **Classic** (white and cobalt), **Cloud** (cool white and lavender),
**Moss** (sage and forest), **Dune** (clay and warm mineral neutrals), and **Plum** (mauve and aubergine).
Every style includes light and dark modes; Device follows the operating system. Theme changes never replace icons or the pixel mark.

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
opening a Team ticket in a person's Werkbord creates a local task for the handoff. Use **plan** for a structured group of related tasks in Work, and **planned dates** for task date ranges in Calendar,
**schedule** for agent starts, **Review** for inspection, **Done** for completed work, and **Archive** for work removed
from the active board.

## Tokens (`internal/uipresentation/static/tokens.css`)

The table shows **Classic**. Every style defines its own neutrals and accent; see [Styles](#styles).

| Role | Light | Dark |
| --- | --- | --- |
| Paper (`--bg`) | `#FFFFFF` | `#17191E` |
| Surface (`--surface`) | `#FFFFFF` | `#1F2229` |
| Tray (`--surface-2`) | `#F5F6F8` | `#282D36` |
| Line (`--border`) | `#E5E7EB` | `#333844` |
| Ink (`--text`) | `#202329` | `#ECEFF6` |
| Secondary text (`--text-2`) | `#656B75` | `#ACB4C6` |
| Cobalt (`--accent`): actions, running work | `#3458D4` | `#9AACFF` |
| Amber (`--amber`): needs you, pending | `#F2A93B` | `#E0A84A` |

Status: running = cobalt, needs you = amber (`--warn` `#B7791F` as a dot), blocked = purple `#7B4FC0`,
ready/done = green `#2F8F55`, failed = red `#C2413B`. Use the `*-text` variants for status words on paper.

Theme follows the OS unless the person picks one in Settings (stored per device as
`werkbord.theme`; `public/theme.js` applies it before first paint because the CSP forbids inline scripts).

### Styles

The shared palettes live in `internal/uipresentation/static/tokens.css`. Paper has no `data-style` attribute; the other choices
use their lower-case names. Each palette defines identical token roles in light, chosen dark and device dark. Primary fills
are independent of the activity accent, and status text remains legible on all three surfaces. `contrast.test.ts` checks all
fourteen palettes, primary button labels and device-dark parity.

All styles use flat surfaces, hairline dividers, 8px panels and 7px controls. Shadows are reserved for floating surfaces.
Classic now uses this same material. Settings and onboarding offer all seven palettes and Device/Light/Dark controls.
The shell propagates both choices to every workspace on opening and on change. Personal and Team retain independent browser
preferences; the desktop window owns the choice when embedded. Before-paint scripts restore it without flashing another style.

## Type

Geist for the interface (400/500/600), Geist Mono for machine text: paths, branches, IDs, counts, times,
commands and keys. Section labels are sentence case, 12px/500, in Geist in every style. Both are bundled (`@fontsource-variable/*`), never loaded from a CDN.
The scale has six steps: 12 / 13 / 14 / 16 / 20 / 24px. Shared desktop page and Work titles are 24px/600,
with −.025em tracking; other existing page titles use 20px/600. Card titles are 14px/500; body 13–14px.
The wordmark is `werkbord`, lowercase, Geist Mono 600, tracking −6%.

**The legibility floor:** no text is smaller than 12px, and all text is at least 4.5:1 against what is behind it (3:1
for large text) in both themes. Status words on paper use the `*-text` tokens; the bare `--ok`, `--warn` and `--danger`
are for dots, borders and fills. A dimmed state (a zero count) lowers the dot, not the words. `web/src/lib/contrast.test.ts`
checks the tokens and `scripts/measure-legibility.cjs` every rendered screen ([docs/LEGIBILITY.md](docs/LEGIBILITY.md)).

## Material

Every style is flat: surfaces are separated by a hairline (`--border`), not a shadow, and lists use dividers instead of
nested cards.

- Buttons (`.btn`) are outlined on `--surface`; the primary is filled (cobalt in Paper, ink in Graphite). Fields are bordered on
  `--surface`. Trays (board columns, wells) are `--surface-2`.
- Corners come from three radii: 4px (chips, keys), 6px (buttons, fields), 10px (cards, panels, trays), and a pill for counts.
  Counts: cobalt `.num`, amber `.num.pend` for what waits, the only loud colour on a page.
- State is a thin left edge or a dot (cobalt = running, amber = needs you), not a coloured border or glow.
- Machine text sits in a `.well` (tray, mono 12px).

Classic shares the flat controls and divided lists used by every theme.

## Mark and icons

The pixel mark (`lib/Mark.svelte`): three slanted bars, eight rows, one flat shade per row; ink, cobalt, amber.
It uses the light ramps on paper and the dark ramps on ink (`light-dark()`). The app icon and favicon are
always on ink with the dark ramps (`public/icon.svg`, `favicon.svg`, PNGs generated from them). Interface icons
(`lib/Icon.svelte`) use [Lucide](https://lucide.dev/) (ISC; the notice is in `web/public/licenses/LUCIDE.txt`),
drawn with one 1.6px line at every size, except Salie’s restored original two-sparkle mark
(`lib/AssistantGlyph.svelte`). Each meaning has its own icon and no icon is used for two meanings
(`lib/app-icons.ts`; the Team console uses the same set). The app icon catalogue is checked by `app-icons.test.ts` for unique meanings. No emoji or font-glyph icons; Salie’s hand-drawn SVG is the named exception to Lucide.

### Button tones

A row of buttons should read as a row of different things before a word is read. In **Classic** every tone is the same raised button
with one quiet difference: the colour of its edge and label, and a faint glow when pointed at. In **Paper and Graphite** there are
four looks: filled (`primary`), outlined (default; `add`, `open`, `ok`, `warn` fall back to it), outlined with a red edge and label
(`danger`) and plain text (`quiet`). The classes stay, so a label can still say which it is, and Classic can still draw them apart.
No button underlines its label. A lone button needs none.

| Class | Leans toward | Examples |
| --- | --- | --- |
| *(none)* | change or manage | Edit, Settings, Rename |
| `primary` | the one thing a place is for (filled) | Save, Edit with Salie |
| `add` | bring something new in (accent edge) | Add record, New icon, Add to Control Deck |
| `open` | go somewhere or look (accent label) | Open, Calendar, Review |
| `ok` | approve, confirm, finish (green) | Merge, Restore, Keep, Start Team service |
| `warn` | stop or pause, can be undone (amber) | Stop Team service, Remove file, Skip |
| `danger` | remove for good (red edge and label) | Delete, Revoke |
| `quiet` | housekeeping, no relief until touched | Reload, Retry, Copy |

The tones are defined once in `app.css` and mirrored in the Team console's `console.css`. Use them where several buttons stand
together (a page header, a card's actions, a menu); the label decides the tone, not the position.

## Layout

- Standalone browser desktop (≥900px): a 220px rail (Workspace, projects with live counts, runners, Analytics, Help and Settings)
  and a main column. Inside the Werkbord desktop app the window's one sidebar replaces this rail
  ([ARCHITECTURE.md §22](docs/ARCHITECTURE.md#the-window-and-its-workspaces)). Inside a project: header (path · branch, name, Jump to ⌘K, project
  settings, New task) and the section tabs. The window is the app: views fit the viewport and scroll inside
  themselves (board columns, panel feed), not the page.
- Phone (under 900px, outside the desktop app): the configurable Control Deck, nine positions in a matte housing, is home, and the bottom bar has two
  places, the deck and the Assistant ([docs/MOBILE.md](docs/MOBILE.md)). The deck is the one place the flat paper gives way to material,
  built from the same tokens; every other phone page is the desktop's page in a phone layout.
- A task opens as a panel over its board (`#/p/<id>/task/<id>`); Esc closes it.
- Core project tabs in Individual and Team include Overview · Work · Calendar · Git · Runs. Calendar shows both planned work dates and scheduled agent starts. Calendar opens on a week.
  Individual work projects keep the same five tabs: Git explains that there is no repository, and Runs explains that
  there is no execution history. Their Calendar shows planned work. Pages use `.split` (main + side column, each
  scrolling inside the window) and `.pn` panels with a `.ph` header; groups that want an action sit in an
  inset `.slot`. Filters are `.pick` (a raised button that is a select); few-way choices are `.seg`.
- Settings show one section at a time beside a section list; each section has an address (`#/settings?runners`).
- Narrow controller pages use stacked content and compact, labelled native section choosers in Git and Settings.
  The selected option states the current subsection; choosing one keeps hash addresses, direct links and browser Back.
  Tables become two-line rows and task-panel actions wrap. Answer actions and the loaded task detail's one close
  control have 44px phone targets. These service-specific responsive controls are distinct from the desktop shell's
  persistent sidebar below. The installed controller PWA starts on the Control Deck (`start_url` `/#/deck`).

### The window's workspace navigation

The desktop shell keeps Work, Calendar, Agents, Analytics and Salie in one 224px sidebar, with Settings separately at
its foot. The sidebar uses the paper/background token, a hairline right edge and 44px destination targets; hover uses
`--surface-2`, and the current destination uses `--surface` with stronger ink and weight. Focus uses `--accent`.
A native project dropdown above the destinations offers All Projects, then Personal projects and Team projects grouped
by ownership. Team options include their workspace name; offline and unavailable contexts remain explicit.
Scope persists across destinations, and All Projects includes only accessible workspace projections.

At 640px and below the sidebar becomes a 64px icon rail. Destination names remain in accessible labels, the project
select keeps its native behaviour behind a folder icon, and the current project name remains visible in the context strip.
The fixed strip sits above the scrolling page content, uses 13px secondary text and a hairline bottom edge, and wraps
recovery notices when a project is unavailable or a workspace's work is excluded. Settings, Help and workspace management
omit this strip. Narrow desktop screenshots demonstrate the shell layout; the controller phone Control Deck remains separate.

Shared desktop page frames and Work use 28px top padding, responsive 18–40px horizontal padding and 32px bottom padding;
below 900px this becomes 20px 16px 24px. A 24px heading and quiet supporting line lead into compact controls. Work uses
one surface canvas, divider-separated task rows and Queue/Plans navigation with 44px targets, secondary counts and a thin
accent underline for the selected section. Search gains a 44px input below 900px; at 640px and below it occupies its own
full row with 16px text, followed by wrapping creation actions. Buttons use 44px targets for coarse pointers.

Workspace management is in Settings → Appearance; theme and style choices are in Settings. Jump to retains project and
workspace detail destinations. Git, reviews, run history and other repository tools remain contextual. The standalone
controller and Team console retain their service-specific navigation: the shared Personal/Team selector belongs to the
desktop shell. See [Unified desktop navigation](docs/UNIFIED_NAVIGATION.md) for context behaviour and verification.
Generated workspaces, their creation actions and Data Library are removed. Calendar has no Timeline presentation.

The [Phase 8 finish review](docs/SIMPLIFICATION_PHASE8.md) disposition is **SHIP** for this bounded unified desktop scope,
with no blocking findings.
Physical-device behaviour and contrast across every theme were not independently tested in this finish pass.
The change extends the approved Buffer-inspired Work direction; it preserves Paper, Graphite, Classic, bundled Geist,
the pixel mark and cobalt/amber roles, without a replacement visual world or generated raster assets.

### Team console

Team uses the same paper, ink, Geist, icon strokes and action colours, with its own coordination sections:
Workspace · Projects · Reviews · People · Analytics. Desktop has a 232px rail (248px above
1800px), project shortcuts and Settings at the foot. Changing projects keeps the current project section. Inside the
desktop app the window's shared sidebar replaces the rail; standalone Team keeps its own coordination navigation.
Inside a workspace or project, one row of tabs carries its sub-areas. A project's Overview, Work, Calendar, Git, Runs and People are tabs above
its content (People is who is on the project, with roles and invitations; Project settings keeps the rest); a team's Workspace,
Projects, Initiatives and People, and Personal's Workspace and Projects, are the same row above its pages. Both rows are drawn by one function. Calendar shows planned dates and shared schedule requests;
Calendar is its sole date presentation. Settings contains This computer, MCP clients, Labels, Devices, Workspace Hosts,
Connectivity, Backups and License, subject to the person's permissions;
the existing `?tab=…` addresses still work. Host resilience lives under Settings → Workspace Hosts.

The workspace's Needs attention view shows Working now beside Needs attention and Projects. Lists use spacing and dividers;
cards are reserved for focused ticket details; Work uses compact rows. Team page titles are 24px,
ticket titles 15px. Work uses vertical Now, Next and Later groups with Completed in a disclosure.
Below 900px, standalone primary navigation
becomes a sticky scrolling row with an always-visible Settings button; the document scrolls, and actionable
attention appears before the working list. N opens a new ticket and Escape closes ticket details.

⌘/Ctrl K and the heading's Search button open a focused jump list of sections, projects and tickets. Its input keeps
focus through live updates; arrows select and Enter opens. Escape restores the original focus and unsent edits.
Members is an organization directory: initials, names, email and roles, with filtering and management in row menus.
Light/dark lives in Settings, with Team's own per-device preference, independent of the
individual app. `theme.js` resolves it before styles load and keeps the mark and browser chrome in step.

Team Runs shows the latest member-supplied execution report per ticket/device for at most 32 assigned tickets per project read. These are
reported states and timestamps, including stale/unknown runner availability, rather than a complete run-history
browser. Raw output and full history stay with the owning member's Werkbord. Team Git presents reported repository
metadata; it does not inspect or change a member's repository, execute agents or verify their work.

## The assistant surface (4.11)

A full-window conversation, opened from the Assistant button at the top of the standalone Individual rail or through the
phone's existing Assistant navigation. In the desktop shell, Salie is one permanent destination in the sidebar, with its
two-sparkle mark; there is no floating button. Standalone Individual and phone navigation retain their existing controls.
The conversation is the quietest screen in the product: paper, one column of at most 46rem, no history list, no chrome beyond
a heading (the provider chooser, New, Close) and the box.

- **Movement.** A circle grows from the button's centre until it covers the window (950ms, `cubic-bezier(.72, 0, .18, 1)`), with a cobalt
  light along its edge that fades as it arrives,
  while the background moves from `--surface` to `--bg` (paper): `#FFFFFF → #F5F5F2`, `#141415 → #0B0B0C`. The content
  arrives from 540ms in, in a short sequence from the heading to the composer. Closing runs it backwards, 30% faster, into the button.
  It can be turned round in either direction half way. In the desktop app a dedicated transparent frame covers the entire viewport,
  including the sidebar, while the workspace beneath keeps its state. Its reveal and collapse use the sidebar button's
  position. Under *reduce motion* it
  is simply there or gone: the movement is a Web Animation, so the page checks the setting itself and the global CSS rule that
  switches off transitions is not what it relies on.
- **Reading.** A reply is plain text on paper, 15px/1.55, no bubble; what you said is a tray (`--surface-2`) at the right. Progress is
  one quiet line, never operation names. A change is a raised card with a status dot and word on its label: amber for "Needs attention", green for "Done", and red for "Failed".
- **The box** is raised, 14px corners, with attach, dictate and send/stop at its edge; 16px on a phone so the page does not zoom, 44px
  targets. The send arrow is cobalt only when there is something to send.
- **Dictation** shows the words in the box while listening. X cancels and preserves the earlier typed draft; the tick finishes into
  an editable draft; Send waits for transcription to finish before sending. Failed recognition keeps the draft for review.
- **Not a place to browse.** No history, no list of conversations, no search of them. *New* starts again.
- **Counts on the button.** Amber `.num.pend` for changes waiting; a cobalt pulse while a reply is on its way.

## Interaction rules

Motion preserves the current icons and layout and acknowledges observed state. Salie’s activity mark uses the six approved pixel studies; Git and attachment transfers use bounded directional and indeterminate feedback, with success reserved for reported results. Salie fades only newly appended text; boards preserve card continuity; charts reveal proportions without delaying their labels or exact values. Initial loads and reconnects establish a baseline rather than replaying accomplishments. All spatial effects respect reduced motion. See [MOTION.md](docs/MOTION.md) for timing, interaction and verification details.

- **Vocabulary.** Needs attention names attention; Questions is its question filter. Calendar shows planned work and scheduled agent starts. A repository project's Calendar schedules agent execution ("Not scheduled to run"); work projects
  show planned work. Team Calendar combines planned
  dates with shared schedule requests that wait for the owner's runner and approval. Review means work is ready
  for a person's inspection, Done means the work was completed or merged, and Archive keeps a task out of the active
  board. Accept work / Reject work record a verdict; Merge… changes the repository after confirmation.
- **Attention counts.** The Needs attention and project interruption badges exclude work ready for review; the Needs attention
  page's All list includes it. Keep that scope visible beside the counts and in their accessible names. Project summaries
  call pending questions "Needs your answer".
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
  the person picks one under Settings → Time format (in a browser, Settings → Alerts and display; kept per device). The Calendar's hour column, its events, the run
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
- The window's Needs attention view orders urgent problems, actions and operational warnings. Each row preserves its workspace,
  project and detail, and names its next action. Individual questions expand in place with context before choices; answering uses the owning Werkbord and stays disabled for stale or offline reports. Team rows open the owning workspace. Saved answers remain unconfirmed until the runner confirms delivery. Only the first urgent or actionable item has primary emphasis.
- In the window's Needs attention view, a collapsed question row says what the agent asked. When expanded, its row changes to
  "Answering this question" while workspace, project and task provenance stay visible; the answer card states the
  question once. Stale/offline refusal and pending acknowledgement remain unchanged.

- What needs you is never more than one click away and always says what it wants (the question itself, on the card).
- Inline actions on cards: Start, Approve/Deny, Reply, Merge…, Move to Review, Run again. A Runs row whose run waits on a
  question has an Answer button that opens the same question card beneath the row.
- **One name: "Needs attention".** Use it for actionable attention in headings, filters and the page title. An individual question says "Needs your answer" in run status, summaries and notifications, and its card can name the request type. A row's kind in the list is its plain type (Question, Blocked, Failed). A shared execution report may say "Waiting for an answer" when it does not establish that the current viewer can answer it.
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
- What needs a person is one list (`lib/attention.ts`): Needs attention shows it for every project in
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
- **Dates move where they are shown.** Individual, Team and Universal calendars use the shared pointer/keyboard date transport in `internal/uipresentation`. Dragging a planned card moves its inclusive date range. Scheduled events reschedule agent starts and preserve time of day. Alt + Left/Right moves a day; Alt + Shift + Left/Right resizes. Controller and Team validation remain authoritative, and a refused drop preserves the original position with actionable feedback. Clicking opens an editor beside the current calendar, with Save changes. Universal sends bounded requests to each authenticated workspace frame, without changing the visible workspace or gaining execution authority.
- Anything that changes the repository is confirmed in a `Sheet` with the controller's own checks.
- Keyboard: ⌘/Ctrl K Jump to (projects, sections, tasks, actions), N new task, Esc closes.
- Motion is state, not decoration: 150–250ms, `cubic-bezier(.16, 1, .3, 1)`, and none under reduced motion.

### Final verification refinements (4.19.1)

Needs attention's All, Questions and Review choices are native filter buttons in a labelled group, with pressed state.
Tab and Space/Enter activate them; they do not announce a tab widget or imply arrow-key tab navigation.
Count scope is unchanged. The attention-card Review action follows the same 44px phone/coarse-pointer height
floor as its answer actions. These refinements do not establish physical-device or screen-reader usability; see the final audit
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
Activity's series choice and Usage's measure choice change the chart, rather than the underlying report scope. Individual's
charts occupy separate panels with coloured top edges, tinted headers and identifying swatches. Activity pairs bounded
animated daily bars with cumulative lines and areas, a daily heatmap and a Done / created ratio ring. The band between the
cumulative totals shows their difference within the selected period, never an outstanding backlog. Done / created compares
period counts, not the completion rate of the same task cohort; work created earlier can make it exceed 100%.

Execution has an outcome donut, normal-exit and retry rings, and provider and runner comparisons. A normal exit remains
separate from verified completion. Usage pairs token or cost comparisons with reporting coverage, whose full scale is all
attempts in the filtered period, and retains provider and runner comparisons. Completion shows an assessment ring beside
accepted/rejected counts and separate completion-signal comparisons; these are separate evidence counts, not stages of one
cohort. Exact daily values are available by pointer and keyboard, with execution breakdowns and complementary evidence
in disclosures. Unknown measurements remain distinct from reported zero, and cost estimates are separate from reported
costs. Chart reveals keep labels and exact values available immediately and respect reduced motion.

Team analytics uses Activity, Categories and Completion tabs, each with its own project and period. Category comparisons
use current labels and explain that tickets may appear in multiple categories. Team exposes coordination evidence only.
Project and period filters keep the console's explicit Apply / refresh snapshot behaviour. Both tab bars support arrows,
Home and End, show selected state and retain visible labels at narrow widths.

Every desktop window opens on Work. Sidebar width follows the viewport, using the 64px rail only at 640px and below.
Stored workspace locations remain available when a person explicitly opens that workspace; they do not choose the startup surface.

### Configurable phone Control Deck (4.46)

The initial deck keeps Needs attention, Calendar, Projects, My Work, Agents and Analytics, with three empty sockets. An icon-only pencil in the header enters editing; its checkmark saves the deck. Every position can be replaced, removed or moved, including by keyboard through Move to
position. Empty sockets remain empty. Preserve the keycap materials, themes and reduced-motion behaviour.

Workflow keys show their name and icon; repository scope remains in their accessible label and configuration. Tap executes a configured workflow; hold,
Shift+F10, context menu and the editing settings control configure it. Configuration never starts work. Workflows target
Individual repositories; ordinary task views provide output, questions, review and cancellation. Phone preferences live in the
controller per enrolled phone. The redundant attention strip is removed; Needs attention retains its live count and destination.
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

Onboarding checks readiness before presenting a setup action. Verified local runner, agent and repository steps become disabled status controls and leave the active path automatically. Team's connected runner has one disabled Runner connected control; configured but unavailable runners can reconnect without repeating installation. The members directory uses compact rows with two-letter initials, a tinted self avatar, role, connected/disconnected runner counts and reported Team phone status. Devices report actual runner connection and authenticated phone sessions; a registered device or paired phone alone never proves a connection. Legacy or stale reports stay unknown. Runner summaries use Connected, Disconnected or Connection unknown instead of occupied/total execution slots; capacity remains in settings.

### Team claims and private agent work (4.52)

Claim moves a ticket from Available to In progress, where it waits for its owner to start work. My tickets filters the project board by current owner; All tickets shows the shared
board. Cards name the owner and relative claim time. Only the owner sees agent controls and execution detail. Teammates see ownership and an active/inactive indicator. A successful agent-only run moves its ticket to Review; human and hybrid work wait for explicit submission. Private controls load directly on an owned ticket, use actual controller model/reasoning
choices, surface questions before configuration, and refresh conversation/activity automatically. Advanced device and long
approval choices stay in a disclosure. Teammate views show coordination and Git/review facts; conversations and controls stay
with the owner. Preserve local policy review, assignment fencing, working branches and the existing PR/review gates.

### Team ticket layout (4.68)

Tickets use a main column for description, requirements, private agent work and comments, with a side column for status, start and due dates, and Git facts. Thin token-coloured borders separate these sections. Narrow panels stack in reading order. Dates have a visible save action and appear as planned ranges in Calendar; comment composers offer @mentions with in-app notifications. Missing host routes have one screen-local explanation and an upgrade destination; errors clear when leaving their screen.

### Agent catalogue and model objectives (4.81)

Settings → Agents owns the complete searchable CLI catalogue, inclusion toggles, billing declarations and optional model estimates. Setup and everyday agent/model choices use collapsing selects, and the selected value remains visible. Task defaults, project/task/run overrides, workflows and Salie share Lowest cost, Max reasoning, Highest intelligence and Manual. Automatic modes hide manual model controls, explain that selection is fixed for a conversation or run, and expose the resolved choice and classifier/rules source. Preserve explicit saved choices as Manual. Mobile activity lists installed agents and active sessions rather than the integration catalogue.


### Team layout and assistant placement (4.82)

Team Analytics follows Individual’s coloured statistic tiles and twelve-column chart panels using coordination data: daily throughput, cumulative progress, Done against created, activity by day and categories. Filters apply on selection. Reviews places To review beside Waiting for review; Projects leads with compact workload cards and keeps create/join controls in disclosures beside them. Workspace groups Needs attention and Working now beneath compact counts. Narrow windows stack these groups in reading order.


Project identity in Team contains only the repository, when present, above the project title; ticket branches belong in ticket and Git views. Individual and Team preserve saved work modes without a Human / Agent / Hybrid selector in create/edit forms. Done cards carry their own Archive action, and the bulk Archive Done button is removed. Archive preserves details and supports restoration.


### Premium operator console (5.5)

The vendor-only licensing page preserves its paper/ink/cobalt and bundled Geist identity, with a searchable person/status register beside the key details and inline issue form. It issues one permanent Premium unlock key per person; no seat, expiry, renewal or workspace terms appear. The plan explanation says teams of up to two are free and teams of three or more need a separate key for every member, including the owner. Operator sign-in, authenticator verification, prepared-request recovery, original-key redelivery and passphrase-encrypted backup stay explicit. Customer activation is Settings → Plan. Narrow layouts stack the header, search, register and details, with full-width search, 16px inputs and visible focus. Signing stays on the protected server; this surface never handles its private key.

### Workspace density and update consistency

Reviews groups To review, Changes requested and Your work in review into adjacent columns when space allows, stacking
in reading order on narrow windows. Team project settings and independent computer settings sections use responsive
columns.

Project sidebar entries open the project; they have no project transfer or Product Map shortcut. Product Map is a project
tab in Team, with Generate or share a map opening the reviewed local snapshot flow. Personal analysis chooses its provider,
model and target branch from connected providers and locally known branches. Ticket forms, cards and planning controls
omit labels, preserving existing label assignments for reports. The project overview lists only ready or active agents.
Git query tabs keep the overview's frame. Runs page through the rows
that fit the available viewport, with filters resetting to the first page.

The shell always sends its theme, including Device, when a workspace opens. A Team service with a different release is
shown as needing an update rather than silently loading its old console. Updating an enrolled service uses verified backups
and post-update identity and health checks for every local workspace. Personal startup also checks the running controller
when the installed executable already matches the app: a current file does not establish that the process is current.

### Work queues and structured plans

Work replaces Kanban in Personal and Team. Show Now, Next and Later as vertical task groups with thin dividers;
never use lanes or drag gestures to change lifecycle state. Derive the grouping from activity, attention, priority,
dates and unfinished dependencies. Completed work stays in a disclosure. Name and link blocking tasks inside each row.
Keep existing task details, review and Git confirmations, assignment and CLI session controls.

**Queue cards.** Each queue is one flat card on the page tray (`--bg`): `--surface`, a hairline `--border`, `--radius`, no shadow, a 14px/600
title with a tabular count chip on `--surface-2`, and a quiet 12px description on the right (hidden on phones). Inside a card, projects and
plans are disclosure rows separated by hairlines, never cards of their own: cards are never nested. A disclosure uses the one global chevron
(`details > summary::before`); its summary shows the name, where it lives (workspace) or its progress ("1 of 3 complete") in 12px secondary
text. Rows stay compact: title, one status line, and only the controls the person needs. A holder is a 24px initials chip on `--surface-2`
(the viewer's own tinted cobalt, never amber); an unheld row has no chip. The drag handle is a 20×28 grip that appears on hover or focus and is
always visible where hover does not exist; a row someone else holds shows a lock with the reason instead of a handle. Motion is state only:
the dragged row dims, one 2px cobalt line shows where it will land, and nothing else animates. Alt plus Up or Down moves a row, a polite live
region announces it ("Alpha moved to position 3 of 4"), and Escape cancels a drag. The shared stylesheet is
`internal/uipresentation/static/work-cards.css` and the gesture transport `work-order.js`; products supply rows, rules and persistence.

Queue and Plans share a compact section navigation. Opening a plan replaces the queue with a dedicated detail page: its description,
current progress and ordered task references lead, with plan information and actions in a narrow sidebar. Sessions, decisions and
questions are sections inside the plan. On narrow screens the information follows the main content. Plan editing
uses explicit Up/Down controls. Salie prepares plan changes through the existing confirmation path. The window's Work
uses the selected project context; All Projects aggregates only currently accessible workspace projections.

### Approved Work design references

The user supplied [Photo 1](docs/design/references/work-direction-01.jpg),
[Photo 2](docs/design/references/work-direction-02.jpg) and
[Photo 3](docs/design/references/work-direction-03.jpg), naming Buffer’s simplicity as the direction for Werkbord.
Photos 2 and 3 lead Work: a light surface, compact task rows, quiet metadata, restrained controls and dedicated plan details.
Keep the established theme tokens and product behaviour. The source sheets are composition references, not authority to
add their example accounts, metrics, login systems or a different assistant name. Work and plan details are the first scope;
other destinations can follow this direction in subsequent work.

### Onboarding readiness and theme selection

Guided setup uses the same surfaces, controls and theme choices as the workspace. Its Appearance disclosure keeps styling
optional. Slow checks remain pending beyond eight seconds, with retry and pause recovery; elapsed time never resolves a check.
Readiness requests share in-flight work. Related GitHub onboarding writes are serialised. Required capabilities are verified
again before completion. Grouped initial/project requests wait for every sibling to settle even when one fails early.
