# Structure: one product, its executables and its trust boundaries

Werkbord is **one product with one version and one release** (`cmd/werkbord/VERSION`, tag `werkbord-vX.Y.Z`; see
[VERSIONING.md](VERSIONING.md)). Personal and Team are project kinds, and Free and Premium are plans. Neither is a separate edition or download.
Premium is per person, checked offline; in teams of 3 or more every member needs it, and members without proof are read-only.
Personal stays fully available on Free. See [PLANS.md](PLANS.md).

The one release contains three executables with different trust responsibilities:

| | **The controller** | **The Team service** | **The desktop app** |
| --- | --- | --- | --- |
| What it is | A local-first control plane for your own coding agents: board, runs, Git, runners (Personal) | A workspace that coordinates a team: members, projects, who is on what | A native window around both, and the installer for the Team service |
| Executable | `werkbord` | `werkbord-team` | `Werkbord.app` (module `desktop/`) |
| Main package | `cmd/werkbord` | `cmd/werkbord-team` | `desktop/` |
| Code lives in | the shared `internal/` packages and `web/` | `internal/team/…` only | `desktop/` and `web/src/shell/` |
| Release assets | controller archives for existing terminal workflows | bundled in the unified desktop app; no standalone Host archive | macOS DMG; Linux packages opt-in after acceptance; Windows publication deferred |
| Installer | `scripts/install.sh`, `scripts/install.ps1`; the desktop app | the app's native service installer | platform package or app installer |
| Data directory | `werkbord` in your user config directory | `werkbord-team` in your user config directory (root-owned for the Mac service) | `werkbord-desktop` (window preferences and per-user Premium key) |
| Settings | `WERKBORD_*` | `WERKBORD_TEAM_*` | — |
| Default address | `127.0.0.1:7420` | `127.0.0.1:7430` (device API `7431`) | — |
| Build | `make build` | `make build-team` | `make desktop` |

Everything in the desktop app has the one version, Team's service included. The controller and the Team service are separate
executables and separate processes on purpose, not for packaging: the Team service runs as a privileged system service
(it creates the private network interface), while the controller runs agents as the person, so the two never share a process or
a credential. Everything is free for one person and for a team of two; a team of three or more needs Premium, an unlock key held by each member ([PLANS.md](PLANS.md)). There is no licence and no vendor login. Adding a Team is an explicit action. Personal work
needs no Team service. How the window, the Team service and the installer fit together:
[ARCHITECTURE.md §22](ARCHITECTURE.md#22-one-app-the-window-the-team-service-and-how-they-are-shipped).

Team defaults to customer-owned Nebula, administrator-approved enrollment and device-signed remote API requests. There is no Team Tailscale account or vendor runtime requirement. Customers operate their Workspace and
Connectivity Hosts, backups and local runners. Personal's tsnet support is separate. See [TEAM_SECURITY.md](TEAM_SECURITY.md),
[TEAM_INSTALL.md](TEAM_INSTALL.md), [PLANS.md](PLANS.md) and the [release gate](TEAM_SECURITY_GATE.md).

> **Words in the interface.** The controller’s projects are **Personal**, and shared projects belong to a **Team**. Existing
> source names such as `Individual` and wire `kind: personal` remain compatible: a project is Personal or a team's. "Universal" is gone; its page is **Home**. The wire values `"personal"` and the saved
> workspaces' `visibility.universal`/`individual` fields keep their names because other programs and stored data speak them.
>
> **Naming.** Werkbord was called Dev Board. Nothing of the old name remains: the executable, the `WERKBORD_*` variables, the data
> directory, the database file, the module path and the branch namespace are all `werkbord`.

## The rule that matters most

**A Team workspace is a coordination layer. It is not a shared execution environment.**

Every developer keeps using their own machine, their own Werkbord runner, their own Git credentials, their own
GitHub credentials, and their own agent and model credentials. Team never starts developer processes or holds Git, agent or model credentials,
and never opens an arbitrary path into another member’s computer. Only a person’s signed semantic requests to their own
locally trusted devices can cross the narrow local runner bridge, subject to the Personal controller’s policies. Team records who is on the team and what they are working on; each person's own Werkbord does the work.

This is enforced, not just intended (see [Enforcement](#enforcement)): Team's build cannot link the code that runs
agents, commands or Git, and its own code cannot import `os/exec`.

## Layout

The repository is one Go module. The usual monorepo split (`apps/` and `packages/`) maps onto Go's own conventions
(`cmd/` for programs, `internal/` for packages), and the existing code keeps its places:

```
cmd/werkbord/            APP    the controller and command line (Personal)
desktop/                 APP    the Mac app: a native window around web/, and Team's installer (its own Go module; DESKTOP.md)
cmd/werkbord-team/       APP    the Team service and command line

internal/sqlitekit/      SHARED opening, migrating and backing up a SQLite database
internal/httpkit/        SHARED JSON responses, error envelope, strict body decoding, request logging, security headers
internal/mcpcontract/    SHARED MCP framing and validated project/operation scope contracts; products supply their own tools and authorization
internal/mcpplan/        SHARED batch-plan request shape, temporary IDs and portable validation; products resolve boards and permissions
internal/mcpbridge/      SHARED stdio MCP client bridge to a configured Streamable HTTP endpoint
internal/integration/    SHARED versioned task/status DTOs and canonical repository identity
internal/planning/       SHARED label names and colours, work modes, planned dates and the analysis of dependencies
internal/logging/        SHARED the structured logger
internal/team/           TEAM-ONLY everything specific to Team (below)
internal/appops/         PERSONAL the application operations a model (the assistant, later MCP) may use: permission, proposal, confirmation, audit
internal/assistant/      PERSONAL the conversational assistant's engine, and its providers (Claude Code, Codex, Gemini CLI, OpenCode) under provider/
internal/<everything else>/       Personal's code (domain, store, service, api, runner, agent, gitrepo, …)
web/                     Personal's web app (Svelte PWA)
```

### Shared packages

A package is **shared** when both engines use it and it holds no behaviour of either: it is plumbing. Today:

| Package | What it is | Used by |
| --- | --- | --- |
| `internal/integration` | Versioned task/status DTOs, the text-only project-transfer contract and canonical repository identity validation; no product behavior | Personal metadata API, Team user connector |
| `internal/planning` | What a label may be called and coloured, the three work modes (human, agent, hybrid), planned date ranges, and the analysis that finds invalid and conflicting dependencies. Pure functions: no storage, workspace, role or process. See [PLANNING.md](PLANNING.md). | `internal/domain`, `internal/service` (Personal); `internal/team/domain`, `internal/team/service` (Team) |
| `internal/sqlitekit` | Opens a SQLite file (one writer, a pool of readers, WAL), runs versioned migrations, copies the database before an upgrade, inspects it read-only. Knows no schema. | `internal/store/sqlite` (Personal), `internal/team/store` (Team) |
| `internal/httpkit` | `WriteJSON`/`WriteError` and the error envelope, strict `DecodeJSON`, and the `LogRequests`, `RecoverPanics` and `SecurityHeaders` middleware. Holds no route. | `internal/api` (Personal), `internal/team/api` (Team) |
| `internal/mcpcontract` | MCP Streamable HTTP framing, bounded tool request/response handling, and validated project/operation scopes. Holds no credentials, board operations or product permissions. Uses the official MCP Go SDK. | Personal MCP server, Team MCP server, `internal/mcpbridge` |
| `internal/mcpplan` | The neutral batch-plan wire shape, temporary task IDs, and validation of single-project requests. Holds no board data or authorization. | Personal appops, Team MCP service |
| `internal/mcpbridge` | Stdio MCP client forwarding tools, resources and prompts to the configured Streamable HTTP endpoint. Holds no product identity or board behavior. | `cmd/werkbord`, `cmd/werkbord-team` |
| `internal/uipresentation` | Product-neutral static board card grammar; products supply tokens, data, permissions and actions. | Both web frontends |
| `internal/nativebridge` | Product-neutral WebKit/Wails request and callback transport. No product methods, credentials or policies. | both desktop frontends |
| `internal/entitlement` | The Free plan's team size and the offline check of a Premium unlock key: pure functions over a public key stamped into the build. No workspace, role, storage or process. See [PLANS.md](PLANS.md). | Team service, vendor tool, the desktop app |
| `internal/logging` | `slog` logger construction | both |
| `internal/transport` | The contract for a private network node (start, stop, local node, listen, dial, peers, connection metadata). Names no network. `memtransport` and `transporttest` are its in-memory implementation and conformance suite. | `internal/netprivate` (Personal, Tailscale); `internal/team/infra/overlaynet` (Team's own private network) |
| `internal/deviceid` | A device's ID, public key and the checks on them. No private key. | Team |
| `internal/deviceid/localidentity` | A device's own identity: the private key and its storage. **Team never imports it** (rule 11). | Personal's runner (when wired) |
| `internal/envelope` | Signed cross-device messages: the format, the semantic actions, signing, verification, expiry and replay checks. | Team (verifies); Personal (will sign and act) |
| `internal/enrollment` | How a device joins a customer's workspace: the signed invitation (`werkbord://join/…`), the enrollment protocol over TLS 1.3 pinned to the workspace's key, its server and client, and what the joining device installs. Holds no private key (a signer is passed in) and names no network. | Team (serves it, and joins as a host); Personal (will join member devices; not yet) |

Each was extracted from Personal (which now uses it too) rather than copied into Team, so there is one
SQLite open path and one set of security headers.

Everything else under `internal/` outside `internal/team` is **Personal's**: `domain`, `store`,
`service`, `api`, `controller`, `appops`, `assistant`, and especially the machinery that executes things — `runner`, `remote`,
`runnerwire`, `agent`, `gitrepo`, `github`, `daemon`, `netprivate`. The assistant (`appops`, `assistant`) runs the person's own
coding agent runtime and is never part of Team ([ASSISTANT.md](ASSISTANT.md)). Team may not import any of it. If Team needs something from one
of those, the answer is to move the non-product-specific part into a shared package (as `sqlitekit` and `httpkit` were),
not to reach across.

The same goes for the web: the PWA in `web/` is Personal's app. `web/src/shell/` is a separately built product-neutral desktop frontend, never embedded in the PWA; it displays Team-owned screens without importing their code. Team has its own small
console (`internal/team/console`). When Team needs real UI reuse, extract the components both use into a shared web
package (for example `web-ui/`) that `web/` and a Team web app both depend on; do not put Team screens in `web/`.

`internal/relay` and its `e2e`, `access`, `client`, `tunnel` and `server` packages are shared protocol plumbing:
opaque client bindings, pairing keys, ciphertext framing and bounded HTTP transport. They hold no product routes or
workspace authority. Personal supplies its phone handler; Team supplies its separate read-only summary grant under
`internal/team/server/daemon_phone.go` and its phone view under `internal/team/phone/`. The company relay is the
`werkbord-team phone-relay` command. See [TEAM_PHONE_SECURITY.md](TEAM_PHONE_SECURITY.md).

### Dependency direction

```
cmd/werkbord ──▶ Personal packages ──▶ shared ◀── internal/team ◀── cmd/werkbord-team
```

- The controller **never** depends on Team: no package outside `internal/team` and `cmd/werkbord-team` may
  import them, and nothing in `go list -deps ./cmd/werkbord` is Team's.
- Team depends on shared packages **only**, from an explicit allow-list.
- Shared packages depend on neither product.

### Where Team-specific functionality belongs

All of it goes under `internal/team/`, and the program under `cmd/werkbord-team/`:

| Directory | Holds |
| --- | --- |
| `internal/team/domain` | The vocabulary and rules: workspace, member, project, project membership, roles and permissions, validation. No I/O. |
| `internal/team/store` | Team's SQLite schema (`migrations/`), including source references and MCP task-plan proposals, and queries, on `internal/sqlitekit`. |
| `internal/team/service` | The use cases. Every one takes the signed-in member and asks their role what is allowed; batch proposals recheck permissions and ticket versions during their atomic approval. |
| `internal/team/api` | The HTTP API (`/api/team/v1`), on `internal/httpkit`. |
| `internal/team/mcp` | Team-owned board and proposal-status tools, authenticated as a current member and routed through Team services. Batch plans wait for a human decision. No execution or model credentials. |
| `internal/team/console` | Team's web console (static files, no build step), including scoped MCP client settings and the editable Plans review. |
| `internal/team/config`, `internal/team/server` | Settings, and wiring and lifecycle (including the private network's runtime and the adapter between the workspace's terms and the infrastructure's). |
| `internal/team/infra/…` | Team's own **infrastructure**, imported only by `server` and `cmd/werkbord-team`: `pki` (the workspace's keys and the network's certificates, sealed), `overlay` (the node description, the default-deny policy, rendering), `nebula` (the supervisor of the one pinned network program, and its pin), `rqlite` (the supervisor of the one pinned database program, its pin and its admin client), `overlaynet` (the network as a `transport.Transport`). Never handles a request. See [TEAM_NETWORK.md](TEAM_NETWORK.md) and [TEAM_STORAGE.md](TEAM_STORAGE.md). |
| `internal/team/store/replicated` | Team's storage on a cluster of Workspace Hosts: a `store.Store` over rqlite (a use case runs on the host's own copy and its writes are one guarded rqlite transaction), the host's copy, backups and restore, and the move of an existing `team.db`. The only code that talks to the database; imported only by `server` and `cmd/werkbord-team`. See [ADR 0003](adr/0003-replicated-workspace-storage.md). |

Team size enforcement and host-verified Premium proof belong here too (the rule itself is the shared `internal/entitlement`). The `workspace`,
`project`, `ticket`, `invite` and `activity` concepts all live in `internal/team/domain`; the ticket workflow is
`internal/team/service/tickets.go`, repository awareness is `internal/team/service/repostate.go`. Do **not**
add a `teamMode` flag, a Team-only branch, or a Team import to one of Personal's packages, and do not add Team screens or
API routes to `web/` or `internal/api`. If one of Personal's packages needs to change so Team can reuse it, the change makes
it more general and stays free of Team's concepts.

## What Team has

Team also coordinates cross-repository [Initiatives](INITIATIVES.md) under `internal/team/`: reference-only
objectives, explicit dependencies, permission-checked current progress and advisory next actions. Salie reads
them through the existing desktop Team bridge; no execution or credential boundary changes.


A **workspace** with exactly one **owner**, **members**, **projects**, **project membership**, **roles** (Owner, Admin and
Member), and a registry of **devices** with their own capabilities (runner, workspace host, connectivity host: independent of
roles); and, on top of that, the collaborative workflow: **project roles** (owner, reviewer, member), **invite links**,
a shared **board** (Available, In Progress, Review, Done), **tickets** that members claim atomically, **Git
metadata** members' own Werkbords report (branch, commits, pull request), **repository awareness** (behind, conflicting,
stale, overlapping branches), an **activity** history, and a ticket's **handoff**, the context a member takes into their own
Werkbord. One console (Workspace, Projects, Board, My Work, Reviews, Repository, Activity) is kept current by a
workspace-wide sync that survives disconnects. See [TEAM.md](TEAM.md) for the model, the API and how to run it, and
[TEAM_SECURITY.md](TEAM_SECURITY.md) for the review of why no member can reach another's machine.

The workflow respects the rule above: Team stores *reports* (a developer's Werkbord says "my branch is 3 commits behind");
it does not run Git, call GitHub, or reach a runner. The Team service on a member's own computer verifies signed requests from
locally approved devices belonging to that same person, then exchanges only semantic actions with the person's own loopback
controller using a narrow access grant ([INTEGRATION.md](INTEGRATION.md), [EXECUTION_COORDINATION.md](EXECUTION_COORDINATION.md)).
The controller's generic local access API knows no Team workspace, credential or policy.

Roles are permission tables, not checks for a name: services ask `Role.Can(permission)`, so adding a role is one entry
in `internal/team/domain/roles.go` and needs no schema change and no handler change.

**The private network**: each workspace can have a network made and run by its own machines: its own trust identity, its own
certificate authority, signed invitations, enrollment over TLS, discovery hosts and relays on machines the customer owns, a
default-deny policy, revocation in two layers, and the authority handed to more than one Workspace Host. Werkbord operates none
of it. See [TEAM_NETWORK.md](TEAM_NETWORK.md) and [the decision](adr/0002-customer-owned-network.md).

Not built yet: a payment backend ([PLANS.md](PLANS.md)), HTTPS (put Team behind a TLS proxy), ownership transfer,
comments and chat (Team coordinates; it is not a messenger), a Windows installer.

## Running and building

Personal Werkbord (needs Go and Node, for the web app):

```bash
make build                 # bin/werkbord, with the web app embedded
./bin/werkbord serve       # or `werkbord setup`; see README.md and docs/INSTALL.md
make dev-api               # the controller, from source
make dev-web               # the Vite dev server (a second terminal)
```

Werkbord Team (needs Go only):

```bash
make build-team            # bin/werkbord-team
./bin/werkbord-team workspace create --name "Acme" --owner "Ada"   # prints the owner's token, once
./bin/werkbord-team serve  # http://127.0.0.1:7430
make dev-team              # the same, from source, in the foreground
make install-team          # copy it to ~/.local/bin (PREFIX=… to change)
```

Tests:

```bash
make check                 # everything: Go tests, vet, gofmt, svelte-check, ESLint, builds, the boundary tests
make test-werkbord         # Personal's tests, and the web app's
make test-team             # Team's tests
make test-install test-install-team   # each installer against a local release server
```

The desktop app (needs the native toolkit for its target OS): `make desktop`, `make desktop-package` (adds a
`.dmg`), `make desktop-dev` (and `make desktop-dev-stop`), `make desktop-check`. See [DESKTOP.md](DESKTOP.md).

There is no separate Team desktop app: `make desktop-package` builds the one app, Team's service and installer included
([TEAM_DESKTOP.md](TEAM_DESKTOP.md)).

Releases: `make dist PRODUCT=werkbord` writes the controller archives for the unified release. `make dist
PRODUCT=werkbord-team` remains an offline source build for controlled operator deployments; those archives are not attached to
public desktop releases. See [VERSIONING.md](VERSIONING.md).

## Enforcement

`internal/archtest` fails `go test ./...` (and so `make check` and CI) when a trust boundary is crossed. They are about who may
run what, with whose credentials, and not about packaging:

1. the controller's build reaches Team's code, or anything outside Team imports it (a shared package must not become the way in);
2. Team's build includes any package of this module other than its own and the allow-listed shared plumbing (`teamAllowed`), in
   particular anything that runs agents, commands or Git;
3. Team's code imports `os/exec`, `plugin` or `net/rpc`, or its build includes `tailscale.com`, SSH or a PTY package (rule 9 below
   is the one reviewed exception); a Team server package makes an outbound connection, or imports `os` outside its configuration;
   or the controller's own code names Team's API, settings or executable (it holds no Team credential and makes no call to Team);
4. Team's build uses a third-party module the controller does not, and that module has not been declared in `teamOnlyModules`;
5. `cmd/werkbord/VERSION` is not `MAJOR.MINOR.PATCH`, or a second version file comes back;
6. the desktop app's window toolkit (Wails, which needs cgo and the system's web view) appears in either executable's build or in
   the root `go.mod`, `desktop/` stops being a Go module of its own, or anything in it links Team's code or an execution engine;
7. the Team installer in the app (`desktop/internal/teaminstall`) imports any code of this repository, or any process is started
   outside it and the app's `main` package, or the installer starts a program that is not a fixed absolute path.

Beyond those, `internal/archtest` also keeps the production foundation honest (see
[the architecture decision](adr/0001-team-production-architecture.md)):

9. Team's one exception to "never starts a process" is a narrow, named grant for *infrastructure* supervision under
   `internal/team/infra/` (Nebula and rqlite supervisors): constant program names only, never a
   shell, Git, an agent or a runtime, importing nothing that handles a request, imported only by Team's wiring. The rule itself
   is tested against code that breaks it;
10. shared packages import only the standard library and each other, and name no network or database vendor;
11. Team's build never includes `internal/deviceid/localidentity`: private signing keys exist only on a device;
12. code written against the contracts (`transport`, `deviceid`, `envelope`, Team's domain, service and API) names no
    network or database vendor (Tailscale, Nebula, DERP, lighthouses, Headscale, rqlite);
13. only `internal/netprivate` imports Tailscale;
14. the network supervisor is not a way to run anything: its exported API is the reviewed list, nothing in it means "a thing
    to run", and it starts exactly one program, by a literal name, with a literal `-config <file>`;
15. no code that runs in Team's network contains a URL that leads anywhere but this computer: Werkbord operates no service
    Team could be pointed at;
16. every setting Team reads is its own (`WERKBORD_TEAM_*`) and none names a service, an account or a URL;
17. coordination records contain no device/authority private signing key. The offline release signer under `cmd/werkbord-team/vendor` is never packaged with customers.

The assistant has its own (`internal/archtest/assistant_test.go`, [ASSISTANT.md](ASSISTANT.md)):

18. Team's build links neither `internal/appops` nor `internal/assistant`;
19. the operations import only the domain, planning, the services and the store (for the assistant's own records), and the
    engine only the operations, the providers, the domain and the store: a model's request reaches the board through the
    services' rules and the confirmation, never round them;
20. only the providers start a process, and they import nothing of the board;
21. no provider is ever run with its safety checks off (`--dangerously…`, `bypassPermissions`, a writable sandbox, MCP
    configuration, extra directories), and the flags that turn its tools off are still there;
22. the assistant's permissions are the reviewed list: none of them is about starting or stopping a run, Git, a setting,
    a repository, or deleting core data. Owner-only personal workspace deletion is a reviewed personal proposal, with mandatory human confirmation and transactional collection-dependency checks (see PERSONAL_WORKSPACES.md).

The gate also fixes the production team-size and authentication wiring in architecture tests and restricts the native Keychain wrapper to reviewed Security/memory calls. There is one build: no release script, build tag or linker flag selects an edition (`TestThereIsOneBuildAndNoEditionIsSelectedAtBuildTime`). Runner operations stay the reviewed semantic bridge, never a general remote administration API.

The Go module is shared, so `go.mod` lists every dependency of every executable; what counts is what each executable is built
from, which is what these checks inspect.

## Adding to the code

- **A change to the controller or the app** that Team does not need: edit it as usual.
- **A change to Team**: stay inside `internal/team` and `cmd/werkbord-team`.
- **Something both need**: make it a shared package with no product behaviour; add it to `teamAllowed` in
  `internal/archtest/boundary_test.go`; migrate the controller onto it if it already had a copy.

Every implementation commit bumps the one version and tags it: [VERSIONING.md](VERSIONING.md) and [AGENTS.md](../AGENTS.md).

## Moving between Personal and Team

The desktop shell coordinates an explicit text-only project move through each engine's authenticated API. A review names
every copied field, native confirmation authorises it, the destination imports atomically and a revision check guards source
archiving. Team never calls the controller to perform a project move; the controller never calls Team. Original history stays
at its source. Project moves create no execution grant or checkout. [Contract and limits](unified-platform/PROJECT_MOVES.md).

The current completion state and the independent checks still needed before a stable release are in
[platform acceptance](unified-platform/ACCEPTANCE.md). Salie's proposed Team read-only policy is still pending; the existing
approved `create_ticket` bridge must not be described as read-only.
