# Dev Board V1 release-readiness audit

Audited commit `c386c8b` (clean tree), 2026-10-04. No V2 functionality was added and no production code was changed.

## Verdict

**Not ready to declare V1 complete.** The single-controller core is solid: schedule claims, project and runner
capacity, Git actions and runner authentication all hold up under review and test. The multi-runner layer has one
Critical defect and four High defects. Between them they can wedge a runner and kill its in-flight work, launch a job
twice, leave a lost runner's work un-releasable, and stall the scheduler without telling the user why.

V1's own exit criterion is "safely orchestrate unattended and multi-runner development while maintaining a single
authoritative controller state". That is not met until C1 and H1–H4 are fixed. Most fixes are small (a state-machine
parity fix, a guard, a retry policy). None needs an architectural rewrite.

| Severity | Count | IDs |
| --- | --- | --- |
| Critical | 1 | C1 |
| High | 4 | H1–H4 |
| Medium | 18 | M1–M18 |
| Low | 10 | L1–L10 |

### How this was established

- Read every file on the execution, scheduling, runner, Git, handoff, usage, auth, store, update and install paths
  (about 60 files).
- Wrote repro tests in a scratch copy of the repo for the nine claims that mattered most. They are in
  `docs/audit/repros/` (saved as `.go.txt` so the Go toolchain ignores them). Seven reproduce a defect (R1–R5, R8, R9);
  R6 and R7 are measurements. Every repro logs its result; none asserts. Convert each into a failing-first regression
  test as part of its fix.
- `go test ./...` passes. `go test -race` passes for `remote`, `runner`, `service`, `events` and `controller`.
  `go vet` is clean. Web unit tests and a browser pass were not run: no UI code was changed.
- **Not verified:** a live Tailscale sign-in, real Claude or Codex CLIs (including whether Claude's `total_cost_usd`
  is cumulative per session), real sleep/wake behaviour of the lease on a laptop, and Windows.

Findings marked *(repro Rn)* were reproduced. Findings marked *(code)* come from reading the code only.

---

## Critical

### C1. One observation the controller cannot apply wedges the runner's entire sync *(repro R1)*

- **Location:**
  - `internal/service/runner_events.go:193-195` (a question is refused unless the run is Running or waiting on a question)
  - `internal/service/runners.go:471-514` (every report, the heartbeat and the acks share one transaction)
  - `internal/remote/worker.go:214-321` (the runner retransmits the same pending observation forever)
  - Every other `ErrInvalid`/`ErrConflict` return in `observe`, the `run not found` path at `runners.go:486-492`, and `observation gap` at `:502` behave the same way.
- **Failure scenario:**
  1. A remote run uses `autonomous_stop_if_blocked`. The agent asks a clarification question.
  2. The policy blocks the run and replies "stop".
  3. The agent asks one more question before ending its turn.
  4. The controller returns `conflict: cannot ask in blocked`. This case is designed for: `agent.StopReply()` exists for "a further question from an agent that is already blocked", and the local path accepts it via `RecordReplied`.
  5. The whole Sync transaction rolls back, so the other runs' reports and the heartbeat are lost too.
  6. The runner resends the same pending observation every 3 seconds, forever.

  After 90 seconds without a successful sync, the runner's lease expires and it stops every session on that machine.
  Other triggers that need no agent misbehaviour: restoring the controller database from a pre-upgrade backup (the
  documented recovery path) while runners hold newer journals, or a controller/runner version difference that fails
  validation.
- **Impact:**
  - **Lost work:** every other run on that runner is killed at lease expiry.
  - **Wedged ownership:** the poisoned run stays non-terminal forever, so the project's concurrency slot is never released.
  - **Revocation blocked:** the runner can never be removed (see H2).
  - **Invisible:** the runner shows as offline, with no reason anywhere (see M18).
  - The repro shows another run's `started` report stuck at `starting` behind the poison.
- **Fix:**
  1. *Parity:* accept a question while Blocked. Record it as policy-answered and re-send the stop reply, as `Runs.RecordReplied` does.
  2. *Isolation:* apply each run's observations in its own savepoint. Apply the heartbeat, capabilities and sequence first.
  3. *Quarantine, don't retry:* on a non-retriable error for run X, acknowledge past the offending observation. Record a visible `Runner report rejected: <reason>` on X's feed. Mark X "uncertain" so ownership stays retained but the sync keeps flowing.
  4. *Rollback detection:* put a controller database ID/epoch in `SyncReply`. A runner that sees it change quarantines its journals and reports, instead of replaying them.
- **Regression tests:**
  - `service`: `TestSyncSecondQuestionWhileBlockedIsPolicyAnswered`.
  - `service`: `TestSyncRejectedObservationDoesNotBlockHeartbeatOrOtherRuns`. A run with an invalid observation is quarantined with a visible reason, run B's report applies, and `LastSeenAt` advances.
  - `service`: `TestSyncAfterControllerRollbackQuarantines`.
  - `remote`: a runner never retransmits a rejected observation more than once.

---

## High

### H1. A runner with no journal relaunches a job the controller already considers running *(repro R5)*

- **Location:** `internal/remote/worker.go:286-303`. A job with no local record always becomes `accepted` and launches, ignoring `j.Run.State` and `j.Ack`.
- **Failure scenario:**
  1. The runner's `runner/runs` journal directory is lost (partial restore, cleanup, reinstall that keeps `identity.json`).
  2. The controller still lists the run as Running with `Ack > 0`.
  3. The next heartbeat delivers the job. The runner launches a second agent for the same run ID.
  4. The controller discards the new process's observations as duplicates, because `ob.Seq <= job.Ack` (`runners.go:498`). The second execution is invisible.
- **Impact:** duplicate, invisible execution of an in-flight run (tokens spent, possibly conflicting file changes). This breaks the documented "never launches an already accepted job a second time", which currently depends on runner disk durability alone.
- **Fix:**
  - Launch a record-less job only if `j.Run.State == starting && j.Ack == 0`. Otherwise create a journal record in phase `uncertain` with the diagnostic "controller lists this run as in progress but this machine has no record of it".
  - Add a per-process `instanceId` to Sync. Have the controller flag a second live instance of one runner identity.
- **Regression tests:**
  - `remote`: promote R5 to `TestWorkerWithoutJournalNeverLaunchesInProgressJob`.
  - `service`: two instance IDs on one runner raise a diagnostic.

### H2. Nothing can release or revoke a run owned by a dead, wiped or compromised runner *(code)*

- **Location:** `internal/service/runners.go:165-177` (`Manage(remove)` refuses while the runner "owns active runs"); `docs/RUNNERS.md` ("deliberately no controller-side force reassignment"). A *disabled* runner still authenticates (`runners.go:476`).
- **Failure scenario:**
  1. A laptop running a scheduled task is lost, stolen, wiped or retired.
  2. Its run stays Running or Starting, because only a signed terminal report can end it.
  3. The project's concurrency slot (default 1) is held indefinitely. Every other scheduled task waits as "Project concurrency limit (1) is occupied", which names neither the run nor the offline owner.
  4. The runner's key cannot be revoked. Disabling it is the only action available.
  5. A disabled runner keeps authenticating and keeps receiving that run's prompt and commands.
- **Impact:** an unattended project stops silently until the machine returns. The user has no revocation path for a lost machine, which fails the "removal/revocation" requirement.
- **Fix** (this adds no new capability; it is the missing escape hatch for the ownership rule):
  - Add an explicit owner action, "Revoke runner", with a confirmation that states the consequences.
  - Revoking marks the runner `Removed` immediately. Its signed requests stop being accepted, so it cannot report, and its 90-second lease can no longer renew.
  - After `Lease + OnlineWindow` (about two minutes), end its remaining runs as `failed: runner revoked; work on that machine is unverified`. Keep the history.
  - Make the scheduler's capacity reason name the occupying run and show an "owner offline since…" marker.
- **Regression tests:**
  - `service`: `TestRevokeFencesRunnerThenReleasesRuns`. Sync returns not-found after revoke, and capacity is freed only after the fence window.
  - `service`: the waiting-capacity reason names the offline owner.

### H3. The scheduler swallows `ErrConflict` from Start: silent per-second retries, network fetches, and queue starvation *(repro R2; head-of-line effect by code)*

- **Location:**
  - `internal/runner/scheduler.go:44-55` (conflicts are neither recorded nor logged)
  - `internal/runner/manager.go:209-213` and `internal/controller/controller.go:152-173` (every start runs `git fetch --prune` for every remote)
  - `internal/service/orchestration.go:443-458` (the plan reserves a slot for the head task)
- **Failure scenario:**
  1. A project has any unreachable remote (network down, dead `upstream`, expired auth, GitHub outage).
  2. Every scheduled start fails with `conflict: cannot refresh remote …`.
  3. `ScheduleOnce` treats conflicts as benign and returns. Nothing is logged and nothing is recorded.
  4. The task still shows "Ready to execute".
  5. The tick repeats every second. R2 shows 5 ticks, 5 fetch attempts, 0 runs, `Orchestration.Error == ""`.
  6. A hanging network blocks the single scheduler goroutine for up to 2 minutes per attempt, for all projects, while holding the project and repository locks.
  7. The same swallow applies to per-task start conflicts such as `previous runner work is uncommitted or unknown`. The failing head task keeps its reserved slot every tick. Lower-priority tasks sit as "Earlier runnable tasks have priority".
- **Impact:** unattended work silently never starts. A remote that merely affects freshness blocks local-only execution too. The user cannot see why. The scheduled-claim safety is intact (nothing runs twice), so this is a liveness and visibility failure.
- **Fix:**
  - Remove the fetch from the scheduled-start path. The local worktree base does not need it. Where freshness is required, fetch `origin` only, cap it at 15 seconds, and treat failure as a warning.
  - Record a durable, user-visible dispatch note for a conflict: reason, attempt count, next retry. Use exponential backoff capped at 5 minutes. Log at warn level, throttled.
  - Exclude backed-off tasks from slot accounting so the next task can run.
- **Regression tests:**
  - `runner`: `TestScheduledStartWithUnreachableRemoteRecordsReasonAndBacksOff`. Fetch count is at most 1 per backoff window and the decision text names the remote.
  - `runner`: `TestConflictingHeadTaskDoesNotStarveNextTask`.

### H4. Planning cost grows with every queued task and runs on every read *(repro R6, measured)*

- **Location:**
  - `internal/service/orchestration.go:428-434` (`inspect()` runs git subprocesses for every `runnable` task *before* slots are counted at `:443`)
  - `internal/service/control.go:238-263` (the Control Center runs `Plan` for every project, per request)
  - `internal/runner/scheduler.go:21-26` (the scheduler does it every second)
  - `web/src/lib/state.svelte.ts:64-75,264` (`run.state_changed` triggers an overview refresh, and so does a 10-second timer)
- **Measured:** `Plan()` takes 24 ms with 1 task, 218 ms with 10, 868 ms with 40 and 2.2 s with 100, on a tiny repo, with 1 runnable task throughout. The cost is linear in queued tasks because each queued task is fully inspected (status, target detection, divergence, per-worktree diff) and then labelled "queued" because it has no slot.
- **Failure scenario:** a 100-task backlog makes every scheduler tick take over 2 seconds on a toy repo, and far longer on a large one. UI reads share the same 4 git slots, so they starve the scheduler and the UI reads. Timeouts surface as `blocked: Cannot inspect repository…`.
- **Impact:** scheduling latency and flapping decisions as backlogs grow. Phones polling the Control Center make it worse.
- **Fix:**
  - Count slots first and skip `inspect()` and `Route()` for tasks that cannot start this tick.
  - Compute the repo-level facts once per project per plan. They are identical for every task: status, target, divergence. Only the pinned-target and previous-worktree checks are per task.
  - Give the API and the Control Center a read-only plan (database decisions plus the last cached inspection, at most 10 seconds old). Only `ScheduleOnce` pays for fresh inspection.
  - Skip projects with no armed task in the scheduler tick via a cheap query.
- **Regression tests:**
  - `service`: `TestPlanGitCallsAreConstantInQueuedTasks`. A counting `gitrepo.Reader` gets at most K calls for 5 and for 200 queued tasks.
  - `api`: the Control Center overview makes zero git calls.

---

## Medium

### M1. Remote runs omit the events the feed and notifications rely on *(repro R8)*
- **Location:** `internal/service/runner_events.go:46-53` (started), `:223-229` and `:259-264` (blocked), `:268-274` (idle). Compare `service/runs.go:208`, `:525`, `:742`.
- **Scenario:** a remote `stop_if_blocked` run becomes Blocked. The events are only `run.state_changed` and `agent.output`; there is no `agent.started`, `agent.blocked` or `agent.waiting`. `notifications.ts:46` never fires "Agent blocked", and the activity feed has no markers.
- **Impact:** an unattended remote run that stops for a human produces no phone notification. The Control Center still lists it, but only if someone looks.
- **Fix:** emit the same events as the local path, and sanitise blockers the way `Runs.block` does.
- **Test:** a table-driven `service` test that drives the same observation sequence through the local and remote paths and compares the emitted event types.

### M2. Any orchestration save, or a Calendar drag, re-arms a finished one-shot
- **Location:** `service/orchestration.go:36-43` (always a new key, clears `RunID`); `web/src/routes/Calendar.svelte:43-50,99-113,147-151,326-337`.
- **Scenario:** a task's scheduled run completed and the task sits in Review. Its Calendar card is draggable. Dropping it on another hour sends `enabled:true` with a fresh key, so the task runs automatically again in its existing worktree, with no prompt. Adding a dependency to a finished task does the same.
- **Impact:** duplicated, non-idempotent agent work, started unattended by an accidental gesture.
- **Fix:**
  - Keep the key (do not re-arm) when `RunID` is set and the schedule fields are unchanged.
  - Require an explicit `rearm:true` when `RunID != ""`.
  - Make the Calendar ask for confirmation ("this ran at …; schedule another attempt?") and exclude dispatched tasks from drag.
- **Test:** `service` `TestPatchWithoutRearmKeepsDispatchedKey`; web test for the confirm step.

### M3. Untracked file contents (including secrets) are copied into the next agent's prompt *(repro R4)*
- **Location:** `service/handoffs.go:293-311`, with the budget logic at `:267-278`.
- **Scenario:** the worktree holds an untracked `.env.local` that `.gitignore` does not cover. A review or fix continuation embeds its contents. R4 shows `STRIPE_SECRET_KEY=…` in the prompt. With a handoff to a different agent or a remote runner, the secret crosses vendors or machines. The user never sees this diff text before launch.
- **Fix:**
  - List untracked paths only, without contents, by default.
  - Add a deny list: `.env*`, `*.pem`, `id_*`, `*credentials*`, `*.key`.
  - Show the exact outgoing context in the handoff editor.
- **Test:** `service` `TestHandoffContextNeverEmbedsSecretLikeUntrackedFiles`.

### M4. Continuing from a stale run mislabels what the agent sees
- **Location:** `runner/manager.go:261-280`, `runner/distributed.go:65-81`, `service/handoffs.go:228-240`.
- **Scenario:** runs A → B → C share one worktree. A continuation from A, which is not the latest run, is accepted. It carries A's summary and next action together with a diff of the current worktree, which holds B's and C's changes.
- **Fix:** require the parent to be the task's latest terminal run, or require `acknowledgeLaterRuns:true`. Label the diff with HEAD and the run IDs that produced it.
- **Test:** `runner` `TestContinueFromNonLatestRunRequiresAcknowledgement`.

### M5. Local runs base on whatever the root checkout's HEAD happens to be
- **Location:** `runner/workspace.go:77-85`; the guard `service/orchestration.go:292-300` only requires "not behind target".
- **Scenario:** the user leaves the project on `feature-x`, which contains `main`. A scheduled overnight task branches from `feature-x`'s tip. The docs describe fresh-target bases only for remote runners. Merging that task later sweeps in `feature-x`'s commits.
- **Fix:** base on the resolved target tip, or block unless HEAD equals the target when no target is pinned. Always show the base ref on the run.
- **Test:** `runner` `TestLocalRunBasesOnTargetNotRootHead`.

### M6. A deleted worktree directory permanently blocks its task, and the self-heal code is unreachable *(repro R3)*
- **Location:** `service/orchestration.go:304-312` runs before `runner/workspace.go:56-70`, which could retire the record and recreate it.
- **Scenario:** disk cleanup or the user removes a task's worktree directory. Both Start and the scheduler return `blocked: Cannot inspect previous worktree: read status: git status … fatal: cannot change to …`. Only the Git view can forget the record. Nothing tells the user that.
- **Fix:** treat a missing directory as "recoverable" in `inspect()`. Let `prepareWorkspace` recreate it, after verifying the branch tip still contains the last known commit. Otherwise say "the worktree was deleted; open Git → clean to forget it".
- **Test:** promote R3 to a passing `runner` test.

### M7. A remote runner's 60-second budget covers clone, fetch and checkout
- **Location:** `remote/worker.go:350`. The context covers `prepare`, which includes `git clone` for a new runner with `--allow-clone`.
- **Scenario:** the first run on a new runner with a big repository times out, fails, and consumes the one-shot schedule.
- **Fix:** budget clone and fetch separately, with at least 10 minutes for clone. Report a `preparing` observation so the controller shows progress instead of a silent "starting".
- **Test:** `remote` with a slow `Prepare` of 90 seconds completes and still reports.

### M8. Unauthenticated runner endpoints take the single SQLite writer before authenticating *(code)*
- **Location:** `service/runners.go:462-481` (Sync verifies the signature inside `s.update`), `:116-121` (Join), `api/runners.go:65-90`.
- **Scenario:** any peer on the tailnet can generate a keypair, sign garbage and POST up to 4 MB repeatedly. Each request parses the JSON and opens a `BEGIN IMMEDIATE` transaction. The controller's own writes (scheduler, events, runs) queue behind a flood.
- **Fix:** look up the public key in a read transaction and verify the signature before touching the writer. Reject unknown runner IDs and bad pairing secrets cheaply. Apply a small pre-auth body cap (about 64 KB) and a per-source rate limit.
- **Test:** `service` with a spy store: a bad signature never calls `Store.Update`.

### M9. Controller secrets are inherited by agent processes, and the token file is readable by them *(code)*
- **Location:** `agent/base.go:230-246` (`SanitizedEnv` strips only `GIT_*`); `controller/network.go:36-40` and `config/config.go:284` (the env names `DEVBOARD_TOKEN`, `DEVBOARD_TS_AUTHKEY`, `TS_AUTHKEY`); `config.go:388-413` (`<data-dir>/token`, mode 0600, same user).
- **Scenario:**
  - A controller started with `DEVBOARD_TS_AUTHKEY` exported, or with `DEVBOARD_TOKEN`, hands those values to every agent.
  - An agent (or injected repo content) can read them. With `bypassPermissions` or `danger-full-access` it can also read the token file and answer its own approval questions over loopback.
  - Under default agent permissions each read or `curl` is itself an approval, so exploitation needs the risky settings the controller already warns about.
- **Fix:**
  - Strip `DEVBOARD_*`, `TS_AUTHKEY` and `TS_*AUTH*` from the agent environment.
  - Add a startup warning when risky agent settings are on, noting that the data directory is readable by agents.
  - Optionally require a UI-session-only credential for approval answers.
- **Test:** `agent` `TestSanitizedEnvDropsControllerSecrets`.

### M10. The owner token is static, never rotated, and doubles as the phone sign-in credential
- **Location:** `config.go:388-413` (generated once); `api/network.go:79-98` (the same token goes in the QR/link); `cmd/devboard/main.go:116-205` (no rotate command).
- **Scenario:** a lost phone or shared screenshot of the sign-in QR is a permanent full-control credential, including starting autonomous agents. A shared tailnet exposes the API to every peer, protected only by this token.
- **Fix:** add `devboard token --rotate`, which invalidates all sessions and updates the service. Consider a separate, revocable per-device token for the phone link.
- **Test:** `api` and `cmd` rotation test: the old token is rejected after rotation.

### M11. The runner protocol is unversioned, and a runner's version is never refreshed *(code)*
- **Location:** `runnerwire/protocol.go:1,21-84` (the comment says "versioned", but there is no version field); `service/runners.go:462-517` (Sync never updates `Runner.Version`); `cmd/devboard/update.go:96-109` (only the controller service restarts).
- **Scenario:** `devboard update` replaces the binary. A running runner service keeps the old code, and its reported version stays at the join-time value. A future controller that adds a safety field to `Job` (like `ExpectedCommit` today) silently loses it on an older runner, because unknown JSON fields are ignored.
- **Fix:**
  - Add `protocol: 1` to Sync, and a controller `minProtocol`.
  - On mismatch, refuse with a specific message and keep ownership.
  - Update `Runner.Version` on each Sync, and show mismatches in the Control Center.
  - Have `devboard update` also restart an installed runner service.
- **Test:** `service` `TestSyncRejectsUnsupportedProtocolWithReason`.

### M12. Failed-upgrade recovery restores the binary but not the database, and it conflicts with live runners
- **Location:** `cmd/devboard/update.go:106-119`; `store/sqlite/backup.go`; `doctor/checks.go:67`.
- **Scenario:** a new version migrates the database, then fails to start. The update flow puts the old binary back. The old binary refuses the newer schema, and recovery becomes a manual file copy from `backups/`. Restoring an older database while runners hold newer journals triggers C1.
- **Fix:** add `devboard db restore --latest`. It stops the controller, moves the current database aside, restores the backup, and logs that runners will be re-synced. Make the update flow offer or run it when the old binary cannot open the database. Pair it with the C1 epoch handshake.
- **Test:** `cmd` simulating a failed post-migration start ends with a running old version and a database from the backup.

### M13. `devboard update`, `stop` and `restart` kill in-flight local runs without warning
- **Location:** `cmd/devboard/app.go:271-301`, `update.go:106`.
- **Scenario:** an update while an unattended local run is mid-flight fails that run. The one-shot is consumed (`interrupted: controller restarted`) and never reruns. This is the intended at-most-once behaviour, but the CLI does not say so.
- **Fix:** preflight active runs. List them and require `--force` or a confirmation. Remote runs are unaffected.
- **Test:** `cmd` stop/update with an active run refuses without `--force`.

### M14. Usage totals: resumed sessions show stale partial numbers as complete, and later partial snapshots erase known values *(code)*
- **Location:** `agent/claude/session.go:232`, `agent/codex/session.go:285` (`resumedUsage` suppresses every update); `service/usage.go:12-35` and `runner_events.go:60-67,275-285` (a snapshot replaces the whole `Usage`).
- **Scenario:**
  - A run resumed after a controller restart keeps its pre-restart snapshot, and later consumption is dropped. The UI shows it as the run's usage with no "partial" marker.
  - A later snapshot with no token detail (for example one model missing counts) resets known token counts to Unknown.
- **Fix:** mark usage partial after a resume (add a note to `Source`). Never replace a known value with unknown, and never let a cumulative counter decrease.
- **Test:** `service` `TestUsageSnapshotsAreMonotonicAndResumeMarksPartial`.

### M15. Cross-project starvation when runner capacity is the limit
- **Location:** `runner/scheduler.go:21` (projects are visited in order); `store/sqlite/projects.go:56` (`ORDER BY name`); the local runner defaults to capacity 1 (`store/sqlite/settings.go:65`).
- **Scenario:** two projects share one local runner at capacity 1. Whenever a slot frees, the alphabetically first project with a runnable task takes it. A busy first project starves the rest.
- **Fix:** order candidates across projects by the same key the per-project plan uses (scheduled time, order, priority, created, id), or rotate the starting project each tick.
- **Test:** `runner` two projects, capacity 1, alternating starts.

### M16. Dependency completion means "the agent's turn ended", not "integrated", and nothing says so at run time
- **Location:** `service/orchestration.go:177`; `docs/ORCHESTRATION.md` ("Dependencies do not transfer or merge changes").
- **Scenario:** B depends on A. A completes on its own branch. B starts from the target without A's changes, and the prompt never says so.
- **Fix:** put the unmerged dependency into the decision reason and B's prompt ("A finished on branch X; its changes are not in your base"). Optionally add a per-dependency "wait until Done".
- **Test:** `service` decision text and prompt include the dependency branch.

### M17. Event log bloat, no retention, and SSE replay that reads every project *(repro R9)*
- **Location:**
  - `service/runs.go:1044-1048` (`emitRun` logs the whole Run, including prompt and handoff, on every state change, session ref and usage snapshot)
  - `store/sqlite/events.go` (`ListAfter` has no project filter)
  - no retention (the only `DELETE FROM` in the store is for resolved health findings)
- **Measured:** 100 usage snapshots on a run with a 30 KB prompt add 3.2 MB to the event log.
- **Impact:** database growth with activity, and slow reconnects for a phone after a long offline period (it reads and discards other projects' events).
- **Fix:**
  - Slim run-event payloads (omit prompt and handoff) and skip usage-only events unless the value changed materially.
  - Prune output events for terminal runs older than N days.
  - Filter by project in `ListAfter`, and cap replay with a "resync required" response.
- **Test:** `service` `TestUsageEventsDoNotRelogPrompt`; `api` replay respects the project filter.

### M18. Runner diagnostics are missing: nothing is logged, and clock skew shows only as "offline"
- **Location:** `remote/worker.go:170-192` (`post` hides the HTTP reason), `:497-507` (`_ = w.Tick(ctx)`); `service/runners.go:479-481`.
- **Scenario:**
  - A runner whose clock is more than 2 minutes off, or whose `identity.json` was restored with a lower sequence, is refused permanently.
  - The runner logs nothing, and `devboard runner status` reports only service state. The Control Center says "offline".
- **Fix:**
  - Have the controller return a structured code plus its server time.
  - Log runner refusals to `runner/logs`, and expose the last error in `devboard runner status`.
  - Surface "clock skew", "stale sequence" and "revoked" on the runner card.
- **Test:** `remote` the worker records the refusal reason; `service` returns the code.

---

## Low

- **L1. Ordering inconsistency.** Untimed queue entries sort before overdue timed tasks (`orchestration.go:388-396`), which can make a `skip`-policy task miss its window. The Calendar's "upcoming" list ignores priority and creation time (`Calendar.svelte:54-62`). Align the UI with `Plan()`, and document or change the ordering.
- **L2. Runner-reported commits are not validated on `ended`.** `runner_events.go:46,58` stores `BaseCommit` and `HeadCommit` unvalidated; only the workspace path checks (`:19`). They flow into `PreviousCommit` for another runner (`remote/workspace.go:106` runs `git merge-base --is-ancestor` with no `--`). The controller side is safe (`checkRev`). Validate full hex at the boundary.
- **L3. Pairing ergonomics.** The code appears in argv and shell history for its 5-minute life. `devboard join` does not show the controller address or ask for confirmation (important with `--allow-clone`), and `DecodeCode` accepts `http`. (`cmd/devboard/runner.go:93-160`, `runnerwire/protocol.go:117-130`.)
- **L4. Release artifacts.** `checksums.txt` comes from the same origin as the archive, so it detects corruption, not tampering. There is no signature. `DEVBOARD_RELEASE_URL` can redirect updates. (`update/update.go`, `scripts/install.sh:93-99`.) Consider signing releases.
- **L5. Housekeeping that grows forever.**
  - Ended runner journals, worktrees and `devboard/<runner>/<run>` branches are never pruned.
  - Every runner re-inspects up to 8 retained worktrees every 3 seconds, indefinitely (`workspace_reports.go:15-48`).
  - Expired `pair:*` settings rows, `runner-job:*` rows (they hold the run's prompt) and `Manager.locks` entries (`manager.go:664`) are never removed.
- **L6. Worker locking and lease clock.** `command()` holds the global worker lock for up to 20 s per command (`worker.go:440-480`). The lease is evaluated only on a failed tick (`:258`) and uses wall time (`time.Now().UTC()` strips the monotonic reading), so a clock step can shorten or extend it.
- **L7. Spurious conflict right after a run ends.** `Handoffs.Context()` calls `Generate()` (`handoffs.go:102-192`), which races with the `Generate()` inside `Runs.End` (`runs.go:241-247`) on the run version. A continuation started within about a second returns `conflict`. It self-heals; retry or serialise.
- **L8. Hardening.** Sync capability payloads are bounded only by the 4 MB body and are rewritten every 3 s (`runners.go:468-469,517`). `Manage()` has no version check, so concurrent edits lose updates (`:155-205`).
- **L9. Release hygiene.** The V1 commit `c386c8b` is untagged (`git tag` ends at `v0.7.0`), and `docs/VERSIONING.md` has no row for it.
- **L10. Cost wording and a blank-runner usage group.** `RunUsage.svelte:20` says "API-equivalent estimate" but never "not a bill". Runs with an empty `runnerId` (older local runs) form a separate usage group from the local runner's current ID (`usage.go:74`).

---

## Checklist coverage

Each area you asked about, with what was checked and what was found.

**Scheduling and dispatch**
- **Duplicate scheduled runs:** sound for one controller. `runs_schedule_attempt` is a unique index; the claim, run and event commit in one transaction (`runs.go:116-140`); a manual start consumes an armed schedule. The duplicate-execution gaps are re-arming (M2) and runner-side journal loss (H1).
- **Timezone and DST:** sound. The backend stores UTC instants and has no recurrence. The browser converter rejects missing hours and offers both repeated hours; its tests cover Madrid and Kathmandu.
- **Controller restarts:** sound by design. Local runs are interrupted and their claims retained. Remote runs are preserved. Race-clean tests cover it. Update/stop messaging is M13.
- **Missed schedules:** `run_late` and `skip` work. The matching of the "missed" reason by string equality (`scheduler.go:38`) is brittle but tested.
- **Dependency loops and races:** cycles are rejected inside the write transaction; evaluation is direct-only, so a loop cannot hang the planner; gates are rechecked inside the claim transaction. The semantic gap is M16.
- **Starvation:** H3 (head-of-line plus silent retries), M15 (cross-project), L1.
- **Concurrency limits:** enforced transactionally for project and runner at claim time. The effective global limit is runner capacity (default 1), which M15 and H2 make more visible.

**Runners and ownership**
- **Duplicate ownership, stale leases, split-brain:** ownership is never reassigned and a lease expiry never frees it, so split-brain cannot arise by design. The exceptions are H1 (journal loss) and clone-of-identity cases covered by the same fix.
- **Runner disconnect, reconnect, duplicate events:** per-run ordered sequence numbers and acks make duplicates inert. C1 is the failure mode.
- **Delayed messages and clock skew:** a ±2 minute window plus a monotonic counter. Skew diagnostics are M18.
- **Task reassignment while the old runner lives:** impossible while a run is active (`Create` checks).
- **Controller restart while runners continue:** covered by the existing `TestWorkerReconnectAndControllerRestartNeverLaunchDuplicate`. A *rolled-back* controller is C1.

**Git**
- **Two runners on one repository, branch collisions:** `devboard/<runner>/<run>` is unique and a duplicate branch is refused.
- **Stale target, remote changed, merge races:** an expected-SHA protocol under the project lock; no force push; remote refresh before review. Sound.
- **Failed pushes:** Dev Board never pushes from a runner; Git actions verify against the remote. Sound.
- **Unsafe cleanup:** a plan then a recheck; clean worktrees only; ownership recorded. One caveat: `RemoveWorktree --force` and `branch -D` are used only to discard a worktree created moments ago.
- **Wrong base:** M5.

**Handoffs**
- Size is bounded (handoff 32 KB, diffs 20 KB, selected context 16 KB), so context cannot snowball. Staleness: M4. Missing Git state for remote runs is stated as unknown, not invented. Secrets: M3. Handoff text is untrusted model output (low risk, noted under L-level hardening).

**Security**
- **Pairing replay:** the code is 192-bit, stored only as a hash, single-use, and expires after 5 minutes. Sound.
- **Runner authentication and impersonation:** Ed25519, persisted monotonic counter, ±2 minute window, a run must belong to the authenticated runner, and capability reports cannot grant access. A test confirms runner keys have no owner API access. The local runner has no key, so it cannot be impersonated.
- **Controller API authorization:** the token is compared in constant time; SSE accepts the token in the query only on `/api/events`, and the query is not logged.
- **Private-network exposure:** M8 (flooding the writer), M10 (static token). The service worker does not cache API responses.
- **Secrets crossing machines:** M3, M9. Runners receive no GitHub or agent credentials.
- **Log leakage:** pairing secrets, tokens and URL queries are not logged; remote URLs are redacted. Sound.
- **Compromised runner capabilities:** limited to the runner's authorized projects and its own runs. Reported data can mislead (fabricated results, usage). L2 is the unvalidated-field gap.
- **Removal and revocation:** H2.

**Usage and economics**
- Snapshots replace rather than add, unknown stays nil, and actual and estimated costs are never summed. A retry is a separate run with its own tokens, so it is not double-counted. Gaps: M14, L10.

**Upgrade and compatibility**
- Migrations are contiguous and each runs in its own transaction; a pre-upgrade `VACUUM INTO` backup keeps 5 copies; the controller refuses a newer schema. M11 (protocol and version), M12 (recovery), M13 (kills in-flight runs). Migration `0010` renames the runners table without disabling foreign keys; no table references `runners`, so it is safe today. Add a note in the migration to keep it that way.

## Scale: what the evidence supports

| Area | Measured or observed | Recommendation |
| --- | --- | --- |
| Planner with many queued tasks | 100 queued tasks: `Plan()` = 2.2 s (R6) | **Fix** (H4) |
| Control Center on a phone | Runs the planner per project per request and every 10 s | **Fix** (H4) |
| Event log | 3.2 MB per 100 usage snapshots at a 30 KB prompt; never pruned (R9) | **Fix** (M17) |
| SSE reconnect | Reads other projects' events then discards them | **Fix** (M17) |
| Runner journals | Polled forever, never pruned | **Fix** (L5), low urgency |
| Run history in SQLite | 20,000 runs of 28 KB: latest-per-task 19 ms, newest 500 = 10 ms (R7) | **Do not optimise** |
| Single SQLite writer | About one sync write per runner per 3 s | **Do not shard.** Only M8's flood matters. |
| Dependency graph, sorting | Direct-only, O(tasks log tasks) | **Do not optimise** |
| Output volume | Capped at 16 MiB per run, 16 KB per event | Sufficient |
| Many Git branches | Not measured here. Inspection is bounded and fails conservatively. | No change without data |

## What must be true before declaring V1 complete

1. **C1.** Parity fix and per-run fault isolation. Include the controller-epoch handshake.
2. **H1.** Never launch a record-less job that is already in progress.
3. **H2.** A fenced revoke/release for runs owned by a dead or lost runner, with a capacity reason that names the owner.
4. **H3.** Record, back off and explain conflicts. Remove the fetch from scheduled starts.
5. **H4.** Constant-cost planning and a read-only plan for the API.
6. Strongly recommended with those: **M1** (notifications for remote blocks), **M2** (silent re-run) and **M3** (secret leakage).
7. Everything else can ship as V1.x, in this order: M8, M9, M10, M11, M12, M13, then the rest.

## Repro files

| File | Target package | Contents |
| --- | --- | --- |
| `docs/audit/repros/service_repros_test.go.txt` | `internal/service` | R1 `TestPoC_SecondQuestionWhileBlockedWedgesSync` (C1), R8 `TestPoC_RemoteBlockedRunEmitsNoBlockedOrStartedEvent` (M1), R9 `TestPoC_UsageUpdatesRelogWholePrompt` (M17) |
| `docs/audit/repros/runner_repros_test.go.txt` | `internal/runner` | R2 `TestPoC_UnreachableRemoteSilentlyStallsSchedule` (H3), R3 `TestPoC_MissingWorktreeDirBlocksTaskForever` (M6), R4 `TestPoC_HandoffContextLeaksUntrackedSecrets` (M3), R6 `TestPoC_PlanCostGrowsWithQueuedTasks` (H4) |
| `docs/audit/repros/remote_repros_test.go.txt` | `internal/remote` | R5 `TestPoC_RunnerJournalLossRelaunchesAcceptedJob` (H1) |
| `docs/audit/repros/sqlite_repros_test.go.txt` | `internal/store/sqlite` | R7 `TestPoC_RunHistoryReadCost` (scale evidence) |

To use one: rename it to `*_test.go` in the target package. Each repro relies on that package's existing test helpers, such as `newRunnerTest`, `newEnv` and `workerFixture`.
