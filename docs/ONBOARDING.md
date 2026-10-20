# Onboarding

First-run help in two layers, both built on real state with no sample data.

## Guided setup (once per computer)

`web/src/routes/Onboarding.svelte` opens by itself on a computer with no projects, and walks one real path:
runner → agent → GitHub (optional) → repository → first task → agent work → publish → phone → completion review.

The welcome dialog explains the path and why it matters. Future steps stay locked until earlier requirements are verified or an optional choice is explicitly recorded. The runner, usable coding agent and Git repository are required; GitHub has a confirmed local-only path, and the example and phone can be explicitly left for later. GitHub guidance includes account creation and explains remote code storage, pull requests, checks and publishing.

Ordinary navigation returns to setup until completion. Settings, Help and the exact example task remain reachable for recovery and answering agent questions. Pausing requires a dialog, does not mark setup complete, and leaves a persistent Resume setup reminder. A pause lasts only for the current page session; reload resumes setup. The desktop shell receives only validated required/paused booleans from its ordinary Individual frame and applies the same navigation gate. Enrolled phones keep their own onboarding.

Completion rechecks required capabilities, records the optional choices and shows a success screen before opening the workspace. Existing installations without a setup record keep their upgrade exemption. A stored in-progress or explicitly reset setup never becomes complete just because a repository was added.

- Required steps complete from verified state, not from skip buttons. Optional decisions are saved and setup can be paused and resumed
  (`domain.Onboarding.Steps`, `Variant`, `Example`; `/api/onboarding/*`).
- With GitHub, the app creates a public example repository (`werkbord-hello`), turns on Pages (deployed by a workflow), clones it and
  registers it (`internal/service/githubcreate.go`). The ticket asks the assistant for a one-page site; the person looks at the
  work, merges it, pushes, and the app waits until the Pages address answers. A template (`example.go`) is the fallback.
- Without GitHub, the same path runs in a local repository and ends after the merge.
- The last step pairs a phone through the relay (docs/RELAY.md): a QR code, no app on the phone.
- GitHub needs the `repo` and `workflow` scopes; the status names what is missing and `POST /api/github/scopes` asks for it.

## Tours (once per surface)

A few tips the first time a surface is opened, then the app is meant to be intuitive. Replay them from Help.

| Surface | Where | Remembered |
| --- | --- | --- |
| Desktop, Settings, Analytics | `web/src/lib/setup/tours.ts`, `Tour.svelte` | on the computer (`Onboarding.Tours`) |
| Phone Control Deck | the same, shown by `Deck.svelte` | on the phone (localStorage), so each phone gets its own |
| Team console (desktop) | `internal/team/console/static/tour.js` | in the browser, per workspace; press `?` to replay |
| Team on the phone | `internal/team/phone/static` | on the phone |

Team's tours are plain JavaScript inside Team's own pages because Individual may not reach into Team (docs/STRUCTURE.md).

## Verification

`make test-onboarding-browser` runs a disposable controller with a scripted agent and the built desktop shell. It covers ordered steps, pause/reload, local-only choices, repository persistence, readiness and completion failures, returning users, both themes and narrow browser layouts. Readiness and trusted-frame validation also have unit tests; `make check` runs the full repository checks.
