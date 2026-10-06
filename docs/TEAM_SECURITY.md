# Werkbord Team: security review

**Question.** Can the Team architecture, even by accident, give one member arbitrary execution access to another
member's computer?

**Answer.** No. Team has no capability that could: it starts no process, serves no file, holds no credential and opens
no connection to any member's machine. What members exchange is *coordination metadata*: names, ticket text, branch
names, commit hashes and subjects, pull-request addresses and states. The one channel through which a teammate's words
reach another person's agent is a ticket's text, and that channel is a human decision at every step (see
[Teammate-written text](#teammate-written-text-the-one-channel-that-remains)).

This review was done against Team 2.0.0 and extended for the device registry (2.4) and the private network (2.5), below. It is kept as tests so it cannot silently go stale: every claim below names the
test that fails if it stops being true. Run them with `make test-team` and `go test ./internal/archtest/`.

## Method

For each surface the question was asked from the attacker's side: *a signed-in member, a member of another workspace, an
unauthenticated caller, or a malicious server operator's client, wants to run something on, read something from, or
act as another member*. The surfaces are the ones the review brief named.

| Surface | Finding | Why it holds | Enforced by |
| --- | --- | --- | --- |
| **Runner APIs** | None exist. Team has no concept of a runner, only a *handoff*: a JSON document a member pulls for a ticket **they hold** and carries into **their own** Werkbord. | There is no route that takes a runner address, and the server makes no outbound connection at all. The CLI that creates the local task (`werkbord-team handoff`) runs on the member's own computer and refuses any address that is not loopback; it never follows redirects. | `TestTheRouteSurfaceIsExactlyTheReviewedList`, `TestNoRouteCanReachAComputer`, `TestTeamServerNeverReachesOut`, `TestHandoffOnlyEverGoesToThisComputer` |
| **WebSocket / events** | None. Changes reach clients by a plain HTTP long poll (`GET /sync`), scoped to the caller. | No WebSocket, SSE, proxy or socket library is linked. The poll returns revisions and *events about projects the caller can see*; events are hints, the views are re-read. One member may hold at most 8 open polls (HTTP 429 beyond that), and shutdown cancels them at once. | `TestEventsAreOnlyThoseOfProjectsTheClientCanSee`, `TestOnlyAFewWaitingRequestsMayBeOpenPerMember`, `TestTooManyOpenSyncRequestsAreRefused`, `TestShutdownDoesNotWaitForOpenSyncRequests`, rule 7 (no `golang.org/x/net`, `websocket`, `httputil`) |
| **Authentication** | Every route except `GET /health` and `POST /invites/redeem` needs `Authorization: Bearer <token>`, on loopback too. Tokens are 256 random bits; only their SHA-256 is stored; a wrong or missing token is a 401 whatever the route. | No cookies, so no ambient authority and no CSRF; browser writes are also refused when `Origin` does not match `Host`. The invite code is the credential of the one public route: 128 random bits, stored as a hash, counted atomically, and every unusable code answers the same 404. | `TestEveryRouteExceptTheTwoPublicOnesNeedsAToken`, `TestTokensAreNotStored`, `TestInviteLimitsExpiryAndRevocation`, `TestTwoPeopleRedeemingASingleUseInviteAtOnce` |
| **Workspace authorization** | The token names the member and so the workspace; no URL or body selects one. Another workspace's owner (who sees all of *their* projects) gets 404 on every project route of ours and sees nothing of ours in any list. | Every query is scoped by `workspace_id`; composite foreign keys make it impossible to put a member of one workspace on another's project. | `TestEveryProjectRouteHidesTheProjectFromOtherWorkspaces`, `TestWorkspacesCannotReachEachOthersBoards`, `TestWorkspacesAreIsolatedFromEachOther` |
| **Project authorization** | A member of the workspace who is not on a project gets 404 (not 403) on every route that names it, so projects cannot be probed. The views that span projects (`/my-work`, `/reviews`, `/overview`, `/sync`) include only projects the caller can see. A plain member is refused (403) everything a project owner alone may do. | One function (`service.access`) decides, from the member's workspace permissions and project role, and every project operation goes through it. Roles are permission tables, not name checks. | `TestEveryProjectRouteHidesTheProjectFromOutsiders`, `TestProjectOwnerRoutesRefuseAPlainMember`, `TestReviewsAcrossProjectsStayWithinWhatTheViewerCanSee` |
| **Filesystem APIs** | None. No route reads, writes, lists or serves a path. The console is served from files compiled into the program. Reported file lists on branches are repository-relative names, validated, and never opened. | `os` is imported only by configuration (the data directory); nothing in a request reaches it. | `TestTeamServerNeverReachesOut` (only `config` may import `os`), `TestNoRouteCanReachAComputer`, the path checks in `TestGitReportsAreValidatedAndPermissioned` |
| **Shell APIs** | None. `os/exec`, `plugin`, `net/rpc`, a pseudo-terminal package, SSH and `tailscale.com` are all absent from the Team build, including indirectly. | Not a convention: the build fails if any of them appears. | `TestTeamCodeNeverExecutesAnything` (rule 4) |
| **Environment variables** | Read only by Team's configuration at start-up (`WERKBORD_TEAM_*`). No API returns, accepts or exposes one. A handoff carries none. | A test walks every field name of everything Team shares. | `TestNothingSharedCarriesACredentialOrAMachine`, `TestTeamServerNeverReachesOut` |
| **Git credentials** | Team holds none and cannot use one: it never runs Git and never calls a Git host. Repository and pull-request addresses are rejected if they contain a user, password, token, query or fragment. Git facts are *reported* by the developer's own Werkbord, signed in as them, and recorded as metadata. | There is no field in the schema that could carry a credential or a path on a machine. | `TestNothingSharedCarriesACredentialOrAMachine`, `TestGitReportsAreValidatedAndPermissioned`, `domain_test.go` repository and pull-request cases |
| **Provider / API credentials** | None. Team never calls a model provider or any API; agents run in each member's own Werkbord with that member's credentials. | No outbound connection exists to carry one. | `TestTeamServerNeverReachesOut` |
| **The individual product** | Never reaches Team and cannot be driven by it. Nothing it is built from names Team's API, settings or executable. | Rules 1, 2 and the string scan. | `TestIndividualProductDoesNotDependOnTeam`, `TestOnlyTeamImportsTeam`, `TestIndividualProductDoesNotMentionTeam`, `make verify-isolation` |

## Admin and the device registry (Team 2.4)

Two additions, reviewed against the same question.

| Surface | Finding | Enforced by |
| --- | --- | --- |
| **Admin role** | An admin administers members, projects and devices but cannot appoint or act on another admin, reissue the owner's token (which would be taking the workspace) or remove the owner. `members.manage` is therefore not a way up. | `TestAnAdminAdministersButDoesNotOwn`, `TestAnAdminCannotReachTheOwnerOrOtherAdmins`, `TestOnlyTheOwnerAppointsAdmins`, `TestAdminsOverHTTP` |
| **Device registry** | Holds a device's ID, owner, name, **public** key, capabilities and last-seen time. No column or field for a private key, credential, path or environment. A key can be registered only with a signature by the key itself, bound to the workspace and member. | `TestTheDeviceTablesHaveNoColumnForASecret`, `TestADeviceHasExactlyTheseFields`, `TestRegistrationNeedsProofOfTheKey` |
| **Private keys** | Live in `internal/deviceid/localidentity`, which Team's build cannot include. | `TestTeamNeverLinksADevicesPrivateKey` |
| **Capabilities versus roles** | Host and connectivity capabilities need `devices.manage`; owning a host grants its owner nothing; revocation is permanent and ends host roles. | `TestCapabilitiesAreIndependentOfRoles`, `TestOnlyDeviceManagersGrantInfrastructureCapabilities`, `TestRevokingADevice` |
| **Signed messages** | Format, expiry, replay, wrong signer, wrong target, revoked device are all refused; payloads have no field that could carry a command. Defined but not yet carried anywhere: Team has no route that takes one. | `internal/envelope` tests, `TestNoPayloadCarriesAnythingExecutable` |

Still true: Team starts no process and contacts no device. The architecture tests now say which kind of process start
would ever be acceptable (Team's own infrastructure, by name) and fail for any other (`internal/archtest/infra_test.go`).

## The private network (Team 2.5)

The same question, asked of what 2.5 adds: a network the customer owns, made of the workspace's own machines, that devices
join with an invitation. What it adds is **infrastructure** (keys, certificates, a supervised network program, an enrollment
endpoint); it adds no way for Team to run anything for, or on, a member. How it works: [TEAM_NETWORK.md](TEAM_NETWORK.md).

| Surface | Finding | Why it holds | Enforced by |
| --- | --- | --- | --- |
| **Werkbord-operated infrastructure** | None is needed, and none can be named. No relay, rendezvous server, control plane, discovery server, network registry or key service: the network's discovery hosts and relays are the customer's machines; a device finds the workspace at addresses in its invitation. | Every setting is an address or a file of the customer's own; no code that runs contains a URL that leads anywhere (the one URL, the pinned release's address, is for a build script and no running code may use it); nothing the workspace generates (invitations, node configurations, responses, the health report) names a host outside the customer's. | `TestNoServiceURLIsBuiltIntoTheNetworkCode`, `TestTheURLRuleCatchesWhatItIsMeantTo`, `TestTheNetworkConfigurationNamesOnlyTheCustomersOwnThings`, `TestNothingThatIsGeneratedNamesAServiceOutsideTheCustomersMachines`, `TestTheRuntimeSettingsHaveNoServiceInThem` |
| **Starting a process** | One program, by name: the pinned Nebula. The supervisor has no function that takes a command, an argument, an environment or a path; it starts a verified private copy with `-config <file>` and an empty environment; a program that does not match its pin, is a symlink, is found on `PATH`, or changed after it was verified is not started. Developer execution is still absent from Team. | The exported API is held to a reviewed list; no parameter or field means "something to run"; one `exec.CommandContext` call with literal arguments; the grant in rule 9 names the program and may never name a shell, Git, an agent or a runtime. | `TestTheNetworkSupervisorIsNotAGeneralRunner`, `TestTheSupervisorRuleCatchesWhatItIsMeantTo`, `TestTheInfrastructureExceptionIsNarrow`, `TestOnlyTheProgramThatMatchesThePinIsStarted`, `TestThePathIsNeverSearched`, `TestASymbolicLinkIsNotFollowed`, `TestACopyChangedAfterVerificationIsNotStarted`, `TestADirectoryOthersCanWriteIsRefused`, `TestTheProgramGetsOnlyTheArgumentsTheSupervisorBuilds` |
| **The pinned program** | A reviewed release, checked against hashes that were taken from the release's own checksum file, with its licences. Never fetched at run time. | Pins in the source; the file the release published is in the repository and a test compares them; the licences ship with the program. | `TestThePinIsWellFormedAndMatchesWhatUpstreamPublished`, `TestTheLicensesOfWhatIsShippedAreInTheRepository`, `scripts/fetch-nebula.sh` |
| **The authority's and the workspace's keys** | Held by Workspace Hosts only, sealed on disk; never in the database, any API response, any log. A different key from the network's, from a host's application key and from a member device's key. | The service, domain, API and store cannot name a private key (they see an interface); the vault seals with AES-256-GCM under a key kept apart or a passphrase (Argon2id), refuses a key file others can read, and refuses a wrong key or a changed file; the schema has no column that could hold one. | `TestNoSigningKeyCanReachAResponse`, `TestTheFirstWorkspaceGetsItsKeysItsNetworkAndItsFirstHost`, `TestTheNetworkTablesHaveNoColumnForASecret`, `TestTheWorkspaceKeyAndTheNetworkKeyAreDifferentKeys`, `TestSealedKeysAreOpenedOnlyByTheKeyAndLabelTheyWereSealedWith`, `TestAKeyFileOthersCanReadIsRefused`, `TestPassphraseSealing`, `TestAHostThatLostItsKeysFileCannotStartAndSaysWhy` |
| **Handing the authority to another host** | Sealed to that host's own key (HPKE), bound to the workspace and the device; the ciphertext is stored, collected once with the host's own credential and removed; refused unless it matches the workspace and authority the host enrolled with. | Only the holder of the private sealing key can open it; the receiver checks before storing. | `TestAnotherHostIsHandedTheAuthoritySealedToItAlone`, `TestTheAuthorityIsSealedToTheHostItIsForAndCollectedOnce`, `TestOnlyAHostWithTheAuthorityCanHandItOn`, `TestASecondAndThirdHostAreEnrolledAndEachCanTakeOverTheAuthority` |
| **What a certificate can say** | The authority signs only addresses inside the network and groups that exist; its own certificate limits even a stolen key to them; a device chooses only its key. | Checked by the issuer and by Nebula's own signing rules. | `TestTheAuthorityRefusesWhatItShouldNotSign`, `TestTheAuthoritysOwnCertificateLimitsWhatItCanSign`, `TestTheAuthorityIssuesCertificatesThatNebulaVerifies` |
| **Invitations** | Signed, expiring, single-use; carry no private key, no reusable certificate and no member token. A changed byte breaks the signature; a changed credential is refused; an expired, spent, withdrawn, wrong or unknown one all get the same answer. | Ed25519 over the exact bytes; the credential is 256 random bits, stored as a hash, spent in the transaction that accepts it. | `TestAModifiedInvitationIsRefused`, `TestMalformedInvitationsAreRefused`, `TestAnExpiredInvitationIsRefused`, `TestAnInvitationNamesTheWorkspacesOwnEndpointsAndKeepsNoCredential`, `TestAnInvitationWorksOnce`, `TestAnInvitationIsUsedOnceAndNeverAfterItExpiresOrIsWithdrawn`, `TestEveryWayAnInvitationCanBeUnusableLooksTheSame`, `TestAnInvitationCannotBeReusedExpiredModifiedOrPointedAtAnotherWorkspace` |
| **The enrollment endpoint** | TLS 1.3 only, no plaintext mode, small bodies and short timeouts, a per-address rate limit. A device sends its credential only to a server that proved it holds the workspace key; the request's proof is bound to the TLS session and to the device's own key. | The device's only trust anchor is the workspace key; channel binding by the TLS exporter; standard X.509 verification. | `TestADeviceNeverSendsItsCredentialToAnImposter`, `TestAWrongExpectedFingerprintIsRefusedBeforeAnythingIsSent`, `TestAnEndpointForTheWrongAddressIsRefused`, `TestTheServerRefusesTLS12`, `TestTheServerHasNoPlaintextMode`, `TestAProofForOneConnectionIsNotAProofForAnother`, `TestAProofFromAnotherKeyIsRefused`, `TestTheRateLimitSlowsGuessing` |
| **Approval** | When required, a device gets nothing until an administrator approves; a denied one gets nothing and its invitation is spent; an approver cannot approve more than they could grant. | Nothing is created for a pending device; the decision is guarded in the write. | `TestAWorkspaceThatRequiresApprovalHoldsDevicesUntilAnAdministratorDecides`, `TestADeniedDeviceGetsNothingAndTheInvitationIsSpent`, `TestApprovingSomethingTheApproverMayNotGrantIsRefused`, `TestWhoMayInviteWhom` |
| **Network policy** | Default-deny in both directions. A member reaches the API and nothing else; no member reaches another member's runner, files or any port; the database's ports are for Workspace Hosts alone; a Connectivity Host exposes nothing but what it relays. | Enforced by the receiving node, so a sender that edited its own configuration gains nothing; the generator cannot write an allow-all rule. | `TestThePolicyAsNebulaEnforcesIt` (real Nebula nodes), `TestTheLabCanTellAllowedFromBlocked` (its control), `TestNoRuleLetsAMemberReachAnotherMember`, `TestThePolicyIsDefaultDeny`, `TestWhatEachRoleMayReach`, `TestEveryRoleGetsAConfigurationTheRealProgramAccepts` |
| **Revocation** | Two layers. The application layer is authoritative at once: a revoked device's credential is refused on its next request even if the network still carries its packets. The network layer follows: its certificates are blocklisted by every host, and short-lived anyway. | Authentication checks the device on every request; revoking deletes the credential and marks every certificate in one transaction; the blocklist outlives the device's records. | `TestARevokedDeviceCannotUseTheAPIEvenWithTheCertificateItHolds`, `TestRevokingADeviceEndsItInBothLayersAtOnce`, `TestEveryCertificateAMemberHadIsRefusedWhenTheyAreRemoved`, `TestTheNetworkRefusesARevokedCertificateAndAnotherAuthoritys`, `TestANewBlocklistIsAppliedWithoutRestartingTheNode`, `TestADeviceThatJoinedReachesTheHostsNetworkAndARevokedOneIsRefusedByIt`, `TestRevokedCertificatesStayOnTheBlocklistWhenTheirDeviceIsDeletedAndLeaveItWhenTheyExpire` |
| **Honesty about reachability** | Team never says remote access works unless a Connectivity Host has been reached from another device at an address that could be reached from outside; a host's check of itself does not count; a private, shared or loopback address is never "public". | The verdict is computed from checks, not from configuration. | `TestReachabilityIsOnlyEverAsStrongAsWhatWasChecked`, `TestTheHealthReportDoesNotPretendNATTraversalIsGuaranteed`, `TestAHostsCheckOfItselfProvesNothingAboutTheOutside` |
| **No single host is authoritative** | A device is told of every discovery host and relay; a node starts and finds the network through the others when one is offline. | Tested with the real program. | `TestSeveralLighthousesAreAllUsedAndOneMayBeOffline`, `TestEveryDeviceIsToldOfEveryDiscoveryHostAndRelay` |
| **The transport** | The network is a `transport.Transport`; it dials nothing outside the workspace's range and accepts only on the host's own address on it. | The one outbound-capable package is named in rule 7 and is tested for exactly that. | `TestItIsATransport`, `TestItDialsNothingOutsideTheWorkspacesNetwork` |

### What is trusted, and what 2.5 does not defend against

- **A Workspace Host is trusted completely, for the workspace's network.** Whoever holds the workspace key and the authority
  key can put any device on the network in any group and sign invitations the whole workspace will believe. Protect those
  machines as the most important ones you have; do not make a host of a laptop that travels. Compromise of a host is
  compromise of the network: there is no in-place rotation yet, so the answer is a new network.
- **A fingerprint nobody compares gives trust on first use.** An invitation shows it was not altered, not that it was meant;
  `--expect-fingerprint` is the defence. A person who is sent an invitation by a channel an attacker controls can be sent to an
  attacker's workspace.
- **The sealing key beside the data is only as safe as the account that runs Team.** Use a passphrase, or keep the key
  elsewhere, if that matters.
- **Reachability checks test the TCP enrollment endpoint.** They do not prove the UDP port is forwarded, and a prober on the
  same network as the host proves little: only a check from another network counts, and Team cannot know which network the
  prober was on. It reports what it can establish and says so.
- **Relays are experimental upstream.** Team uses them, reports whether any exists, and does not promise they suffice.
- **The data is on one host.** A second Workspace Host holds the authority, not the records.
- **Nebula itself.** Team ships an unmodified release, pinned and verified, but is not an audit of Nebula; its own security
  notes are at <https://github.com/slackhq/nebula/security>.

## Teammate-written text: the one channel that remains

A ticket is written by one member and read by another, and "Open in my runner" turns it into the task text of the
reader's own agent. That is by design, and it is the only way one member's words reach another member's machine. Team
treats it as untrusted input:

- **The reader decides.** Team cannot start a run. `werkbord-team handoff` creates a *task* in the member's own
  Werkbord; the member starts the run, under that Werkbord's own execution policy and approvals.
- **Only the holder gets the text.** The handoff is issued only to the member who holds the ticket (not to a project
  owner, not to a reviewer).
- **It says where it came from.** The task text begins with the ticket's key and title, then a plain statement that the
  description was written by a named teammate and is a description of work, *not* instructions with authority over this
  computer: it should not make the agent run commands, read or send files or credentials, or touch anything outside
  the repository, and unusual requests are to be put to the person it works for. (`TestHandoffGivesTheHolderTheirTicketContext`.)
- **It carries no secret.** There is nothing sensitive in it for a teammate to harvest: no path, environment variable,
  credential or token.

This does not make a hostile teammate harmless: a person who can write tickets can ask an agent to do foolish things, as
a person who can send you a pull request can. The defence is the same: review what you run. A project's owner decides
who is on the project.

## Residual risks and operator guidance

- **Plain HTTP.** Team serves HTTP. Off loopback, put it behind HTTPS or a private network; tokens cross the network in
  the clear otherwise. Team says so in its log at start-up.
- **Failed sign-ins are not rate-limited.** Tokens are 256 random bits, so guessing is infeasible; add a limit at the
  proxy if you want to see and slow scanning.
- **Pull-request addresses are arbitrary https links** shown to the whole project. The console follows them only if they
  are https, in a new tab, with `noopener noreferrer`. A member could post a misleading link; that is social, not
  technical, and project owners can remove the person.
- **The operator can read everything Team stores** (tickets, names, branch names). They cannot read credentials,
  because there are none.
- **The first sign-in link carries the token in the URL fragment.** The browser never sends a fragment to a server and
  the console removes it from the address bar at once, but it can remain in browser history until cleared. Reissue the
  token if that matters.
- **Reports are claims.** Team shows "as last reported by"; a member can misreport their own branch state. A reviewer
  confirms on the Git host. A *merged* pull request cannot be reported back to open (a late or replayed report is
  refused), so a stale client cannot undo a recorded merge.

## Concurrency

Writes are serialised (one write transaction at a time, taken up front) and every guard is also in the SQL, so the
guarantees hold even if the service's own reads were stale. See [TEAM.md](TEAM.md#concurrency) for the list and the tests.
