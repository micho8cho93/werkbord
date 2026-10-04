import type { Agent, Health, Project, Question, Run, Task, TaskState } from './types';

const TOKEN_KEY = 'devboard.token';

/** Error returned by the controller, carrying its machine-readable code. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
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
  history.replaceState(null, '', location.pathname + '#/board');
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
    try {
      const data = (await res.json()) as { error?: { code: string; message: string } };
      if (data.error) ({ code, message } = data.error);
    } catch {
      // Non-JSON error body.
    }
    throw new ApiError(res.status, code, message);
  }
  return (await res.json()) as T;
}

export const api = {
  health: () => request<Health>('GET', '/api/health'),

  listProjects: () => request<{ projects: Project[] }>('GET', '/api/projects').then((r) => r.projects),
  registerProject: (path: string, name: string) => request<Project>('POST', '/api/projects', { path, name }),
  refreshProject: (id: string) => request<Project>('POST', `/api/projects/${encodeURIComponent(id)}/refresh`),

  listTasks: (projectId: string) =>
    request<{ tasks: Task[] }>('GET', `/api/projects/${encodeURIComponent(projectId)}/tasks`).then((r) => r.tasks),
  createTask: (projectId: string, title: string, description = '') =>
    request<Task>('POST', `/api/projects/${encodeURIComponent(projectId)}/tasks`, { title, description }),
  moveTask: (task: Task, state: TaskState) =>
    request<Task>('PATCH', `/api/tasks/${encodeURIComponent(task.id)}`, { state, version: task.version }),

  listActiveRuns: () => request<{ runs: Run[] }>('GET', '/api/runs').then((r) => r.runs),
  listPendingQuestions: () => request<{ questions: Question[] }>('GET', '/api/questions').then((r) => r.questions),
  listAgents: () => request<{ agents: Agent[] }>('GET', '/api/agents').then((r) => r.agents),
};

/** URL for the event stream. EventSource cannot send headers, so the token goes in the query. */
export function eventsURL(): string {
  const token = getToken();
  return token ? `/api/events?access_token=${encodeURIComponent(token)}` : '/api/events';
}
