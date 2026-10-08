# Werkbord

A local-first remote control for coding agents. The controller runs on your computer and
owns the board, calendar and database. Your machines execute agents with their own repositories and credentials. Your phone, tablet or
browser connects to it. There is no hosted backend and no Werkbord account.

Werkbord is two products in one repository: this one, **individual Werkbord** (the `werkbord` program), and
**Werkbord Team**, a separately installed and versioned workspace that coordinates a team's members and projects
without ever running anything on anyone's computer. See [docs/PRODUCTS.md](docs/PRODUCTS.md) and
[docs/TEAM.md](docs/TEAM.md). The rest of this README is about the individual product.

Team uses customer-owned Nebula networking by default and needs no Tailscale account or vendor runtime service. Customers run their Workspace Hosts, Connectivity Hosts, backups and local runners. Team requires a verified offline license and authenticated release artifacts; see [Team installation](docs/TEAM_INSTALL.md), [threat model](docs/TEAM_SECURITY.md), and the [production gate report](docs/TEAM_SECURITY_GATE.md). Individual Werkbord's tsnet support remains separate.

## Install

### Most people: the Mac app

**Testing before Apple Developer approval:** download the [unsigned Mac preview](https://github.com/micho8cho93/werkbord/releases/download/werkbord-v1.3.1-preview.1/Werkbord-preview.dmg),
open the DMG, drag **Werkbord** to **Applications**, then open it. If macOS blocks it, go to **System Settings → Privacy & Security → Open Anyway** after the first launch attempt.
This universal preview is ad-hoc signed, not notarized, and has no Sparkle app updater. It is for testing;
replace it with the signed app when available. See [Apple’s instructions](https://support.apple.com/en-us/102445).

The signed and notarized Mac release below is pending the owner’s Apple Developer approval ([release setup](docs/DESKTOP_RELEASE.md)).

1. Download **[`Werkbord.dmg`](https://github.com/micho8cho93/werkbord/releases/latest/download/Werkbord.dmg)** (the same
   file as `Werkbord_<version>_darwin_universal.dmg` on the [latest release](https://github.com/micho8cho93/werkbord/releases/latest)).
   It is one file for every Mac, Apple Silicon and Intel, for macOS 13 or later. From a terminal:
   `curl -fL -o Werkbord.dmg https://github.com/micho8cho93/werkbord/releases/latest/download/Werkbord.dmg`.
2. Open it, drag **Werkbord** onto **Applications**, and open it from there.

That is all: no Terminal. The app then keeps itself up to date when you ask it to (**Werkbord → Check for Updates…**, or
**Update now** in the app), after verifying a signature you can read about in [docs/DESKTOP.md](docs/DESKTOP.md#updates).
The disk image is signed with a Developer ID and notarized by Apple, so the first launch shows
only the usual question macOS asks about any app from the Internet ("Werkbord is an app downloaded from the Internet. Are
you sure you want to open it?"): choose **Open**. The first time, the app sets Werkbord up on your computer (the program,
your data and database, and a background service that keeps it running when you close the window), then shows it. Closing
the window or quitting the app never stops your agents or your schedules, and the same Werkbord stays reachable from a
browser on this computer and from your phone. See [docs/DESKTOP.md](docs/DESKTOP.md).

### Advanced users, servers and Linux: the installer

macOS or Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/micho8cho93/werkbord/main/scripts/install.sh | sh
```

**Both ways install the same Werkbord**: the same `werkbord` program in `~/.local/bin`, the same data directory,
database and token, the same login service and the same controller. Install one and then use the other and you have
one installation, not two; `werkbord update` updates what the app uses, and the app's **Update now** runs it.

The installer downloads the release for your computer, checks it against its published
checksum, and runs `werkbord setup`: it creates the data directory and database, installs a background service
that starts when you log in, registers this computer as your first runner, joins your own private network so a
phone can reach it (signing you in through the browser if needed), and opens Werkbord in your browser. There
you connect GitHub (optional), see which coding agents it found, and pick repositories. It needs no account of
Werkbord's own, no Docker, no root and no hosted database. On Windows (experimental) use `install.ps1`; see
[docs/INSTALL.md](docs/INSTALL.md) for every option, upgrades, and what is installed where.

Afterwards: `werkbord status`, `open`, `doctor`, `start`, `stop`, `restart`, `update`.
Stop/restart/update refuse to interrupt active local agents or unresolved runner journals without
`--force`. `werkbord token --rotate` atomically replaces a file-managed API token and takes effect
without restarting; reconnect browsers with the new token. Environment/configuration-managed tokens
must be rotated at their source.

Updates snapshot SQLite as well as the executable. A failed restart restores both and retains the
failed database for inspection. To restore manually with the controller stopped, use
`werkbord db restore /path/to/backup.db` or `werkbord db restore --latest`; use `--force` before the
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

**Multiple runners.** Pair your own machines from Settings → Runners with `werkbord join <code>`.
Assign a task to a specific machine or use deterministic automatic routing based on repository access,
agent availability, resources and capacity. Disconnected machines retain ownership until their work is
reconciled. Control Center shows workloads and measured usage; unknown tokens and costs stay unknown.
See [docs/RUNNERS.md](docs/RUNNERS.md) for pairing, repositories, recovery, routing rules and economics.

**Git Control Center.** Each project's Git section shows what the agents' branches look like and lets you act
on them from your phone: a repository summary (this computer and the remote, kept apart), the branches that
need you (Werkbord's first, with their task and run), pull requests from GitHub, recent commits, working
changes, and a drill-down from a branch to its changed files to a file's diff. You can fetch, push, merge,
delete a branch Werkbord made, clean a Werkbord worktree and open a pull request, each after a check that
refuses rather than guesses. Nothing is ever forced, and **an agent finishing a run never merges**. See
[docs/GIT.md](docs/GIT.md) for the safety model.

**Repository health.** The Git screen leads with *Healthy*, or with what is wrong (`1 risk · 3 items need attention`):
findings that say what, why, the evidence, the next step, and whether Werkbord can do it. A finding can open an editable Backlog task, or an editable investigation task where you choose the agent and interaction policy before starting it. They are worked out from
Git metadata and Werkbord's own records, never by a model, are recalculated when something changes (not on a timer),
and never act on their own. The Control Center is for exceptions: needs input, blocked, failed, ready for review, and
repository risk. See [docs/HEALTH.md](docs/HEALTH.md).

**Setup and defaults.** First-run setup is a single page, not a wizard: this computer (already your runner),
phone access, GitHub, your coding agents, your repositories. Each task has an agent, a model, a reasoning level,
an interaction policy and a priority, set globally, per project, and per task; the task wins. Left alone, the
agent picks its own model and reasoning. See [docs/EXECUTION.md](docs/EXECUTION.md).

**Work without losing its history.** Messages send with Enter; Shift+Enter adds a line. An active task's header
keeps **Stop agent** visible. Stop its run before using **Close task**, which moves the task into the board's
searchable **Archive**. **Clear Done** archives the whole Done column; descriptions, branches, handoffs and runs
remain available, and **Restore** returns a task to its previous column with automatic scheduling disabled.
Project settings and Git use section controls so their details do not compete for the same viewport.

**Adding repositories.** Projects offers **Choose folder** (a native picker in the Mac app, a folder browser in the
web app) and **GitHub repository**. Connect your GitHub account to select accessible repositories; Werkbord uses
an existing local checkout or clones one using your own sign-in.

**Other programs.** A program on your computer that only needs to hand Werkbord a task or see what is running is given a narrow,
revocable *local access token* instead of yours (`werkbord access list`, `revoke`): [docs/LOCAL_ACCESS.md](docs/LOCAL_ACCESS.md).

**Phone access.** Werkbord embeds a Tailscale node, so your phone reaches the controller without you setting up a
VPN, a tunnel or certificates, and without any server of Werkbord's own. Nothing is exposed to the Internet. See
[docs/PHONE.md](docs/PHONE.md).

**Versions.** Each milestone is a minor version, tagged `vMAJOR.MINOR.PATCH` (this one is v0.7.0). See
[docs/VERSIONING.md](docs/VERSIONING.md).

**Interaction policy.** Each task says how its agent may deal with you: *Ask me when needed* (the
default), *Work autonomously*, or *Work autonomously — stop if blocked*. See below.
See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

The [V0–V2 stabilization audit](docs/audit/V2_STABILIZATION.md) records integration validation, recovery guarantees,
release notes and the remaining limits before V3.

## Requirements

To run it, only what the installer checks for: `git` on `PATH`, and, to run agents, the `claude` and/or `codex`
CLI, installed and signed in (Werkbord shows what it found, says how to install what is missing, and never
installs an agent without you or asks for an API key). The [GitHub CLI](https://cli.github.com) (`gh`) is
optional: it is how Werkbord uses your own GitHub identity, and everything local works without it.

To build from source:

- Go 1.27.1+ (no C toolchain needed: SQLite is pure Go; Tailscale is embedded as a Go library)
- Node 20+ and npm, to build the web app

## Build and run

```bash
make build
```

```bash
./bin/werkbord serve
```

The API needs an access token even on this computer, because it can start
processes as you. The controller creates one on first start. Print a link that signs your
browser in and open it:

```bash
./bin/werkbord token --url
```

In another terminal, register a repository (the CLI finds the token by itself):

```bash
./bin/werkbord project add ~/code/my-app
```

Then open the board, tap a task and press *Start agent*. A project's page is
`#/p/<project id>/board`; `#/control` is the Control Center.

Other commands: `werkbord project list`, `werkbord token`, `werkbord migrate`,
`werkbord version`. Run
`werkbord <command> -h` for flags.

Werkbord Team is built and run separately: `make build-team`, with a build-time license verification public key and a valid offline license. macOS hosts also need the C toolchain for Keychain and the pinned rqlite source build.
See [docs/TEAM.md](docs/TEAM.md).

The Mac app is built separately too (it needs the Xcode command line tools, for the system's web view):
`make desktop` makes `dist/desktop/Werkbord.app`, `make desktop-package` also a `.dmg`, and `make desktop-dev` runs
it from source against its own data and port. See [docs/DESKTOP.md](docs/DESKTOP.md#building-it).

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
for it once: paste the output of `werkbord token`.

Checks (Go tests, `go vet`, `gofmt`, `svelte-check`, ESLint, both builds; this covers both products and the
architecture tests that keep them separate):

```bash
make check
```

Every implementation commit bumps the changed product's `VERSION` file and gets a product tag
(`werkbord-vX.Y.Z` or `werkbord-team-vX.Y.Z`): see [docs/VERSIONING.md](docs/VERSIONING.md).

## Using it from your phone

`werkbord setup` (run by the installer) turns on phone access: Werkbord joins your own Tailscale network from
inside the controller and serves the app there, over HTTPS when your tailnet has certificates turned on. Sign in
to Tailscale once when it asks (a free account is enough), install the Tailscale app on your phone with the same
account, and scan the QR code from `werkbord open --qr`, or from Settings, to open Werkbord already signed in.
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
| `addr` | `WERKBORD_ADDR` | `--addr` | `127.0.0.1:7420` |
| `dataDir` | `WERKBORD_DATA_DIR` | `--data-dir` | OS config dir + `/werkbord` (`/devboard` on an install from before the rename) |
| `logLevel` | `WERKBORD_LOG_LEVEL` | `--log-level` | `info` |
| `logFormat` | `WERKBORD_LOG_FORMAT` | `--log-format` | `text` (or `json`) |
| `token` | `WERKBORD_TOKEN` | — | generated when needed |
| `requireToken` | `WERKBORD_REQUIRE_TOKEN` | `--require-token` | `true`. `false` (or `--require-token=false`) allows tokenless access on `127.0.0.1` only, and logs a warning; off loopback the token is always required. Anything but `true`/`false` in the environment is an error |
| `allowedHosts` | — | — | `[]` |
| `network.enabled` | `WERKBORD_NETWORK` | — | unset: the app decides (`werkbord setup` turns it on). `true` always joins the private network, `false` never does |
| `network.hostname` | `WERKBORD_NETWORK_HOSTNAME` | — | `werkbord-<this computer's name>` (`devboard-…` for a computer that joined before the rename) |
| `network.controlUrl` | `WERKBORD_NETWORK_CONTROL_URL` | — | Tailscale's. A self-hosted Headscale URL, if you run one |
| `noUpdateCheck` | `WERKBORD_NO_UPDATE_CHECK` | — | `false`. `true` stops the controller from ever asking GitHub whether a newer release exists (what `GET /api/update` and the "update available" banner use); the controller's other network use (Git remotes, GitHub through your own `gh`, the private network) is something you asked for; this one is not, which is why it can be turned off |
| `shutdownTimeout` | — | — | `10s` |
| `worktreesDir` | `WERKBORD_WORKTREES_DIR` | — | `<data dir>/worktrees` |
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
warnings. Werkbord's Codex defaults override your own `~/.codex/config.toml` for approvals and the
sandbox, and its model setting is left to Codex unless you set `model`.

### Interaction: asking, autonomous, stop if blocked

Choose it when you create or edit a task (and, for one run, when you start it):

| Choice | The agent |
| --- | --- |
| **Ask me when needed** (default) | May stop and ask you. The run waits for your answer, then the same session carries on. |
| **Work autonomously** | Investigates the repository, makes reasonable decisions and carries on until it thinks the task is done. If it asks an ordinary question anyway, Werkbord tells it to decide for itself and the run keeps going. |
| **Work autonomously — stop if blocked** | As above, but a decision it cannot safely make stops the run: it becomes **Blocked**, with the reason (and any options the agent saw), instead of a guess. Reply to unblock it, or finish or stop it. |

**Autonomous never means unrestricted.** This setting is about conversation only. What an agent may do
is still decided by its own permission settings above, and a request for permission (run this command,
apply these changes) always comes to you, under every choice; Werkbord never grants one on your behalf.
A run keeps the policy it started with, so editing a task changes its next run, not one that is
working. On the board a task in *Doing* shows one of **Running**, **Needs input**, **Blocked** or
**Failed**; these describe the run, and there is no extra column.

Agents run as you, in a Git worktree under `worktreesDir` on a branch named
`devboard/<task>-<id>`, so your own checkout is never touched. The work stays there when the run
ends; Werkbord never deletes it by itself (you clean a worktree, and delete a merged branch, from the Git section, and only
when nothing would be lost).

## Layout

```
cmd/werkbord          CLI: setup, service commands, doctor, update; and the controller (`serve`). Individual Werkbord.
desktop/              the Mac app: a native window (Wails) around the web app; its own Go module (docs/DESKTOP.md)
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
internal/doctor       `werkbord doctor`'s checks (never prints a secret)
internal/update       finds, checksums and installs a newer release; says whether one exists (GET /api/update)
internal/launcher     what the desktop app does first: find or install the program, start the controller, sign the window in
scripts/              install.sh, install.ps1, release build, the .app/.dmg build, installer tests
internal/webui        embedded PWA
web/                  Svelte 5 + TypeScript PWA (a global store, plus one scope per project)
docs/                 architecture; the Git safety model (GIT.md); install, phone access, execution defaults
```
