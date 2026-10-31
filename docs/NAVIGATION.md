# Navigation: where Back goes

Work, Plans, Tasks, Agents, an agent session, the Calendar and Salie are one connected set of places. This page says how a
person moves between them and how Back knows where to go. It is the front-end half of
[`AGENT_SESSIONS.md`](AGENT_SESSIONS.md): nothing here starts, stops or copies an agent.

## One router, one session store

There is no second router and no second session store. The Individual page's router (`web/src/lib/router.svelte.ts`,
addresses in `location.ts`) is still the only router; the desktop window (`web/src/shell`) still only decides which frame is
on show. A session is still named by its key (`run:<id>` / `terminal:<id>`) and listed from the controller's records.

## The route is in the address

Back never depends on browser history. A page reached from another carries **where it came from** in its address:

```
#/p/prj_a/task/tsk_1?from=session%3Arun%253Arun_a>calendar
```

`from` is a chain, nearest first, of at most three origins (`web/src/lib/origin.ts`):

| Origin | Meaning |
| --- | --- |
| `work` | the Work page (the window's, or Control in a plain browser) |
| `calendar[:<project>]` | the window's Calendar; with a project, that project's own calendar |
| `agents` | the Agents list |
| `salie[:<conversation>]` | Salie, on that conversation |
| `board:<project>` | a project's Work board |
| `plan:<project>:<plan>` | a Work plan |
| `task:<project>:<task>` | a task |
| `session:<key>` | an agent session |

Going back pops the nearest origin and hands the rest on to the page it leads to, so each step back still knows its own way
back. A chain with any step that is not exactly one of those shapes is no chain at all (an address cannot send Back
somewhere odd), and the oldest steps are dropped before a place would be too long for the window to accept.
A reload, a pasted link and a window opened fresh all go back the same way.

When an address names no origin, the page uses the parent it has anyway: a task goes back to its plan (`?plan=`) or its board,
and a session goes back to the list.

## The routes

- Work → Plan → Task → Session: the plan's task links and the task's *Open Session* push the page they leave.
- Agents → Session → Task: the session's *Open task* pushes the session.
- Salie → Session → Work: a Salie card pushes `salie:<conversation>`; the session's task pushes the session.
- Calendar → Task → Session: the window stamps `calendar` on a task opened from it; the task pushes itself for the session.

The window stamps `work`, `calendar` or `agents` on a task, plan or session of the person's own Werkbord that it opens from
one of those pages (`Model.stamped`). A Team workspace's page cannot carry that in its address, so the window remembers it and
shows one **‹ Work** (or Calendar, Agents) line above it (`ReturnBar.svelte`) until the person goes somewhere else.

## What Back brings back

- **Project context.** Opening a task moves the window's project selector to the task's project (as it always did). The window
  remembers the context it left (`Model.departed`) and Back to Work or Calendar restores it, All Projects included.
- **Work.** The section (Queue/Plans), the search and "Mine/Everyone" are kept by the window (`view-state.svelte.ts`).
- **Scroll.** Each page of the window keeps how far it was scrolled (`remembered` action in `view-state.svelte.ts`).
- **Agents.** The filter and the list's scroll are kept for the tab (`agent-view.ts`, `sessionStorage`). In the window the
  project is the window's own project context, told to Agents in its address (`?project=`); Agents has no selector of its own
  there. Changing the window's context re-scopes the list and leaves the open session open.

## One header, one trail

- A page has one heading. Agents used to draw its own "Agents" under the app header's, and the window drew "Reported in this
  context" above it. Agents now adds no heading; the window's strip above it lists **only what Team workspaces report** (the
  person's own Werkbord reports the same runs the sessions list shows) and carries no title of its own.
- A detail page shows one trail, the pages above it, oldest first, the nearest last (`lib/Crumbs.svelte`). The nearest is
  Back. The page's own title is the heading under it, so it is not repeated. Beside the Agents list the trail to "Agents" is not
  drawn, because the list is on screen; it appears wherever the list is out of sight (a phone, a narrow window).
- Salie is reached from a session by the trail when Salie started it. The *Ask Salie* button remains for a session that no
  conversation started.

## Never a second session

Navigation only changes which page shows a session: `openAgents(key, from)` sets an address or posts a validated message. It
calls no API. A session page attaches to the feed or terminal of the existing record; leaving it closes the viewer, not the
process (the terminal's *Terminate* and the run's *Stop* are explicit buttons). The window keeps the person's Werkbord frame
alive while another page is on show, and Back to a page of the window changes only which page is shown.

## Frame messages

Two additions to the messages a frame may post (`web/src/shell/frames.ts`), both from the person's own Werkbord's main frame
only: `agent_session` may carry `from` (a chain that parses, nothing else), and `page` (`inbox` or `calendar` only) goes back
to one of the window's own pages.

## Validation

- Vitest: `origin.test.ts`, `origin-nav.test.ts`, `agent-view.test.ts`, `shell/model.test.ts` (stamping, context restore,
  scoping), `shell/frames.test.ts`, `shell/nav.test.ts`, `shell/aggregate.test.ts`.
- `node scripts/browser-shell-agents.cjs`: session → task → session → Salie; Work → task → Back with context and search; Work →
  plan → task → session and back; a pasted address that goes back after a reload with no history.
- `node scripts/browser-agent-sessions.cjs`, `node scripts/browser-project-context.cjs`.

## Not done here

Team's own detail pages have no breadcrumb of their own (the window's line is all they get). The aggregate Calendar's own view
state (the month shown) is not kept. Scroll inside a frame is whatever the page keeps while it is hidden.
