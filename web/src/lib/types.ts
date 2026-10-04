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
  /** How runs of this task are carried out by default. */
  policy: ExecutionPolicy;
  version: number;
  createdAt: string;
  updatedAt: string;
}

export interface Run {
  id: string;
  taskId: string;
  projectId: string;
  agentId: string;
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

export interface Agent {
  id: string;
  name: string;
  available: boolean;
  version?: string;
  detail?: string;
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
}
