import type {
  Agent,
  ExecutionPolicy,
  Health,
  Overview,
  Project,
  Question,
  Run,
  RunEventsPage,
  Task,
  TaskState,
  Worktree,
} from './types';

const TOKEN_KEY = 'devboard.token';

/** Error returned by the controller, carrying its machine-readable code. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    /** For a refused answer: the question as it now stands (answered elsewhere, or closed). */
    readonly question?: Question,
  ) {
    super(message);
  }
}

function storage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function getToken(): string {
  try {
    return storage()?.getItem(TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

export function setToken(token: string): void {
  try {
    if (token) storage()?.setItem(TOKEN_KEY, token);
    else storage()?.removeItem(TOKEN_KEY);
  } catch {
    // Private mode: the token lasts for this page only.
  }
}

/**
 * Accepts a pairing link of the form /#token=<token> once, stores the token,
 * and removes it from the address bar.
 */
export function adoptTokenFromURL(): void {
  const m = /(?:^#|&)token=([^&]+)/.exec(location.hash);
  if (!m) return;
  setToken(decodeURIComponent(m[1]));
  history.replaceState(null, '', location.pathname + '#/');
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  } catch {
    throw new ApiError(0, 'offline', 'Cannot reach the controller.');
  }
  if (!res.ok) {
    let code = 'http_error';
    let message = `${res.status} ${res.statusText}`;
    let question: Question | undefined;
    try {
      const data = (await res.json()) as { error?: { code: string; message: string }; question?: Question };
      if (data.error) ({ code, message } = data.error);
      question = data.question;
    } catch {
      // Non-JSON error body.
    }
    throw new ApiError(res.status, code, message, question);
  }
  return (await res.json()) as T;
}

const enc = encodeURIComponent;

/** Everything that belongs to a project is asked for through it: there is no way to name a task, run or question without its project. */
const inProject = (projectId: string) => `/api/projects/${enc(projectId)}`;

/** What a task can be changed to. Fields left out are left alone. */
export interface TaskEdit {
  title?: string;
  description?: string;
  state?: TaskState;
  policy?: ExecutionPolicy;
}

export const api = {
  health: () => request<Health>('GET', '/api/health'),
  listAgents: () => request<{ agents: Agent[] }>('GET', '/api/agents').then((r) => r.agents),

  // Global: the projects, and the one view that looks across all of them.
  listProjects: () => request<{ projects: Project[] }>('GET', '/api/projects').then((r) => r.projects),
  registerProject: (path: string, name: string) => request<Project>('POST', '/api/projects', { path, name }),
  controlCenter: () => request<Overview>('GET', '/api/control-center'),

  // In a project.
  refreshProject: (projectId: string) => request<Project>('POST', `${inProject(projectId)}/refresh`),

  listTasks: (projectId: string) => request<{ tasks: Task[] }>('GET', `${inProject(projectId)}/tasks`).then((r) => r.tasks),
  createTask: (projectId: string, title: string, description = '', policy?: ExecutionPolicy) =>
    request<Task>('POST', `${inProject(projectId)}/tasks`, { title, description, ...(policy ? { policy } : {}) }),
  editTask: (task: Task, edit: TaskEdit) =>
    request<Task>('PATCH', `${inProject(task.projectId)}/tasks/${enc(task.id)}`, { ...edit, version: task.version }),
  moveTask: (task: Task, state: TaskState) => api.editTask(task, { state }),

  startRun: (projectId: string, taskId: string, agentId: string, instructions = '', resume = false, policy?: ExecutionPolicy) =>
    request<Run>('POST', `${inProject(projectId)}/tasks/${enc(taskId)}/runs`, {
      agentId,
      instructions,
      resume,
      ...(policy ? { policy } : {}),
    }),
  listTaskRuns: (projectId: string, taskId: string) =>
    request<{ runs: Run[] }>('GET', `${inProject(projectId)}/tasks/${enc(taskId)}/runs`).then((r) => r.runs),
  /** The latest run of each task: what the board's cards show. */
  listProjectRuns: (projectId: string) => request<{ runs: Run[] }>('GET', `${inProject(projectId)}/runs`).then((r) => r.runs),
  /** The project's run history, newest first. */
  listActivity: (projectId: string, limit = 100) =>
    request<{ runs: Run[] }>('GET', `${inProject(projectId)}/activity?limit=${limit}`).then((r) => r.runs),
  getRun: (projectId: string, id: string) => request<Run>('GET', `${inProject(projectId)}/runs/${enc(id)}`),
  runEvents: (projectId: string, id: string, before = 0, limit = 200) =>
    request<RunEventsPage>('GET', `${inProject(projectId)}/runs/${enc(id)}/events?limit=${limit}${before ? `&before=${before}` : ''}`),
  sendInput: (projectId: string, id: string, text: string) =>
    request<Run>('POST', `${inProject(projectId)}/runs/${enc(id)}/input`, { text }),
  finishRun: (projectId: string, id: string) => request<Run>('POST', `${inProject(projectId)}/runs/${enc(id)}/finish`),
  stopRun: (projectId: string, id: string) => request<Run>('POST', `${inProject(projectId)}/runs/${enc(id)}/stop`),

  listPendingQuestions: (projectId: string) =>
    request<{ questions: Question[] }>('GET', `${inProject(projectId)}/questions`).then((r) => r.questions),
  answerQuestion: (q: Pick<Question, 'projectId' | 'id'>, answer: string) =>
    request<Question>('POST', `${inProject(q.projectId)}/questions/${enc(q.id)}/answer`, { answer }),
  getWorktree: (projectId: string, id: string) => request<Worktree>('GET', `${inProject(projectId)}/worktrees/${enc(id)}`),
};

/** URL for the event stream. EventSource cannot send headers, so the token goes in the query. */
export function eventsURL(): string {
  const token = getToken();
  return token ? `/api/events?access_token=${encodeURIComponent(token)}` : '/api/events';
}
