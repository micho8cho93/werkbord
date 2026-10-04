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

export type RunState = 'starting' | 'running' | 'waiting_for_user' | 'completed' | 'failed' | 'stopped';

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
  /** The latest thing the agent did, as one line. */
  activity?: string;
  activityAt?: string;
  exitCode?: number;
  version: number;
  createdAt: string;
  updatedAt: string;
  endedAt?: string;
}

export interface Question {
  id: string;
  runId: string;
  kind: 'ask' | 'approval';
  prompt: string;
  options?: string[];
  status: 'pending' | 'answered' | 'cancelled';
  answer?: string;
  createdAt: string;
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
