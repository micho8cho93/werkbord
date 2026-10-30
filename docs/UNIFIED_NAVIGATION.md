# Unified desktop navigation

The desktop shell has one navigation list: Work, Calendar, Agents, Analytics and Salie, with Settings pinned separately.
A native project dropdown above it groups Personal projects and Team projects by ownership. On narrow windows the same
sidebar becomes an icon rail; the current context remains visible above the content. Settings → Appearance contains
workspace management shortcuts. The command palette continues to expose project and workspace detail routes.

`web/src/shell/project-context.ts` is a presentation adapter over the existing permission-filtered workspace summaries.
A project identity is `{ workspace, project }`; storage and option keys encode the pair as a JSON tuple to avoid collisions.
`null` means All Projects. It is a virtual selection, never a persisted domain project or service-side entity.
The selection is remembered per browser/window origin and survives destination changes, settings and app reloads.
Opening project details synchronises the shell selection from the owning frame's existing route.

Work and Calendar consume a filtered view of the existing summaries. Agents shows the person's own coding-agent sessions
(runs and terminals, one entry per execution, by session key: see [AGENT_SESSIONS.md](AGENT_SESSIONS.md)) in a page of their own
Werkbord, with the execution activity already reported in those summaries in a strip above it; absence of a report does not
establish an agent's availability. When the person's own Werkbord cannot be shown, Agents is that reported activity alone.
Salie is one permanent sidebar destination: there is no floating button and no placement choice. Analytics continues to call
each workspace's existing report adapter, passing the selected project only to its owning workspace. It clears prior
figures on context changes and ignores obsolete responses. Salie displays the same context and receives it as the default
for subsequent turns. This conversational hint is not a tool restriction or an access grant; every existing backend
permission check and mutation approval still applies.

The summary contract uses project names for display and workspace-local links for identity. Filtering extracts the
project ID from those links, never from a display name. Project-scoped activity excludes events without an attributable
project route and marks their coverage incomplete. Workspace-wide catch-up remains available in All Projects.
No Team screen is imported into Personal and no data is copied between services. Detail pages, editing, calendar
permissions and execution controls remain owned by their original services and frames.

Offline, unavailable or failed workspace reads contribute explicit status notices and no current work records. A selected
offline project is retained for recovery. An authoritative successful project list that no longer includes the selected
project resets the selection to All Projects with a notice. Leaving a workspace removes it from both selection and aggregate;
an older in-flight overview cannot restore it. Permission filtering remains the responsibility of each source service.

Validation: `make check`; `node scripts/browser-project-context.cjs` after `make web-shell`. The browser suite uses isolated
presentation fixtures and covers one/multiple personal projects, multiple teams, mixed ownership, duplicate local IDs,
All Projects, destination/reload persistence, offline workspaces, permission-filtered removal, scoped analytics and Salie
context propagation. Existing service authorisation and architecture tests run in `make check`. Screenshots are written
to ignored `output/playwright/project-context/`.

Phase 8 shares Work title, search and Queue/Plans geometry through `ui/work-layout.css`, keeps a 224px
sidebar (64px at narrow widths) and uses 44px destination targets. Narrow search takes a full row.
All Projects → Work retains “Since you were last here”; its attention link expands the questions and
workspace notices directly. Project detail routes remain in the command palette.

The standalone controller browser and Team administration console keep their own service-specific
navigation. They are also the sources of embedded detail pages; neither gains cross-service authority
from the desktop's unified selector. The Mobile Control Deck keeps its dedicated phone navigation.
