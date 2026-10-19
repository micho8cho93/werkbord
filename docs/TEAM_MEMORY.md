# Werkbord Team shared memory

Phase 2A defines the schema and policy below. [Phase 2B publication](MEMORY_SYNC.md) adds the durable selective connector, settings, corrections and explicit withdrawal lifecycle. The manual Phase 2A share endpoint remains compatible; its expiry rules apply only to that older manual path.

Team memory is a separate shared coordination domain. Individual memory remains private on its owner’s Werkbord. Team stores only bounded statements, allowlisted metadata and their coordination metadata in the Team workspace database. This feature does not make memories into instructions an agent can execute.

## Scope and trust boundary

Each memory has exactly one scope: workspace, project, repository, or cross_repository. A project scope is identified by workspace and project IDs. A repository scope also contains the canonical, credential free identity validated by internal/integration.RepositoryIdentity; local paths, credentials, query strings, fragments and traversal are refused. Cross repository scopes list 2–8 distinct projects and repository identities. Every operation checks current access to every listed project in the same Team database transaction. Workspace scope is exact, not a wildcard.

Team authenticates with the existing workspace member or enrolled device credential. Remote device calls retain the existing Ed25519 request proof, quorum fence, nonce replay protection and current membership/device checks. Project access uses the existing project roles and preserves not found behavior for a project the member cannot see. Repository records must still match the project’s current repository identity. An archived project cannot accept memory changes. A stale replica cannot authorize revocation; memory reads during a network partition require a fresh authorization view and writes require quorum.

Team has no connector controller token. The local owner may approve an immutable outgoing projection and issue a separate memory-v1 local grant bound to one Team workspace. That grant is loopback only, revocable, and can read only unexpired, team-classified approved projections from the local owner’s share store. The normal integration-v1 grant retains its existing route and body allowlist. It cannot read memory, and the memory grant cannot import tasks, see execution metadata or approve sharing. The memory endpoint does not query or export private Individual memory records: a person supplies the sanitized statement and confirms review explicitly. Each share expires within 30 days; removal or expiry locally does not delete an already shared Team record.

The connector sends a proposal through its enrolled device and the existing signed Team API. It sends no Individual controller token, model or agent credentials, arbitrary file/directory paths, shell/Git requests, raw conversation or terminal transcript, handoff, or diff. The wire DTO contains no fields for those values. Text is length bounded, plain text, checked for common secret, path, transcript and diff markers, and must be explicitly reviewed locally. Those checks are an additional filter, not a substitute for the person’s review.

## Permissions

Permissions are checked against current authority on every operation, in the service/database transaction.

| Operation | Permission | Contributor | Reviewer | Project owner | Workspace owner/admin |
| --- | --- | --- | --- | --- | --- |
| Read | memory.read | yes | yes | yes | yes |
| Read restricted classification | memory.read_restricted | no | yes | yes | yes |
| Propose | memory.propose | yes | yes | yes | yes |
| Publish approved record | memory.publish | no | yes | yes | yes |
| Edit | memory.edit | no | yes | yes | yes |
| Approve verified version | memory.approve | no | yes | yes | yes |
| Delete | memory.delete | no | no | yes | yes |
| Approve/publish policy | memory.policy plus approve/publish | no | no | yes | yes |

A proposal is only a suggestion. Approving requires a verified version and a human member different from its author. A policy additionally requires the stronger policy permission. Publishing requires the approval to cover the exact current version. Editing clears approval and returns the record to suggested; changing a record therefore cannot preserve its authority. Preference, observation, convention and policy are distinct kinds. No preference or observation is upgraded by repetition, verification or publication. A convention is not an enforceable policy; policy authority requires a separately proposed policy and explicit human approval. Superseding the previous published record, its new record and their audit revisions are committed atomically.

The `restricted` classification is omitted from results unless the reader also has `memory.read_restricted` for the full scope. `team` records use `memory.read`. The local Individual sharing grant exports only `team` classified projections.

## werkbord.memory/v1

The schema lives in the data-only shared internal/integration package. It is a separate protocol namespace and path from werkbord.integration/v1. Decoders reject unknown fields, trailing JSON, invalid nested values and unsupported versions. The Team API is under /api/team/v1/memory:

- POST /proposals: create an idempotent suggestion (clientId is scoped to the authenticated author).
- POST /query: read one exact scope; published=true returns only published records. Pages contain at most 50 records and include a stable ID cursor when more remain. Use the cursor only within that read pass; start with an empty cursor for each replacement read so newly approved records are found. Apply a replacement only after all pages are collected.
- POST /{memoryId}: version checked edit, approve, publish, or delete.

Proposals contain schema, exact scope, content, optional supersedes, and clientId. Content contains kind, bounded summary, classification, verification, source (kind, stable id, optional source revision), and optional Phase 2C claim/branch/task/PR context. Author and device identity are taken from authentication, never from the request. The server adds version, state, timestamps and approval identity/version. Delete clears the returned summary and leaves a tombstone. Updates write a versioned audit revision in the same transaction.

A local owner approves a share through the authenticated loopback Individual controller API POST /api/memory/v1/shares with a proposal, reviewed=true, and an expiry. The owner asks the controller for a distinct grant using POST /api/local-access with scope=memory-v1 and the exact workspaceId. That credential can read GET /api/memory/v1/shares?workspaceId=… and, in Phase 2B, read/acknowledge only its workspace-bound publication journal; revoke it through the existing owner-only local access revocation endpoint. The Team-side ApprovedMemory bridge refuses any other grant, workspace or unsupported schema. A user-initiated connector call sends one bounded page and never approves or publishes the Team proposal. Continue a multi-page pass with the returned cursor; start each new synchronization pass at the first page. Repeated proposals are idempotent for the authenticated author and clientId.

The persistent Team migration is versioned with Team’s existing SQLite/rqlite schema. Replicated writes use the existing single transaction log; no separate memory service, Team version or release is introduced. See TEAM_SECURITY.md for the broader Team security model.

## Phase 2C: freshness and reconciliation

Human approval covers a statement, not the continuing state of a repository. Read
responses now include a separate `freshness` object. The original content,
verification, approval and version stay available for attribution and audit.
Consumers must examine freshness even when `state` is `published` or content
verification is `verified`:

| Status | Meaning |
| --- | --- |
| `source_matches` | A recent, currently authorized source report matches the statement's exact branch revision. This does **not** establish correctness or deployment. |
| `stale` | Relevant project activity invalidated the source snapshot, its revision changed, its PR closed/was superseded, or its repository binding changed. |
| `uncertain` | Verification is unavailable, a participating source is offline/expired/revoked, or there is insufficient branch/revision evidence. |
| `conflict` | Comparable claims or source reports disagree. Both remain attributable; no authoritative revision is selected. |

Every project mutation advances a generation and records an invalidation time in
its existing transaction. Related memories bind to the project's repository and
generation when proposed or edited. Retrieval recomputes freshness against the
current authorization view, project generation, invalidation time and source
snapshots. It never waits for a worker tick. A Git report that omits a new head
cannot reaffirm an older matching head. Repository changes require an explicit
memory edit/review to establish the new binding. Cross-repository records require
current access to **every** constituent project and remain conservative when
there is no single verifiable source revision.

For comparable knowledge, include optional `content.context`:

```json
{"claim":"storage-engine","branch":"feature/sqlite","taskId":"ttk_example","pullRequest":7}
```

`claim` is a stable, explicitly chosen subject key. Different summaries with the
same claim, kind, classification, exact scope and branch are **potential**
contradictions; Team preserves both and marks the conflict. Team performs no
semantic inference about unrelated free text. Different branches retain their
own implementations. Identical proposals from the same author and device with
this context reuse the existing record, even with a new client key; durable key
aliases reject later reuse with different contents. Identical reports from
different authors retain both provenances and link through `freshness.duplicateOf`.
Delivery event IDs, per-source revisions and per-device stream sequences retain
the Phase 2B replay guarantees.

### Source reconciliation

Team never executes Git, fetches GitHub with a member's credentials, or runs a
model. Existing authenticated `ReportGit`, branch reports and ordered progress
snapshots from members' independent controllers provide recovery from missed PR
notifications. A source integration may also send a bounded snapshot to:

`POST /api/team/v1/projects/{projectId}/memory/evidence`

```json
{
  "scope":{"workspaceId":"tw_example","kind":"project","projectId":"tpj_example"},
  "repository":"github.com/acme/shop",
  "branch":"feature/sqlite",
  "revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "authoritativeRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "pullRequest":7,
  "prState":"open",
  "targetBranch":"main",
  "available":true,
  "observedAt":"2026-10-19T12:00:00Z",
  "sequence":1
}
```

The reporter is taken from authentication. `memory.evidence` uses the existing
project `git.report` authority, rechecked in the write transaction. The repository
must match the project's canonical identity. `authoritativeRevision` is optional:
include it only when the source has obtained the authoritative remote revision
for this branch using its own authenticated Git metadata. Team records this as
**member-supplied evidence**, not independent provider attestation. The backend
cannot verify a Git provider directly because it holds no Git/provider credentials.
Disagreeing reports never elect one revision by arrival order. A newer normal
branch snapshot can invalidate an earlier dedicated evidence snapshot.

Sequences increase per workspace/project/member/device/branch/PR stream, including
unavailability reports. An identical sequence and payload is idempotent; a changed
payload, older sequence or backwards observation time is rejected. Set
`available:false` when source verification fails. Evidence expires after 24 hours;
reads become uncertain without requiring an online author or a successful worker.
A source that cannot be refreshed never remains implicitly fresh. Fresh snapshots
can reconcile after reconnecting. All participating source reports must remain
available and authorized; conflicts require explicit review rather than an
assumption that an offline reporter agrees.

`freshness.integrationState` is `branch_only`, `entered_target` (a reported merge),
or `unknown`; it never asserts deployment. An open PR describes work on its source branch. A merged PR records entry into
its named target branch; neither state proves deployment, correctness or current
code in that target. Memory branch context is never rewritten to `main` by a
merge. Target branch facts require their own source revision. Related ticket and
review workflow states are returned separately; finishing a task does not prove
that a reviewer approved the implementation. Closures and replacement PRs make
old PR context stale. A merged ticket report contradicted by a later non-merged
source report is a conflict.

### Project retrieval and explicit resolution

These endpoints use the existing member/device authentication and fresh quorum
checks. MCP grants cannot use these human memory endpoints.

- `GET /api/team/v1/projects/{projectId}/memory?q=storage&after=mem_cursor` searches
  project, repository and cross-repository relationships. Search is a literal,
  case-insensitive substring, at most 200 bytes. Pages contain at most 50 records.
- `GET /api/team/v1/projects/{projectId}/memory/{memoryId}` reads one live record
  with computed freshness. Deleted, superseded and inaccessible records return 404.
- `GET /api/team/v1/projects/{projectId}/memory/{memoryId}/history` reads retained
  content versions, freshness transitions and source evidence with reporter
  provenance, including supersession links. Historical restricted content requires
  current restricted-read authority even if the current version is public.
- `POST /api/team/v1/projects/{projectId}/memory/{replacementId}/resolve` accepts
  `{"version":3,"replaced":[{"id":"mem_competing","version":1}]}`. A curator
  chooses an already published, human-approved replacement and pins up to eight
  competing versions. Scope, claim, branch and classification must match.
  Edit/approve/publish authority is required; policy replacements additionally
  require policy authority and a human-sourced policy. Supersession, links and
  audit revisions commit atomically. Exact retries are idempotent; concurrent
  unseen changes cause a conflict. New content goes through the normal proposal,
  approval and publication steps before resolution.

Every result rechecks current access to the full memory scope, classification and
repository binding. The legacy exact-scope `/memory/query` also returns computed
freshness. Revoking a member/device or project membership immediately revokes
search, direct reads, history and evidence authority, including cached actors.
Approved Team policy cannot be edited into a preference or inference, superseded
by one, or withdrawn/corrected through its author's Individual publication stream.
Such a stream receives a permission error until the Team curator handles the
policy; it cannot silently change workspace policy.

### Background bounds and retention

The existing Team server owns one cancellable worker. It starts on server startup,
then runs every 30 seconds, with a five-second transaction deadline and at most
50 memories per tick. The least recently checked records go first, so restart and
multiple Workspace Hosts use the same durable scheduling state and quorum writes.
Worker failures retry on later ticks. Retrieval computes freshness independently,
so worker lag or a partition cannot bless stale cached knowledge. No LLM executes
in this worker. Optional semantic summarization would require a separately,
explicitly authorized Individual execution; no automatic summarization is added.

Retention is deliberately bounded:

- Current records, tombstones, replacement links, stream high-water marks and
  replay keys remain for the lifetime of the workspace, within existing quotas.
- Content audit payloads retain at most the latest 20 revisions per memory and
  90 days of history, preserving the current record.
- Freshness audit retains at most 64 transitions per memory and 90 days.
- Source evidence audit retains at most 64 observations per reporter stream and
  90 days; history returns at most 64 matching observations. Each tick additionally
  removes up to 500 expired evidence audit rows.
- There are at most 10,000 current evidence streams per workspace and 32 reporters
  per project/branch/PR. At most 10,000 proposal aliases are retained per workspace.
  Existing 10,000 memory revision/delivery receipt admission quotas still apply;
  reaching a quota rejects new work rather than deleting replay protection.
- Expired historical delivery receipt payloads are compacted to record IDs and
  sequence keys. Replays retrieve the current record after rechecking authority.
  Content and freshness compaction proceeds with the bounded memory sweep; reads
  enforce the age limit immediately, even before physical cleanup.

This is deterministic compaction, not semantic merging or archival backup.
Customer-owned database backups have their separate configured retention policy.

### Two-member demonstration

`TestMemorySyncTwoControllersOfflineRestartReplayAndRevocation` now enrolls two
**different members**, each with an independent Individual API, database, device,
runner manager and agent adapter. Both selectively publish sanitized knowledge;
the test checks both author identities on Team records, unchanged private
summaries (including deliberately private path/credential markers), and refusal
to read the other controller's private record. One member's offline delivery,
restart and lost acknowledgement do not prevent the other member publishing.
Corrections, deletion and device revocation are exercised without sharing an
execution environment.

`internal/team/service/memory_reconciliation_test.go` additionally exercises
concurrent branch implementations, contradictory member reports, duplicate aliases,
explicit resolution, missed PR/branch events, unknown heads, offline/unavailable
sources, revoked members/devices and stale legacy/project retrieval.
`internal/team/store/memory_reconciliation_test.go` verifies the bounded sweep and
historical compaction. These run with the ordinary Team/integration tests and the
repository's architecture boundary tests.
