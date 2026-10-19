# Individual ↔ Team memory publication (Phase 2B)

Memory stays local by default. A member's existing Team connector delivers small,
explicit projections to a customer-owned Workspace Host using the enrolled device's
signed requests. There is no vendor backend, new synchronisation daemon, database
replication, or Team connection to Individual's database. Team does not gain an
execution grant or the controller's owner credential.

## Choices and classification

Individual → Settings → **Individual memory** lists sharing destinations, eligible
records, proposed statements, delivery states and acknowledged Team records. Add
an exact local project, canonical repository, Team workspace ID and Team project
ID. Those identities are visible in Team → Settings → **Team memory**.
The grant is initially Git metadata only. Each workspace/project has independent
choices; enabling one does not enable another.

| Classification | Rule |
| --- | --- |
| Private individual knowledge | Default; personal, sensitive, secret, unverified and non-allowlisted records have no outgoing projection. Individual retrieval continues to use its private store. |
| Eligible | Current, verified, allowlisted Git/PR facts from an enabled, uniquely associated repository with a completed successful freshness checkpoint. |
| Awaiting user approval | A verified implementation summary/solution or architectural decision, after that project's separate semantic sharing choice. |
| Queued / suggested / approved | A sanitised immutable projection is journalled locally; a signed receipt records the actual Team review state. Delivery is not publication. |
| Published | The authorised Team scope reports publication of a version approved by another authorised human. |
| Revocation pending / revoked / deleted / superseded | Explicit withdrawals or supersessions; the UI distinguishes queued withdrawal from acknowledged removal. |

Automatic export constructs a new statement from a closed allowlist:

- `git_head`: a validated Git object ID matching the successful freshness
  checkpoint's HEAD, rendered as “Observed Git commit …”.
- `git_pr` / `pull_request`: a positive numeric PR identity whose credential-free
  HTTPS reference belongs to the exact repository and appears in the successful
  authenticated PR observation; rendered with a repository commit independently present in the observed HEAD/refs.
  PR titles, descriptions, comments, branches and URLs are not copied.
- Scope: canonical repository identity and the explicitly selected workspace and
  project IDs. Provenance: opaque local memory identity and source Git revision.

Stored summaries, details, branches, file lists, paths, excerpts, run/handoff IDs,
private sessions, transcripts, preferences, source code, credentials and environment
variables do not enter this automatic projection. Unknown types are denied.
An LLM is not involved. Existing text/secret-marker checks are defence in depth,
not the export policy.

Semantic sharing is off by default for **each destination project**. After enabling
it, the owner sees the local candidate and writes the exact sanitised statement
for review. Approval covers that memory's current source version; stale reviews
fail. No free-text semantic statement is automatically copied from the private
memory into a proposal. Individual's approval permits transmission only: Team's
existing permissions and independent human approval still govern publication.

Repository mismatches, duplicate local repository associations, suspended or
incomplete freshness checks, archived Team projects, changed Team repository
bindings, removed memberships and revoked devices fail closed. Discovery is
bounded to 64 registered repositories and 64 sharing destinations; exceeding the
repository bound requires reducing ambiguity before publication. Disabling an
existing destination remains possible when its repository is unavailable.

## Connection and lifecycle

1. Create the exact local sharing choice. The existing Individual knowledge
   maintenance loop reconciles approved destinations even while Team is offline.
   Reading the publication journal also reconciles current durable source state.
2. **Connect this workspace** creates a separate `memory-v1` local-access grant.
   Paste it into that workspace's Team memory connection disclosure. Team checks
   the grant's workspace binding before storing it sealed alongside that member
   device's existing enrollment material. Runner connection never enables memory.
   Reconnection to another workspace requires a separate grant and consent.
3. The existing member connector cadence reads only the workspace's projection
   journal over the guarded loopback bridge. A page has at most 50 outbound events and status checks combined,
   additionally bounded to 250 KiB of encoded JSON. The grant cannot read private memories, change choices,
   approve statements, launch work, or access another workspace.
4. Creating a projection, allocating its monotonically increasing workspace
   sequence and inserting the outbound event happen in one local transaction.
   Event IDs remain stable across retries; each event records its source memory
   identity and source version, and Git facts include their Git object revision.
5. Team rechecks current member/device/project/repository authority inside the
   write transaction. It enforces contiguous sequencing per workspace/member/
   enrolled device, rejects regressing source versions and conflicting event ID
   reuse, and commits the operation with its durable receipt atomically. Sequence
   and idempotency checks survive Host restart and replicated-store failover.
6. Individual advances its durable acknowledgement cursor only after the signed
   Host response and local receipt transaction succeed. A lost Host response or a
   lost local acknowledgement safely repeats the same event. An already processed
   proposal returns its existing record, never another shared memory.
7. Status checks rotate through acknowledged projections and refresh their current
   Team state. They do not import Team statements into Individual's private memory
   or treat them as agent instructions. This phase implements outbound publication
   and review-state reconciliation, not bidirectional private-store replication.

There is no journal TTL. Reopening the local database resumes after restart or a
long offline period. The journal accepts up to 2,048 outstanding proposals per
workspace; additional proposals wait for capacity. Withdrawals can still be queued
under proposal pressure. Neither pressure nor a Team failure prevents private
memory extraction, retrieval or correction. Reconciliation projects the latest
durable source revision; transient intermediate revisions between observations
are not a complete edit audit.

Each workspace's grants, outbound journal, acknowledgement sequence and rotating
status cursor are independent. One workspace's outage does not advance another's
cursor. Within a workspace, an unauthorised or conflicting oldest event stops
later delivery until authority is restored; it is never silently skipped.

## Corrections, deletion and withdrawal

A source correction receives a new event ID and higher source revision. A
correction of an unpublished suggestion withdraws its predecessor and starts a
new review. A correction of a published statement proposes an explicit
`supersedes` link; the predecessor becomes superseded when the replacement is
approved and published through the existing Team workflow. Semantic source changes
require another local review. Deleted, archived, invalidated or no-longer-eligible
sources are explicitly withdrawn on the next reconciliation. A source superseded
by a different local record is withdrawn rather than silently leaving stale
shared knowledge active.

**Revoke publication** persists a withdrawal event. A source author/device can
withdraw its own projection through this protocol without gaining curator delete
permission over other people's memories. Disabling a project queues withdrawals
for its projections. An unsent proposal that is no longer authorised is replaced
by a fixed cancellation tombstone before the bridge returns it; its statement text
is not sent. If the original event committed but its reply was lost, the same
cancellation identity withdraws that record idempotently. A request already in
flight at the instant of disabling can have reached Team; the subsequent
withdrawal resolves that uncertainty.

An explicit per-record revocation suppresses that source revision. A new verified
source revision, or an explicit disable/re-enable of the project, can create a new
projection. An association cannot silently be rebound to another local project or
repository. Team curators can inspect, approve, publish and delete records from
Team's settings using the existing role permissions. A second human is required
to approve the author's proposal; the connector never auto-approves.

Disable publication and wait for withdrawals to be acknowledged **before** using
**Disconnect this grant**. Disconnect immediately prevents further bridge access,
including pending withdrawals. If project authority or the device has already
been revoked, the connector cannot bypass that decision to withdraw records; a
currently authorised Team owner must remove them. Team's settings explain these
limits and expose current records.

## Privacy and retention limits

A Team memory becomes visible under the existing exact-scope read permissions
(including suggestions where the reader has review access). Only published
records qualify for published-only retrieval. Human review can still mistakenly
approve sensitive text; read the exact statement and destination before sharing.
The structured automatic allowlist is intentionally much narrower than a general
secret detector.

Withdrawal clears the live record summary and prevents ordinary active retrieval.
It does not remotely erase material a member already read, exported, copied,
incorporated into other work, or kept in a backup. Team's existing immutable audit
revisions retain historical contents under the workspace's storage/backup policy;
this is access withdrawal, not cryptographic erasure. Local approved projections
and receipts also remain in the owner's existing controller database. Workspace
operators control their storage and retention. No promise of erasing those copies
is made.

## Verification

`make check` covers the product and architectural boundaries. Focused scenarios:

```sh
go test ./internal/service -run TestMemoryPublication
go test ./cmd/werkbord-team -run TestMemorySync
go test ./internal/api -run 'Memory.*Grant|MemoryPublication'
```

The integration tests host two independent Individual controllers, enroll distinct
signed devices into one Team workspace, disconnect and reconnect, reopen the
local database, lose delivery/withdrawal replies, replay receipts, publish through
a second human, correct/supersede, withdraw, cancel before delivery, and revoke
device authority. Privacy tests exercise unknown/non-shareable fields, per-project
semantic opt-in and exact-version review, ambiguous/suspended repositories and
independent workspace streams. Neither product imports the other's database or
execution packages.
