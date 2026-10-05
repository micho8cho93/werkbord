# Devboard

A local-first remote control for coding agents. The controller runs on your computer and
owns the board, calendar and database. Your machines execute agents with their own repositories and credentials. Your phone, tablet or
browser connects to it. There is no hosted backend and no Dev Board account.

Werkbord is two products in one repository: this one, **individual Werkbord** (the `devboard` program), and
**Werkbord Team**, a separately installed and versioned workspace that coordinates a team's members and projects
without ever running anything on anyone's computer. See [docs/PRODUCTS.md](docs/PRODUCTS.md) and
[docs/TEAM.md](docs/TEAM.md). The rest of this README is about the individual product.

## Install

macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install.sh | sh
```

That is the whole install. It downloads the release for your computer, checks it against its published
checksum, and runs `devboard setup`: it creates the data directory and database, installs a background service
that starts when you log in, registers this computer as your first runner, joins your own private network so a
phone can reach it (signing you in through the browser if needed), and opens Dev Board in your browser. There
you connect GitHub (optional), see which coding agents it found, and pick repositories. It needs no account of
Dev Board's own, no Docker, no root and no hosted database. On Windows (experimental) use `install.ps1`; see
[docs/INSTALL.md](docs/INSTALL.md) for every option, upgrades, and what is installed where.

Afterwards: `devboard status`, `open`, `doctor`, `start`, `stop`, `restart`, `update`.
Stop/restart/update refuse to interrupt active local agents or unresolved runner journals without
`--force`. `devboard token --rotate` atomically replaces a file-managed API token and takes effect
without restarting; reconnect browsers with the new token. Environment/configuration-managed tokens
must be rotated at their source.

Updates snapshot SQLite as well as the executable. A failed restart restores both and retains the
failed database for inspection. To restore manually with the controller stopped, use
`devboard db restore /path/to/backup.db` or `devboard db restore --latest`; use `--force` before the
backup argument only when deliberately interrupting active work. Database restoration requires remote
runner ownership recovery. Agents and Git helpers do not inherit controller configuration secrets;
they still run as your user and can read files your user can read.

**Status.** You can register local Git repositories, keep a four-column board
(Backlog, Doing, Review, Done) per project, and run **Claude Code** or **Codex** on a task. Each
run is an interactive session in its own Git worktree: watch what the agent does, answer its
questions and approve its actions from your phone, send follow-up messages, and finish or stop
it. The selected runner owns the processes, so a closed browser does not interrupt anything.

**Projects are the scope.** Each repository is a project with its own board, Calendar, Git view, activity and
runs, and inside one you see that project and nothing else. Switch with the project switcher (the
project name at the top on a phone; Ctrl/⌘ K anywhere). The **Control Center** is the one view across
all projects: what needs you, wherever it is.

**Orchestration.** Schedule the same Board tasks in a day/week Calendar, queue work by order and priority,
and add dependencies. The controller runs schedules from SQLite while your browser is closed, with explicit
missed-time policies and durable claims that prevent duplicate dispatch after restart. Conservative repository
checks and a project concurrency limit gate launches. Finished runs carry editable handoffs; *Continue with…*
starts a fresh run with another agent/model for implementation, review or fixes. See [docs/ORCHESTRATION.md](docs/ORCHESTRATION.md).

**Multiple runners.** Pair your own machines from Settings → Runners with `devboard join <code>`.
Assign a task to a specific machine or use deterministic automatic routing based on repository access,
agent availability, resources and capacity. Disconnected machines retain ownership until their work is
reconciled. Control Center shows workloads and measured usage; unknown tokens and costs stay unknown.
See [docs/RUNNERS.md](docs/RUNNERS.md) for pairing, repositories, recovery, routing rules and economics.

**Git Control Center.** Each project's Git section shows what the agents' branches look like and lets you act
on them from your phone: a repository summary (this computer and the remote, kept apart), the branches that
need you (Dev Board's first, with their task and run), pull requests from GitHub, recent commits, working
changes, and a drill-down from a branch to its changed files to a file's diff. You can fetch, push, merge,
delete a branch Dev Board made, clean a Dev Board worktree and open a pull request, each after a check that
refuses rather than guesses. Nothing is ever forced, and **an agent finishing a run never merges**. See
[docs/GIT.md](docs/GIT.md) for the safety model.

**Repository health.** The Git screen leads with *Healthy*, or with what is wrong (`1 risk · 3 items need attention`):
findings that say what, why, the evidence, the next step, and whether Dev Board can do it. A finding can open an editable Backlog task, or an editable investigation task where you choose the agent and interaction policy before starting it. They are worked out from
Git metadata and Dev Board's own records, never by a model, are recalculated when something changes (not on a timer),
and never act on their own. The Control Center is for exceptions: needs input, blocked, failed, ready for review, and
repository risk. See [docs/HEALTH.md](docs/HEALTH.md).

**Setup and defaults.** First-run setup is a single page, not a wizard: this computer (already your runner),
phone access, GitHub, your coding agents, your repositories. Each task has an agent, a model, a reasoning level,
an interaction policy and a priority, set globally, per project, and per task; the task wins. Left alone, the
agent picks its own model and reasoning. See [docs/EXECUTION.md](docs/EXECUTION.md).

**Phone access.** Dev Board embeds a Tailscale node, so your phone reaches the controller without you setting up a
VPN, a tunnel or certificates, and without any server of Dev Board's own. Nothing is exposed to the Internet. See
[docs/PHONE.md](docs/PHONE.md).

**Versions.** Each milestone is a minor version, tagged `vMAJOR.MINOR.PATCH` (this one is v0.7.0). See
[docs/VERSIONING.md](docs/VERSIONING.md).

**Interaction policy.** Each task says how its agent may deal with you: *Ask me when needed* (the
default), *Work autonomously*, or *Work autonomously — stop if blocked*. See below.
See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Requirements

To run it, only what the installer checks for: `git` on `PATH`, and, to run agents, the `claude` and/or `codex`
CLI, installed and signed in (Dev Board shows what it found, says how to install what is missing, and never
installs an agent without you or asks for an API key). The [GitHub CLI](https://cli.github.com) (`gh`) is
optional: it is how Dev Board uses your own GitHub identity, and everything local works without it.

To build from source:

- Go 1.27.1+ (no C toolchain needed: SQLite is pure Go; Tailscale is embedded as a Go library)
- Node 20+ and npm, to build the web app

## Build and run

```bash
make build
```

```bash
./bin/devboard serve
```

The API needs an access token even on this computer, because it can start
processes as you. The controller creates one on first start. Print a link that signs your
browser in and open it:

```bash
./bin/devboard token --url
```

In another terminal, register a repository (the CLI finds the token by itself):

```bash
./bin/devboard project add ~/code/my-app
```

Then open the board, tap a task and press *Start agent*. A project's page is
`#/p/<project id>/board`; `#/control` is the Control Center.

Other commands: `devboard project list`, `devboard token`, `devboard migrate`,
`devboard version`. Run
`devboard <command> -h` for flags.

Werkbord Team is built and run separately (it needs Go only): `make build-team`, then `./bin/werkbord-team serve`.
See [docs/TEAM.md](docs/TEAM.md).

## Development

Run the controller and the Vite dev server (with hot reload, proxying `/api`) in two
terminals:

```bash
make dev-api
```

```bash
make dev-web
```

Then open http://127.0.0.1:5173. The controller still requires its token, so the app asks
for it once: paste the output of `devboard token`.

Checks (Go tests, `go vet`, `gofmt`, `svelte-check`, ESLint, both builds; this covers both products and the
architecture tests that keep them separate):

```bash
make check
```

Every implementation commit bumps the changed product's `VERSION` file and gets a product tag
(`werkbord-vX.Y.Z` or `werkbord-team-vX.Y.Z`): see [docs/VERSIONING.md](docs/VERSIONING.md).

## Using it from your phone

`devboard setup` (run by the installer) turns on phone access: Dev Board joins your own Tailscale network from
inside the controller and serves the app there, over HTTPS when your tailnet has certificates turned on. Sign in
to Tailscale once when it asks (a free account is enough), install the Tailscale app on your phone with the same
account, and scan the QR code from `devboard open --qr`, or from Settings, to open Dev Board already signed in.
Then "Add to Home Screen". Details, the security model and troubleshooting: [docs/PHONE.md](docs/PHONE.md).

The controller on this computer still listens on `127.0.0.1` only; the private network is a second door that
only your devices can reach, and it demands the access token like any other. Nothing is exposed to the Internet.

Browser notifications are optional and can be enabled in the Control Center. They are generated
by the connected browser for questions, blocked or failed runs, completed work and important
repository risks. The controller does not run a push relay: if the browser or installed app is
closed, backgrounded by the operating system, or disconnected long enough to suspend its event
stream, agents keep running on the controller but notifications may wait until the app reconnects.

## Configuration

Settings come from defaults, then `<data dir>/config.json`, then environment variables,
then flags.

| Setting | Env | Flag | Default |
| --- | --- | --- | --- |
| `addr` | `DEVBOARD_ADDR` | `--addr` | `127.0.0.1:7420` |
| `dataDir` | `DEVBOARD_DATA_DIR` | `--data-dir` | OS config dir + `/devboard` |
| `logLevel` | `DEVBOARD_LOG_LEVEL` | `--log-level` | `info` |
| `logFormat` | `DEVBOARD_LOG_FORMAT` | `--log-format` | `text` (or `json`) |
| `token` | `DEVBOARD_TOKEN` | — | generated when needed |
| `requireToken` | `DEVBOARD_REQUIRE_TOKEN` | `--require-token` | `true`. `false` (or `--require-token=false`) allows tokenless access on `127.0.0.1` only, and logs a warning; off loopback the token is always required. Anything but `true`/`false` in the environment is an error |
| `allowedHosts` | — | — | `[]` |
| `network.enabled` | `DEVBOARD_NETWORK` | — | unset: the app decides (`devboard setup` turns it on). `true` always joins the private network, `false` never does |
| `network.hostname` | `DEVBOARD_NETWORK_HOSTNAME` | — | `devboard-<this computer's name>` |
| `network.controlUrl` | `DEVBOARD_NETWORK_CONTROL_URL` | — | Tailscale's. A self-hosted Headscale URL, if you run one |
| `shutdownTimeout` | — | — | `10s` |
| `worktreesDir` | `DEVBOARD_WORKTREES_DIR` | — | `<data dir>/worktrees` |
| `agents` | — | — | per-agent settings, below |
| `github` | — | — | `{"command": "gh", "disabled": false}`: the GitHub CLI to use for pull requests, or turn the integration off |

### Agents

Both agents are always listed; one that is not installed or not signed in says why and cannot be
started. Tune them in `config.json`:

```json
{
  "agents": {
    "claude-code": { "command": "claude", "permissionMode": "acceptEdits" },
    "codex":       { "command": "codex",  "model": "gpt-5.5", "approvalPolicy": "on-request", "sandbox": "workspace-write" }
  }
}
```

Everything is optional. `model` here is what "Agent default" means for that agent on this computer. `models` (a
list of `{"id", "name", "description"}`) and `reasoning` (a list of level names) replace the choices the app offers
when you do not want what the agent reports; a model that is not listed can still be typed in. Whatever an agent
asks permission for reaches you as a question, so the
defaults let it edit files in its worktree and ask about the rest. `permissionMode`
(`acceptEdits`, `manual`, `plan`, `auto`, `dontAsk`, `bypassPermissions`), `approvalPolicy`
(`untrusted`, `on-request`, `never`) and `sandbox` (`read-only`, `workspace-write`,
`danger-full-access`) are checked at start-up; the settings that stop an agent asking are logged as
warnings. Devboard's Codex defaults override your own `~/.codex/config.toml` for approvals and the
sandbox, and its model setting is left to Codex unless you set `model`.

### Interaction: asking, autonomous, stop if blocked

Choose it when you create or edit a task (and, for one run, when you start it):

| Choice | The agent |
| --- | --- |
| **Ask me when needed** (default) | May stop and ask you. The run waits for your answer, then the same session carries on. |
| **Work autonomously** | Investigates the repository, makes reasonable decisions and carries on until it thinks the task is done. If it asks an ordinary question anyway, Devboard tells it to decide for itself and the run keeps going. |
| **Work autonomously — stop if blocked** | As above, but a decision it cannot safely make stops the run: it becomes **Blocked**, with the reason (and any options the agent saw), instead of a guess. Reply to unblock it, or finish or stop it. |

**Autonomous never means unrestricted.** This setting is about conversation only. What an agent may do
is still decided by its own permission settings above, and a request for permission (run this command,
apply these changes) always comes to you, under every choice; Devboard never grants one on your behalf.
A run keeps the policy it started with, so editing a task changes its next run, not one that is
working. On the board a task in *Doing* shows one of **Running**, **Needs input**, **Blocked** or
**Failed**; these describe the run, and there is no extra column.

Agents run as you, in a Git worktree under `worktreesDir` on a branch named
`devboard/<task>-<id>`, so your own checkout is never touched. The work stays there when the run
ends; Devboard never deletes it by itself (you clean a worktree, and delete a merged branch, from the Git section, and only
when nothing would be lost).

## Layout

```
cmd/devboard          CLI: setup, service commands, doctor, update; and the controller (`serve`). Individual Werkbord.
cmd/werkbord-team     Werkbord Team: its own program, version and installer (docs/PRODUCTS.md)
internal/team         everything specific to Team: domain, store, service, api, console (nothing else may import it)
internal/sqlitekit    shared: open, migrate and back up a SQLite database (both products)
internal/httpkit      shared: JSON responses, strict decoding, request logging, security headers (both products)
internal/archtest     tests that enforce the boundary between the two products
internal/domain       entities, states, rules (no dependencies)
internal/store        persistence interfaces; sqlite/ implementation and migrations
internal/service      use cases
internal/api          HTTP API, SSE, auth and security middleware
internal/events       live event fan-out
internal/gitrepo      Git boundary (inspection, worktrees, and the Git Control Center's reads and guarded writes)
internal/github       GitHub boundary: the user's own `gh` CLI, for pull requests
internal/agent        agent adapter boundary, process handling, execution-policy rules; claude/ and codex/ adapters
internal/runner       owns agent processes: start, stream, input, stop, recovery
internal/controller   wiring and lifecycle
internal/netprivate   the embedded Tailscale node: private address, sign-in state, QR codes
internal/daemon       the background service: launchd, systemd, Task Scheduler, or a detached process
internal/doctor       `devboard doctor`'s checks (never prints a secret)
internal/update       finds, checksums and installs a newer release
scripts/              install.sh, install.ps1, release build, installer tests
internal/webui        embedded PWA
web/                  Svelte 5 + TypeScript PWA (a global store, plus one scope per project)
docs/                 architecture; the Git safety model (GIT.md); install, phone access, execution defaults
```
