# Products: individual Werkbord and Werkbord Team

This repository builds two products. They share code and infrastructure, and they are otherwise separate: each has its
own executable, version, data directory, installer, release artifacts and (eventually) licence.

| | **Werkbord** (individual) | **Werkbord Team** |
| --- | --- | --- |
| Product ID | `werkbord` | `werkbord-team` |
| What it is | A local-first control plane for your own coding agents: board, runs, Git, runners | A shared workspace that coordinates a team: members, projects, who is on what |
| Executable | `devboard` (the program's name today; the product is Werkbord) | `werkbord-team` |
| Main package | `cmd/devboard` | `cmd/werkbord-team` |
| Team/own code lives in | the shared `internal/` packages and `web/` | `internal/team/…` only |
| Version file | `cmd/devboard/VERSION` | `cmd/werkbord-team/VERSION` |
| Release tag | `werkbord-vX.Y.Z` | `werkbord-team-vX.Y.Z` |
| Release archives | `devboard_<version>_<os>_<arch>` | `werkbord-team_<version>_<os>_<arch>` |
| Installer | `scripts/install.sh`, `scripts/install.ps1` | `scripts/install-team.sh` |
| Data directory | `devboard` in your user config directory | `werkbord-team` in your user config directory |
| Settings | `DEVBOARD_*` | `WERKBORD_TEAM_*` |
| Default address | `127.0.0.1:7420` | `127.0.0.1:7430` |
| Build | `make build` (or `make werkbord`) | `make build-team` (or `make werkbord-team`) |

The two can be installed, run and upgraded independently, on the same computer or on different ones. Installing one
never installs, starts or changes the other.

> **Naming.** The product is Werkbord; the individual program's executable, Go module (`devboard`), data directory and
> service names are still `devboard`, and renaming them would break existing installs and the updater, so that is a
> separate change. Product IDs, tags and version files use the product name.

## The rule that matters most

**A Team workspace is a coordination layer. It is not a shared execution environment.**

Every developer keeps using their own machine, their own Werkbord runner, their own Git credentials, their own
GitHub credentials, and their own agent and model credentials. Team never starts a process, never holds a credential,
and never opens a path into another member's computer. A team member cannot make another member's machine run
anything. Team records who is on the team and what they are working on; each person's own Werkbord does the work.

This is enforced, not just intended (see [Enforcement](#enforcement)): Team's build cannot link the code that runs
agents, commands or Git, and its own code cannot import `os/exec`.

## Layout

The repository is one Go module. The usual monorepo split (`apps/` and `packages/`) maps onto Go's own conventions
(`cmd/` for programs, `internal/` for packages), and the existing code keeps its places:

```
cmd/devboard/            APP    the individual product
cmd/werkbord-team/       APP    Werkbord Team

internal/sqlitekit/      SHARED opening, migrating and backing up a SQLite database
internal/httpkit/        SHARED JSON responses, error envelope, strict body decoding, request logging, security headers
internal/logging/        SHARED the structured logger
internal/team/           TEAM-ONLY everything specific to Team (below)
internal/<everything else>/       the individual product's code (domain, store, service, api, runner, agent, gitrepo, …)
web/                     the individual product's web app (Svelte PWA)
```

### Shared packages

A package is **shared** when both products use it and it holds no behaviour of either: it is plumbing. Today:

| Package | What it is | Used by |
| --- | --- | --- |
| `internal/sqlitekit` | Opens a SQLite file (one writer, a pool of readers, WAL), runs versioned migrations, copies the database before an upgrade, inspects it read-only. Knows no schema. | `internal/store/sqlite` (individual), `internal/team/store` (Team) |
| `internal/httpkit` | `WriteJSON`/`WriteError` and the error envelope, strict `DecodeJSON`, and the `LogRequests`, `RecoverPanics` and `SecurityHeaders` middleware. Holds no route. | `internal/api` (individual), `internal/team/api` (Team) |
| `internal/logging` | `slog` logger construction | both |

Each was extracted from the individual product (which now uses it too) rather than copied into Team, so there is one
SQLite open path and one set of security headers.

Everything else under `internal/` outside `internal/team` is the **individual product's**: `domain`, `store`,
`service`, `api`, `controller`, and especially the machinery that executes things — `runner`, `remote`, `runnerwire`,
`agent`, `gitrepo`, `github`, `daemon`, `netprivate`. Team may not import any of it. If Team needs something from one
of those, the answer is to move the non-product-specific part into a shared package (as `sqlitekit` and `httpkit` were),
not to reach across.

The same goes for the web: `web/` is the individual product's app and is untouched by Team. Team has its own small
console (`internal/team/console`). When Team needs real UI reuse, extract the components both use into a shared web
package (for example `web-ui/`) that `web/` and a Team web app both depend on; do not put Team screens in `web/`.

### Dependency direction

```
cmd/devboard ──▶ individual packages ──▶ shared ◀── internal/team ◀── cmd/werkbord-team
```

- The individual product **never** depends on Team: no package outside `internal/team` and `cmd/werkbord-team` may
  import them, and nothing in `go list -deps ./cmd/devboard` is Team's.
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
| `internal/team/config`, `internal/team/server` | Settings, and wiring and lifecycle. |

Billing, licensing, SSO, audit logs, and anything else only the paid product has belong here too. The `workspace`,
`project`, `ticket`, `invite` and `activity` concepts all live in `internal/team/domain`; the ticket workflow is
`internal/team/service/tickets.go`, repository awareness is `internal/team/service/repostate.go`. Do **not**
add a `teamMode` flag, a Team-only branch, or a Team import to an individual package, and do not add Team screens or
API routes to `web/` or `internal/api`. If an individual package needs to change so Team can reuse it, the change makes
it more general and stays free of Team's concepts.

## What Team has today

A **workspace** with exactly one **owner**, **members**, **projects**, **project membership**, and **roles** (Owner and
Member); and, on top of that, the collaborative workflow: **project roles** (owner, reviewer, member), **invite links**,
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
it does not run Git, call GitHub, or reach a runner. The hand-off to a runner is the member's own action, on their own
computer (`werkbord-team handoff`, which only addresses a loopback Werkbord), and the individual product needed no change
for it.

Roles are permission tables, not checks for a name: services ask `Role.Can(permission)`, so adding a role is one entry
in `internal/team/domain/roles.go` and needs no schema change and no handler change.

Not built yet, by design: licensing and payment, remote execution of any kind (never), a Team update command, a
Windows installer, HTTPS (put Team behind a TLS proxy), ownership transfer, comments and chat (Team coordinates; it is
not a messenger), a Team-side automatic reporter in the individual Werkbord. Members can use the developer-owned
`werkbord-team handoff --report` / `--watch` client; the individual product stores only generic task
provenance and has no Team configuration, credential, API or background bridge.

## Running and building

Individual Werkbord (needs Go and Node, for the web app):

```bash
make build                 # bin/devboard, with the web app embedded
./bin/devboard serve       # or `devboard setup`; see README.md and docs/INSTALL.md
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
make verify-isolation      # copies the repo with every Team file removed, then builds, vets, tests and runs the individual product
make test-install test-install-team   # each installer against a local release server
```

Releases: `make dist PRODUCT=werkbord` or `PRODUCT=werkbord-team` writes the archives and `checksums.txt` to `dist/`.
CI does this when a product tag is pushed. See [VERSIONING.md](VERSIONING.md).

## Enforcement

`internal/archtest` fails `go test ./...` (and so `make check` and CI) when:

1. anything in the individual product's build or tests reaches Team;
2. any package outside Team imports Team's packages (a shared package must not become the way in);
3. Team's build includes any package of this module other than its own and the allow-listed shared plumbing;
4. Team's code imports `os/exec`, `plugin` or `net/rpc`, or its build includes `tailscale.com`, SSH or a PTY package;
   (rule 7 also fails when a Team server package makes an outbound connection, imports `os` outside its configuration,
   or when the individual product's own code names Team's API, settings or executable);
5. Team's build uses a third-party module the individual product does not, and that module has not been declared in
   `teamOnlyModules` (declaring it makes the test check the individual product never picks it up);
6. a product's `VERSION` file is not `MAJOR.MINOR.PATCH`.

`make verify-isolation` is the empirical version of 1–2, and CI runs it. The Go module is shared, so `go.mod` lists
every dependency of both; what counts is what each executable is built from, which is what these checks inspect. If
Team grows dependencies of its own that make that unwieldy, give it its own module (`go.work`); the layout above
already allows it.

## Adding to either product

- **A change to the individual product** that Team does not need: edit it as usual. Bump `werkbord`'s version.
- **A change to Team**: stay inside `internal/team` and `cmd/werkbord-team`. Bump `werkbord-team`'s version.
- **Something both need**: make it a shared package with no product behaviour; add it to `teamAllowed` in
  `internal/archtest/boundary_test.go`; migrate the individual product onto it if it already had a copy. Bump each
  product whose behaviour the change affects.

Every implementation commit bumps and tags the product(s) it changes: [VERSIONING.md](VERSIONING.md) and
[AGENTS.md](../AGENTS.md).
