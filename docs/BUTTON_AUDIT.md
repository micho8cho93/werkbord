# Button audit — 18 October 2026

Build: `4.51.0-preview.1`.

## Scope and evidence

The source pass covered 414 button elements in 165 Svelte components, plus the
Team console's button factories and desktop-shell actions. The five Svelte
buttons without a local event handler use an explicit form target or the shared
board drag/keyboard handler; none was an unwired placeholder. Source wiring alone
does not establish that an external service or physical device will respond.

Browser verification uses disposable controllers, Team services, databases,
repositories and credentials. It exercises actions and checks their API or stored
results, rather than treating a click without an error as success.

| Area | Exercised flows |
| --- | --- |
| Setup and settings | Readiness, completed setup controls, project creation, folder selection, dirty defaults, runner settings, pairing, capacity validation and persistence |
| Tasks and boards | Creation, edit, start, questions and answers, stop, finish, retry, drag and keyboard movement, review, assessment, archive, undo and restore |
| Calendar and Timeline | Navigation, planning, drag, resize, contextual editing, execution scheduling, preapproval and cancellation |
| Git | Local review evidence, worktree/commit handoff, merge confirmation, reported PR state, separate reviewer and movement to Done |
| Phone | Control Deck, navigation, workflow configuration, runner inspection, questions, responsive layouts, both themes and reduced motion |
| Team | Members, directory filtering, invitations, enrollment and approval, roles, project membership, tickets, reported execution, local execution controls, scheduling and signed device handoff |
| Desktop shell | Workspace/project navigation, Universal views, retained frames, Team → Individual task opening, runner grant, theme, narrow navigation, background work, permissions and leaving |
| Assistant | Composer, streaming, stop, confirmations, generated controls/forms, recovery, attachments, dictation fixtures and keyboard/phone behaviour |

## Changes

- Runner summaries use **Connected**, **Disconnected**, or **Connection unknown**.
  Disabling new work is stated separately from connection. Occupied/total run-slot
  fractions are removed from the rail, runner settings headings, attention
  inventory, execution chooser and phone cards. Capacity remains editable in Settings.
- Team Members shows connected/disconnected runner counts and Team phone
  availability. The member's own device reports this; Team never probes a runner.
  An authenticated phone tunnel establishes a connection; a relay connection or
  saved access grant alone does not. Stale and legacy reports remain qualified.
- Members can see aggregate connection status without receiving other members'
  device identities, public keys, phone grants or device-management permissions.
  Revoked devices are excluded, and presence refreshes while the directory is open.
- The project header has one switcher opener: Jump to on a computer, the project
  name on a phone. Keyboard access remains available.
- Task details keeps one Edit entry instead of repeating it under Planning and
  How it runs. The edit sheet disables Save until something changes.
- Phone Runners has one inventory and links computer access to the existing
  management page instead of repeating every runner below its card.
- Unchanged runner settings, project capacity, Team computer settings, Team ticket
  text/planning, Git reports and project roles cannot be saved repeatedly. Restored
  Team drafts recompute the enabled state after live updates.
- Selected run assessments cannot be submitted again; the opposite assessment
  remains available so a person can change their decision.
- A displayed, live runner pairing code or Team invitation cannot be generated
  again accidentally. Terminal enrollment invitations do not offer Revoke.
- Team execution options invalidate and update the policy preview immediately.
  Existing previews and saved approvals disable their repeated action; active
  execution disables another immediate policy/start flow.
- Queued or delivered device requests disable repeated handoff/status/start controls until the request reaches its final result.
- The workspace runner-connect action reflects the shell's busy state. The
  desktop regression now targets the current connect/reconnect/connected controls.

## Reproduction

```sh
make check
make test-browser
BROWSER_CONTROLS_ONLY=1 node scripts/test-browser.cjs
make test-team-desktop-browser
make test-unified-desktop-browser
make test-assistant-browser
```

The focused control regression is `scripts/browser-controls.cjs`. Team service
and phone tests additionally cover profile validation and storage, ordinary-member
visibility, legacy/expired reports, revocation, member/workspace scoping and
compatibility with an older Workspace Host during an update.

## Practical limits

Browser phone sizes and scripted dictation/agent providers are test fixtures,
not physical-device certification. Real GitHub sign-in, live agent-provider
accounts, operating-system permission dialogs, installer/updater interactions and
remote phone/private-network connectivity still require their actual accounts or
devices. This audit does not claim every possible combination of app state and
external service was clicked. Destructive actions were exercised only in the
isolated fixtures.
