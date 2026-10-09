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
commands, keys, and the uppercase section labels (`.lab`, 11px, `letter-spacing: .14em`). Both are bundled
(`@fontsource-variable/*`), never loaded from a CDN. Page titles 20px/600; card titles 14px/500; body 13–14px.
The wordmark is `werkbord`, lowercase, Geist Mono 600, tracking −6%.

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

- Desktop (≥900px): a 220px rail (Control Center, projects with live counts, runners, Settings, theme)
  and a main column. Inside the Werkbord desktop app the window's one sidebar replaces this rail
  ([UNIFIED_DESKTOP.md](docs/UNIFIED_DESKTOP.md)). Inside a project: header (path · branch, name, Jump to ⌘K, project
  settings, New task) and the section tabs. The window is the app: views fit the viewport and scroll inside
  themselves (board columns, panel feed), not the page.
- A task opens as a panel over its board (`#/p/<id>/task/<id>`); Esc closes it.
- Project tabs: Overview · Board · Calendar · Git · Runs. Pages use `.split` (main + side column, each
  scrolling inside the window) and `.pn` panels with a `.ph` header; groups that want an action sit in an
  inset `.slot`. Filters are `.pick` (a raised button that is a select); few-way choices are `.seg`.
- Settings show one section at a time beside a section list; each section has an address (`#/settings?runners`).
- Phone (<900px): sticky heading, the page scrolls, bottom tab bar (Needs you · Board · Calendar · Git ·
  Runs), floating New task button, column pills on the board, tables as two-line rows.

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
Below 1280px, a single selected column replaces the five-column board. Below 900px, primary navigation
becomes a sticky scrolling row with an always-visible Settings button; the document scrolls, and actionable
attention appears before the working list. N opens a new ticket and Escape closes ticket details.

⌘/Ctrl K and the heading's Search button open a focused jump list of sections, projects and tickets. Its input keeps
focus through live updates; arrows select and Enter opens. Escape restores the original focus and unsent edits.
Members is an organization directory: initials, names, email and roles, with filtering and management in row menus.
Light/dark sits beside Settings (an icon on phones), with Team's own per-device preference, independent of the
individual app. `theme.js` resolves it before styles load and keeps the mark and browser chrome in step.

## Interaction rules

- What needs you is never more than one click away and always says what it wants (the question itself, on the card).
- Inline actions on cards: Start, Approve/Deny, Reply, Merge…, Move to Review, Run again.
- What needs a person is one list (`lib/attention.ts`): the Control Center shows it for every project in
  segments, a project's Overview for that project. Each item is answerable where it is shown.
- Numbers are only what was measured: the run history strip colours half hours by real runs; usage says
  which runs reported tokens or cost.
- Board drops follow the work: Backlog → Doing starts an agent; Review → Done opens the merge confirmation
  and moves the card once merged; other drops move the card.
- Anything that changes the repository is confirmed in a `Sheet` with the controller's own checks.
- Keyboard: ⌘/Ctrl K Jump to (projects, sections, tasks, actions), N new task, Esc closes.
- Motion is state, not decoration: 150–250ms, `cubic-bezier(.16, 1, .3, 1)`, and none under reduced motion.
