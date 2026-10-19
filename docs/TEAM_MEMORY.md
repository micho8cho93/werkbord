# Werkbord Team shared memory (Phase 2A)

Team memory is a separate shared coordination domain. Individual memory remains private on its owner’s Werkbord. Team stores only bounded, human reviewed statements and their coordination metadata in the Team workspace database. This feature does not make memories into instructions an agent can execute.

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

Proposals contain schema, exact scope, content, optional supersedes, and clientId. Content contains only kind, bounded summary, classification, verification, and source (kind, stable id, optional source revision). Author and device identity are taken from authentication, never from the request. The server adds version, state, timestamps and approval identity/version. Delete clears the returned summary and leaves a tombstone. Updates write a versioned audit revision in the same transaction.

A local owner approves a share through the authenticated loopback Individual controller API POST /api/memory/v1/shares with a proposal, reviewed=true, and an expiry. The owner asks the controller for a distinct grant using POST /api/local-access with scope=memory-v1 and the exact workspaceId. That credential can only call GET /api/memory/v1/shares?workspaceId=…; revoke it through the existing owner-only local access revocation endpoint. The Team-side ApprovedMemory bridge refuses any other grant, workspace or unsupported schema. A user-initiated connector call sends one bounded page and never approves or publishes the Team proposal. Continue a multi-page pass with the returned cursor; start each new synchronization pass at the first page. Repeated proposals are idempotent for the authenticated author and clientId.

The persistent Team migration is versioned with Team’s existing SQLite/rqlite schema. Replicated writes use the existing single transaction log; no separate memory service, Team version or release is introduced. See TEAM_SECURITY.md for the broader Team security model.
