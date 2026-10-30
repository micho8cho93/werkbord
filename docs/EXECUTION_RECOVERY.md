# Durable agent execution recovery

Werkbord uses the existing local scheduler, SQLite run records and runner journals.
Recovery needs no vendor account, hosted scheduler or author-operated control plane.
The local controller must be running to dispatch work. A browser need not be open.
The Team service coordinates requests; it does not execute agents or hold credentials.

## Offline scheduling

An armed schedule remains in the task's persisted orchestration. When no suitable
runner is connected, its scheduling decision says **Waiting for runner**. No run is
created, and an explicitly assigned runner never falls back to the controller.
The scheduler polls once per second. On reconnect it checks the task again:
project and task eligibility, dependencies, deadline and missed-time policy,
repository safety, capacity, runner authorisation and runtime availability all apply.
A restart reads the same schedule from SQLite.

The schedule's key and its run claim are committed in the same transaction. A
unique `(task_id, schedule_key)` index prevents duplicate claims, including repeated
reconnects and competing dispatch requests. Manual starts consume an eligible
armed schedule too. Editing a used schedule does not authorise another execution;
**Schedule another run** explicitly rearms it with a new key.

In **Schedule and dependencies → Recovery preferences**, choose whether to run
when reconnected (the default) or require approval after Werkbord has observed a
due task waiting for its runner. Approval uses **Schedule another run**; all gates
are checked again. When the controller itself was offline, it cannot know when a
runner disconnected. Use **If the scheduled time is missed → Require approval
after grace period** to cover that case. The existing `skip` missed-time policy
records the missed occurrence and holds it until explicitly rearmed. Deadlines
still forbid late starts.

## Interrupted work

A network disconnect is not proof that execution stopped. Remote runs retain
ownership, worktree reservations and capacity while disconnected. The existing
runner journal records observation sequence numbers and command acknowledgements;
reconnect replays observations rather than dispatching a replacement run. This
also works when the controller restarts. Permission removal is rechecked at sync
and queues a stop; a disconnected machine cannot be assumed to have received it.

On a local controller restart, Werkbord attempts to stop only processes whose
recorded PID, process identity and process group match. If identity is ambiguous,
`run.recovery` is `intervention`: ownership remains reserved and send, finish and
stop cannot pretend that the unknown process ended. Inspect the runner and stop
the original execution yourself. After the recorded PID is gone, restart Werkbord
to reconcile it. Werkbord never kills an unrelated process merely to clear a run.

After a verified stop, a working run with a saved provider session handle becomes
**Blocked**, with a request to review previous actions. An idle or already blocked
session retains its state. Sending a message resumes the same run, worktree,
provider, model and policy through the existing resume path. Pending questions
from the dead process are cancelled. Without a saved session handle, the run
fails with an interruption reason; its work and execution record remain available
for inspection and an explicitly requested new attempt.

Remote runner restarts use the existing journal recovery: known processes are
stopped, their results are reported, and uncertain launches are quarantined.
Stop the runner service and use `werkbord runner resolve <run-id> --confirm-stopped`
on that runner only after confirming that its execution has stopped. A quarantined run is never automatically
restarted or migrated to another machine.

## Provider failures

Failed turns from Claude Code, Codex and ACP adapters are separate from successful
turn completion. Their failure becomes a persisted blocker, including a category:

| Category | Recovery |
| --- | --- |
| `rate_limit` | Temporary throttling; review an already-started session before continuing. |
| `quota_exhausted` | Restore allowance or credits; no automatic retry. |
| `context_exhausted` | Prepare a session handoff; no automatic replay or provider switch. |
| `authentication` | Sign in on the executing runner; no automatic retry. |
| `other` | Inspect the error and previous work; no automatic retry. |

When a CLI supplies only error text, classification is conservative and based on
recognised phrases. Unknown failures remain `other`; numeric HTTP 429 with quota
wording is quota exhaustion. Ordinary agent/tool output is not parsed as a failure.
A failed scheduled turn is never automatically finished as a successful turn.
A session that then exits retains its blocker with its terminal execution record.

The safe default is **Pause for intervention**, with zero automatic retries.
Optionally enable **Retry temporary rate limits** and set a limit
of 0–5 retries. This applies only to scheduler dispatch failures **before any run
has been claimed**. Backoff is 30, 60, 120, 240 and 480 seconds. Retry count, category,
error and next retry time persist on the task; the next scheduler tick after that
time rechecks eligibility. The budget survives restart and is reset only when the
person rearms the schedule. Once exhausted, the schedule stays blocked.

Once a run is claimed, even an adapter start failure can have an ambiguous outcome.
Werkbord does not automatically create a replacement run, replay the prompt, retry
a failed turn, or choose another provider. Agent CLIs can implement their own
internal retries; these are outside Werkbord's dispatch retry budget. Cross-provider
fallback remains deferred until reliable session handoff is available.

## Guarantees and limits

The guarantee is at most one persisted run claim per armed occurrence, not exactly
once execution of every external side effect. A provider session handle does not
prove that its last command completed or rolled back. Review Git changes and
external effects before resuming interrupted work. Resumption depends on the
provider retaining that session and supporting its existing resume mechanism.
No provider or machine migration is attempted by recovery. Failed-turn classification
requires updated runners: older runner binaries cannot report the new failure field.

Durability assumes intact SQLite storage and runner journals. Restoring a stale
controller database or losing a runner journal invokes the existing quarantine
and ownership-reconciliation rules, not automatic redispatch. The controller
must reconnect to runners and must be running for retries to become due; power,
network and provider availability cannot be guaranteed.

Regression coverage lives in `internal/runner/recovery_test.go`, the existing
runner lifecycle/orchestration tests, `internal/remote/reliability_test.go` and
runner service tests. It covers offline dispatch, restart, repeated reconnect,
manual approval, dependency blocking, bounded backoff, quota/context/authentication
failures, claimed launch failure, failed scheduled turns, session resume and
uncertain process ownership.
