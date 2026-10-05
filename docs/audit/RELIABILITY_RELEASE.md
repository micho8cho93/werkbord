# Reliability fixes: Werkbord 0.9.0 and Team 2.1.0

The findings were revalidated against `152fd52` on 2026-10-05. These changes keep
the separate product executables, installers, credentials and execution boundaries.
The older V1 audit remains a historical record of the commit it audited.

## Reproductions and regression coverage

| Area | Reproduced failure and resulting behavior | Regression coverage |
| --- | --- | --- |
| Runner sync | A second blocked question wedged the batch; permanent poison rolled back other reports. Policy answers remain valid, per-report savepoints isolate invalid reports and impossible terminal transitions, quarantined work stops, healthy heartbeats continue. | `internal/service/reliability_test.go`, `internal/remote/reliability_test.go` |
| Ownership recovery | Deleting a live journal launched the same job again. Durable two-phase acceptance distinguishes new work; missing/legacy journals and restored controller state remain uncertain until owner recovery. Revocation fences signatures and waits for leases before releasing unverified work. | Same tests; runner lost-journal, protocol, restored-database and revocation cases |
| Team authentication | Sole-owner renewal signed the console out before showing the new token. The replacement is adopted/persisted first; old-token requests cannot invalidate a newer session. | `internal/team/api/api_test.go`, `scripts/browser-team.cjs` |
| Concurrent editing | The baseline browser accepted a stale save with HTTP 200. The changed browser sends the original version, gets 409, preserves empty text/selects/disclosures/focus and offers explicit recovery. Personal defaults and concurrently saved handoff notes also survive updates. | Browser scripts; `internal/runner/reliability_test.go` |
| Execution eligibility | Controller availability incorrectly disabled remote execution. Resolved inherited settings and authorized, online, available runner capabilities govern execution and option choices. | `web/src/lib/execution.test.ts`, personal browser script |
| Scheduler | Repeated scheduled starts fetched each tick; queue inspection scaled with every task. Capacity and path gates now precede Git, conflicts require rearm, and shared slots rotate between projects. Remote worktrees do not block independent declared scopes. | Runner reliability/orchestration tests; capacity, fairness and bounded inspection assertions |
| Import and metadata | Repeated imports duplicated work; prefixed Unicode titles/long context failed; task branches diverged from tickets. Generic source association reuses the task, preserves local edits and carries explicit branch/base. The developer-owned client reports commits and matching PR metadata. | Individual import/service and branch tests; Team CLI report test; live bridge flow against disposable servers and a real Git commit |
| Board/reviews | Board detail omitted commits, completion gates differed, partial PR edits erased existing evidence. Details load actual commits, both screens share gates, and PR updates retain omitted fields. Direct links and history work. | Team API/service tests and browser script |
| Handoff and preparation | Untracked `.env.local` content reached outgoing prompts; a missing task worktree prevented recovery. Untracked content is excluded; deleted worktrees recover their branches. Explicit bases, longer preparation budgets, progress and remote lifecycle notifications are covered. | Runner and remote workspace/reliability tests |
| Usage and operations | Partial snapshots erased known usage, resumed totals were entirely omitted, and repeated events stored full prompts. Measurements merge conservatively, resumed deltas are Partial, events are compact and retained by age. Controller secrets are filtered; token rotation is live; update rollback restores SQLite and the binary even if interrupted; active interruption requires explicit force. | Adapter usage tests, SQLite retention/API replay tests, agent environment test, CLI reliability tests |

The 100-task queue reproduction originally took approximately 2.2 seconds. The
changed isolated measurement took 28–41 ms with one available project slot and
exactly one Git status inspection at every queue size (1, 10, 40 and 100).
Two one-slot runner candidates produce two runnable tasks and two inspections;
a single shared slot dispatches another queued project on the next tick. Timings
are diagnostic; tests assert bounded work and capacity, not machine-dependent speed.

## Browser checks

The scripts use Playwright and run only in disposable environments. They mutate
fixtures, create members and projects, and renew the disposable owner's token.
They cover concurrent clients, stale-save rejection, deliberate clearing, selects,
keyboard selection/focus, reconnects, project draft isolation, direct links/history,
actual Board commits, remote-only agents and models, personal settings drafts, and
1440 px desktop / 390 px mobile layouts. They fail on JavaScript errors and mobile
horizontal overflow. Screenshots are saved outside the repository.

Build with `make build build-team`. To run the personal fixture in one terminal:

```sh
go run ./scripts/browser-fixture
```

Create an empty disposable Git repository with an initial commit, then run:

```sh
WERKBORD_BROWSER_REPO=/absolute/path/to/disposable/repository \
  node scripts/browser-personal.cjs
```

For Team, create and serve a disposable sole-owner workspace on port 17430 using
`WERKBORD_TEAM_DATA_DIR`, `werkbord-team workspace create` and `werkbord-team serve`.
Pass its token via `TEAM_BROWSER_TOKEN` or a private `TEAM_BROWSER_TOKEN_FILE`:

```sh
TEAM_BROWSER_TOKEN_FILE=/absolute/path/to/private/disposable-owner-token \
  node scripts/browser-team.cjs
```

With both disposable servers running, `scripts/browser-bridge.cjs` verifies Unicode/context
import boundaries, repeated import preserving local edits, intended branch/base, and reporting an
actual Git commit. Give it the same `TEAM_BROWSER_TOKEN_FILE` (the replacement token if renewal
already ran); `WERKBORD_TEAM_BINARY` optionally chooses the built CLI. It writes only to the
fixture's disposable repository and workspace.

`PLAYWRIGHT_MODULE` can point at an installed Playwright module and
`BROWSER_EXECUTABLE` at an installed Chromium/Chrome executable. Otherwise the
scripts use `playwright` and its default browser. `BROWSER_ARTIFACT_DIR` selects
where screenshots and the replacement Team token are saved; default directories
are temporary. The personal fixture listens only on `127.0.0.1:17421`, uses a
known test credential, temporary storage and an unavailable fake controller agent.
Its signed remote is simulated; it never runs a real coding agent. Do not point
these tests at personal or shared production data. Stop the fixture after testing.

## Recovery and limits

Controller and runner upgrades must agree on protocol 1. Legacy unacknowledged
jobs are quarantined because the old protocol cannot prove whether they executed.
Unknown work still requires owner inspection, resolution/revocation and branch
reconciliation. Revocation cannot undo Git changes or control an arbitrary process
outside the runner contract.

The metadata bridge runs on the developer's computer, in the foreground. Reporting
remote-runner work requires publishing/fetching its branch. A report exceeding
200 commits or 200 changed files fails explicitly rather than replacing metadata
with a partial list. Team holds no member Git/agent credentials, executes nothing,
and never connects to a member's computer. Agents still run as the operating-system
user; filtering environment secrets does not create an OS sandbox.

Terminal output expires after 30 days, other terminal run events after a year.
Runs, handoffs and active execution evidence remain; reconnecting browsers whose
cursor predates retention reload authoritative state. Usage on resumed sessions
covers only verifiable increments after the baseline and remains visibly Partial.

Release validation includes `make check`, `make verify-isolation`, race checks for
the changed stateful Go packages, independent installer tests, the browser scripts,
real Git bridge smoke testing, and the bounded scheduler measurements above.
