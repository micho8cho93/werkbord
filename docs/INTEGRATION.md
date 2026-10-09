# Individual ↔ Team integration, Phase 1

The two backends remain separate security domains. Individual owns execution,
repositories, credentials, runner approvals and local history. Team owns shared
projects, memberships, ticket text, assignments and review. A login-user connector
imports the member's held tickets and reports a reviewed projection of local
execution metadata. Claiming a ticket never launches an agent. A successful run
never approves a pull request, submits work for review or completes a Team ticket.

This feature requires Individual's `werkbord.integration/v1` endpoints and Team's
progress endpoints (introduced in Individual 1.5.0-preview.1 and Team 3.3.0).
Older `werkbord-team handoff`, `--report` and `--watch` workflows remain available.

## Authority and implementation

| Component | Authority and retained data |
| --- | --- |
| Individual controller | Local task/run history, execution settings, Git/agent sign-ins, repository paths, local durable events and provenance bindings |
| Team backend | Ticket requirements/assignment, memberships, device registry, reported execution metadata and existing Git reports |
| `werkbord-team connector` | Its enrolled member/device identity, opted-in project selections, associations, event cursors and outbound journal; runs as the login user |
| Network node / Workspace Host | Existing network/storage duties; never runs the connector or receives its Individual access grant |
| `internal/integration` | Product-neutral v1 DTOs and canonical repository identity validation; no storage, credentials, network or execution behavior |

The connector reuses `internal/team/hostclient` for signed device API requests,
workspace address restrictions and no-redirect handling. Enrollment uses the
existing fingerprint-checked invitation protocol and sealed device vault. The
local bridge uses `internal/team/localwerkbord` with literal authenticated loopback
access; DNS redirection and remote controller addresses are refused.

A one-time local exchange mints a revocable `wba_` grant with scope
`integration-v1`. That grant permits project discovery, text-only task imports,
metadata status and projected events. It cannot start/stop runs, answer questions,
change execution settings, read raw events/transcripts/files, change Git or mint
other grants. Legacy unscoped grants retain their existing reviewed behavior.
Unknown scopes fail closed. The full controller token is used only for this
exchange, is never saved by the connector and is never sent to a Team host. The
connector rejects a full controller token in its runtime configuration.

The connector does not poll the device-message execution inbox or execute any
signed command. Existing device-message/handoff protocols and their local trust
checks are independent. There is no remotely selectable controller URL, runner,
agent, shell command, environment, filesystem path or execution policy in v1.

## In the desktop app

With the Werkbord desktop app no connector setup is needed. Connecting your Individual runner to a Team workspace
(Team Settings → Connect my Individual runner) hands that workspace's Team service on your computer the narrow
`execution-local-v1` grant, and the service runs this same synchronization every cycle and right after a claim: every
project you are on, except those turned off under Team Settings → Tickets in Individual. That grant can import text and
read projected status; it cannot start a run. Held tickets whose repository is not a project in Individual are reported
with `PUT /api/integration/v1/waiting` (one replaceable set per workspace source, text only, at most 64) and shown in
Individual's Control Center until the person chooses a matching folder (`POST /api/integration/waiting/link`, which
refuses a folder that is not a clone of the repository) or clones it with their GitHub sign-in. The CLI connector below
remains the way to synchronize without the desktop app; both use the same provenance, so they never duplicate a task.

## Enable synchronization as your login user

Install both products using their existing installers. Enroll a separate member
runner device with the existing member invitation, using one private user-owned
data directory per Team. A Workspace Host invitation/authority vault is refused.
Confirm the invitation's workspace fingerprint through a trusted channel:

```sh
werkbord-team connector join 'werkbord://join/…' \
  --data-dir /absolute/user/team-a-device \
  --name 'My connector' --expect-fingerprint 'verified-fingerprint'

werkbord-team connector connect \
  --runner http://127.0.0.1:7420 \
  --controller-token-file /absolute/user/individual-data/token \
  --access-file /absolute/user/connector/local-access-token
```

Input configuration/credential files must be private regular files (0600 is
appropriate); symlinks and files readable by other users are refused. The access
file must not already exist or be the controller token file. Revoke the old local
grant before replacing it, then remove its old access file. The controller's
existing local-access management API/UI remains the revocation authority.

Create a private `connector.json`:

```json
{
  "version": 1,
  "stateDir": "/absolute/user/connector",
  "runnerBase": "http://127.0.0.1:7420",
  "accessTokenFile": "/absolute/user/connector/local-access-token",
  "workspaces": [
    {
      "dataDir": "/absolute/user/team-a-device",
      "keyStorage": "os",
      "bases": ["http://100.80.0.1:7430"],
      "projects": {
        "tpj_team_project_id": "prj_individual_project_id"
      }
    }
  ]
}
```

Use the actual IDs returned by the project APIs. Omit `bases` to use the workspace
addresses saved by enrollment. The connector accepts only loopback addresses or
addresses inside that workspace's enrolled network. `keyStorage` reuses existing
`os` or `file` sealed storage; file storage also needs `passphraseFile`.

Each entry in `projects` opts that Team project into synchronization. An empty
local project value selects the unique canonical repository match. Multiple
matches require an explicit matching Individual project ID. Project names never
establish identity. Projects without a validated repository identity cannot sync.
Add another workspace entry with its own enrolled device directory and project
map to connect another Team (at most 16 workspaces).

```sh
chmod 600 /absolute/user/connector/connector.json
werkbord-team connector run --config /absolute/user/connector/connector.json --once
werkbord-team connector status --config /absolute/user/connector/connector.json
scripts/install-team-connector.sh /absolute/user/connector/connector.json
```

The installer creates a macOS **LaunchAgent** or Linux **systemd user service**;
`--write-only` writes the service without loading it. Both installer and connector
refuse root. Network functionality that needs a TUN device/root remains in the
existing separate network-node service. Do not point the connector at the
privileged Workspace Host's data/vault. Network setup is described in
[TEAM_NETWORK.md](TEAM_NETWORK.md). No service is installed merely by installing
Individual or Team.

Once configured, claim/accept an assignment in Team. The connector imports it
into Individual's Backlog with ticket context, without copying commands or
launching execution. Start it through your normal Individual workflow. The Team
ticket panel shows execution status, timestamps, runner availability and handoff
availability, independently of ticket/review status. Git/PR facts use the existing
Team Git views. Detailed handoffs and logs remain in the owner's Individual.

## Durable associations and conflicting edits

The user's SQLite journal binds workspace, Team project/ticket, Individual
project/task, owning member/device, canonical repository, assignment generation,
claim time, imported text and controller cursor. Individual also binds provenance
to the same project/task in its own transaction. A lost import response cannot
create a second task, including after connector restart or project reselection.

Canonical identity validates before normalization: credential-free HTTPS, SSH
`git@` and Git URLs; normalized host/default port/`.git`, significant repository
path case and nondefault ports. Queries, fragments, local paths, credentials,
traversal and escaped separators are refused. Both the connector and Individual
check the selected repository. Changing the Team repository's identity suspends
the association; an existing source cannot silently move to another repository.
Use a reviewed new ticket for work in a different repository.

New connector tasks use a workspace-prefixed working branch to keep identical
Team ticket keys/slugs isolated in a shared local repository. Provenance aliases
for the connector's known host addresses reuse existing URL-based manual
handoffs. Imported legacy tasks keep their actual branch and local text. If a
legacy handoff used a different historical host URL, include that workspace host
address among the configured `bases` when migrating. The user must review any
already-existing duplicate sources; ambiguous provenance is refused.

Team text updates apply only to an untouched Backlog task that has never run,
using the previous imported text as a comparison. Local edits, archived tasks,
active work and any execution history produce a durable `conflict` flag; local
text/execution settings are preserved. Inspect `connector status` and merge the
requirements deliberately in Individual. Progress continues for the associated
local task. Reporting a branch/PR never counts as a text change or resets work.

## Contract

All local responses and import requests declare `schema: werkbord.integration/v1`.
Strict decoders refuse unknown request fields. Future incompatible versions need
a different schema/path; the connector refuses unsupported schema responses.

| Endpoint | v1 behavior |
| --- | --- |
| `GET /api/integration/v1/projects` | IDs, names and validated credential-free remotes only; omits paths, settings and instructions |
| `POST /api/integration/v1/import` | Validated repository selection, provenance plus bounded legacy aliases, text/branch context, optional previous-text comparison; returns project/task IDs and conflict flag |
| `GET /api/integration/v1/projects/{pid}/tasks/{tid}/status` | Atomic latest run/event cursor and current runner availability; allowlisted Git/PR projection, `gitUnavailable` when incomplete/unavailable |
| `GET /api/integration/v1/projects/{pid}/tasks/{tid}/events?after=N` | Pages the existing durable lifecycle log; cursor advances across omitted/raw events; `reset` signals a retention gap |
| `PUT /api/team/v1/projects/{pid}/tickets/{tid}/progress` | Device-signed, assignment-fenced ordered execution/Git report; returns acknowledged sequence and whether applied |
| `GET /api/team/v1/projects/{pid}/tickets/{tid}/progress` | Latest report per current holder device, with report time and derived stale flag; project ticket-view permission required |

Run states project to `queued`, `running`, `needs_input`, `blocked`, `completed`,
`failed` and `canceled`. Start time is the local attempt creation time once it
leaves Starting; completion time is its recorded end time. Runner availability
uses current heartbeat, project permissions, disabled/removed state and capacity.
The `execution.summary` object contains the fixed outcome, measured elapsed
milliseconds (when an end time exists), and handoff-available flag. Completion
time and Git/PR facts accompany it; no agent-generated summary text is extracted.

Outgoing Git facts contain branch, head/base, ahead/behind, commit SHAs/times and
PR number/URL/state/draft/base/mergeability. Existing Team Git validation and
merged-PR protection apply atomically with ordered progress. Unavailable or
truncated Git results retain prior Team facts and are explicitly marked. Phase 1
does not expose credentials, environment variables, terminal output, transcripts,
questions, errors/blocker text, handoff contents, commit subjects/authors, changed
file paths, file contents or repository paths in reports.

## Delivery, reconciliation and failure handling

Each workspace has its own polling/retry worker. Healthy reconciliation runs about
every three seconds; a metadata refresh at least once a minute makes availability
stale after two minutes without a report. Network failures use durable exponential
backoff (one second, doubling, capped by five minutes). Other connected Teams
continue even if one is offline or has lost quorum.

Outbound observations and the local replay cursor commit together, before
sending. Each association has a monotonic sequence; its workspace/project/ticket/
device/assignment plus sequence identifies an update. Team commits its high-water
mark and metadata together. Identical retries acknowledge without applying again;
older messages acknowledge the high-water mark, while a reused current sequence
with different content conflicts. Out-of-order messages never regress progress.

The journal holds at most 256 pending observations per workspace and 4096
associations overall. Controller feeds are paged and delivery is batched. A full
queue stops advancing the cursor; it neither drops unacknowledged updates nor
blocks another workspace. When controller retention has removed older events,
the latest authoritative snapshot reconciles state. Transitions inside that
expired retention gap cannot be reconstructed.

Every reconnect reads current membership, held tickets and assignment generation
before importing or delivering. The Team write transaction independently checks
current membership, device revocation, project access, holder, claim timestamp
and assignment generation. Release/reassignment/reclaim changes the generation
even within the same millisecond. Archived/deleted tickets and disconnected
projects suspend reporting and purge stale outbound work, preserving local tasks
and runs. Revocation does the same. No connector action stops or restarts an
agent; those remain the user's decisions in Individual.

After reviewing a refusal/conflict, stop the user service and use:

```sh
werkbord-team connector resume --config /absolute/user/connector/connector.json \
  --workspace tws_id --ticket ttk_id
```

Resume clears only the local suspension/queued reports; it preserves task identity
and sequence. The next run rechecks authority and repository. It cannot override
revocation or move an association to another project. A lock prevents two running
connectors from writing the same journal. `status` is safe while the service runs.

## Validation

`cmd/werkbord-team/connector_integration_test.go` hosts both real independent APIs
in a test-only client harness with signed device authentication and a fake local
agent. It exercises claim/import, local execution, lost acknowledgment,
offline/restart recovery, preserved text conflicts, revocation, archive/release,
multi-workspace isolation and secret/output exclusion. Service tests cover every
state projection, concurrent idempotent imports, retention reset, provenance
aliases, stale/out-of-order reports and same-timestamp assignment changes. Journal
and connector tests cover bounded atomic queues, persistent retries, selection
ambiguity and ordered replay. Existing architecture/isolation tests remain intact;
the only new shared dependency is the data/validation-only contract.

The focused progress service tests also run through the existing three-node
replicated service harness with a pinned source build, including no-quorum
refusal and retry after healing. `scripts/test-team-integration-browser.cjs`
checks real signed progress and stale/multiple-device/empty states on desktop and
phone, using disposable APIs and storage.

Phase 2 adds separately scoped, owner-approved execution and shared schedules.
See [EXECUTION_COORDINATION.md](EXECUTION_COORDINATION.md); the v1 metadata grant
and existing synchronization behavior remain unchanged.
