# Your machines as runners

Dev Board remains local-first. One controller owns SQLite, project/task metadata, Board,
Calendar, schedules, Control Center and the web app. Every runner owns its local agent
processes, credentials, Git clones and worktrees. The controller is also a runner. Dev Board
operates no hosted orchestration service; V1 connects one user's machines.

## Pair a machine

1. Connect the controller's private network in Settings.
2. In Settings → Runners, choose **Add runner**, select the projects this machine may execute,
   and generate a code. Enable cloning only if you want the runner to clone those configured origins.
3. Install the same Dev Board release on the other machine. The normal installer sets up a local
   controller too; you may stop that controller with `devboard stop` if you only need a runner.
4. Paste the generated `devboard join <code>` command. Sign in to the same private network when
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
devboard runner repo <project-id> /absolute/path/to/clone
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
published runner branch; it never silently reuses an older local task checkout. Dev Board does not
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
second time. It reconciles known process identities and reports stopped work. If a crash occurred
between launch and recording its process identity, the run remains uncertain. Inspect that runner,
ensure no processes remain, stop the runner service, then explicitly resolve:

```sh
devboard runner stop
devboard runner resolve <run-id> --confirm-stopped
devboard runner start
```

The next heartbeat reports the failure and releases ownership. There is deliberately no controller-side
force reassignment while a runner might still be executing. Disabling a remote runner or revoking
project access queues stops for its active work. Removal revokes its key and is allowed only after
all its runs end. History is retained. Local disable prevents new launches.

Useful commands: `devboard runner start`, `stop`, `status`, and `serve`. Runner state is in
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
client-side API-equivalent estimates, including subagents. Resumed adapter sessions leave these
measurements unknown because their lifetime totals cannot be safely attributed to just the new Run.
Fresh handoff runs can report their own usage. API-call counts stay unknown unless explicitly reported.
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
