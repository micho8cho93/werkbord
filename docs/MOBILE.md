# The phone: the Control Deck and the Assistant

Under 900px, outside the desktop app, Werkbord opens on the **Control Deck** and has two places in a bottom bar: the **Control
Deck** and the **Assistant**. Everything else is reached from the deck and has a Back. It is the same web app, the same data, the
same sign-in and the same permissions as the desktop layout; only the arrangement is different. The desktop layout (900px and wider)
and the desktop app's window (which has its own sidebar) are unchanged.

Code: `web/src/lib/mobile/`. Wired in `web/src/App.svelte`. Tests: `web/src/lib/mobile/*.test.ts` and `scripts/browser-phone.cjs`
(`make test-phone-browser`).

## The Control Deck

A header (the mark, whether the controller is reachable, Settings), one line on what needs you, nine keys, and what the agents are
doing. The deck only reads what the app already holds (the Control Center overview and the runners). A key is a link to a page; it
starts, stops and changes nothing.

| Key | Page | Reuses | Counts |
| --- | --- | --- | --- |
| Needs you | `#/control` | the Needs you page (`routes/ControlCenter.svelte`) | what needs you (the app's own count) |
| Calendar | `#/schedule` | `lib/mobile/Schedule.svelte`: Month, Week, Timeline | |
| Projects | `#/projects` | the Projects page, each project with what it asks of you | |
| My Work | `#/mywork` | `lib/mobile/MyWork.svelte`, the board's status words | |
| Agents | `#/agents` | `lib/mobile/Agents.svelte`, `RunBadge`, the task page for output, questions and replies | runs working |
| Analytics | `#/analytics` | the Analytics page, unchanged | |
| Reviews | `#/reviews` | `lib/mobile/Reviews.svelte`, the task page for the review itself | tasks ready for review |
| Git | `#/repositories` | `lib/mobile/Repositories.svelte` over the Git Control Center's store; each repository opens it | repository findings that need attention |
| Runners | `#/runners` | the Runners panel Settings already has | |

The icons are the ones the desktop navigation already draws for each place (`lib/Icon.svelte`); the Assistant's is the Assistant's.
Counts are amber when they are work waiting for you, cobalt when they are work going on, and absent when zero.

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
  `#/reviews`, `#/repositories` and `#/runners`. On a wide window the pages only the phone has are settled to Needs you.
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

The Assistant is unchanged: same conversation engine, providers, model picker, tools, dictation, streaming and confirmation cards. On a phone
its surface stops where the bar begins (and takes the room when the on-screen keyboard is up), its Close button gives way to the bar, and the
bar's two places are part of its focus order. Nothing was added to what it may do.

## Individual and Team

The Control Deck is **Individual's** phone interface. It does not show an Individual | Team control, because Team cannot be reached from a phone
today. This is the backend work that is missing, not a presentation choice:

- A Team workspace answers remote requests only over its own Nebula network, and only to a member computer that signs each request with its
  enrolled device key (`TEAM_SECURITY.md`, points 5 and 7). Member bearer credentials work on loopback only. A phone is not a member
  computer: it has no Nebula certificate and no signing client.
- The desktop window reaches Team through the app's native bridge (`web/src/shell/native.ts`, `internal/workspace`). A phone has no native app.
- Adding Team to the phone therefore needs a new, narrow, read-mostly relay on the person's **own** computer: a Team client already runs there
  with the device key, and it would answer a fixed list of read questions (My Work, Calendar, Reviews, Analytics, Projects) for the phone, over
  the existing Tailscale door and behind a revocable grant of its own, with `internal/archtest` tests that it starts nothing and reaches nothing
  on a member's machine. That is Team code (`internal/team/`), it needs its own design and security review, and it is not started.

The deck's screens are written against the Individual controller's own endpoints and ask for nothing Team-specific, so a workspace switch
(a segmented control on Projects, Calendar, Analytics, My Work and Reviews, and a sheet to choose between several Team workspaces) can be added
above them when that relay exists, without changing them.

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
- `scripts/browser-phone.cjs` (`make test-phone-browser`, and part of `make test-browser`): in a real browser against the disposable fixture, the nine keys
  are equal and in three columns with material round them, each opens its page and Back returns, a held key drops (and does not move with reduced motion),
  Month, Week and Timeline, the Assistant's place in the bar, 320 to 430px and landscape in both themes without horizontal overflow, one board column at a time,
  and a wide window that stays the desktop.
- `scripts/measure-legibility.cjs --check` measures the phone-only pages too (the deck, My Work, Agents, Reviews, Git, Runners and the Calendar's three views): no text
  under 11.5px and none under its contrast floor. (It reports one finding that is not on these pages and was there before: dark-theme "Run settings" in the
  desktop task panel.)
