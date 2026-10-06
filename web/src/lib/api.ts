import type {
  Handoff,
 Orchestration,
 SchedulingDecision,
 Agent,
  AgentOptions,
  BranchScope,
  ExecutionConfig,
  GitActionResult,
  GitCleanPlan,
  GitCommitPage,
  GitComparison,
  GitDeletePlan,
  GitFileDiff,
  GitHubLogin,
  GitHubState,
  GitHubStatusInfo,
  GitMergePlan,
  GitOverview,
  Health,
  InteractionPolicy,
  NetworkStatus,
  Onboarding,
  Overview,
  PhoneLink,
  RepoList,
  RepositoryHealth,
  Project,
  Question,
  Run,
  RunEventsPage,
  Runner,
  Task,
  TaskState,
  UpdateStatus,
  Worktree,
  WorkingChanges,
} from './types';

const TOKEN_KEY = 'devboard.token';
let memoryToken: string | null = null;

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
  if (memoryToken !== null) return memoryToken;
  try {
    return storage()?.getItem(TOKEN_KEY) ?? '';
  } catch {
    return '';
  }
}

export function setToken(token: string): void {
  memoryToken = token;
  try {
    const store = storage();
    if (!store) return;
    if (token) store.setItem(TOKEN_KEY, token);
    else store.removeItem(TOKEN_KEY);
    memoryToken = null;
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
  try {
    setToken(decodeURIComponent(m[1]));
  } catch {
    // A malformed link must not crash startup or replace a valid credential.
  }
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
  orchestration?: Orchestration;
 title?: string;
  description?: string;
  state?: TaskState;
  /** Replaces the task's overrides: what is left out is inherited. */
  execution?: ExecutionConfig;
}

/** What may differ for one run only: the task's, the project's and the global choices apply to the rest. */
export interface RunChoice {
 runnerId?: string;
  agentId?: string;
  model?: string;
  reasoning?: string;
  interaction?: InteractionPolicy;
}

export const api = {
schedule: (projectId: string) => request<{decisions: SchedulingDecision[]}>('GET', `${inProject(projectId)}/schedule`).then(r => r.decisions),
 orchestrationSettings: (projectId: string) => request<{concurrencyLimit: number}>('GET', `${inProject(projectId)}/orchestration`),
 setOrchestrationSettings: (projectId: string, concurrencyLimit: number) => request<{concurrencyLimit: number}>('PUT', `${inProject(projectId)}/orchestration`, {concurrencyLimit}),
 generateHandoff: (projectId: string, runId: string) => request<Run>('POST', `${inProject(projectId)}/runs/${enc(runId)}/handoff`),
 saveHandoff: (run: Run, handoff: Handoff) => request<Run>('PUT', `${inProject(run.projectId)}/runs/${enc(run.id)}/handoff`, {version: run.version, handoff}),
 continueRun: (run: Run, choice: RunChoice, purpose: string, instructions: string, selectedContext: string) => request<Run>('POST', `${inProject(run.projectId)}/runs/${enc(run.id)}/continue`, {runnerId: choice.runnerId, agentId: choice.agentId, model: choice.model, reasoning: choice.reasoning, purpose, instructions, selectedContext, ...(choice.interaction ? {policy: {interaction:choice.interaction}} : {})}),
 health: () => request<Health>('GET', '/api/health'),
  /** Whether a newer release exists. It only reports: installing is done on the computer that runs Werkbord. */
  updateStatus: (refresh = false) => request<UpdateStatus>('GET', refresh ? '/api/update?refresh=1' : '/api/update'),
  listAgents: () => request<{ agents: Agent[] }>('GET', '/api/agents').then((r) => r.agents),
  /** What can be chosen for an agent; asks the agent, so slower than listAgents. */
  agentOptions: (agentId: string) => request<AgentOptions>('GET', `/api/agents/${enc(agentId)}/options`),

  // Defaults and first-time setup.
  getSettings: () => request<{ execution: ExecutionConfig }>('GET', '/api/settings').then((r) => r.execution ?? {}),
  setGlobalExecution: (execution: ExecutionConfig) =>
    request<{ execution: ExecutionConfig }>('PUT', '/api/settings/execution', execution).then((r) => r.execution ?? {}),
  setProjectExecution: (projectId: string, execution: ExecutionConfig) =>
    request<Project>('PUT', `${inProject(projectId)}/execution`, execution),
  pairRunner: (projects: string[], allowClone: boolean) => request<import('./types').Pairing>('POST', '/api/runners/pair', {projects, allowClone}),
  saveRunner: (runner: Runner) => request<Runner>('PUT', `/api/runners/${enc(runner.id)}`, {name: runner.name, capacity: runner.capacity, automatic: runner.automatic, disabled: runner.disabled, projects: runner.projects, allowClone: runner.allowClone}),
  revokeRunner: (id: string) => request('POST', `/api/runners/${enc(id)}/revoke`),
  removeRunner: (id: string) => request<Runner>('DELETE', `/api/runners/${enc(id)}`),
  routingRules: () => request<{rules: import('./types').RoutingRule[]}>('GET', '/api/routing-rules').then(r=>r.rules),
  saveRoutingRules: (rules: import('./types').RoutingRule[]) => request<{rules: import('./types').RoutingRule[]}>('PUT', '/api/routing-rules', {rules}),
  assessRun: (run: Run, acceptance: 'accepted'|'rejected') => request<Run>('PATCH', `${inProject(run.projectId)}/runs/${enc(run.id)}/assessment`, {acceptance}),
  setUsage: (run: Run, usage: import('./types').Usage) => request<Run>('PUT', `${inProject(run.projectId)}/runs/${enc(run.id)}/usage`, usage),
  listRunners: () => request<{ runners: Runner[] }>('GET', '/api/runners').then((r) => r.runners),
  onboarding: () => request<Onboarding>('GET', '/api/onboarding'),
  completeOnboarding: (skipped: string[] = []) => request<Onboarding>('POST', '/api/onboarding/complete', { skipped }),
  resetOnboarding: () => request<Onboarding>('POST', '/api/onboarding/reset', {}),

  // The private network that lets a phone reach this controller.
  network: () => request<NetworkStatus>('GET', '/api/network'),
  enableNetwork: () => request<NetworkStatus>('POST', '/api/network/enable', {}),
  disableNetwork: () => request<NetworkStatus>('POST', '/api/network/disable', {}),
  phoneLink: () => request<PhoneLink>('GET', '/api/network/phone'),

  // GitHub, through the user's own GitHub CLI. Optional.
  github: () => request<GitHubStatusInfo>('GET', '/api/github'),
  githubLogin: () => request<GitHubLogin>('POST', '/api/github/login', {}),
  githubCancelLogin: () => request<GitHubStatusInfo>('POST', '/api/github/login/cancel', {}),
  githubRepos: () => request<RepoList>('GET', '/api/github/repos'),
  githubAddRepo: (fullName: string, path = '') =>
    request<{ project: Project; cloned: boolean }>('POST', '/api/github/repos/add', { fullName, path }),

  // Global: the projects, and the one view that looks across all of them.
  listProjects: () => request<{ projects: Project[] }>('GET', '/api/projects').then((r) => r.projects),
  registerProject: (path: string, name: string) => request<Project>('POST', '/api/projects', { path, name }),
  controlCenter: () => request<Overview>('GET', '/api/control-center'),

  // In a project.
  refreshProject: (projectId: string) => request<Project>('POST', `${inProject(projectId)}/refresh`),

  listTasks: (projectId: string) => request<{ tasks: Task[] }>('GET', `${inProject(projectId)}/tasks`).then((r) => r.tasks),
  createTask: (projectId: string, title: string, description = '', execution?: ExecutionConfig) =>
    request<Task>('POST', `${inProject(projectId)}/tasks`, { title, description, ...(execution ? { execution } : {}) }),
  editTask: (task: Task, edit: TaskEdit) =>
    request<Task>('PATCH', `${inProject(task.projectId)}/tasks/${enc(task.id)}`, { ...edit, version: task.version }),
  moveTask: (task: Task, state: TaskState) => api.editTask(task, { state }),

  startRun: (projectId: string, taskId: string, choice: RunChoice = {}, instructions = '', resume = false) =>
    request<Run>('POST', `${inProject(projectId)}/tasks/${enc(taskId)}/runs`, {
      ...(choice.runnerId ? { runnerId:choice.runnerId } : {}),
      ...(choice.agentId ? { agentId: choice.agentId } : {}),
      ...(choice.model ? { model: choice.model } : {}),
      ...(choice.reasoning ? { reasoning: choice.reasoning } : {}),
      instructions,
      resume,
      ...(choice.interaction ? { policy: { interaction: choice.interaction } } : {}),
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

  // The Git Control Center. Reads change nothing. Every action answers with a GitActionResult
  // whose `outcome` says what happened: a refusal is an answer, not an error.
  git: {
    overview: (projectId: string) => request<GitOverview>('GET', `${inProject(projectId)}/git`),
    /** Asked separately: GitHub being slow or signed out must not hold up the local picture. */
    pullRequests: (projectId: string) => request<GitHubState>('GET', `${inProject(projectId)}/git/pull-requests`),
    commits: (projectId: string, scope: BranchScope, branch: string, skip = 0, limit = 30) =>
      request<GitCommitPage>('GET', `${inProject(projectId)}/git/commits${query({ scope, branch, skip, limit })}`),
    compare: (projectId: string, scope: BranchScope, branch: string, offset = 0, limit = 50, target = '') =>
      request<GitComparison>('GET', `${inProject(projectId)}/git/compare${query({ scope, branch, target, offset, limit })}`),
    /** A window onto one file's diff between two commits the comparison reported. */
    diff: (projectId: string, from: string, to: string, path: string, oldPath = '', offset = 0, lines = 400) =>
      request<GitFileDiff>('GET', `${inProject(projectId)}/git/diff${query({ from, to, path, oldPath, offset, lines })}`),
    changes: (projectId: string, worktree = '') =>
      request<WorkingChanges>('GET', `${inProject(projectId)}/git/changes${query({ worktree })}`),
    changeDiff: (projectId: string, worktree: string, kind: string, path: string, oldPath = '', offset = 0, lines = 400) =>
      request<GitFileDiff>('GET', `${inProject(projectId)}/git/changes/diff${query({ worktree, kind, path, oldPath, offset, lines })}`),

    fetch: (projectId: string) => request<GitActionResult>('POST', `${inProject(projectId)}/git/fetch`),
    push: (projectId: string, branch: string, expectedSha: string) =>
      request<GitActionResult>('POST', `${inProject(projectId)}/git/push`, { branch, expectedSha }),
    mergePlan: (projectId: string, m: MergeRequest) => request<GitMergePlan>('POST', `${inProject(projectId)}/git/merge/plan`, m),
    merge: (projectId: string, m: MergeRequest) => request<GitActionResult>('POST', `${inProject(projectId)}/git/merge`, m),
    deletePlan: (projectId: string, d: DeleteRequest) =>
      request<GitDeletePlan>('POST', `${inProject(projectId)}/git/branches/delete-plan`, d),
    deleteBranch: (projectId: string, d: DeleteRequest) =>
      request<GitActionResult>('POST', `${inProject(projectId)}/git/branches/delete`, d),
    cleanPlan: (projectId: string, worktreeId: string, headSha: string) =>
      request<GitCleanPlan>('POST', `${inProject(projectId)}/git/worktrees/clean-plan`, { worktreeId, headSha }),
    clean: (projectId: string, worktreeId: string, headSha: string) =>
      request<GitActionResult>('POST', `${inProject(projectId)}/git/worktrees/clean`, { worktreeId, headSha }),
    createPullRequest: (projectId: string, p: { branch: string; expectedSha: string; title: string; body: string; draft: boolean }) =>
      request<GitActionResult>('POST', `${inProject(projectId)}/git/pull-requests`, p),

    // Repository health. Reading is a database read; refresh looks at Git (never the network)
    // and changes nothing in the repository. Dismissing only says "I know".
    health: (projectId: string) => request<RepositoryHealth>('GET', `${inProject(projectId)}/git/health`),
    refreshHealth: (projectId: string) => request<RepositoryHealth>('POST', `${inProject(projectId)}/git/health/refresh`),
    dismissFinding: (projectId: string, id: string) =>
      request<RepositoryHealth>('POST', `${inProject(projectId)}/git/health/findings/${enc(id)}/dismiss`),
    reopenFinding: (projectId: string, id: string) =>
      request<RepositoryHealth>('POST', `${inProject(projectId)}/git/health/findings/${enc(id)}/reopen`),
  },
};

/** What a merge is asked of: the commits the user reviewed travel with it, so a branch that moved is refused. */
export interface MergeRequest {
  branch: string;
  branchSha: string;
  target: string;
  targetSha: string;
  strategy: 'merge' | 'ff-only';
}

export interface DeleteRequest {
  branch: string;
  branchSha: string;
  deleteRemote?: boolean;
}

/** A query string from the values that are set. Branch names and paths are data: they are always encoded. */
export function query(params: Record<string, string | number | undefined>): string {
  const parts: string[] = [];
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === '' || v === 0) continue;
    parts.push(`${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`);
  }
  return parts.length ? `?${parts.join('&')}` : '';
}

/** URL for the event stream. EventSource cannot send headers, so the token goes in the query. */
export function eventsURL(): string {
  const token = getToken();
  return token ? `/api/events?access_token=${encodeURIComponent(token)}` : '/api/events';
}
