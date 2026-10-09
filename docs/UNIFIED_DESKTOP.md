# Unified desktop workspaces (Phase 3)

Werkbord 1.8.0-preview.1 and Team 3.7.0 provide one everyday macOS window with
Personal and multiple Team workspaces. Phase 1 association/progress and Phase 2
owner-approved execution remain the prerequisites; their APIs, fencing,
credentials and independent services are preserved.

## Use

Build with `make desktop` or run with `make desktop-dev`. `make web-shell` builds
the desktop Svelte entry separately from the Individual PWA. The shell shares
Geist, tokens, icons, mark, theme and dialog components with the PWA. It has no
Team backend imports. Team continues serving its existing console, including
`?tab=…&project=…&ticket=…` routes. Personal keeps its `#/p/…` routes, CLI,
`devboard` aliases and every `DEVBOARD_*` variable.

The persistent switcher offers Personal, enrolled Team workspaces, and Add a
Team. Creating and joining reuse Team's first-host and administrator-approved
enrollment screens. Installation is an explicit action, followed by a native
confirmation and macOS administrator authorization. The existing separately
versioned Team installation supplies the signed installer; it need not be opened
for everyday work. No Team installation or activation happens on startup.

My Work, Calendar and Needs you read a bounded neutral summary from each
accessible service. Links open the relevant workspace and project in the same
window. Shared reviews/PRs, member management, personal agent controls on Team
tickets and all existing project views remain in their workspace's existing
screens. The shell never sends an aggregate or Personal data to Team.

The last accessible workspace and its internal route are saved in the user's
`werkbord-desktop/shell.json` configuration file (mode 0600); otherwise Personal
opens. No credential or workspace content is saved there. Open workspace frames
remain mounted while switching; removed, leaving or unavailable workspaces are
removed from the UI. Personal runner failure does not prevent the Team UI opening.

## Devices and hosts

Workspaces and devices shows independent Runner, Workspace Host and Connectivity
Host roles, host counts, write availability, replication, quorum, backup and
network checks when the member has permission to read them. It links to the
existing guarded management screens for promotion, demotion, replacement,
revocation, backups, connectivity and leaving. A replacement is added and checked
before retiring an old host. Final-host removal and insufficient quorum remain
refused by the existing backend; the frontend cannot override those checks.

In Team Settings, an owner can describe the computer, declare automatic sleeping
and volunteer it for either host role. Offers travel in the device's signed
heartbeat and appear in the administrator's Devices/Workspace Hosts screen.
They grant no capability. An administrator must explicitly authorize the role;
provisioning and reachability checks still apply. A device may hold multiple
roles. With today's fixed hosting ports, a computer can host only one Team
workspace, while belonging to up to 16. Overlapping private networks are refused.

Connectivity enabling/disabling explains potential loss of remote access. A role
alone does not establish reachability: unsuitable NAT/firewall arrangements are
reported as unconfirmed remote access. Sleeping/traveling devices remain poor
hosting choices. Reachability reports are observations, not guarantees.

## Authority and lifecycle

The desktop window is a user-scoped frontend and launcher. The Individual
controller executes agents as the user; Team's privileged network/database
service owns only its infrastructure. Neither CLI nor service is replaced.
Closing/hiding/quitting the window cancels only desktop reads, never an agent,
schedule or opted-in service. Workspace selection only writes shell preferences;
accepted runs retain their original authority and owner.

Wails bindings are allowed only for the bundled shell origin, and Wails rejects
calls from subframes on macOS. Workspace pages use the neutral `postMessage`
transport. The shell verifies both the source frame window and its exact origin,
then the Go relay applies the method allow-list for the registry's workspace
kind. Frames cannot request the workspace registry, aggregates or another
workspace's native operations. URLs are literal loopback, without proxy or
redirects. Summary and navigation inputs are bounded and validated.

Connecting a Personal runner is a separate explicit native action. The desktop
mints only an `execution-local-v1` revocable grant and delivers it to the selected
Team slot; it never delivers the controller credential. Grant names include the
slot identity so teams with the same display name remain independent. Individual
still requires exact-context local approval before any run starts. Leaving a
Team removes its local bridge/authority through the existing leave procedure;
it does not stop runs already accepted by Individual or affect another slot.

## Validation

- `make check`: both products, architecture/security/permissions tests, frontend
  unit tests, Svelte checks, lint, builds, desktop logic and release-script checks.
- `make verify-isolation`: Individual builds, tests and runs with every Team source
  removed, including tests of the desktop shell's HTTP clients and registry.
- `make desktop-check team-desktop-check`: native macOS compilation/vet and logic.
- `make test-rqlite`: pinned real clusters, promotion/demotion, lost quorum,
  failover, backup/restore and service behavior with replicated storage.
- `make test-unified-desktop-browser`: built Svelte components, real Go shell,
  Personal controller and multi-workspace Hub, real separate databases and
  signed enrollment. Covers different membership permissions, explicit grant,
  Team ticket execution, switching without interruption, saved project/ticket
  routes, desktop restart, frontend-close survival, hosting offers and isolated
  leave. Native dialogs and overlay TCP routing are fixture substitutions.
- `make test-team-desktop-browser`: existing Team routes, infrastructure and
  owner execution/schedule controls remain compatible.

Live customer networks, real agent-provider accounts, macOS privileged install /
Keychain authorization, sleep/wake and signed/notarized release distribution
remain production acceptance checks. This phase does not add automatic NAT
traversal, remote arbitrary commands, recurring schedules, live pause or automatic
runner failover. Team setup still requires its separately installed signed
installer and a valid customer license; release publication is a separate task.
