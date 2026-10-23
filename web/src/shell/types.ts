// What the desktop app's Go side tells the shell page. These mirror internal/workspace (the neutral contract every workspace
// is described in) and desktop/internal/workspaces; the shell never sees a Team or a controller type, only these words.

export type Kind = 'personal' | 'team';
export type State = 'ready' | 'connecting' | 'offline' | 'setup' | 'leaving' | 'unavailable';
export type Execution = '' | 'queued' | 'starting' | 'running' | 'needs_input' | 'idle' | 'waiting' | 'blocked' | 'completed' | 'failed' | 'canceled';
export type Status = 'todo' | 'doing' | 'review' | 'done';
export type Severity = 'info' | 'warning' | 'critical';

export interface Entry {
  id: string;
  kind: Kind;
  name: string;
  role?: string;
  state: State;
  detail?: string;
  deviceRoles?: string[];
}

export interface Item extends Entry {
  source: string;
}

export interface Problem {
  source: string;
  kind: 'not_running' | 'refused' | 'outdated' | 'not_set_up' | 'failed';
  detail: string;
}

export interface TeamStatus {
  state: 'ready' | 'not_installed' | 'stopped' | 'outdated' | 'refused';
  detail?: string;
}

export interface WorkspaceView {
  items: Item[];
  selected: string;
  problems: Problem[] | null;
  team: TeamStatus;
  teamInstaller: boolean;
  invitation: boolean;
}

export interface Target {
  id: string;
  kind: Kind;
  url: string;
  origin: string;
  place?: string;
}

export interface Project {
  id: string;
  name: string;
  href: string;
}

export interface WorkItem {
  id: string;
  title: string;
  project: string;
  status: Status;
  execution?: Execution;
  href: string;
  updatedAt?: string;
  report?: ExecutionReport;
}

export interface ExecutionReport {
  recordedAt?: string;
  stale: boolean;
  pendingRequests?: ('send' | 'respond' | 'stop' | 'finish')[];
}

export interface InboxQuestion {
  id: string; runId: string; projectId: string; taskId: string;
  kind: 'clarification' | 'decision' | 'approval' | 'selection' | 'instruction';
  prompt: string; options?: string[]; allowFreeText: boolean; context?: string; askedAt: string;
}

export interface Attention {
  id: string;
  kind: string;
  severity: Severity;
  title: string;
  detail?: string;
  href?: string;
  project?: string;
  at?: string;
  question?: InboxQuestion;
}

export interface Scheduled {
  id: string;
  title: string;
  project: string;
  at: string;
  zone?: string;
  state: string;
  href: string;
}

export interface Check {
  label: string;
  value: string;
  state: 'ok' | 'warn' | 'bad' | 'unknown';
}

export interface Infra {
  writable: boolean;
  hostsConfigured: number;
  hostsOnline: number;
  checks?: Check[];
  warnings?: string[];
}

export interface Summary {
  schema: string;
  workspace: Entry;
  projects: Project[];
  work: WorkItem[];
  attention: Attention[];
  schedule: Scheduled[];
  planned?: { id: string; title: string; project: string; start: string; end: string; href: string }[];
  infra?: Infra;
  at: string;
}

export interface OverviewEntry {
  workspace: Entry;
  summary?: Summary;
  error?: string;
}

export interface Overview {
  entries: OverviewEntry[];
  at: string;
}

/** The app's page-callable surface, as the shell uses it. */
export interface App {
  PersonalWorkspaceCall(method: string, path: string, body: unknown): Promise<{status: number; body: unknown}>;
  PersonalTeamProjects(workspaceId: string, teamId: string): Promise<{projects: {id: string; name: string}[]; at: string}>;
  Workspaces(): Promise<WorkspaceView>;
  OpenWorkspace(id: string): Promise<Target>;
  LoadWorkspace(id: string): Promise<Target>;
  AssistantWorkspaces(): Promise<{ id: string; name: string }[]>;
  AssistantWorkspaceCall(session: string, request: string, proposal: string, id: string, method: string, args: unknown): Promise<unknown>;
  Overview(): Promise<Overview>;
  RememberPlace(id: string, place: string): Promise<void>;
  AddTeam(): Promise<Target>;
  ActivateTeam(): Promise<TeamStatus>;
  TeamService(action: 'start' | 'stop' | 'uninstall'): Promise<void>;
  ForgetWorkspace(id: string): Promise<void>;
  PendingInvitation(): Promise<string>;
  Relay(id: string, method: string, args: unknown[]): Promise<unknown>;
  OpenExternal(url: string): Promise<void>;
  Info(): Promise<{ version: string; platform: string }>;
  AlertStatus(): Promise<string>;
  Alert(title: string, body: string, urgent: boolean): Promise<boolean>;
  TestAlert(): Promise<boolean>;
  OpenAlertSettings(): Promise<void>;
}
