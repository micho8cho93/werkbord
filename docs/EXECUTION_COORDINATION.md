# Team execution coordination (Phase 2)

Individual 1.6.0-preview.1 and Team 3.4.0 extend the tested Phase 1 association,
identity and progress contract. Install both products independently. Team remains
coordination software: its backend cannot link the agent runtime or launch
processes on a member's machine. Individual's existing `Start`, runner capacity,
repository, scheduler, agent permission and question mechanisms perform execution.

## Owner controls

In the Team desktop console, open a ticket you hold and choose **Agent controls**.
Connect your own Individual controller in Settings first; reconnect existing older
connections to obtain the new execution scope. The local device API requires its
separate authenticated desktop credential and same-origin requests. Workspace
credentials and manager roles cannot authenticate this interface.

Select the local runner, installed agent (Claude Code or Codex), model, reasoning
and conversation policy. **Review execution policy** displays the exact local
context, effective adapter sandbox/approval configuration and selection. Those
runtime permissions are read from Individual's configuration; Team cannot change
them. **Approve and start my run** grants one execution. Ordinary approvals expire
in an hour. Explicitly checking the preapproval option retains this exact one-shot
permission for up to 30 days, bounded by a skip schedule's deadline. There is no
broad automatic-start or recurring-policy authorization in this phase.

Approval binds an immutable execution ID, local project/task, external context
fence, repository identity/path, task text/branch, execution defaults, explicit
agent/model/reasoning/runner/interaction, effective runtime permissions, earliest
start and optional deadline. A changed digest requires a new reviewed approval and
execution ID. Dispatch cannot supply policy overrides or extra instructions.
Individual rechecks local policy and capacity even for a correctly signed request.
Its approval consumption and run ID commit in the same transaction as run
creation. Retrying a committed execution retrieves that run, including a failed
or stopped run; it never starts a second process. A restart uses a new approval.

The ticket controls show task-scoped run progress, pending questions, branch and
final handoff. Questions retain Individual's original approval handling; a
conversation policy does not grant tool permissions. These details go directly
from the owner's loopback controller to their authenticated local renderer, never
to a Workspace Host. Team's existing PR and review views retain their independent
workflow. An agent's completion does not approve a PR or mark a ticket done.

The current adapters support **stop/restart**, not live process pause. The UI
states that explicitly. Review and stop remain available in Individual if Team
membership or connectivity is lost.

## Other enrolled devices of the same owner

Use **Open in my runner** to select one of your own registered runner devices.
Opening uses the Phase 1 canonical source and workspace-prefixed branch, so later
synchronization and local controls reuse the same task. Approve the desired
execution locally on that target device. Its owner may then request runner status
and select that approval from their other locally pinned, enrolled devices.

The closed `start_authorized_run` semantic action carries only project, ticket,
execution and fence identifiers. It uses the existing signed mailbox, pinned keys,
expiry and durable replay protection. The recipient refreshes ticket ownership
and context, checks its own remote-start setting, and invokes Individual's local
approval-bound dispatch. A manager's valid signature still cannot start a
member's runner, answer their privileged question or select their policy. Legacy
unbound start approvals are refused by the production bridge; the old `auto`
setting does not authorize these new executions.

Cancel/answer actions continue through the existing same-owner signed actions.
Question text, handoff contents and raw logs are deliberately not placed in
mailbox status replies: review them in the target owner's Individual/local Team
console. All workspace progress remains the Phase 1 allowlisted projection.

A completed mailbox result is returned only for the exact previously accepted
signed request; changed bytes cannot retrieve an owner's cached result. A crash
between verification and dispatch may refuse that particular envelope on replay.
Send a new signed request with the same execution ID to recover its Individual
claim safely. Do not restore an old local replay/approval database while requests
remain live; existing recovery/expiry requirements still apply.

## Shared schedules

The ticket's **Shared schedule** stores an offset-bearing one-time instant,
explicit IANA display timezone, `run_late` or `skip` missed policy, grace window,
priority, order and dependency ticket IDs. A manager can propose, reschedule or
cancel a held ticket's request. Only its holder's enrolled runner device may
acknowledge dispatch. A proposed schedule is never permission to launch.

Schedule APIs under `/api/team/v1`:

| Method and path | Purpose |
| --- | --- |
| `GET /projects/{id}/schedules` | Permission-filtered requests and current eligibility |
| `PUT /projects/{id}/tickets/{tid}/schedule` | Create/reschedule with expected version |
| `POST /projects/{id}/tickets/{tid}/schedule/dispatch` | Owner-device fenced state/acknowledgment |
| `POST /projects/{id}/tickets/{tid}/schedule/cancel` | Cancel with expected version |

Rescheduling creates fresh schedule/execution IDs and invalidates the old context
fence. A queued/device-bound request must first be explicitly canceled. Claims
bind an execution permanently to one owner device; host election/reconnect does
not move it to a different member, device or controller. Shared writes use the
existing replicated revision fence plus schedule version compare-and-swap.

States are `waiting_for_runner`, `awaiting_approval`, `queued`, `executing`,
`blocked`, `completed`, and `canceled`. Unacknowledged runner status becomes stale
instead of claiming current availability. Earlier nonterminal ordered requests
block later ones for the same member/project. Dependencies need a completed
execution or a Done ticket; canceled/failed work does not satisfy dependencies.
Cycles are rejected. Assignment generation, member/device/project membership,
repository and ticket text changes block dispatch and require deliberate review.

The Team desktop service polls eligible requests with locally recorded approvals.
Individual's scheduler entry point applies normal local gates and honors the
locally approved earliest-start/deadline. Offline/at-capacity runners wait;
unavailable approvals wait. Disconnected/revoked workspace access authorizes
nothing. A lost dispatch response recovers the local committed execution ID
before acknowledging progress. An accepted process is never silently migrated,
restarted or killed by a schedule cancellation or membership revocation.

Cancellation prevents further shared claims. A dispatch already accepted by
Individual can finish; cancel the local run explicitly if that is intended.
Authority refresh and local acceptance are separate transactions in separate
security domains, not a distributed atomic process-launch transaction. A malicious
Workspace Host can lie about membership and scheduling data or hide revocation;
it still cannot manufacture an owner-device signature, local approval or broader
policy. Short-lived, exact-context approvals bound this trust. Cryptographic
identity does not make ticket text trustworthy.

## Optional CLI connector scheduling

Phase 1's `integration-v1` grant remains metadata-only. A separate opt-in
`execution-dispatch-v1` grant can retrieve an already-approved execution and
request its idempotent dispatch. It cannot mint approvals, choose policies, read
questions/handoffs/raw logs, modify settings or launch an unapproved task.

```sh
werkbord-team connector connect --execution \
  --runner http://127.0.0.1:7420 \
  --controller-token-file /absolute/user/individual/token \
  --access-file /absolute/user/connector/dispatch-token
```

Add `"executionTokenFile": "/absolute/user/connector/dispatch-token"` to the
existing private connector configuration. Keep its original `accessTokenFile`
for synchronization. The connector accepts only the dispatch scope, matches the
locally approved task to its persistent Phase 1 association, and reconciles Team
eligibility before each dispatch. Nothing new is installed implicitly.

The controller's neutral local APIs are `/api/execution/v1/preview`, `approvals`,
`approvals/{executionId}` (including revoke) and `dispatch`. `execution-local-v1`
authorizes authenticated local desktop interaction; `execution-dispatch-v1`
permits only approval lookup and dispatch. Neither extends a metadata or legacy
grant. Both remain revocable literal-loopback grants. Unknown fields/scopes fail
closed. No shell, arbitrary path, environment, command or HTTP proxy is accepted.

## Validation and operational limits

Tests exercise both real APIs with signed enrolled clients and a fake local agent,
concurrent local starts, lost acknowledgments, restart recovery, approval/policy
changes, signature forgery/tampering/replay/expiry, another member's refusal,
offline/revoked work, conflicting assignments, dependency/order/timezone/missed
policies, cancellation/rescheduling, cluster election/no quorum and existing
Individual scheduling. The desktop browser harness covers the effective-policy
screen, start, privileged question answer, stop and scheduled preapproval on
desktop and phone layouts.

Real Claude Code/Codex accounts, live agent sessions, multi-machine customer
networks, sleep/wake and production deployment are not acceptance-tested by these
fixtures. Adapter tests verify existing protocol behavior. Recurring schedules,
live pause and automatic runner failover are intentionally unavailable; explicit
one-shot schedules and stop/restart are supported. Approval and dispatch records
are retained locally to prevent ID reuse; normal product backup/recovery rules
apply.
