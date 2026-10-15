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
  Label,
  LabelUse,
  Plan,
  PlanIssue,
  TimelineWarning,
  WorkMode,
  NetworkStatus,
  RelayStatus,
  Onboarding,
  Overview,
  PhoneAddress,
  PhoneEnrollment,
  PhoneEntry,
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
  WaitingItem,
  WorkingChanges,
  MCPConnection,
  MCPBoardOperation,
  MCPClientGrant,
  MCPProposal,
  MCPTaskPlan,
} from './types';
import { normalizeRunner, normalizeRunners } from './runners';
import { startRefusal } from './scheduling';
import { isPhone } from './phones/capability';

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
    /** Fields the user can correct when a plan or dependency draft is rejected. */
    readonly issues?: PlanIssue[],
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

/**
 * Takes the enrollment code out of a phone link of the form /#enroll=<code> and removes it from the address bar. It is
 * the one thing a scanned QR code carries; the browser never sends a fragment to a server. Null when there is none.
 */
export function takeEnrollmentCode(): string | null {
  const m = /(?:^#|&)enroll=([^&]+)/.exec(location.hash);
  if (!m) return null;
  history.replaceState(null, '', location.pathname + '#/');
  try {
    return decodeURIComponent(m[1]);
  } catch {
    return '';
  }
}

export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  } catch {
    throw new ApiError(0, 'offline', 'Cannot reach Werkbord.');
  }
  if (!res.ok) {
    let code = 'http_error';
    let message = 'Werkbord returned an unexpected response. Check the current state before repeating an action.';
    let question: Question | undefined;
    let issues: PlanIssue[] | undefined;
    try {
      const data = (await res.json()) as { error?: { code: string; message: string }; question?: Question; issues?: PlanIssue[] };
      if (data.error) ({ code, message } = data.error);
      question = data.question;
      issues = data.issues;
    } catch {
      // Non-JSON error body.
    }
    throw new ApiError(res.status, code, message, question, issues);
  }
  return (await res.json()) as T;
}

/** For an answer with no body (204). Errors are the same as `request`'s. */
export async function requestNoContent(method: string, path: string): Promise<void> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  let res: Response;
  try {
    res = await fetch(path, { method, headers });
  } catch {
    throw new ApiError(0, 'offline', 'Cannot reach Werkbord.');
  }
  if (res.ok) return;
  let code = 'http_error';
  let message = 'Werkbord returned an unexpected response. Check the current state before repeating an action.';
  try {
    const data = (await res.json()) as { error?: { code: string; message: string } };
    if (data.error) ({ code, message } = data.error);
  } catch {
    // Non-JSON error body.
  }
  throw new ApiError(res.status, code, message);
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
  workMode?: WorkMode;
  /** Replaces the whole set of labels. */
  labelIds?: string[];
  /** Replaces the planned dates; `{}` clears them. */
  plan?: Plan;
}

/** What a new task can say beyond its title and description. */
export interface NewTaskOptions {
  execution?: ExecutionConfig;
  workMode?: WorkMode;
  labelIds?: string[];
  plan?: Plan;
}

/** What may differ for one run only: the task's, the project's and the global choices apply to the rest. */
export interface RunChoice {
 runnerId?: string;
  agentId?: string;
  model?: string;
  reasoning?: string;
  interaction?: InteractionPolicy;
}

/** What a phone may not choose: how work is carried out is the owner's, at the computer (internal/api/phone.go). */
function withoutExecution<T extends object>(body: T): T {
  if (!isPhone()) return body;
  const copy = { ...body } as Record<string, unknown>;
  for (const key of ['execution', 'runnerId', 'agentId', 'model', 'reasoning', 'policy']) delete copy[key];
  return copy as T;
}

export const api = {
  analytics: (project: string, days: number, provider: string) => request<import('./analytics').AnalyticsReport>('GET', `/api/analytics?${new URLSearchParams({ project, days: String(days), provider })}`),
schedule: (projectId: string) => request<{decisions: SchedulingDecision[]}>('GET', `${inProject(projectId)}/schedule`).then(r => r.decisions),
 orchestrationSettings: (projectId: string) => request<{concurrencyLimit: number}>('GET', `${inProject(projectId)}/orchestration`),
 setOrchestrationSettings: (projectId: string, concurrencyLimit: number) => request<{concurrencyLimit: number}>('PUT', `${inProject(projectId)}/orchestration`, {concurrencyLimit}),
 generateHandoff: (projectId: string, runId: string) => request<Run>('POST', `${inProject(projectId)}/runs/${enc(runId)}/handoff`),
 saveHandoff: (run: Run, handoff: Handoff) => request<Run>('PUT', `${inProject(run.projectId)}/runs/${enc(run.id)}/handoff`, {version: run.version, handoff}),
 continueRun: (run: Run, choice: RunChoice, purpose: string, instructions: string, selectedContext: string) => request<Run>('POST', `${inProject(run.projectId)}/runs/${enc(run.id)}/continue`, withoutExecution({runnerId: choice.runnerId, agentId: choice.agentId, model: choice.model, reasoning: choice.reasoning, purpose, instructions, selectedContext, ...(choice.interaction ? {policy: {interaction:choice.interaction}} : {})})),
 health: () => request<Health>('GET', '/api/health'),
  mcpConnections: () => request<{connections:MCPConnection[]}>('GET', '/api/connections').then(r=>r.connections),
  mcpOperations: () => request<{operations:MCPBoardOperation[]}>('GET', '/api/mcp/operations').then(r=>r.operations),
  mcpClientGrants: () => request<{grants:MCPClientGrant[]}>('GET', '/api/mcp/grants').then(r=>r.grants),
  createMCPClientGrant: (grant:{name:string; projects:string[]; operations:string[]}) =>
    request<{grant:MCPClientGrant; token:string}>('POST', '/api/mcp/grants', grant),
  revokeMCPClientGrant: (id:string) => requestNoContent('DELETE', `/api/mcp/grants/${enc(id)}`),
  mcpProposals: () => request<{proposals:MCPProposal[]}>('GET', '/api/mcp/proposals').then(r=>r.proposals),
  replaceMCPTaskPlan: (grantId:string, actionId:string, plan:MCPTaskPlan) =>
    request<MCPProposal>('PUT', `/api/mcp/proposals/${enc(grantId)}/${enc(actionId)}`, plan),
  resolveMCPProposal: (grantId:string, actionId:string, approve:boolean) =>
    request('POST', `/api/mcp/proposals/${enc(grantId)}/${enc(actionId)}`, {approve}),
  saveMCPConnection: (connection: Partial<MCPConnection> & {name:string; transport:MCPConnection['transport']; bearerToken?:string}) =>
    request<{connection:MCPConnection}>(connection.id ? 'PUT' : 'POST', connection.id ? `/api/connections/${enc(connection.id)}` : '/api/connections', connection).then(r=>r.connection),
  deleteMCPConnection: (id:string) => requestNoContent('DELETE', `/api/connections/${enc(id)}`),
  testMCPConnection: (id:string) => request<{connection:MCPConnection}>('POST', `/api/connections/${enc(id)}/test`, {}).then(r=>r.connection),
  reauthorizeMCPConnection: (id:string) => request<{connection:MCPConnection}>('POST', `/api/connections/${enc(id)}/reauthorize`, {}).then(r=>r.connection),
  setMCPConnectionEnabled: (id:string, enabled:boolean) => request<{connection:MCPConnection}>('POST', `/api/connections/${enc(id)}/enabled`, {enabled}).then(r=>r.connection),
  setMCPContext: (id:string, context:'assistant'|'coding', enabled:boolean) => request<{connection:MCPConnection}>('POST', `/api/connections/${enc(id)}/contexts/${context}`, {enabled}).then(r=>r.connection),
  setMCPToolPolicy: (id:string, tool:string, context:'assistant'|'coding', policy:'deny'|'confirm'|'allow') => request<{connection:MCPConnection}>('PUT', `/api/connections/${enc(id)}/tools/${enc(tool)}/policy`, {context,policy}).then(r=>r.connection),
  readMCPResource: (id:string, context:'assistant'|'coding', uri:string) => request<{result:unknown}>('POST', `/api/connections/${enc(id)}/resources/read`, {context,uri}).then(r=>r.result),
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
  saveRunner: (runner: Runner) => request<Runner>('PUT', `/api/runners/${enc(runner.id)}`, {name: runner.name, capacity: runner.capacity, automatic: runner.automatic, disabled: runner.disabled, projects: runner.projects, allowClone: runner.allowClone}).then(normalizeRunner),
  revokeRunner: (id: string) => request('POST', `/api/runners/${enc(id)}/revoke`),
  removeRunner: (id: string) => request<Runner>('DELETE', `/api/runners/${enc(id)}`).then(normalizeRunner),
  routingRules: () => request<{rules: import('./types').RoutingRule[]}>('GET', '/api/routing-rules').then(r=>r.rules),
  saveRoutingRules: (rules: import('./types').RoutingRule[]) => request<{rules: import('./types').RoutingRule[]}>('PUT', '/api/routing-rules', {rules}),
  assessRun: (run: Run, acceptance: 'accepted'|'rejected') => request<Run>('PATCH', `${inProject(run.projectId)}/runs/${enc(run.id)}/assessment`, {acceptance}),
  setUsage: (run: Run, usage: import('./types').Usage) => request<Run>('PUT', `${inProject(run.projectId)}/runs/${enc(run.id)}/usage`, usage),
  listRunners: () => request<{ runners: Runner[] }>('GET', '/api/runners').then((r) => normalizeRunners(r.runners)),
  onboarding: () => request<Onboarding>('GET', '/api/onboarding'),
  completeOnboarding: (skipped: string[] = []) => request<Onboarding>('POST', '/api/onboarding/complete', { skipped }),
  setOnboardingStep: (step: string, state: 'done' | 'skipped' | '') => request<Onboarding>('POST', '/api/onboarding/step', { step, state }),
  setOnboardingVariant: (variant: 'github' | 'local') => request<Onboarding>('POST', '/api/onboarding/variant', { variant }),
  markTourSeen: (surface: string) => request<Onboarding>('POST', '/api/onboarding/tours/seen', { surface }),
  resetTours: (surface = '') => request<Onboarding>('POST', '/api/onboarding/tours/reset', { surface }),
  resetOnboarding: () => request<Onboarding>('POST', '/api/onboarding/reset', {}),

  // The private network that lets a phone reach this controller.
  network: () => request<NetworkStatus>('GET', '/api/network'),
  relay: () => request<RelayStatus>('GET', '/api/relay'),
  enableNetwork: () => request<NetworkStatus>('POST', '/api/network/enable', {}),
  disableNetwork: () => request<NetworkStatus>('POST', '/api/network/disable', {}),
  phoneAddress: () => request<PhoneAddress>('GET', '/api/network/phone'),

  // Phones: each has a credential of its own, that the owner can see and revoke. A phone enrolls with a single-use code
  // from its QR code and is given its token once; it can then read and rename itself and sign itself out.
  phones: () => request<{ phones: PhoneEntry[] }>('GET', '/api/phones').then((r) => r.phones),
  createPhoneCode: () => request<PhoneEnrollment>('POST', '/api/phones/codes', {}),
  revokePhone: (id: string) => requestNoContent('DELETE', `/api/phones/${enc(id)}`),
  renamePhone: (id: string, name: string) => request<PhoneEntry>('PATCH', `/api/phones/${enc(id)}`, { name }),
  enrollPhone: (code: string, name: string) => request<PhoneEntry & { token: string }>('POST', '/api/phones/enroll', { code, name }),
  phoneSelf: () => request<PhoneEntry>('GET', '/api/phones/self'),
  renameSelf: (name: string) => request<PhoneEntry>('PATCH', '/api/phones/self', { name }),
  signOutSelf: () => requestNoContent('POST', '/api/phones/self/signout'),

  // GitHub, through the user's own GitHub CLI. Optional.
  github: () => request<GitHubStatusInfo>('GET', '/api/github'),
  githubScopes: () => request<GitHubLogin>('POST', '/api/github/scopes', {}),
  githubLogin: () => request<GitHubLogin>('POST', '/api/github/login', {}),
  githubCancelLogin: () => request<GitHubStatusInfo>('POST', '/api/github/login/cancel', {}),
  githubRepos: () => request<RepoList>('GET', '/api/github/repos'),
  githubAddRepo: (fullName: string, path = '') =>
    request<{ project: Project; cloned: boolean }>('POST', '/api/github/repos/add', { fullName, path }),

  githubCreateRepo: (req: { name: string; description: string; private: boolean }) =>
    request<{ repository: { fullName: string; url: string; private: boolean }; project: Project | null; pagesUrl?: string; pagesError?: string }>('POST', '/api/github/repos/create', req),
  githubPages: (repo: string) => request<{ enabled: boolean; url?: string; state: 'disabled' | 'waiting' | 'live' }>('GET', `/api/github/pages?${new URLSearchParams({ repo })}`),

  // Global: the projects, and the one view that looks across all of them.
  listProjects: () => request<{ projects: Project[] }>('GET', '/api/projects').then((r) => r.projects),
  registerProject: (path: string, name: string) => request<Project>('POST', '/api/projects', { path, name }),
  /** A board and timeline with no Git repository behind it. */
  createWorkProject: (name: string) => request<Project>('POST', '/api/projects', { kind: 'work', name }),
  controlCenter: () => request<Overview>('GET', '/api/control-center').then((o) => (o.runners ? { ...o, runners: normalizeRunners(o.runners) } : o)),
  waiting: () => request<{ items: WaitingItem[] }>('GET', '/api/integration/waiting'),
  /** Registers the folder chosen for a waiting ticket; refused unless it is a clone of that repository. */
  linkWaiting: (path: string, repository: string) => request<Project>('POST', '/api/integration/waiting/link', { path, repository }),

  // In a project.
  refreshProject: (projectId: string) => request<Project>('POST', `${inProject(projectId)}/refresh`),

  listTasks: (projectId: string) => request<{ tasks: Task[] }>('GET', `${inProject(projectId)}/tasks`).then((r) => r.tasks),
  folders: (path = '') => request<{ path: string; parent: string; folders: { name: string; path: string; repository: boolean }[] }>('GET', `/api/folders?path=${enc(path)}`),
  archiveTask: (task: Task, archived = true) => request<Task>('PATCH', `${inProject(task.projectId)}/tasks/${enc(task.id)}`, { archived, version: task.version }),
  archiveDone: (projectId: string) => request<{ tasks: Task[] }>('POST', `${inProject(projectId)}/tasks/archive-done`).then(r => r.tasks),
  createTask: (projectId: string, title: string, description = '', execution?: ExecutionConfig, more: Omit<NewTaskOptions, 'execution'> = {}) =>
    request<Task>('POST', `${inProject(projectId)}/tasks`, withoutExecution({ title, description, ...(execution ? { execution } : {}), ...more })),
  /** What is wrong with a project's dependencies and planned dates. It only reports. */
  timeline: (projectId: string) => request<{ warnings: TimelineWarning[] }>('GET', `${inProject(projectId)}/timeline`).then((r) => r.warnings ?? []),

  // Labels: defined once, reused by every project.
  listLabels: () => request<{ labels: LabelUse[] }>('GET', '/api/labels').then((r) => r.labels),
  createLabel: (name: string, color: string, description = '') => request<Label>('POST', '/api/labels', { name, color, description }),
  editLabel: (label: Label, edit: { name?: string; color?: string; description?: string }) =>
    request<Label>('PATCH', `/api/labels/${enc(label.id)}`, { ...edit, version: label.version }),
  deleteLabel: (id: string) => requestNoContent('DELETE', `/api/labels/${enc(id)}`),
  editTask: (task: Task, edit: TaskEdit) =>
    request<Task>('PATCH', `${inProject(task.projectId)}/tasks/${enc(task.id)}`, withoutExecution({ ...edit, version: task.version })),
  moveTask: (task: Task, state: TaskState) => api.editTask(task, { state }),

  startRun: (projectId: string, taskId: string, choice: RunChoice = {}, instructions = '', resume = false) =>
    request<Run>('POST', `${inProject(projectId)}/tasks/${enc(taskId)}/runs`, withoutExecution({
      ...(choice.runnerId ? { runnerId:choice.runnerId } : {}),
      ...(choice.agentId ? { agentId: choice.agentId } : {}),
      ...(choice.model ? { model: choice.model } : {}),
      ...(choice.reasoning ? { reasoning: choice.reasoning } : {}),
      instructions,
      resume,
      ...(choice.interaction ? { policy: { interaction: choice.interaction } } : {}),
    })).catch((err: unknown) => {
      // A start the scheduler refused is said in plain words everywhere a run is started; the code stays "conflict".
      throw err instanceof ApiError && err.code === 'conflict' ? new ApiError(err.status, err.code, startRefusal(err.message), err.question) : err;
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
