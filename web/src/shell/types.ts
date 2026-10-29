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

export interface Change { id: string; key: string; kind: 'completed' | 'pr_merged'; title: string; href: string; origin?: string; at: string }
export interface RecentChanges { since: string; through: string; cursor: number; items: Change[]; incomplete: boolean }

export interface Summary {
  changes?: RecentChanges;
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

/** What the app says about this person's plan. The key itself never reaches a page: only its ID and the label its issuer gave it. */
export interface PremiumStatus {
  active: boolean;
  id?: string;
  holder?: string;
  /** This build carries the public key that checks unlock keys; a development build does not. */
  canCheck: boolean;
  /** Where to get Premium; empty until it is on sale. */
  buyUrl: string;
}

/** What an invitation link says about itself, read on this computer. */
export interface Invitation {
  name: string;
  expiresAt: string;
}

/** How far a team being created or joined has got. */
export interface SetupView {
  working: string;
  error?: string;
  done: boolean;
  waiting: boolean;
  /** The team is on this computer already; a failure is then about reaching it. */
  exists?: boolean;
  name?: string;
}

/** The app's page-callable surface, as the shell uses it. */
export interface App {
  PreviewProjectMove(source: string, destination: string, project: string): Promise<ProjectMovePreview>;
  ConfirmProjectMove(token: string): Promise<ProjectMoveResult>;
  Workspaces(): Promise<WorkspaceView>;
  OpenWorkspace(id: string): Promise<Target>;
  LoadWorkspace(id: string): Promise<Target>;
  AssistantWorkspaces(): Promise<{ id: string; name: string }[]>;
  AssistantWorkspaceCall(session: string, request: string, proposal: string, id: string, method: string, args: unknown): Promise<unknown>;
  Overview(): Promise<Overview>;
  PreviewProductMapShare(local: string, workspace: string, project: string): Promise<{ token: string; workspace: string; project: string; snapshot: import('../../../internal/uipresentation/static/product-map.js').MapSnapshot }>;
  ConfirmProductMapShare(token: string): Promise<string>;
  RememberPlace(id: string, place: string): Promise<void>;
  TeamDefaults(): Promise<{ name: string; device: string }>;
  CheckInvitation(link: string): Promise<Invitation>;
  CreateTeam(name: string, owner: string): Promise<{ id: string }>;
  JoinTeam(link: string, name: string, device: string): Promise<{ id: string }>;
  TeamSetupState(id: string): Promise<SetupView>;
  CancelTeamSetup(id: string): Promise<void>;
  ActivateTeam(): Promise<TeamStatus>;
  TeamService(action: 'start' | 'stop' | 'uninstall'): Promise<void>;
  ForgetWorkspace(id: string): Promise<void>;
  PendingInvitation(): Promise<string>;
  Relay(id: string, method: string, args: unknown[]): Promise<unknown>;
  OpenExternal(url: string): Promise<void>;
  /** One workspace's analytics report, as it reports it, for the window's one Analytics page. */
  Analytics(id: string, project: string, days: number): Promise<unknown>;
  Premium(): Promise<PremiumStatus>;
  UnlockPremium(key: string): Promise<PremiumStatus>;
  Info(): Promise<{ version: string; platform: string }>;
  AlertStatus(): Promise<string>;
  Alert(title: string, body: string, urgent: boolean): Promise<boolean>;
  TestAlert(): Promise<boolean>;
  OpenAlertSettings(): Promise<void>;
}

export interface ProjectMovePreview {
  token: string; source: string; destination: string;
  snapshot: { source: string; name: string; repository: string; tickets: {title: string; description: string}[] };
}
export interface ProjectMoveResult { id: string; archived: boolean; warning?: string; cancelled?: boolean }
