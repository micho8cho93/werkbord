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

export interface GitRemote {
  name: string;
  url: string;
}

export interface GitRepository {
  projectId: string;
  rootPath: string;
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
  reason?: string;
  version: number;
  createdAt: string;
  updatedAt: string;
  endedAt?: string;
}

export interface Question {
  id: string;
  runId: string;
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
