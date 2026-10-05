# Werkbord Team: security review

**Question.** Can the Team architecture, even by accident, give one member arbitrary execution access to another
member's computer?

**Answer.** No. Team has no capability that could: it starts no process, serves no file, holds no credential and opens
no connection to any member's machine. What members exchange is *coordination metadata*: names, ticket text, branch
names, commit hashes and subjects, pull-request addresses and states. The one channel through which a teammate's words
reach another person's agent is a ticket's text, and that channel is a human decision at every step (see
[Teammate-written text](#teammate-written-text-the-one-channel-that-remains)).

This review was done against Team 2.0.0. It is kept as tests so it cannot silently go stale: every claim below names the
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
