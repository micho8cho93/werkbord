# Your machines as runners

Werkbord remains local-first. One controller owns SQLite, project/task metadata, Board,
Calendar, schedules, Control Center and the web app. Every runner owns its local agent
processes, credentials, Git clones and worktrees. The controller is also a runner. Werkbord
operates no hosted orchestration service; V1 connects one user's machines.

## Pair a machine

1. Connect the controller's private network in Settings.
2. In Settings → Runners, choose **Add runner**, select the projects this machine may execute,
   and generate a code. Enable cloning only if you want the runner to clone those configured origins.
3. Install the same Werkbord release on the other machine. The normal installer sets up a local
   controller too; you may stop that controller with `werkbord stop` if you only need a runner.
4. Paste the generated `werkbord join <code>` command. Sign in to the same private network when
   the browser opens. No configuration-file edits are needed.

Joining installs a separate runner login service. `--foreground` runs it in your terminal instead.
`--name 'Mac Mini'` sets the initial display name. `--allow-clone` opts this machine into cloning;
both controller permission and this local opt-in are required. The generated command includes
that flag when cloning was selected. Agents and Git must be authenticated locally on each runner.

Codes expire after five minutes, contain a 192-bit random secret, and are stored on the controller
only by hash. A consumed code cannot pair another identity. Within its expiry, the same signed
identity can recover a lost successful response; the CLI persists its pending key before joining.
If the expiry passes before recovery, remove the unused runner entry and generate a new code.
New remote runners begin with automatic routing off and capacity one.

## Repository access

Bind an existing clone on the runner:

```sh
werkbord runner repo <project-id> /absolute/path/to/clone
```

Project IDs appear in the runner's management details. Bindings are reloaded on the next heartbeat.
With authorized cloning, the runner clones the controller's configured `origin` into its own data
directory. It checks the exact origin URL before execution. Credential-bearing URLs, local paths,
Git remote helpers and unsafe transports are refused. Use ordinary HTTPS or Git SSH origins.
The runner uses its own Git credential helper or SSH keys. No GitHub tokens or agent credentials
are sent by the runner protocol. Projects without an origin require an explicitly bound local clone.

Before execution, each runner fetches its origin, starts from the fresh target and creates
`devboard/<runner-id>/<run-id>` in a separate worktree. Pinned stale targets are refused. Continuing
work requires the previous branch to contain both its latest reported commit and the fresh target.
Uncommitted work is retained on its owning machine. To change machines, commit and push the prior
branch first, and keep its runner connected so it can report the clean worktree and updated commit.
The runner periodically reports changes to retained worktrees after execution ends. Unknown or dirty
worktree state blocks a machine change. Returning to the controller creates a new worktree from the
published runner branch; it never silently reuses an older local task checkout. Werkbord does not
commit or push automatically.

Remote worktrees remain on their runner. Commit and push there using your local Git tools, then
Fetch in the controller's Git view. Remote branches carry their task/run ownership. Review comparisons
fetch again and use the fresh shared target. Local merge checks fetch again and refuse if the local
target lacks new remote commits; update it before reviewing. Existing merge checks still test conflicts,
busy/dirty checkouts and reviewed commit IDs. To merge through the controller, first create a local
tracking branch for the pushed runner branch using Git. Remote worktrees cannot be cleaned or deleted
through controller-local worktree actions.

## Assignment and automatic routing

Execution settings at global, project, task and run levels support Automatic or a specific runner.
The most specific runner choice wins. A specific offline, disabled, full or unauthorized runner
produces an explicit issue; schedules remain queued. There is no silent fallback.

Automatic considers online/enabled status, owner opt-in, project scope, repository availability
(or authorized cloning), installed agent, detectable model/reasoning availability, capacity and
optional CPU/available RAM requirements. Among eligible machines it prefers an existing project
clone, lower capacity utilization, more available RAM, more CPU, then stable runner ID. The scheduler
orders tasks by its existing priority and queue order. Resource readings and CLI model catalogs are
observations, not guarantees; unavailable measurements display Unknown.

Settings → Routing rules offers ordered, deterministic title/description substring and retry rules.
Rules can set an agent/model/reasoning default or minimum CPU/available RAM. Matching rules apply
in list order; later matching choices win and resource requirements take the maximum. Explicit task
or one-run agent/model/reasoning choices take precedence over rule defaults. No LLM routes machines.

## Capacity, disconnects and recovery

Heartbeats run every three seconds. A machine is offline after 30 seconds without one. Executing
sessions need a 90-second lease; the runner stops them when it cannot renew. Starting, running,
blocked and idle sessions all occupy capacity. Offline status and expired leases never free the
controller's task ownership: a signed terminal report must reconcile it before another execution.
The same task's run creation and machine capacity claim are serialized atomically in SQLite.

Runner journals are atomic, owner-only files. Reports have per-run ordered sequence numbers;
retransmissions are acknowledged without repeating transitions or accounting. Commands are durable
and delivered at most once. A delivery interrupted by a runner crash may fail rather than be silently
replayed; the recovered process is stopped and its terminal report makes that outcome visible.
Stop/finish/replies on an offline machine are queued and remain pending until acknowledged.

Controller restart preserves remote runs. Runner restart never launches an already accepted job a
second time. New jobs first receive a durable acceptance acknowledgement; only a later
heartbeat may execute them. A missing journal for accepted work is quarantined, never relaunched.
Jobs created before this acceptance contract are also quarantined after an upgrade. It reconciles
known process identities and reports stopped work. If a crash occurred
between launch and recording its process identity, the run remains uncertain. Inspect that runner,
ensure no processes remain, stop the runner service, then explicitly resolve:

```sh
werkbord runner stop
werkbord runner resolve <run-id> --confirm-stopped
werkbord runner start
```

The next heartbeat reports the failure and releases ownership. A rejected run is isolated from
other reports and heartbeat renewal, stopped on its runner, and quarantined with a diagnostic.
A restored controller database quarantines that identity durably; recover or revoke it and re-pair.
Do not clear journals or copy an identity onto another machine to recover it.

Settings → Runners → Revoke identity handles a permanently lost machine. It immediately rejects
its signatures and holds capacity for 120 seconds (lease plus the in-flight request allowance).
Its unresolved runs then become failed with **unverified work**, retained for inspection. Revocation
never launches a replacement run. Inspect the owning machine and reconcile its branch before retrying.
Disabling a runner or revoking project access queues stops; ordinary removal still requires all
runs to end. History is retained. Local disable prevents new launches.

Controller and runner must both use protocol 1 (Werkbord 0.9.0). Upgrade idle services together;
legacy jobs with unknown acceptance require owner recovery. `werkbord update` restarts an already
running runner service after a successful binary replacement. A version/protocol mismatch or clock
skew is an actionable sync error; `werkbord runner status` exposes the last connection error, and
Settings displays runner recovery diagnostics. Clone preparation allows eight minutes, fetch two,
and total preparation ten; progress appears in the run output. Leases use a monotonic clock.

Useful commands: `werkbord runner start`, `stop`, `status`, and `serve`. Runner state is in
`<data-dir>/runner`, separate from controller SQLite and services. Settings allows rename, capacity,
automatic-routing opt-in, project/clone permissions, disable/remove and inspection of diagnostics,
OS/architecture, CPU, RAM, storage, agent versions and last heartbeat.

## Usage and economics

Runs retain agent, model, reasoning, machine, attempt/retry, status, elapsed ownership time and
human acceptance/rejection. Token/API-call/cost fields are nullable: an unreported count is Unknown,
not zero. Usage updates are cumulative snapshots, not additions of repeated observations. Human
assessment is recorded separately from adapter measurements and does not automatically merge work.

Cost categories remain separate:

- **Known API cost**: an explicitly reported actual API charge with a named source.
- **API-equivalent estimate**: reported client-side estimated usage cost; never treated as a subscription bill.
- **Usage only**: tokens may be available, but monetary cost is unknown.

Codex reports cumulative input/output/cached token counts from app-server notifications; it does not
supply an actual charge here. Claude's `modelUsage` and `total_cost_usd` supply cumulative token and
client-side API-equivalent estimates, including subagents. Resumed adapter sessions establish a
baseline and report only verifiable later increments, marked **Partial**. Counts before that baseline
remain unknown; lifetime measurements are never charged to a second run. Incomplete updates retain
previously known cumulative counts and cost provenance. Fresh handoff runs report their own usage. API-call counts stay unknown unless explicitly reported.
Owner-only usage and assessment endpoints support recording other reliable measurements with provenance.
See [Codex's notification schema](https://raw.githubusercontent.com/openai/codex/main/codex-rs/app-server-protocol/schema/json/v2/ThreadTokenUsageUpdatedNotification.json)
and [Claude's cost tracking documentation](https://code.claude.com/docs/en/agent-sdk/cost-tracking).

Control Center filters actionable work, orchestration, runner workloads and recent usage by project,
runner and agent. Usage covers the last 30 days and latest 500 runs per project. Coverage counts show
how many runs reported tokens or costs. Actual charges and estimates are never summed together.
Runtime measures elapsed ownership (including idle/blocked/disconnected time), not CPU time.

## Authentication and scope

Pairing establishes an Ed25519 runner identity. Sync bodies are signed, with a persisted monotonic
request counter and a bounded timestamp window to prevent replay. The private network encrypts
transport. Only `/api/runner/join` and `/api/runner/sync` accept this protocol; runner keys cannot
access owner APIs, impersonate other runners, alter arbitrary tasks or receive unrelated projects.
Capability reports cannot grant permissions. Run observations must match the authenticated owner.
Private keys stay in local owner-only files; pairing secrets are not logged. Owner web authentication
remains separate. Remote jobs include only execution metadata, prompts, authorized origin URLs and
commands needed for their own runs.

The test suite simulates paired machines, controller replacement, replay/reconnect, lease expiry,
capacity races, command acknowledgement, lost pairing responses, unique worktrees, authorized clones,
fresh remote targets, stale branches, cross-runner ownership and unknown/reported usage without any
hosted service or real agent calls. Live multi-machine private-network sign-in still depends on your
network and each machine's installed/authenticated CLIs.

## Scheduling and retained evidence

Ordinary schedule editing, including disabling and enabling it, preserves the existing attempt.
Use **Rearm another attempt** to clear a missed/failed/consumed dispatch and authorize another run.
Pre-launch conflicts are recorded and do not retry every second or fetch every queued task.
Git inspection follows capacity/scope filtering; runner capacity is shared across projects and queued
reservations. Projects take turns at shared capacity. A completed dependency whose task is not Done
is explicitly described as integration unverified; downstream context includes its branch evidence.
An imported task's explicit base branch controls its worktree and scheduler checks.

Untracked file contents are excluded from generated handoff prompts. Filenames may be shown as
Git evidence; review files and manually add selected non-secret context when it is needed. A deleted
task worktree can be recreated from its preserved branch. Compact state/usage events omit repeated
prompts. Terminal transcript output is retained for 30 days, other terminal run events for a year;
active run output, stored runs and handoff summaries remain. A browser whose replay cursor predates
retention reloads authoritative state instead of replaying an incomplete history.
