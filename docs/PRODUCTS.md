# Products: individual Werkbord and Werkbord Team

> **Direction.** Werkbord is **one product with one version and one release** (`cmd/werkbord/VERSION`, tag
> `werkbord-vX.Y.Z`). The table and sections below still describe the two executables, data directories and code
> boundaries that exist today; where they say "separately versioned", "own version file" or "own release tag" they are
> out of date. The separate version, tag, release and installer are retired, and the rest of the separation is removed in
> stages: see [UNIFICATION.md](UNIFICATION.md).

This repository builds two executables that are released together. They share code and infrastructure, and they have their
own data directory and CLI. The primary desktop installer bundles both components; Personal is free, Team workspaces carry offline licenses.

| | **Werkbord** (individual) | **Werkbord Team** |
| --- | --- | --- |
| Product ID | `werkbord` | `werkbord-team` |
| What it is | A local-first control plane for your own coding agents: board, runs, Git, runners | A shared workspace that coordinates a team: members, projects, who is on what |
| Executable | `werkbord` (and `devboard`, its name before the rename) | `werkbord-team` |
| Main package | `cmd/werkbord` | `cmd/werkbord-team` |
| Team/own code lives in | the shared `internal/` packages and `web/` | `internal/team/…` only |
| Version file | `cmd/werkbord/VERSION` | the same file (`cmd/werkbord-team/VERSION` is gone) |
| Release tag | `werkbord-vX.Y.Z` | the same tag |
| Release archives | `werkbord_<version>_<os>_<arch>` (also published as `devboard_…` for older updaters) | `werkbord-team_<version>_<os>_<arch>` |
| Installer | `scripts/install.sh`, `scripts/install.ps1`; on a Mac, the app in `desktop/` ([DESKTOP.md](DESKTOP.md)), which installs the same program | `scripts/install-team.sh` for a command-line host; on a Mac, the same app, which installs Team's service ([TEAM_DESKTOP.md](TEAM_DESKTOP.md)) |
| Data directory | `werkbord` in your user config directory (`devboard` on an install from before the rename) | `werkbord-team` in your user config directory |
| Settings | `WERKBORD_*` (and the older `DEVBOARD_*`) | `WERKBORD_TEAM_*` |
| Default address | `127.0.0.1:7420` | `127.0.0.1:7430` |
| Build | `make build` (or `make werkbord`); the Mac app: `make desktop` | `make build-team` (or `make werkbord-team`) |

The two backend products can be installed, run and upgraded independently, on the same computer or on different ones. Phase 3 adds a product-neutral desktop shell for everyday Personal and Team workspaces; see [UNIFIED_DESKTOP.md](UNIFIED_DESKTOP.md). Bundling one does not activate or start the other; native Team activation remains explicit. The app offers an explicit, optional local
installation of the free individual runner through that product’s normal setup.

Team 3.0 defaults to customer-owned Nebula, administrator-approved enrollment, device-signed remote API requests and offline signed license enforcement. There is no Team Tailscale account or vendor runtime requirement. Customers operate their Workspace/Connectivity Hosts, backups and local runners. Individual tsnet and `DEVBOARD_*` compatibility stay separate. See [TEAM_SECURITY.md](TEAM_SECURITY.md), [TEAM_INSTALL.md](TEAM_INSTALL.md), [TEAM_LICENSE.md](TEAM_LICENSE.md) and the [release gate](TEAM_SECURITY_GATE.md).

> **Naming.** Werkbord was called Dev Board. Since Werkbord 1.0 the executable is `werkbord`, and everything a person
> sees or types says so. What existing installs depend on keeps working, indefinitely and without nagging: the `devboard`
> command (installed beside `werkbord`), every `DEVBOARD_*` variable (read after its `WERKBORD_*` name), the data
> directory of an install from before the rename, its tailnet name (a phone's address), and `devboard update` on older
> releases (every release is also published under the old archive name). A login service installed under the old label is
> found and managed, and `werkbord setup` moves it to the new one. Not renamed, because they are protocols rather than
> names people use: the Go module path (`devboard/…`), the `devboard/` namespace of the branches agents work on (older
> remote runners create it), the database file name and the agents' blocker marker.

## The rule that matters most

**A Team workspace is a coordination layer. It is not a shared execution environment.**

Every developer keeps using their own machine, their own Werkbord runner, their own Git credentials, their own
GitHub credentials, and their own agent and model credentials. Team never starts developer processes or holds Git, agent or model credentials,
and never opens an arbitrary path into another member’s computer. Only a person’s signed semantic requests to their own
locally trusted devices can cross the narrow local runner bridge, subject to the individual controller’s policies. Team records who is on the team and what they are working on; each person's own Werkbord does the work.

This is enforced, not just intended (see [Enforcement](#enforcement)): Team's build cannot link the code that runs
agents, commands or Git, and its own code cannot import `os/exec`.

## Layout

The repository is one Go module. The usual monorepo split (`apps/` and `packages/`) maps onto Go's own conventions
(`cmd/` for programs, `internal/` for packages), and the existing code keeps its places:

```
cmd/werkbord/            APP    the individual product
desktop/                 APP    the individual product's Mac app: a native window around web/ (its own Go module; DESKTOP.md)
cmd/werkbord-team/       APP    Werkbord Team

internal/sqlitekit/      SHARED opening, migrating and backing up a SQLite database
internal/httpkit/        SHARED JSON responses, error envelope, strict body decoding, request logging, security headers
internal/integration/    SHARED versioned task/status DTOs and canonical repository identity
internal/logging/        SHARED the structured logger
internal/team/           TEAM-ONLY everything specific to Team (below)
internal/<everything else>/       the individual product's code (domain, store, service, api, runner, agent, gitrepo, …)
web/                     the individual product's web app (Svelte PWA)
```

### Shared packages

A package is **shared** when both products use it and it holds no behaviour of either: it is plumbing. Today:

| Package | What it is | Used by |
| --- | --- | --- |
| `internal/integration` | Versioned task/status DTOs and canonical repository identity validation; no product behavior | Individual metadata API, Team user connector |
| `internal/sqlitekit` | Opens a SQLite file (one writer, a pool of readers, WAL), runs versioned migrations, copies the database before an upgrade, inspects it read-only. Knows no schema. | `internal/store/sqlite` (individual), `internal/team/store` (Team) |
| `internal/httpkit` | `WriteJSON`/`WriteError` and the error envelope, strict `DecodeJSON`, and the `LogRequests`, `RecoverPanics` and `SecurityHeaders` middleware. Holds no route. | `internal/api` (individual), `internal/team/api` (Team) |
| `internal/nativebridge` | Product-neutral WebKit/Wails request and callback transport. No product methods, credentials or policies. | both desktop frontends |
| `internal/logging` | `slog` logger construction | both |
| `internal/transport` | The contract for a private network node (start, stop, local node, listen, dial, peers, connection metadata). Names no network. `memtransport` and `transporttest` are its in-memory implementation and conformance suite. | `internal/netprivate` (individual, Tailscale); `internal/team/infra/overlaynet` (Team's own private network) |
| `internal/deviceid` | A device's ID, public key and the checks on them. No private key. | Team |
| `internal/deviceid/localidentity` | A device's own identity: the private key and its storage. **Team never imports it** (rule 11). | the individual product's runner (when wired) |
| `internal/envelope` | Signed cross-device messages: the format, the semantic actions, signing, verification, expiry and replay checks. | Team (verifies); the individual product (will sign and act) |
| `internal/enrollment` | How a device joins a customer's workspace: the signed invitation (`werkbord://join/…`), the enrollment protocol over TLS 1.3 pinned to the workspace's key, its server and client, and what the joining device installs. Holds no private key (a signer is passed in) and names no network. | Team (serves it, and joins as a host); the individual product (will join member devices; not yet) |

Each was extracted from the individual product (which now uses it too) rather than copied into Team, so there is one
SQLite open path and one set of security headers.

Everything else under `internal/` outside `internal/team` is the **individual product's**: `domain`, `store`,
`service`, `api`, `controller`, and especially the machinery that executes things — `runner`, `remote`, `runnerwire`,
`agent`, `gitrepo`, `github`, `daemon`, `netprivate`. Team may not import any of it. If Team needs something from one
of those, the answer is to move the non-product-specific part into a shared package (as `sqlitekit` and `httpkit` were),
not to reach across.

The same goes for the web: the PWA in `web/` is the individual product's app. `web/src/shell/` is a separately built product-neutral desktop frontend, never embedded in the PWA; it displays Team-owned screens without importing their code. Team has its own small
console (`internal/team/console`). When Team needs real UI reuse, extract the components both use into a shared web
package (for example `web-ui/`) that `web/` and a Team web app both depend on; do not put Team screens in `web/`.

### Dependency direction

```
cmd/werkbord ──▶ individual packages ──▶ shared ◀── internal/team ◀── cmd/werkbord-team
```

- The individual product **never** depends on Team: no package outside `internal/team` and `cmd/werkbord-team` may
  import them, and nothing in `go list -deps ./cmd/werkbord` is Team's.
- Team depends on shared packages **only**, from an explicit allow-list.
- Shared packages depend on neither product.

### Where Team-specific functionality belongs

All of it goes under `internal/team/`, and the program under `cmd/werkbord-team/`:

| Directory | Holds |
| --- | --- |
| `internal/team/domain` | The vocabulary and rules: workspace, member, project, project membership, roles and permissions, validation. No I/O. |
| `internal/team/store` | Team's SQLite schema (`migrations/`) and queries, on `internal/sqlitekit`. |
| `internal/team/service` | The use cases. Every one takes the signed-in member and asks their role what is allowed. |
| `internal/team/api` | The HTTP API (`/api/team/v1`), on `internal/httpkit`. |
| `internal/team/console` | Team's web console (static files, no build step). |
| `internal/team/config`, `internal/team/server` | Settings, and wiring and lifecycle (including the private network's runtime and the adapter between the workspace's terms and the infrastructure's). |
| `internal/team/infra/…` | Team's own **infrastructure**, imported only by `server` and `cmd/werkbord-team`: `pki` (the workspace's keys and the network's certificates, sealed), `overlay` (the node description, the default-deny policy, rendering), `nebula` (the supervisor of the one pinned network program, and its pin), `rqlite` (the supervisor of the one pinned database program, its pin and its admin client), `overlaynet` (the network as a `transport.Transport`). Never handles a request. See [TEAM_NETWORK.md](TEAM_NETWORK.md) and [TEAM_STORAGE.md](TEAM_STORAGE.md). |
| `internal/team/store/replicated` | Team's storage on a cluster of Workspace Hosts: a `store.Store` over rqlite (a use case runs on the host's own copy and its writes are one guarded rqlite transaction), the host's copy, backups and restore, and the move of an existing `team.db`. The only code that talks to the database; imported only by `server` and `cmd/werkbord-team`. See [ADR 0003](adr/0003-replicated-workspace-storage.md). |

Billing, licensing, SSO, audit logs, and anything else only the paid product has belong here too. The `workspace`,
`project`, `ticket`, `invite` and `activity` concepts all live in `internal/team/domain`; the ticket workflow is
`internal/team/service/tickets.go`, repository awareness is `internal/team/service/repostate.go`. Do **not**
add a `teamMode` flag, a Team-only branch, or a Team import to an individual package, and do not add Team screens or
API routes to `web/` or `internal/api`. If an individual package needs to change so Team can reuse it, the change makes
it more general and stays free of Team's concepts.

## What Team has today

A **workspace** with exactly one **owner**, **members**, **projects**, **project membership**, **roles** (Owner, Admin and
Member), and a registry of **devices** with their own capabilities (runner, workspace host, connectivity host: independent of
roles); and, on top of that, the collaborative workflow: **project roles** (owner, reviewer, member), **invite links**,
a shared **board** (Backlog, Available, In Progress, Review, Done), **tickets** that members claim atomically, **Git
metadata** members' own Werkbords report (branch, commits, pull request), **repository awareness** (behind, conflicting,
stale, overlapping branches), an **activity** history, and **"Open in my runner"**, which hands a ticket's context to the
member who holds it for use in their own Werkbord. Team 2 joins these into one console (Workspace, Projects, Board,
My Work, Reviews, Repository, Activity) kept current by a workspace-wide sync that survives disconnects, and hardens the
concurrent paths. See [TEAM.md](TEAM.md) for the model, the API and how to run it, and
[TEAM_SECURITY.md](TEAM_SECURITY.md) for the review of why no member can reach another's machine. It
reuses the shared packages above and the individual product's ideas (token-authenticated API, SQLite, graceful shutdown),
but none of its code beyond the shared plumbing.

The workflow respects the rule above: Team stores *reports* (a developer's Werkbord says "my branch is 3 commits behind");
it does not run Git, call GitHub, or reach a runner. The desktop daemon verifies signed requests from locally approved devices belonging to that same person, then exchanges
only semantic actions with the person’s own loopback controller using a narrow access grant. The individual product’s
generic local access API knows no Team workspace, credential or policy. A ticket's handoff is a document the console offers to copy or download.

Roles are permission tables, not checks for a name: services ask `Role.Can(permission)`, so adding a role is one entry
in `internal/team/domain/roles.go` and needs no schema change and no handler change.

**The private network** (Team 2.5): each workspace can have a network made and run by its own machines: its own trust
identity, its own certificate authority, signed invitations, enrollment over TLS, discovery hosts and relays on machines the
customer owns, a default-deny policy, revocation in two layers, and the authority handed to more than one Workspace Host.
Werkbord operates none of it. See [TEAM_NETWORK.md](TEAM_NETWORK.md) and [the decision](adr/0002-customer-owned-network.md).

Team 2.8 adds a separately packaged Wails desktop app, a persistent macOS system service, offline license activation,
automated first-host creation and approved device enrollment, multi-admin management and actionable resilience status.
See [TEAM_DESKTOP.md](TEAM_DESKTOP.md) for the shipped workflow and service/security details.

Not built yet: a payment backend, automatic seat enforcement, a Team update command, a
Windows installer, HTTPS (put Team behind a TLS proxy), ownership transfer, comments and chat (Team coordinates; it is
not a messenger), a Team-side automatic reporter in the individual Werkbord. The Team service on a member's own computer does the reporting
(docs/INTEGRATION.md); the individual product stores only generic task provenance and has no Team configuration, credential,
API or background bridge.

## Running and building

Individual Werkbord (needs Go and Node, for the web app):

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
make check                 # everything: both products and the shared packages (Go tests, vet, gofmt, svelte-check, ESLint, builds)
make test-werkbord         # the individual product's tests, and the web app's
make test-team             # Team's tests
make test-install test-install-team   # each installer against a local release server
```

The individual Mac app (needs the Xcode command line tools): `make desktop`, `make desktop-package` (adds a
`.dmg`), `make desktop-dev` (and `make desktop-dev-stop`), `make desktop-check`. See [DESKTOP.md](DESKTOP.md).

There is no separate Team Mac app: `make desktop-package` builds the one app, Team's service and installer included
([TEAM_DESKTOP.md](TEAM_DESKTOP.md)).

Releases: `make dist PRODUCT=werkbord` or `PRODUCT=werkbord-team` writes the archives and `checksums.txt` to `dist/`.
CI does this when a product tag is pushed. See [VERSIONING.md](VERSIONING.md).

## Enforcement

`internal/archtest` fails `go test ./...` (and so `make check` and CI) when:

1. the controller's build reaches Team;
2. any package outside Team imports Team's packages (a shared package must not become the way in);
3. Team's build includes any package of this module other than its own and the allow-listed shared plumbing;
4. Team's code imports `os/exec`, `plugin` or `net/rpc`, or its build includes `tailscale.com`, SSH or a PTY package;
   (rule 7 also fails when a Team server package makes an outbound connection, imports `os` outside its configuration,
   or when the individual product's own code names Team's API, settings or executable);
5. Team's build uses a third-party module the individual product does not, and that module has not been declared in
   `teamOnlyModules` (declaring it makes the test check the individual product never picks it up);
6. a product's `VERSION` file is not `MAJOR.MINOR.PATCH`;
7. the desktop app's window toolkit (Wails, which needs cgo and the system's web view) appears in either executable's build
   or in the root `go.mod`, or `desktop/` stops being a Go module of its own, or anything in it reaches Team.

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
17. coordination records contain no device/authority private signing key. The service verifies licenses using only a vendor public key; the separate offline issuer is under `cmd/werkbord-team/vendor` and is never packaged with customers.

The gate also fixes the production license/authentication wiring in architecture tests and restricts the native Keychain wrapper to reviewed Security/memory calls. Typed local license-file/public-key configuration is permitted; no online license/vendor URL is introduced. Runner operations stay the reviewed semantic bridge, never a general remote administration API.

The Go module is shared, so `go.mod` lists every dependency of every executable; what counts is what each executable is
built from, which is what these checks inspect. (There used to be an empirical copy-the-repository-without-Team build,
`make verify-isolation`; it proved the controller could be separated from Team, which is no longer a goal. The import-graph
checks above remain, for the security reason: the controller runs a person's agents as them and must not link Team.)

## Adding to either product

- **A change to the individual product** that Team does not need: edit it as usual. Bump `werkbord`'s version.
- **A change to Team**: stay inside `internal/team` and `cmd/werkbord-team`. Bump `werkbord-team`'s version.
- **Something both need**: make it a shared package with no product behaviour; add it to `teamAllowed` in
  `internal/archtest/boundary_test.go`; migrate the individual product onto it if it already had a copy. Bump each
  product whose behaviour the change affects.

Every implementation commit bumps and tags the product(s) it changes: [VERSIONING.md](VERSIONING.md) and
[AGENTS.md](../AGENTS.md).

## Phase 1 integration

The Team service on the member's own computer synchronizes held Team tickets and safe Individual execution metadata through a versioned contract; in the desktop app the Team service on the member's own computer runs the same synchronization once the member connects their runner with its narrow grant. The backends retain separate authority and credentials; no other computer, and no remote Workspace Host, ever receives that grant. See [INTEGRATION.md](INTEGRATION.md) for setup, project matching, reconciliation and the authority model.

## Phase 2 execution coordination

Team stores shared schedule requests and routes owner-signed semantic controls.
Individual retains task-bound local approval, execution policy and idempotent
run creation through its existing scheduler/runtime. See
[EXECUTION_COORDINATION.md](EXECUTION_COORDINATION.md).

## Phase 3 desktop unification

The neutral shell in `desktop/` reaches separately installed Personal and Team
services through authenticated loopback HTTP. `web/src/shell/` reuses existing
Svelte primitives and tokens, and has its own build. Team screens and backend
behavior remain under `internal/team`; the shell never links either execution
engine or Team backend. See [UNIFIED_DESKTOP.md](UNIFIED_DESKTOP.md).

## Phase 4 distribution

One primary desktop bundle contains the isolated backends and Team’s own native installer. Independent versions, offline release trust and databases remain intact. See [UNIFIED_DISTRIBUTION.md](UNIFIED_DISTRIBUTION.md) and [PHASE4_READINESS.md](PHASE4_READINESS.md).
