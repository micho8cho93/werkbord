# Onboarding

First-run help in two layers, both built on real state with no sample data.

## Guided setup (once per computer)

`web/src/routes/Onboarding.svelte` opens by itself on a computer with no projects, and walks one real path:
runner → agent → GitHub (optional) → repository → ticket → assistant → publish → phone.

- Each step completes from what the app verifies, not from clicks. A skipped step stays skipped and setup can be left and resumed
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
