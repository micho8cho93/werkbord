# Memory implementation audit: Phases 1–4

Audited: 19 October 2026, on `main` at `e4a3791` (4.63.0-preview.1), against the code, not the documentation.
Fixes from this audit ship as **4.64.0-preview.1**. Scope: the nine memory commits `55da439..e4a3791` (180 files, +22,613 lines,
of which 8,297 are tests) and what they touch.

The audit method was: read the implementation paths end to end; try to break each documented guarantee with a test that fails
if the guarantee is false; measure rather than estimate; fix what was demonstrably wrong; and say plainly what was not tested.
Every new test is named below and lives in the repository (`*_audit_test.go`, `memory_screen_test.go`, `memory_scale_test.go`).

## 1. Implemented features

| Phase | What exists | Where |
| --- | --- | --- |
| 1A Individual memory | Private records in the controller's own `devboard.db`: exact `(owner, project, repository)` scope, closed authority vocabulary (observation, preference, decision, policy), compare-and-swap versions, exact-evidence deduplication, FTS5 literal search, quotas (10,000 records / 32 MiB per owner by default, hard global ceilings), explicit retention | `internal/domain/memory.go`, `internal/service/memory.go`, `internal/store/sqlite/memory*.go`, migration `0017` |
| 1B Git freshness | One serial worker (30 s, plus event wake-ups) compares each code-linked fact's source commit, branch, paths, PR and run with committed Git state, authenticated PR metadata and the durable event log; a change means "needs verification", never "false"; resumable, bounded checkpoints | `internal/service/memory_freshness.go`, migration `0018` |
| 1C Knowledge | Deterministic extraction of structured handoff fields and run lifecycle into unverified suggestions, evidence compaction, user decisions/preferences/facts/policies saved with server-assigned approval, corrections, pins, archive, versioned deletion with evidence tombstones, bounded retrieval with a token budget, Project Settings → Memory | `internal/service/memory_knowledge.go`, `memory_context.go`, `internal/api/memory.go`, `web/src/lib/ProjectMemory.svelte`, migration `0019` |
| 2A Team memory | Workspace/project/repository/cross-repository scoped shared memory in the Team database: propose → independent human approval → publish, role permissions checked in every transaction, supersession, restricted classification, audit revisions | `internal/team/{domain,service,store,api}/memory*.go`, `internal/integration/memory.go`, Team migration `0022` |
| 2B Publication | Individual → Team: explicit per-project destination, closed automatic allowlist (a Git commit and PR number only), reviewed statements for decisions/solutions, durable ordered journal, a separate workspace-bound `memory-v1` grant, signed delivery, receipts, corrections and withdrawal | `internal/service/memory_publication.go`, `internal/team/connector/memory.go`, `internal/team/server/daemon_memory.go`, migrations `0020`–`0021`, Team `0023` |
| 2C Team freshness | Freshness computed at read time from reported branch/PR/evidence snapshots, conflicts preserved rather than resolved by arrival order, explicit curator resolution, bounded retention and compaction | `internal/team/store/memory_reconciliation.go`, Team migration `0024` |
| 3A Salie memory | `retrieve_memory` (read-only, `memory:read`, assistant principals only): Individual memory through the same gate as the UI, Team memory through the desktop's reviewed bridge, with citation guidance | `internal/appops/memory.go`, `desktop/internal/teamlink/assistant_memory.go` |
| 3B Suggestions | A pure rules engine (restrictions forbid or require; history only orders what is left), at most two cards with evidence, feedback kept as a private bounded preference | `internal/domain/recommend*.go`, `internal/service/recommend.go`, `web/src/lib/recommend/` |
| 4A Relationships | Versioned `repository_relationship` observations in the same store (no graph database), manifest discovery from explicit Git URLs only, endpoint-checked traversal; Team approved relationships through existing memory approval | `internal/service/relationships.go`, `internal/integration/relationship*.go`, `internal/team/service/relationships.go` |
| 4B Initiatives | Team coordination records that reference existing projects, tickets and published memory; visible only to a reader who may see every linked project; never executes | `internal/team/service/initiatives.go`, Team migration `0025`, Team console |

## 2. Architecture findings

* **Boundaries intact.** All 38 `internal/archtest` tests pass, including: the controller links no Team code, Team imports only the
  shared allow-list, Team never executes anything and holds no Git/agent credential, the assistant offers no route to runs, Git or
  settings, suggestions only read, and the desktop module links neither Team nor an execution engine.
* **No new database, service, daemon or model.** Individual memory is tables in the existing SQLite file, using the existing migration
  loader, pre-upgrade backup and transactional writer; Team memory is tables in the existing Team store. There are two new
  background loops in the controller (the 5-second knowledge worker and the 30-second freshness worker) and one in Team (30 s).
  None of the memory code imports a provider, agent, runner or `os/exec` (checked by import grep and by `TestOnlyProvidersStartProcesses`).
* **Reuse is real.** Repository identity is `integration.RepositoryIdentity` everywhere; Git reads go through `GitControl`; the Team
  protocol lives in the data-only shared package; publication rides the existing signed member connector cadence (about 10 s) and
  a `local-access` grant, not a new channel.
* **One duplicated abstraction found and fixed:** the credential screen existed only inside Team's wire validation. Individual
  extraction, manual saves and Team now share `integration.LooksSensitive`.
* **Migrations are additive.** Individual `0017`–`0021` and Team `0022`–`0025` only create tables, indexes and triggers, apart from two
  `ADD COLUMN … DEFAULT` statements in `0019`. Tests: pre-memory upgrade and restore (existing), Phase 1A upgrade (existing) and a
  **new staged upgrade from schema 18 with 50 live rows** that checks the data, defaults, freshness checkpoint, `integrity_check`,
  `foreign_key_check` and the FTS index (`TestAuditUpgradeFromPhase1BKeepsMemoryAndIndex`). An older build refuses a newer database.
* **Backward compatibility:** the new `origin` field is additive JSON; the manual-share endpoint stays compatible.

## 3. Verified behavior

Each row names a test that fails if the claim is false. "new" tests were written for this audit; the others already existed and were
re-run (the whole suite passes, section 6).

| Requirement | Evidence |
| --- | --- |
| Individual and Team boundaries | `internal/archtest` (38 tests); `TestMemoryUnavailableOnOtherListeners` |
| Team cannot read private local memory, files, Git, agents or credentials | **new** `TestAuditLocalAccessScopesNeverReachPrivateMemory`: every owner memory/relationship/recommendation/publication/local-access route is refused to the `memory-v1`, `integration-v1`, `execution-*` and unknown scopes; a `memory-v1` grant reaches only its own workspace's journal, with no extra body fields. Wire DTO closure: `TestMemoryProtocolIsClosedAndIndependent` |
| Cross-project authorization and revocation (Team) | `TestMemoryIsolationAndRevocationEveryOperation`, `TestMemoryRetrievalRechecksRevocationAndRestrictedHistory`, `TestRelationshipGraphApprovalAndAccessRevocation`, `TestInitiativeCoordinatesExistingWorkAndRevokesWholeScope`, `TestInitiativeMemoryClassificationAndCredentialRevocation`, `TestMemoryProjectSearchRechecksEveryRepositoryRelationship` |
| Cross-project authorization (Salie) | `TestConnectedMemoryObeysEveryAssistantProjectGrant`, `TestMemoryContextPermissionScopeRelevanceAndBudget` |
| Secrets cannot enter or leave | **new** `TestLooksSensitiveCatchesCredentialsAndLocalData` / `…LeavesOrdinaryEngineeringProseAlone`; **new** `TestAuditCredentialLookingClaimsAreSecretAndNotRetrievable`; **new** `TestAuditPublicationReviewScreensStatementsAndDeletionWithdraws`; automatic export allowlist `TestMemoryPublicationAllowlistAndSemanticReview` |
| Malicious commit/PR text | **new** `TestAuditPullRequestTextNeverEntersMemory` (a hostile PR title with an injection and an API key reaches no memory, evidence, checkpoint or retrieval). Commit messages are never read: memory code asks Git only for refs, SHAs and changed paths |
| Prompt injection cannot gain authority | Structural, and tested: results are JSON-escaped and cannot close their wrapper or open a call (`TestResultsCannotBreakOutOfTheirWrapperOrOpenACall`); memory is never in the system prompt or in agent runs; `TestSuggestionsAddNoPermissionToTheAssistant`, `TestNoSuggestionActionStartsExecution`, `TestHistoryCanOnlyOrderWhatTheRestrictionsLeftStanding` (400 random combinations); policy prose is read fail-closed and never as permission (`recommend_memory.go`); `TestMemoryPolicyCannotBeReplacedByPreference` |
| Edit/delete leaves no residual copy | **new** `TestAuditDeletedMemoryLeavesNoReadableResidue` and `TestAuditDeletingAMemoryLeavesNoReadableCopy` (read the database file after delete, correction and checkpoint). Both failed before the fix (section 4). Deletion also clears evidence excerpts (`TestKnowledgeEvidenceRestartDeleteAndCapacity`) |
| Stale facts invalidated | **new** `TestAuditFreshnessAfterRapidCommits`: 80 commits in a burst, 0 stale facts left current at 12 and at 60 anchors; plus rebase/reset/branch deletion/CI/PR changes in `TestMemoryFreshness*` |
| Missed events reconcile | `TestMemoryFreshnessMissedUpdatesReconcileAfterDatabaseRestart`, `TestMemoryFreshnessCompletedRunAndReplayGap`, `TestMemoryEvidenceMissedEventsOfflineAndReplay`, `TestMemoryPRMergeIsNotDeploymentAndMissedPRRecovered` |
| Offline Team never breaks Individual | Individual maintenance and journal reconciliation need no connection (`TestMemoryPublicationProjectDecisionNeedsExactReviewAndOfflineMaintenance`); **new** `TestAuditSlowTeamMemoryReadDegradesToUnavailable` |
| Idempotent delivery, ordering, revision validation | `TestMemoryDeliveryRechecksAuthorityOrderAndImmutableIdentity`, `TestMemoryCancellationWithdrawsOriginalWithLostReply`, `TestMemoryIdenticalUpdatesKeepOneRecordAndDurableAlias`, `TestMemoryCompactedReceiptStillReplaysCurrentRecord` |
| Contradictory decisions / concurrent PRs / two developers | `TestMemoryConcurrentBranchesContradictionsAndExplicitResolution`, `TestMemoryCompetingSourceRevisionsAndRevokedDevice`, `TestMemoryConflictingPRSnapshotsDoNotReopenClosedWork`, `TestMemorySyncTwoControllersOfflineRestartReplayAndRevocation` (two members, two controllers, restart, lost replies, revocation), `TestInitiativeAcrossRepositoriesAndIndependentMemberRunners` |
| Concurrency | `go test -race` over the memory suites of both products, and over the whole module (section 6): no race reported |

## 4. Defects found and fixed

| # | Defect (evidence) | Fix | Result |
| --- | --- | --- | --- |
| 1 | **Idle polling.** The 5-second knowledge worker observed every registered repository on every tick, which included a `gh pr list` GitHub request and 18 Git processes (counted with a logging `git` shim): 10 ticks made 10 GitHub requests (≈ 720 per hour per repository, ≈ 3.6 Git processes per second), and each tick also rewrote two cursor rows (72 ms wall, mostly fsync). `TestAuditKnowledgeTickDoesNotPollGitHubWhenIdle` failed | Knowledge observation is Git-only (`observeSource(…, false)`), at most once per repository per 30 s, and unchanged cursors are not rewritten | 0 GitHub requests; idle tick 72 ms → under 1 ms; knowledge-worker Git processes 3.6/s → 0.6/s per repository |
| 2 | **Superlinear search.** FTS ranking re-ran the `MATCH` for every candidate row: a common word took 107 ms at the default 10,000-record cap and **1.96 s at 50,000** | One FTS scan supplies filter and `bm25` rank | 1.9 ms at 10,000; 7.5 ms at 50,000 |
| 3 | **Deleted text stayed readable** in the database file (free pages and FTS segments), so delete was not erasure | `secure_delete` on the controller database (new `sqlitekit.Options.SecureDelete`, Individual only) and an FTS merge after delete or prune | file contains no copy after checkpoint |
| 4 | **Credential screen both too broad and too narrow.** `sk-` matched inside ordinary words ("task-runner", "risk-based") and rejected legitimate statements; AWS and GCP keys, JWTs, Slack and Stripe keys, npm tokens, connection strings with passwords and `token=…` were accepted | One shared `integration.LooksSensitive`: prefixes match at word start; added the missing forms; prose such as "token: refresh early" passes | 32 secret forms detected, 11 prose forms accepted (`TestLooksSensitive…`) |
| 5 | **Credential-looking agent text was retrievable.** A pasted bearer header or connection string in a handoff test/failure was stored as ordinary `private` memory, and the opt-in "include unverified" retrieval served it | Such claims are stored with sensitivity `secret` (kept as local evidence, never retrieved or published); saving such text by hand is refused with an explanation | `TestAuditCredentialLookingClaimsAreSecretAndNotRetrievable` |
| 6 | **A confirmed fact un-confirmed itself.** A fact saved in Project Settings → Memory became "unverified" at the next freshness pass (within 30 s) with no code change, because any unanchored observation in a Git project was treated as stale. `TestAuditConfirmedManualFactSurvivesTheNextFreshnessPass` failed | A statement a person wrote and confirmed (source `user`) names nothing in Git, so it is left confirmed; agent and import observations without an anchor are still invalidated | test passes; existing invalidation tests unchanged |
| 7 | `memory-v1` gate and handler each parsed `workspaceId` on their own, so a repeated parameter was ambiguous | The gate refuses a repeated parameter (no exploit found; hardening) | `TestAuditLocalAccessScopesNeverReachPrivateMemory` |
| 8 | Team memory at its 10,000-revision workspace cap answered `429 busy`, which reads as "try again shortly", but waiting never helps | The message states the limit and the remedy; status unchanged | message only |
| 9 | A Workspace Host that accepts a request and never answers could hold Salie for the transport's 20 s timeout on each of three or four sequential Team calls | Team memory read bounded to 10 s in total; degrades to "unavailable" | `TestAuditSlowTeamMemoryReadDegradesToUnavailable` |
| 10 | The memory and suggestion guidance (~700 tokens) was sent on every turn even to principals that cannot call the operations | Sent only when the catalog contains `retrieve_memory` / `recommend_next_actions` | `TestAuditMemoryPromptOverheadIsOnlyPaidWhereTheOperationsExist` |
| 11 | UX: Project Settings → Memory never said the memory is private and local; Salie's sources did not say whose memory an item was; a validation error ended with "Refresh the list before retrying" | Scope sentence added (with "do not save passwords or keys"); each source says **Your private memory** or **Team memory** (`origin`, set by the fetching layer, never by a stored record); the refresh hint only follows a conflict | `TestMemoryContext…`, `TestConnectedMemory…` assert origins |
| 12 | **Release blockers outside memory (stale tests).** CI and the release workflow run `make test-browser`, `test-team-desktop-browser`, `test-unified-desktop-browser` and `test-rqlite`; four browser scripts predated two earlier Team/UI commits (claim before work `655e9c6`; "Needs you" → "Workspace" and "Workspaces and devices" → "Devices & Hosting" `c014304`) and failed (`browser-bridge.cjs`, `browser-analytics.cjs`: "WB-38 is Available; only a ticket in progress can be submitted for review"; `test-team-desktop-browser.cjs` and `test-unified-desktop-browser.cjs`: navigation labels, and the removed "Agent controls" button). A tag would not have published | Scripts updated to the current flow (start work after a claim; the private-agent panel and "Review and start agent"; current labels; Initiatives added to the checked tabs); `BROWSER_BRIDGE_ONLY=1` added | all four pass; see section 6 |
| 13 | **Flaky Team tests** (pre-existing, found by running the replicated-store suite): `TestNothingSharedCarriesACredentialOrAMachine` treated a map key that is a random ticket ID as a field name and failed whenever the ID contained "ssh", "dir", "env" and so on (about 1 run in a hundred); `TestMemoryEvidenceMissedEventsOfflineAndReplay` assumed its memory was among the 50 checked by one worker tick, which is false on the shared cluster database | Generated-ID keys are skipped; the test ticks until its memory has been checked | the replicated-store suite passes |

## 5. Security risks and mitigations

| Risk | Mitigation in the code | Residual risk |
| --- | --- | --- |
| Secrets in memory | Handoff claims are structured fields only (no transcript or diff); credential screen classifies as `secret`; manual saves refuse; sensitive/secret never retrieved, listed for the assistant, or published | The screen is a pattern filter. An unusual secret format, or a secret in free prose the screen does not recognise, is stored as private local memory. Local memory is protected by the data directory's permissions, not encrypted per record |
| Secrets leaving to Team | Automatic export is a closed allowlist (commit SHA, PR number); anything else is a statement a person writes and approves for that exact version, screened again by Team's wire validation; Team needs a second human to publish | A reviewer can approve text that contains something sensitive and no pattern matches; withdrawal is access removal, not erasure of what was read |
| Team reaching local data | Separate `memory-v1` grant bound to one workspace, loopback only, revocable, reading only that workspace's journal; Team has no Git/agent/credential code (archtest) | none found |
| Prompt injection through memory | Memory is data: JSON-escaped in results, labelled untrusted, absent from the system prompt and from agent runs; rules treat prose only as a restriction; every state change still passes its confirmation gate | A model can still be influenced by text it reads (including a Team policy or an initiative objective written by a colleague). Defence is the gates, not detection |
| Stale authority | Individual: worker invalidation plus live HEAD check for HEAD facts; Team: freshness recomputed at every read | See U6: a ≤30 s window for non-HEAD code-anchored Individual facts after an external commit |
| Residual copies on delete | `secure_delete` and FTS merge; evidence excerpt cleared; replay tombstones hold hashes only | Backups and pre-upgrade copies, the original run/handoff, the local publication journal (U3) and Team's audit revisions (90 days) are separate copies. There is no Individual audit record of a deletion, by design |
| Cross-member data in Initiatives | An Initiative is invisible unless the reader may see every linked project | Free text is not credential-screened (as for tickets) |
| Abuse of limits | bounded pages, byte and record quotas, 1 MiB manifest, 256 KiB messages, bounded Git diffs and refs | Team's 10,000-revision cap can be exhausted by one member (U5) |

## 6. Test results

Everything below was run on this Mac (Apple silicon, macOS, Go 1.27.1, Node 22.15) against 4.64.0-preview.1 plus these changes.

| Suite | Result |
| --- | --- |
| `go test ./...` (`WERKBORD_SKIP_RQLITE=1`, as `make test` sets it) | all 75 packages with tests pass (27 have none) |
| `go vet ./...`, `gofmt`, `svelte-check`, `eslint` (`make lint`) | clean |
| Web unit tests (`vitest`) | 75 files, 760 tests pass |
| `desktop/` tests (`make desktop-test`) | pass |
| Browser: suggestions and Salie (`test-recommendations-browser`, `test-assistant-browser`) | pass |
| Node tests (`seed-browser-fixture`, `team-mentions`, relay e2e and connector) | pass |
| `make test-rqlite` (real rqlite clusters: failover, partitions, backups, migration, and the Team service suite on the replicated store, which is where Team memory runs in production) | pass after defect 13 (first run: the two flakes above; every other package passed: `replicated` 392 s, `server` 136 s) |
| `make test-team-desktop-browser`, `make test-unified-desktop-browser` | failed at the start of the audit (defect 12); pass after the script fixes |
| `make test-browser` (individual, phone, deck, controls, Team, comments, Initiatives, analytics, bridge) | 60 checks pass after defect 12 |
| `make test-notarize-desktop`, `make desktop-check`, `make test-workflows` | pass |
| `go test -race ./...` (whole module, rqlite skipped as CI does) | pass, no race reported |

### Tests added by this audit

`internal/integration`: `TestLooksSensitiveCatchesCredentialsAndLocalData`, `TestLooksSensitiveLeavesOrdinaryEngineeringProseAlone`.
`internal/service`: `TestAuditKnowledgeTickDoesNotPollGitHubWhenIdle`, `TestAuditCredentialLookingClaimsAreSecretAndNotRetrievable`,
`TestAuditPublicationReviewScreensStatementsAndDeletionWithdraws`, `TestAuditFreshnessAfterRapidCommits`,
`TestAuditPublicationReconcileCostWithManyUnverifiedClaims`, `TestAuditDeletingAMemoryLeavesNoReadableCopy`, `TestAuditIdleWorkerCost`,
`TestAuditConfirmedManualFactSurvivesTheNextFreshnessPass`, `TestAuditFreshnessOnLargeRepository`, `TestAuditPullRequestTextNeverEntersMemory`.
`internal/store/sqlite`: `TestAuditDeletedMemoryLeavesNoReadableResidue`, `TestAuditUpgradeFromPhase1BKeepsMemoryAndIndex`, `TestAuditMemoryScale`.
`internal/api`: `TestAuditLocalAccessScopesNeverReachPrivateMemory`. `internal/appops`: `TestAuditSlowTeamMemoryReadDegradesToUnavailable`
and origin assertions. `internal/assistant`: `TestAuditMemoryPromptOverheadIsOnlyPaidWhereTheOperationsExist`.
`internal/team/service`: `TestAuditTeamMemoryScale`.

### Not run, or not run for real

* **Real GitHub.** Every PR/CI scenario uses a fake `gh` client. Request counts were measured against the fake; real latency and
  rate-limit behaviour were not.
* **Real multi-host Team networks.** Nebula tests that need the pinned program and independent-host failover were not exercised
  beyond what `make test-rqlite` covers (below). Two-machine and Tailscale scenarios were simulated with two controllers in one process.
* **Linux and Windows.** CI runs Ubuntu as well; only macOS was run here.
* **Signing, notarization, Sparkle update.** `make test-notarize-desktop` runs against fakes; `test-desktop-sign` and
  `test-desktop-update` (which build the app twice) were not run. The release workflow signs and verifies on a macOS runner.
* **Long soak.** No multi-day run; growth was measured by loading records, not by elapsed time.
* **A real model.** Prompt-injection resistance was tested at the protocol and permission layers, not against a live provider.

## 7. Performance measurements

Machine: Apple silicon laptop, APFS, SQLite `synchronous=FULL`. Numbers are means of 20 runs unless stated.

**Individual memory store** (one project holding all records, the worst case; default quota is 10,000 per owner)

| Records | Insert | File per record | List newest 100 | Retrieval, no text | Retrieval, FTS common word (before → after fix) | FTS merge |
| --- | --- | --- | --- | --- | --- | --- |
| 2,000 | 7,900/s | 1,458 B | 4.8 ms | 5.2 ms | 9.7 → 0.7 ms | 2 ms |
| 10,000 | 7,400/s | 1,278 B | 23.7 ms | 26.3 ms | 107 → 1.9 ms | 6 ms |
| 50,000 | 6,000/s | 1,271 B | 119 ms | 133 ms | 1,962 → 7.5 ms | 28 ms |

Go heap in use after the 10,000-record run: 3.9 MiB. The no-text paths scale linearly with the project's record count (the five-key
ordering has no covering index); see U2.

**Team memory** (service level, one project, 2 authors and 1 reviewer; one record in five approved and published)

| Records | Project search, first page | Search with no match (full scan) | Exact-scope read | Published only | Worker tick (50) |
| --- | --- | --- | --- | --- | --- |
| 1,000 | 2.4 ms | not measured | 1.4 ms | 2.9 ms | 4.5 ms |
| 3,000 | 2.4 ms | not measured | 1.4 ms | 3.2 ms | 5.9 ms |
| 6,000 | 2.5 ms | 55 ms | 1.6 ms | 3.4 ms | 6.9 ms |

Proposal plus approval throughput 1,800–3,300 per second. A workspace stops accepting at 10,000 stored revisions. A record uses one
when proposed and two more when approved and published, so the cap is about 3,300 fully published records, or 10,000 unreviewed
suggestions; see U5.

**Background work**

| Item | Cost |
| --- | --- |
| Knowledge tick, idle (after fix) | under 1 ms; Git observed at most once per repository per 30 s |
| Freshness reconcile, one idle repository scope | 75 ms wall, 18 Git processes and one `gh pr list`, every 30 s per scope (so each repository costs about 1.2 Git processes per second when idle: 0.6 from freshness, 0.6 from the knowledge worker) |
| Freshness reconcile, 3,800 commits and 3,000 files, 500 refs | 129 ms; a fact on an unchanged path stays current |
| Same, 3,000 refs (over the 2,048 ceiling) | 145 ms; the fact becomes "needs verification" (conservative, as designed) |
| Rapid commits (80 commits, 60 anchored facts) | one pass, about 2 s including building the repository |
| Publication reconcile, idle, 5,000 unverified claims in the project | 31 ms, holding the single writer (every 5 s and on each ~10 s connector read) |
| Salie prompt | +2,814 characters (about 700 tokens) per turn, only for principals with the operations. A retrieval is capped at 1,500 JSON bytes by default, per relevant project, per turn |
| Team round trips per Salie retrieval | 3–4 (project search, workspace, workspace-scope read, optionally ticket), all bounded to 10 s in total |

CPU and memory were not profiled beyond the above; no unbounded structure was found. Storage is the table above, plus FTS and
backups; Team storage per record was not measured.

Polling summary: no UI polling (the recommendation store caches 30 s per project and refreshes when the window becomes visible);
three server loops, all bounded, all driven by timers that were measured above. No LLM or summarisation runs anywhere in the
memory path. Active agents are not blocked: extraction, freshness and publication run on their own goroutines; the longest hold of the
single SQLite writer measured was 31 ms (publication reconcile at 5,000 claims).

## 8. User experience

* **Scopes.** Project Settings → Memory now says it is private to this computer and that nothing reaches the Team unless shared under
  Team Memory sharing. Sharing is a separate settings section with a separate choice per destination project, a second choice for
  statements, an exact-text review ("Approve this exact text for sharing") and a plain statement that withdrawal cannot erase what was
  read. Sharing decisions are explicit; nothing is shared by default.
* **Salie.** Evidence is a collapsed "Memory sources" disclosure naming each source's id, **whose memory it is**, kind, verification,
  freshness, summary and source reference and date; suggestions are at most two cards with "Why this". The guidance tells the model to
  call missing or stale memory a gap in evidence. Browser tests confirm the cards sit beside existing content without moving it.
* **Clutter.** No new top-level screen. Memory is a section of Project Settings, sharing a section of Settings, relationships a section of
  Project Settings; initiatives are a Team console page.
* **Remaining UX issues.** Sharing needs three pasted identifiers (workspace ID, Team project ID, canonical repository) and a pasted
  grant; a picker would be friendlier. "Publish Git metadata" is a commit and PR number, which the label does not make obvious.

## 9. Unresolved problems

| # | Problem | Impact |
| --- | --- | --- |
| U1 | Freshness precision: more than 16 distinct source commits in a 100-record page exceed the comparison budget and the rest are invalidated conservatively (41 of 60 unrelated facts in the test). Only HEAD observations carry a commit anchor today | Safe; noisy only if code-anchored facts multiply |
| U2 | Listing and no-text retrieval cost grows linearly with a project's records (24 ms at 10,000, 119 ms at 50,000) | Acceptable at the default quota |
| U3 | The local publication journal keeps reviewed statement text with no expiry, including after a withdrawal is acknowledged | Residual copy on the owner's own machine |
| U4 | Pull-request reads are not cached across the freshness worker, the recommender (60 s cache) and the UI: about 120 `gh pr list` per hour per repository from freshness | Fine for a handful of repositories; shares the user's GitHub quota |
| U5 | Team's 10,000-revision cap per workspace is reached after about 3,300 fully published records (three revisions each) and nothing in the console surfaces it | A busy team stops being able to propose until compaction or a curator deletes |
| U6 | For Individual, only HEAD facts are rechecked against the live checkout at read time; other code-anchored facts rely on the worker (30 s; immediate for actions made through Werkbord). Team recomputes at every read | Today almost no Individual fact other than HEAD has an anchor |
| U7 | Prompt injection is bounded by gates, not detected | Residual by nature |
| U8 | Not tested against real GitHub, real multi-host networks, a live model, or over days | See section 6 |

## 10. Recommended follow-up priorities

1. **Surface Team's revision cap** (U5): show usage in Team settings, and compact approval revisions sooner.
2. **Scrub journal text after acknowledged withdrawal** (U3) and give acknowledged journal rows an expiry.
3. **Share one PR cache** between freshness and recommendations (U4); it also halves latency of an idle reconcile.
4. **A covering index or materialised rank for no-text retrieval** (U2) before raising any quota.
5. **Read-time recheck for anchored Individual facts** (U6), mirroring Team, before any feature creates anchored facts.
6. **A guided sharing set-up** (pick the Team workspace and project instead of pasting identifiers), then a usability test with five people.
7. **Run the unrun checks** in section 6: a real-GitHub soak on two repositories for a week, a three-host Team failover with memory
   traffic, and an injection exercise against each supported provider.
8. Consider optional per-record encryption of `secret`-classified local memory if the product ever stores them for a purpose.

## 11. Assessment

**Phases 1–4 are ready for wider testing as a preview (4.64.0-preview.1), on disposable Team workspaces and with sharing left
off unless a tester chooses it. They are not shown to be ready for production, and nothing here supports saying so.**

What supports wider testing:

* The trust boundaries hold under attack tests, not only by design: 38 architecture tests; every owner memory route refused to
  every other credential scope; Team has no path to local memory, files, Git or agents; memory text cannot close its wrapper, open a
  call or become a permission; the rules engine cannot be made to permit anything.
* Correctness held where it was pushed: stale facts never stayed current through 80 rapid commits, rebases, resets, branch deletion,
  contradictory Team reports, lost replies, restarts and revoked devices; two independent members and controllers synchronised and
  were revoked correctly; Individual works with no Team at all.
* The whole product test suite is green on this machine, including the real-cluster Team suite (the store Team memory runs on in
  production), every browser suite, `go test -race ./...`, lint and vet.
* Costs are small at the default quotas: a retrieval takes 2–26 ms at 10,000 records, a Team search about 2–4 ms at 6,000, an idle
  repository costs about 1.2 Git processes a second, the longest single-writer hold measured was 31 ms, and nothing calls a model.

What the audit also found, and why the verdict is conditional:

* The implementation **was not ready when the audit began**: it polled GitHub and spawned Git every five seconds, a deleted memory
  stayed readable in the database file, search was quadratic, credential-looking agent text was retrievable, a confirmed fact
  un-confirmed itself, and four browser scripts that gate every release had been broken by earlier commits. All are fixed and tested,
  but a feature set that needed twelve fixes in one audit deserves a staged rollout, not a general one.
* Several guarantees rest on a pattern filter or a human reviewer (credentials, injected instructions in an approved Team statement).
  They are layered with structural limits, but they are not detectors, and the residual risk is stated in section 5.
* Not exercised: real GitHub at scale or over days, a real multi-host Team network with memory traffic, a live model, Linux and Windows,
  signing and the update path, and people using the sharing screens. Eight unresolved items (U1–U8) remain; U3, U4 and U5 should be
  closed before a wider audience, because they are about retained text, shared quota and team capacity.

Suggested gate for "wider": ten testers, two weeks, sharing opt-in, a week-long GitHub soak on two repositories, and the
follow-ups numbered 1–3 in section 10. Suggested gate for production: all of section 10, plus a clean result from the checks listed
under "Not run".
