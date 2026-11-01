# Werkbord

Werkbord is a local-first remote control for coding agents. Your controller stores tasks, plans, schedules and history;
your machines run agents with their own repositories and credentials. A native Mac app, browser or phone connects to it.
There is no customer Werkbord account or hosted workspace backend.

Werkbord is **one product, one app and one release**. Personal projects live on your computer; Team projects coordinate
shared work through a separate, customer-operated Team service. Team never runs developer processes or holds your Git or
agent credentials. See [structure and trust boundaries](docs/STRUCTURE.md).

| | Free | Premium |
| --- | --- | --- |
| Personal projects, agents, Git and Salie | Included | Included |
| Teams | Up to 2 people per team, owner included | Teams of any size; every member needs Premium |
| Unlock | No key needed | Per-person key, verified offline; one-time purchase, not yet on sale |

Both plans use the same download. A member without Premium in a team of 3 or more keeps read-only access.
Extra devices do not count as people. See [plans](docs/PLANS.md).

## Current application

The desktop's project dropdown selects All Projects, a Personal project or a Team project. It scopes **Work**, **Calendar**,
**Agents**, **Analytics** and **Salie**. Settings is separate; Git, reviews, memory and administration are contextual tools.
The standalone controller, Team console and phone retain their own navigation.

- **Work:** Now, Next and Later queues, with structured plans, dependencies, labels and planned dates. Completed work stays
  in a disclosure; Archive preserves history and supports restoration. Older Board and Timeline links redirect to Work
  and Calendar. See [Work](docs/WORK.md) and [planning](docs/PLANNING.md).
- **Agents:** Claude Code, Codex, Gemini CLI and OpenCode sessions, with questions, approvals, follow-ups and output.
  Each task run has its own Git worktree. Closing the window does not stop agents or schedules. See
  [agent sessions](docs/AGENT_SESSIONS.md) and [execution](docs/EXECUTION.md).
- **Salie:** a conversation using your existing provider sign-in. Board operations use explicit confirmations and audit
  records. Context selection does not grant authority. See [Salie](docs/ASSISTANT.md).
- **Calendar and runners:** schedules run from SQLite while the browser is closed. Pair your own computers as runners;
  disconnected runners retain ownership until reconciled. See [orchestration](docs/ORCHESTRATION.md) and
  [runners](docs/RUNNERS.md).
- **Git and Product Map:** inspect branches, diffs, pull requests and repository health; generate evidence-linked maps
  of implemented features. Git actions retain their own checks, and finishing a run never merges. See
  [Git](docs/GIT.md), [health](docs/HEALTH.md) and [Product Map](docs/PRODUCT_MAP.md).
- **Phone:** an encrypted relay connects your phone to your controller. Tailscale is optional for remote Personal
  runners. Team uses customer-owned Nebula networking, approved enrolment and customer-operated hosts and backups.
  See [phone access](docs/PHONE.md) and [Team](docs/TEAM.md).

## Install

Use the signed and notarised universal macOS preview from the [releases page](https://github.com/micho8cho93/werkbord/releases).
Choose the preview explicitly; previews are not marked latest. Download **Werkbord.dmg**, drag **Werkbord** to Applications
and open it. It supports Apple Silicon and Intel Macs running macOS 13 or later. The app sets up the local controller and
background service; Team setup is optional. See [desktop installation and updates](docs/DESKTOP.md).

Production admission remains **blocked**. Evaluate Team with disposable workspaces and customer-controlled backups.
Linux packages are opt-in after platform acceptance; Windows downloads remain deferred. See
[beta distribution](docs/BETA_READINESS.md) and [production acceptance](docs/PRODUCTION_ACCEPTANCE.md).

For advanced macOS/Linux controller installations, use an independently trusted installer and public key:

```bash
WERKBORD_RELEASE_PUBLIC_KEY_FILE=/trusted/werkbord-release.pub sh /trusted/install.sh
```

The installer verifies the release manifest and runs `werkbord setup`. The CLI and desktop use the same controller and
data directory. Useful commands include `werkbord status`, `open`, `doctor`, `start`, `stop`, `restart` and `update`.
Stop, restart and update refuse to interrupt active work without `--force`. See [installation](docs/INSTALL.md).

## Build and run

Requirements: Go 1.27.3+, Node 20+, npm and Git. Running agents also requires the corresponding CLI installed and signed in.
The GitHub CLI (`gh`) is optional. The Mac desktop and Team infrastructure source builds require the Xcode command line tools.

```bash
make build
./bin/werkbord serve
```

The API requires a token, including on this computer. In another terminal:

```bash
./bin/werkbord token --url
./bin/werkbord project add ~/code/my-app
```

Open the sign-in link, choose the project in Work and start a task. Project Work routes are `#/p/<project id>/work`.
`werkbord <command> -h` lists command options.

`make build-team` builds the separate Team service. `make desktop` builds `dist/desktop/Werkbord.app`;
`make desktop-package` also creates a DMG. `make desktop-dev` uses isolated development data and ports.
See [Team](docs/TEAM.md) and [desktop builds](docs/DESKTOP.md#building-it).

For hot reload, run these in separate terminals:

```bash
make dev-api
```

```bash
make dev-web
```

Open http://127.0.0.1:5173 and sign in with the development controller's token.

## Validation and documentation

```bash
make check
```

This runs Go tests, architecture boundaries, formatting, vet, frontend types/lint/tests/builds, desktop tests and release
workflow checks. Browser acceptance uses disposable fixtures and needs the Playwright browsers
(`cd web && npx playwright install chromium webkit`). Relevant targets include:

```bash
make test-project-context-browser
make test-work-browser
make test-work-order-browser
make test-assistant-browser
make test-planning-browser
make test-browser
make test-unified-desktop-browser
make test-rqlite
make test-unified-installer
```

A development DMG proves packaging, not signed release or clean-hardware acceptance. See the
[documentation index](docs/README.md) for current architecture, feature contracts and operating guides.

Ordinary work is committed without a version bump or tag. Releases use the single `cmd/werkbord/VERSION` file and
`werkbord-vMAJOR.MINOR.PATCH` tag series. See [versioning](docs/VERSIONING.md) and [agent instructions](AGENTS.md).
