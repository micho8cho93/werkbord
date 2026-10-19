// What the suggestions API speaks (internal/domain/recommend.go, internal/api/recommend.go). The rules that produce this run on
// the controller; the screen only shows it, and can open the screens it points at. Nothing here can start, approve or change
// anything.

export type RecTier = 1 | 2 | 3 | 4 | 5;
export type RecIntent = 'continue_work' | 'prepare_integration' | 'review_issues' | 'find_bugs' | 'refactor_codebase' | 'next_tasks';
export type RecVariant = 'default' | 'direct_merge' | 'pull_request';
export type RecBasis = 'required' | 'evidence' | 'preference';
export type RecVerdict = 'useful' | 'not_useful' | 'dismissed';
export type RecActionKind = 'open_workflow' | 'open_task' | 'open_branch' | 'open_changes' | 'open_git' | 'open_health' | 'open_board';

export interface RecEvidence {
  tier: RecTier;
  source: string;
  kind: string;
  ref?: string;
  label: string;
  verified: boolean;
  note?: string;
}

export interface RecConstraint {
  tier: RecTier;
  source: string;
  code: string;
  message: string;
  ref?: string;
}

export interface RecAction {
  kind: RecActionKind;
  label: string;
  workflow?: string;
  taskId?: string;
  runId?: string;
  branch?: string;
  pullRequest?: number;
  /** Always false: a suggestion only opens something. */
  startsExecution: boolean;
  confirmation: 'at_destination' | 'none';
}

export interface Recommendation {
  id: string;
  projectId: string;
  intent: RecIntent;
  variant: RecVariant;
  title: string;
  detail: string;
  basis: RecBasis;
  confidence: 'high' | 'medium' | 'low';
  action: RecAction;
  evidence: RecEvidence[];
  applied: RecConstraint[];
}

export interface RecWithheld {
  intent: RecIntent;
  variant?: RecVariant;
  tier: RecTier;
  code: string;
  reason: string;
}

export interface RecSources {
  individualMemory: string;
  teamMemory: string;
  git: string;
  pullRequests: string;
  tasks: string;
  health: string;
}

export interface RecommendationSet {
  projectId: string;
  projectName?: string;
  generatedAt: string;
  items: Recommendation[];
  standing: RecConstraint[];
  withheld: RecWithheld[];
  sources: RecSources;
  notes: string[];
  boundary: string;
  /** Where a verdict is kept: this person's own memory on the computer, or only on this device. Absent on a set from the assistant. */
  feedbackStoredIn?: 'memory' | 'device';
}
