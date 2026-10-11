# Werkbord design

The visual system of the individual app (`web/`) and, from Werkbord Team 2.2, the Team console. It is the
identity sheet "Werkbord · Identity — Quiet technical · v1" made into code. The landing site
(`werkbord-site`, `assets/css/site.css` and `app.css`) copies the same tokens; when they disagree, the sheet wins.

## Character

Quiet technical. Paper and ink, one action colour, one attention colour. The tool disappears into the work:
density over decoration, every state readable at a glance, nothing hidden that you need weekly.

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

- Desktop (≥900px): a 220px rail (Needs you, projects with live counts, runners, Settings, theme)
  and a main column. Inside the Werkbord desktop app the window's one sidebar replaces this rail
  ([ARCHITECTURE.md §22](docs/ARCHITECTURE.md#the-window-and-its-workspaces)). Inside a project: header (path · branch, name, Jump to ⌘K, project
  settings, New task) and the section tabs. The window is the app: views fit the viewport and scroll inside
  themselves (board columns, panel feed), not the page.
- A task opens as a panel over its board (`#/p/<id>/task/<id>`); Esc closes it.
- Project tabs: Overview · Board · Calendar · Timeline · Git · Runs. Pages use `.split` (main + side column, each
  scrolling inside the window) and `.pn` panels with a `.ph` header; groups that want an action sit in an
  inset `.slot`. Filters are `.pick` (a raised button that is a select); few-way choices are `.seg`.
- Settings show one section at a time beside a section list; each section has an address (`#/settings?runners`).
- Phone (<900px): sticky heading, the page scrolls, bottom tab bar (Needs you · Board · Calendar · Timeline · Git ·
  Runs · More; one equal column per tab, so a work project's shorter set fills the bar too), floating New task button,
  horizontally scrolling Kanban columns, tables as two-line rows. The installed app starts on Needs you
  (`start_url` `/#/control`). More is a sheet reaching what the bar has no room for: the project's Overview and
  settings, Projects and Settings. The board opens on the first column holding something that needs you (a question,
  a reply, blocked work, or work in Review). Buttons that answer from a card are at least 44px tall, and a task
  panel's actions wrap onto their own row rather than clip.

### Team console (3.2)

Team uses the same paper, ink, Geist, icon strokes and action colours, with its own coordination sections:
Workspace · Projects · Board · My Work · Reviews · Git · Activity · Members. Desktop has a 232px rail (248px above
1800px), project shortcuts and Settings at the foot. Changing projects keeps the current project section. Inside the
desktop app the window's sidebar replaces the rail and a project's Board, Git, Activity and People are tabs above it.
Settings contains This computer, Devices, Workspace Hosts, Connectivity, Backups and License;
the existing `?tab=…` addresses still work. Host resilience lives under Settings → Workspace Hosts.

The default Workspace shows Working now beside Needs you and Projects. Lists use spacing and dividers;
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

- **Vocabulary.** Needs you names attention; Questions is its question filter. Calendar schedules agent execution
  ("Not scheduled to run"); Timeline plans work dates and milestones ("Not planned yet"). Review means work is ready
  for a person's inspection, Done means the work was completed or merged, and Archive keeps a task out of the active
  board. Accept work / Reject work record a verdict; Merge… changes the repository after confirmation.
- **Attention counts.** The Needs you and project interruption badges exclude work ready for review; the Needs you
  page's All list includes it. Keep that scope visible beside the counts and in their accessible names. Project summaries
  call pending questions "Needs you".
- **Planning dates and execution schedules.** Date fields on Timeline plan work; they do not start an agent. Calendar
  schedules when the controller starts agent work. Explain this at both sets of date controls. Work projects keep
  their timeline planning fields and have no execution controls.
- **Touch and keyboard.** Answer choices and card actions are at least 44px tall on phones and coarse pointers, with
  spacing between choices. Coarse-pointer form fields use 16px text to prevent zoom. A task panel receives focus on
  its heading and returns focus to its opener on close. These rules complement the 11.5px type floor and shared
  disclosure chevron above; compact secondary controls still need a separate touch-target audit.
- **A confirmation keeps focus.** Only the topmost Sheet handles its keyboard events. Tab and Shift+Tab cycle through
  visible enabled controls, background focus returns to the Sheet, Escape closes it, and closing restores a connected
  opener. Collapsed content and disabled actions never enter that cycle.

- **One clock.** Every time of day is written by `lib/clock.svelte.ts`: 12-hour or 24-hour as the device's locale writes it, unless
  the person picks one under Settings → Alerts and display (kept per device). The Calendar's hour column, its events, the run
  history, the feed and the desktop Calendar all follow it; nothing hard-codes `HH:MM`.
- **One disclosure.** Every `<details>` shows the same chevron, which turns down when it opens (`app.css`); the browser's own
  triangle is not used, because it disappears on a flex summary.
- **An empty view says so.** A Calendar week with nothing scheduled says "Nothing is scheduled to run this week" above the grid.
- **A work project speaks of work, not agents.** Its board has no Agent or Runner filter and no agents legend, its cards do not
  say "Human", its task panel has no Runs tab and its Timeline does not send you to a Calendar it does not have.
- **First run is quiet.** Failing to look up an optional connection (GitHub, phone access) is a muted line with *Try again*;
  red is for something the person asked for that failed.
- **A blocked permission is an explanation, not a button.** Alerts blocked by the browser say where to allow them.

- First-run setup leads with three local requirements: an enabled online runner with capacity configured, a usable coding
  agent and a registered Git repository. A planning project does not count as a repository. One primary action advances
  the missing requirement or opens task creation; planning remains available separately. Phone access and GitHub are
  optional connections, collapsed initially, and skipped connections stay labelled as skipped.
- The window's My Work view separates reported running, queued and starting states. Idle turns and unanswered questions
  have different labels. Source timestamps say when the state was recorded; they do not claim agent activity. Offline
  or stale reports retain their last state with availability unknown. Pending requests remain awaiting acknowledgment.
- The window's Needs you view orders urgent problems, actions and operational warnings. Each row preserves its workspace,
  project and detail, and names its next action. Individual questions expand in place with context before choices; answering uses the owning controller and stays disabled for stale or offline reports. Team rows open the owning workspace. Saved answers remain awaiting acknowledgment until the runner confirms delivery. Only the first urgent or actionable item has primary emphasis.
- In the window's Needs you view, a collapsed question row says what the agent asked. When expanded, its row changes to
  "Answering this question" while workspace, project and task provenance stay visible; the answer card states the
  question once. Stale/offline refusal and pending acknowledgment remain unchanged.

- What needs you is never more than one click away and always says what it wants (the question itself, on the card).
- Inline actions on cards: Start, Approve/Deny, Reply, Merge…, Move to Review, Run again. A Runs row whose run waits on a
  question has an Answer button that opens the same question card beneath the row.
- **One name: "Needs you".** The rail and tab, the run status, the card's eyebrow ("Needs you · Decision"), the page title,
  the banner ("1 question needs you") and the notification all say it; a row's kind in the list is its plain type
  (Question, Blocked, Failed). Nothing says "Needs input", "Needs answer" or "Needs attention" for the same thing.
- **A question is stated once per screen.** The banner stays out of the way where the question is already asked with its
  answer: the task panel that has it open, and the project's Overview, Board and Runs. In the task panel the dock below
  the feed is the one statement; the feed shows the question again once it is answered, and the status line does not.
- **Archive, then undo.** A task panel's "Archive task" moves the task to the board's Archive and says so in a toast with
  Undo (12 seconds), which puts it back in the column it left. Nothing is called "closing a task".
- **Review ends where it starts.** A finished run's result card offers Review changes and, for the run's own local branch
  while it is ahead and unmerged, Merge…, the same confirmation Sheet as the board's; merging moves the task to Done.
  Accept work / Reject work say what they do: they record your verdict (counted as Kept in the usage by runner and agent)
  and never merge, move or archive.
- **An answer is confirmed.** Answering a question, on a card, in the task panel, on Runs or in the window's Inbox, shows
  one line saying what was sent and whether the agent has it yet ("Answered “Yes”. The agent has it and is carrying on.").
- **Only a refusal of a permission is red.** An approval's Deny or Reject is drawn as a refusal; a "No" to a decision is an
  answer like Yes, in the neutral button.
- What needs a person is one list (`lib/attention.ts`): Needs you shows it for every project in
  segments, a project's Overview for that project. Each item is answerable where it is shown.
- Numbers are only what was measured: the run history strip colours half hours by real runs; usage says
  which runs reported tokens or cost.
- Board drops follow the work: Backlog → Doing starts an agent; Review → Done opens the merge confirmation
  and moves the card once merged; other drops move the card.
- Anything that changes the repository is confirmed in a `Sheet` with the controller's own checks.
- Keyboard: ⌘/Ctrl K Jump to (projects, sections, tasks, actions), N new task, Esc closes.
- Motion is state, not decoration: 150–250ms, `cubic-bezier(.16, 1, .3, 1)`, and none under reduced motion.
