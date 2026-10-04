// Mirrors of the controller's JSON types (internal/domain). Keep in sync by
// hand until type generation is introduced.

export type TaskState = 'backlog' | 'doing' | 'review' | 'done';

export const TASK_STATES: readonly TaskState[] = ['backlog', 'doing', 'review', 'done'];

export const TASK_STATE_LABELS: Record<TaskState, string> = {
  backlog: 'Backlog',
  doing: 'Doing',
  review: 'Review',
  done: 'Done',
};

/**
 * Execution state of a run. It is never a Kanban column: a task in Doing can have a
 * run that is running, waiting for you, blocked or failed.
 */
export type RunState = 'starting' | 'running' | 'waiting_for_user' | 'blocked' | 'completed' | 'failed' | 'stopped';

/** How much an agent may lean on the user. About conversation only: it never grants a permission. */
export type InteractionPolicy = 'interactive' | 'autonomous' | 'autonomous_stop_if_blocked';

/** How a task is carried out. A struct so that finer-grained permissions can be added as fields later. */
export interface ExecutionPolicy {
  interaction: InteractionPolicy;
}

export type Priority = 'low' | 'normal' | 'high';

/**
 * How work is carried out, at one level of the hierarchy: global defaults, a project, a task.
 * A field that is not there is not set at that level, and the next level decides. `model` and
 * `reasoning` may be `default` (the agent's own default), and belong to an `agent`: a level that
 * sets either must set the agent too.
 */
export interface ExecutionConfig {
  agent?: string;
  model?: string;
  reasoning?: string;
  interaction?: InteractionPolicy;
  priority?: Priority;
}

/** Why a run stopped rather than guess. Persisted on the run. */
export interface Blocker {
  summary: string;
  detail?: string;
  options?: string[];
  /** `question`: the agent asked something its policy did not allow; `report`: it said it was blocked. */
  source: 'question' | 'report';
  kind?: QuestionKind;
  raisedAt: string;
}

/** What a run in `waiting_for_user` is waiting for: an answer, or the next message. */
export type WaitingKind = 'question' | 'idle';

export interface GitRemote {
  name: string;
  url: string;
}

export interface GitRepository {
  projectId: string;
  rootPath: string;
  commonDir: string;
  currentBranch: string;
  headCommit: string;
  defaultBranch: string;
  remotes: GitRemote[];
  inspectedAt: string;
}

export interface Project {
  id: string;
  name: string;
  repoPath: string;
  /** The project's defaults for how its tasks are carried out. */
  execution: ExecutionConfig;
  createdAt: string;
  updatedAt: string;
  repository?: GitRepository;
}

export interface Task {
  id: string;
  projectId: string;
  title: string;
  description: string;
  state: TaskState;
  position: number;
  /** What this task overrides about how its runs are carried out; anything unset is inherited. */
  execution: ExecutionConfig;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface Run {
  id: string;
  taskId: string;
  projectId: string;
  agentId: string;
  /** What the agent was started with; absent means the agent's own default. */
  model?: string;
  reasoning?: string;
  state: RunState;
  worktreeId?: string;
  sessionRef?: string;
  reason?: string;
  prompt?: string;
  waiting?: WaitingKind;
  /** The policy the run started with. A copy: editing the task does not change a run that is working. */
  policy: ExecutionPolicy;
  /** Set while the run is `blocked`. */
  blocker?: Blocker;
  /** The latest thing the agent did, as one line. */
  activity?: string;
  activityAt?: string;
  exitCode?: number;
  version: number;
  createdAt: string;
  updatedAt: string;
  endedAt?: string;
}

/** What the agent wants from the user; the interface words each differently. */
export type QuestionKind = 'clarification' | 'decision' | 'approval' | 'selection' | 'instruction';

export type QuestionState = 'pending' | 'answered' | 'cancelled';

/** Why a question was closed without its answer reaching the agent. */
export type CancelReason = 'run_ended' | 'interrupted' | 'withdrawn';

/** Something an agent asked the user. Self-contained: it says where it belongs. */
export interface Question {
  id: string;
  runId: string;
  taskId: string;
  projectId: string;
  kind: QuestionKind;
  prompt: string;
  /** What the user needs to judge it: the command, the plan, the change. Plain text. */
  context?: string;
  /** Suggested answers, shown as one-tap choices. */
  options?: string[];
  /** Whether a typed answer is accepted. When false the answer must be one of `options`. */
  allowFreeText: boolean;
  state: QuestionState;
  answer?: string;
  /** Who gave the answer: the user, or the run's policy on their behalf (never for an approval). */
  answeredBy?: 'user' | 'policy';
  cancelReason?: CancelReason;
  askedAt: string;
  answeredAt?: string;
  deliveredAt?: string;
  closedAt?: string;
}

export type SignIn = 'signed_in' | 'signed_out' | 'unknown';

export interface Agent {
  id: string;
  name: string;
  /** The executable was found and runs. */
  installed: boolean;
  signIn?: SignIn;
  /** It can be started now. */
  available: boolean;
  version?: string;
  detail?: string;
  /** What to do next when it is not available: how to install it, or sign in. Never done for you. */
  guidance?: string;
  docsUrl?: string;
}

/** One choice in an agent's list of models or reasoning levels. */
export interface AgentOption {
  id: string;
  name: string;
  description?: string;
  /** The option the agent itself uses when nothing is passed. */
  default?: boolean;
  /** On a model: the reasoning levels it supports, when the agent says. */
  reasoning?: string[];
}

/** What can be chosen for an agent. The first of each list is always "Agent default". */
export interface AgentOptions {
  agentId: string;
  models: AgentOption[];
  reasoning: AgentOption[];
  modelsSource: 'agent' | 'configured' | 'builtin';
  reasoningSource: 'agent' | 'configured' | 'builtin';
  /** A model that is not listed may still be typed in. */
  customModels: boolean;
  note?: string;
}

export interface Runner {
  id: string;
  name: string;
  kind: 'local';
  hostname: string;
  os: string;
  arch: string;
  version: string;
  createdAt: string;
  lastSeenAt: string;
  online: boolean;
}

export interface Onboarding {
  completedAt?: string;
  skipped?: string[];
}

export type NetworkState = 'off' | 'starting' | 'needs_login' | 'needs_approval' | 'connected' | 'error';

export interface NetworkStatus {
  state: NetworkState;
  enabled: boolean;
  /** Where to sign in, while `needs_login`. A one-time link for the user. */
  authUrl?: string;
  hostname?: string;
  ips?: string[];
  /** The address to open on a phone. Carries no token. */
  url?: string;
  https: boolean;
  httpsHint?: string;
  tailnet?: string;
  health?: string[];
  error?: string;
  /** Who decided: the user in the app, nobody yet, or config.json. */
  choice: 'on' | 'off' | 'unset' | 'pinned_on' | 'pinned_off';
}

/** What a phone needs: the address, and a link and QR code that open it already signed in. */
export interface PhoneLink {
  url: string;
  link: string;
  qrSvg: string;
  https: boolean;
}

export type GitHubConnection = 'disabled' | 'missing' | 'signed_out' | 'signing_in' | 'signed_in' | 'error';

export interface GitHubLogin {
  state: 'idle' | 'pending' | 'done' | 'failed';
  /** The one-time code to enter on GitHub. */
  code?: string;
  url?: string;
  error?: string;
}

export interface GitHubStatusInfo {
  state: GitHubConnection;
  account?: { host: string; login: string; name?: string };
  version?: string;
  message?: string;
  guidance?: string;
  login: GitHubLogin;
}

/** A repository the user can reach on GitHub, with what this computer knows about it. */
export interface RepoChoice {
  fullName: string;
  owner: string;
  name: string;
  description?: string;
  private: boolean;
  fork: boolean;
  archived: boolean;
  defaultBranch?: string;
  pushedAt?: string;
  url: string;
  canPush: boolean;
  /** Clones found on this computer. Empty: GitHub only, it would have to be cloned first. */
  localPaths: string[];
  /** Set when it is already a Dev Board project. */
  project?: { id: string; name: string; path: string };
}

export interface RepoList {
  account: { host: string; login: string; name?: string };
  repos: RepoChoice[];
  truncated: boolean;
  /** Where a repository that is only on GitHub would be cloned. */
  cloneDir: string;
}

export interface Worktree {
  id: string;
  projectId: string;
  path: string;
  branch: string;
  baseRef: string;
  state: 'active' | 'removed';
}

export type OutputStream = 'assistant' | 'tool' | 'user' | 'system' | 'stderr';

/** Payload of an `agent.output` event. */
export interface AgentOutput {
  stream: OutputStream;
  text: string;
}

/** One page of a run's activity, oldest first. */
export interface RunEventsPage {
  events: ControllerEvent[];
  hasMore: boolean;
}

export interface Health {
  status: string;
  version: string;
  uptimeSeconds: number;
  database: string;
}

export interface ControllerEvent {
  seq: number;
  type: string;
  projectId?: string;
  taskId?: string;
  runId?: string;
  payload?: unknown;
  createdAt: string;
}

/** One project's activity, as counted by the Control Center. */
export interface ProjectActivity {
  projectId: string;
  name: string;
  needsInput: number;
  blocked: number;
  idle: number;
  running: number;
  /** Runs that failed on a task still waiting on them. */
  failed: number;
  /** Tasks waiting for review with nothing running. */
  review: number;
  /** Open repository-health findings of attention level or worse. */
  repoAttention: number;
  /** ... of risk level or worse. */
  repoRisk: number;
}

export interface AttentionReview {
  task: Task;
  projectName: string;
  /** The task's latest run, if it ever ran: who did the work. */
  lastRun?: Run;
}

export interface AttentionQuestion {
  question: Question;
  projectName: string;
  taskTitle: string;
  agentId: string;
}

export interface AttentionRun {
  run: Run;
  projectName: string;
  taskTitle: string;
}

/** What `GET /api/control-center` returns: what needs the user, and what is going on, in every project. */
export interface Overview {
  projects: ProjectActivity[];
  questions: AttentionQuestion[];
  runs: AttentionRun[];
  /** Runs that failed on a task still waiting on them, newest first. */
  failed: AttentionRun[];
  /** Tasks waiting for review with nothing running, oldest first. */
  review: AttentionReview[];
  /** Open repository-health findings that are a risk or worse, in every project, worst first. */
  repository: HealthFinding[];
}

// ---- the Git Control Center (internal/domain/gitstate.go) ----
//
// Local state and remote state are kept apart on purpose: what is known of a remote
// comes from remote-tracking refs, which are only as fresh as the last fetch, and
// from GitHub, which is asked separately and can be unavailable.

export interface GitCommit {
  sha: string;
  subject: string;
  author: string;
  date: string;
  merge?: boolean;
}

export interface GitCommitPage {
  items: GitCommit[];
  total: number;
  truncated: boolean;
}

export type FileStatus = 'added' | 'modified' | 'deleted' | 'renamed' | 'copied' | 'typechange' | 'conflicted' | 'untracked';

export interface GitFileChange {
  path: string;
  oldPath?: string;
  status: FileStatus;
}

export interface GitChangeCounts {
  staged: number;
  unstaged: number;
  untracked: number;
  conflicted: number;
}

export interface GitWorkingTree {
  branch: string;
  head: string;
  detached: boolean;
  upstream?: string;
  ahead: number;
  behind: number;
  staged: GitFileChange[];
  unstaged: GitFileChange[];
  untracked: GitFileChange[];
  conflicted: GitFileChange[];
  counts: GitChangeCounts;
  clean: boolean;
  truncated: boolean;
  operation?: string;
}

export type UpstreamState = 'none' | 'gone' | 'in_sync' | 'ahead' | 'behind' | 'diverged';
export type TargetRelation = 'target' | 'same' | 'merged' | 'behind' | 'ahead' | 'diverged' | 'unknown';
export type TaskPhase = 'none' | 'active' | 'review' | 'completed' | 'idle';

export interface GitUpstream {
  name?: string;
  state: UpstreamState;
  ahead: number;
  behind: number;
}

export interface GitVsTarget {
  ahead: number;
  behind: number;
  relation: TargetRelation;
}

export interface GitBranchWorktree {
  path: string;
  primary: boolean;
  owned: boolean;
  worktreeId?: string;
  missing?: boolean;
  locked?: boolean;
  dirty?: GitChangeCounts;
  operation?: string;
}

export interface GitBranchOwnership {
  created: boolean;
  namespace: boolean;
  worktreeIds?: string[];
  taskId?: string;
  taskTitle?: string;
  taskState?: TaskState;
  phase: TaskPhase;
  runId?: string;
  runState?: RunState;
  runWaiting?: WaitingKind;
  agentId?: string;
  activeRun: boolean;
}

export type AttentionKind =
  | 'review'
  | 'unpushed'
  | 'diverged'
  | 'behind'
  | 'cleanup'
  | 'stale'
  | 'dirty'
  | 'no_remote'
  | 'gone'
  | 'missing'
  | 'operation';

export interface GitAttention {
  kind: AttentionKind;
  severity: 'action' | 'warn' | 'info';
  message: string;
}

export type BranchScope = 'local' | 'remote';

export interface GitBranch {
  name: string;
  ref: string;
  scope: BranchScope;
  remote?: string;
  sha: string;
  subject: string;
  commitDate: string;
  head: boolean;
  target: boolean;
  protected: boolean;
  unusual?: string;
  localName?: string;
  upstream: GitUpstream;
  vsTarget: GitVsTarget;
  merged: boolean;
  notPushed: number;
  stale: boolean;
  staleWhy?: string;
  worktree?: GitBranchWorktree;
  devboard: GitBranchOwnership;
  attention: GitAttention[];
}

export interface GitWorktree {
  path: string;
  head?: string;
  branch?: string;
  detached: boolean;
  primary: boolean;
  locked?: boolean;
  prunable?: boolean;
  missing?: boolean;
  owned: boolean;
  worktreeId?: string;
  dirty?: GitChangeCounts;
  operation?: string;
  taskId?: string;
  taskTitle?: string;
  runId?: string;
  runState?: RunState;
  activeRun: boolean;
}

export interface GitTarget {
  name: string;
  source: string;
  sha?: string;
  localExists: boolean;
  checkedOut?: string;
  upstream: GitUpstream;
}

export interface GitHead {
  branch: string;
  commit: string;
  subject?: string;
  detached: boolean;
  unborn: boolean;
}

export interface GitLocal {
  name: string;
  rootPath: string;
  head: GitHead;
  target: GitTarget;
  workingTree: GitWorkingTree;
  recentCommits: GitCommit[];
}

export interface GitSync {
  branch: string;
  upstream: GitUpstream;
  notPushed: GitCommitPage;
  notPulled: GitCommitPage;
  basis: string;
}

export interface GitRemoteInfo {
  remotes: GitRemote[];
  lastFetchedAt?: string;
  sync?: GitSync;
  github: { detected: boolean; host?: string; repo?: string; remote?: string };
}

export interface GitSummary {
  branches: number;
  devboard: number;
  needsYou: number;
  warnings: number;
  mergeable: number;
  cleanup: number;
  unpushed: number;
  worktrees: number;
  dirtyTrees: number;
}

export interface GitOverview {
  projectId: string;
  generatedAt: string;
  local: GitLocal;
  remote: GitRemoteInfo;
  branches: GitBranch[];
  worktrees: GitWorktree[];
  summary: GitSummary;
  notes?: string[];
}

export interface GitDiffFile {
  path: string;
  oldPath?: string;
  status: FileStatus;
  additions: number;
  deletions: number;
  binary?: boolean;
}

export interface GitComparison {
  branch: string;
  branchSha: string;
  target: string;
  targetSha: string;
  mergeBase?: string;
  basis: string;
  ahead: number;
  behind: number;
  relation: TargetRelation;
  unique: GitCommitPage;
  missing: GitCommitPage;
  files: GitDiffFile[];
  filesTotal: number;
  offset: number;
  limit: number;
  additions: number;
  deletions: number;
  binaryFiles: number;
  truncated: boolean;
}

export interface GitFileDiff {
  path: string;
  oldPath?: string;
  status?: FileStatus;
  binary: boolean;
  additions: number;
  deletions: number;
  diff: string;
  offset: number;
  lines: number;
  totalLines: number;
  hasMore: boolean;
  truncated: boolean;
}

export interface WorkingChanges {
  path: string;
  primary: boolean;
  owned: boolean;
  worktreeId?: string;
  tree: GitWorkingTree;
  taskId?: string;
  taskTitle?: string;
}

export type ActionOutcome =
  | 'done'
  | 'refused'
  | 'noop'
  | 'conflict'
  | 'rejected'
  | 'unavailable'
  | 'auth_failed'
  | 'failed'
  | 'unverified';

export interface GitBlocker {
  code: string;
  message: string;
}

export interface GitConflictCheck {
  method: 'simulation' | 'overlap' | 'none';
  result: 'clean' | 'conflicts' | 'possible' | 'unknown';
  files?: string[];
  note: string;
}

export interface GitMergePlan {
  branch: string;
  branchSha: string;
  target: string;
  targetSha: string;
  strategy: 'merge' | 'ff-only';
  targetWorktree?: string;
  ahead: number;
  behind: number;
  relation: TargetRelation;
  fastForwardable: boolean;
  filesChanged: number;
  additions: number;
  deletions: number;
  conflicts: GitConflictCheck;
  blockers: GitBlocker[];
  warnings: string[];
  canMerge: boolean;
  checkedAt: string;
}

export interface GitDeletePlan {
  branch: string;
  branchSha: string;
  target: string;
  merged: boolean;
  mergedVia?: 'ancestry' | 'pull_request';
  remoteExists: boolean;
  remoteRef?: string;
  remoteSha?: string;
  blockers: GitBlocker[];
  warnings: string[];
  canDelete: boolean;
  canDeleteRemote: boolean;
  checkedAt: string;
}

export interface GitCleanPlan {
  worktreeId: string;
  path: string;
  branch: string;
  head?: string;
  missing: boolean;
  dirty?: GitChangeCounts;
  blockers: GitBlocker[];
  warnings: string[];
  canClean: boolean;
  checkedAt: string;
}

export interface GitActionResult {
  action: string;
  outcome: ActionOutcome;
  ok: boolean;
  message: string;
  blockers?: GitBlocker[];
  warnings?: string[];
  /** What changed on this computer. */
  local?: { ref?: string; before?: string; after?: string; note?: string };
  /** What the remote itself confirmed, when it was asked. `verified` is true only then. */
  remote?: { remote?: string; ref?: string; sha?: string; verified: boolean; checkedAt: string; note?: string };
  pullRequest?: GitHubPR;
  git?: string;
}

export interface GitHubPR {
  number: number;
  title: string;
  url: string;
  state: 'open' | 'closed' | 'merged';
  draft: boolean;
  headBranch: string;
  baseBranch: string;
  headSha?: string;
  author?: string;
  crossRepo?: boolean;
  review?: 'approved' | 'changes_requested' | 'review_required';
  mergeable?: 'mergeable' | 'conflicting';
  checks: { state: 'passing' | 'failing' | 'pending' | 'none'; total: number; passed: number; failed: number; pending: number };
  createdAt?: string;
  updatedAt?: string;
  mergedAt?: string;
  closedAt?: string;
  taskId?: string;
  taskTitle?: string;
  runId?: string;
}

export interface GitHubState {
  available: boolean;
  reason?: 'no_remote' | 'not_github' | 'gh_missing' | 'unauthenticated' | 'error';
  message?: string;
  host?: string;
  repo?: string;
  pullRequests: GitHubPR[];
  fetchedAt: string;
}

// ---- repository health (internal/domain/health.go) ----
//
// What Git state needs attention, as findings. Each says what is wrong, why, the evidence,
// the next step, and whether Dev Board can do it. Nothing here is ever executed by the
// health system: an action opens the same confirmation as anywhere else in the Git screen.

export type HealthSeverity = 'info' | 'attention' | 'risk' | 'critical';
export type HealthState = 'open' | 'resolved' | 'dismissed';
/** Whether a signal is a fact (deterministic) or a reasoned guess (heuristic, worded as "may"). */
export type HealthBasis = 'deterministic' | 'heuristic';
export type HealthCategory = 'uncommitted' | 'unsynced' | 'branch' | 'worktree' | 'orchestration' | 'operation';

export type HealthFindingType =
  | 'uncommitted_work'
  | 'unpushed_commits'
  | 'branch_not_pushed'
  | 'remote_ahead'
  | 'upstream_diverged'
  | 'remote_branch_deleted'
  | 'remote_state_stale'
  | 'merged_branch_present'
  | 'branch_content_on_target'
  | 'abandoned_branch'
  | 'stale_branch'
  | 'branch_far_behind'
  | 'task_done_unmerged'
  | 'finished_work_unmerged'
  | 'missing_branch_for_task'
  | 'branch_overlap'
  | 'branch_conflict'
  | 'orphaned_worktree'
  | 'worktree_without_run'
  | 'worktree_retained_after_done'
  | 'worktree_metadata_mismatch'
  | 'operation_interrupted'
  | 'unresolved_conflicts'
  | 'automation_blocked'
  | 'repository_unreadable';

export type HealthActionKind =
  | 'review_changes'
  | 'push_branch'
  | 'sync_branch'
  | 'merge_branch'
  | 'delete_branch'
  | 'clean_worktree'
  | 'finish_operation'
  | 'fetch'
  | 'create_task'
  | 'ask_agent'
  | 'inspect';

export interface HealthEvidence {
  label: string;
  value: string;
}

export interface HealthSubject {
  branch?: string;
  /** Other branches involved, for an overlap. */
  related?: string[];
  worktreeId?: string;
  worktreePath?: string;
  taskId?: string;
  taskTitle?: string;
  runId?: string;
}

export interface HealthAction {
  kind: HealthActionKind;
  label: string;
  detail?: string;
  /** Dev Board has a guarded operation for this and what it needs is true now. It is never run automatically. */
  canPerform: boolean;
  /** Why not, and what to do instead. */
  reason?: string;
  destructive?: boolean;
  branch?: string;
  worktreeId?: string;
  taskTitle?: string;
  taskDescription?: string;
}

export interface HealthFinding {
  id: string;
  projectId: string;
  projectName?: string;
  type: HealthFindingType;
  category: HealthCategory;
  severity: HealthSeverity;
  basis: HealthBasis;
  title: string;
  explanation: string;
  subject: HealthSubject;
  evidence: HealthEvidence[];
  action: HealthAction;
  state: HealthState;
  detectedAt: string;
  updatedAt: string;
  resolvedAt?: string;
  dismissedAt?: string;
  dismissedSeverity?: HealthSeverity;
}

export interface HealthSummary {
  state: 'healthy' | 'attention' | 'risk' | 'critical';
  /** "Healthy", or what is wrong: "1 risk · 3 items need attention". */
  headline: string;
  counts: { info: number; attention: number; risk: number; critical: number };
  needsAttention: number;
  dismissed: number;
  /** 0-100. Secondary: the findings say what is wrong; a number cannot. */
  score: number;
}

export interface RepositoryHealth {
  projectId: string;
  summary: HealthSummary;
  /** Open findings, worst first. Info ones are housekeeping. */
  findings: HealthFinding[];
  dismissed: HealthFinding[];
  /** Findings that stopped being true in the last day. */
  resolved: HealthFinding[];
  check?: { projectId: string; checkedAt: string; durationMs: number; error?: string };
}
