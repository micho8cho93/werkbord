import type { RunState } from './types';

export interface Measurements {
  inputTokens: number | null; outputTokens: number | null; cachedTokens: number | null;
  inputRuns: number; outputRuns: number; cachedRuns: number; partialRuns: number;
  actualCostUsd: number | null; estimatedCostUsd: number | null;
  actualCostRuns: number; estimatedCostRuns: number;
}
export interface AnalyticsExecution extends Measurements {
  projectId: string; runnerId: string; provider: string; model: string;
  attempts: number; retries: number; outcomes: Partial<Record<RunState, number>>;
  limitEvents: number; accepted: number; rejected: number;
}
export interface AnalyticsReport extends Measurements {
  start: string; end: string; projectId: string; provider: string; providers: string[];
  created: number; completed: number; durationSamples: number;
  averageTaskSeconds: number | null; medianTaskSeconds: number | null;
  blocked: number; stopped: number; agentReportedCompletion: number | null; normalSessionCompletions: number; verifiedCompletion: number | null;
  verifiedMerges: number; accepted: number; rejected: number; attempts: number; retries: number;
  limitEvents: number; outcomes: Partial<Record<RunState, number>>;
  days: { date: string; created: number; completed: number }[];
  executions: AnalyticsExecution[];
  categories: { labelId: string; name: string; created: number; completed: number }[];
  truncated: boolean; retentionRemoved: boolean; missingTaskHistory: number;
}

const number = new Intl.NumberFormat();
export const measuredNumber = (value: number | null | undefined): string => value == null ? 'Not reported' : number.format(value);
export function analyticsDuration(seconds: number | null): string {
  if (seconds === null) return 'No samples';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${(seconds / 60).toFixed(1)}m`;
  if (seconds < 86400) return `${(seconds / 3600).toFixed(1)}h`;
  return `${(seconds / 86400).toFixed(1)}d`;
}
export const measuredCost = (value: number | null): string => value === null ? 'Not reported' : new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }).format(value);

// Preserve unknown usage when filtering execution evidence; zero is a measured value.
export function executionTotals(rows: AnalyticsExecution[]) {
  const total = (key: 'inputTokens' | 'outputTokens' | 'cachedTokens' | 'actualCostUsd' | 'estimatedCostUsd') => {
    const values = rows.map(row => row[key]).filter((value): value is number => value != null);
    return values.length ? values.reduce((sum, value) => sum + value, 0) : null;
  };
  const count = (key: 'attempts' | 'retries' | 'accepted' | 'rejected' | 'limitEvents' | 'inputRuns' | 'outputRuns' | 'cachedRuns' | 'actualCostRuns' | 'estimatedCostRuns' | 'partialRuns') => rows.reduce((sum, row) => sum + row[key], 0);
  const outcomes: Partial<Record<RunState, number>> = {};
  for (const row of rows) for (const [state, value] of Object.entries(row.outcomes)) outcomes[state as RunState] = (outcomes[state as RunState] ?? 0) + (value ?? 0);
  return { attempts: count('attempts'), retries: count('retries'), accepted: count('accepted'), rejected: count('rejected'), limitEvents: count('limitEvents'), inputRuns: count('inputRuns'), outputRuns: count('outputRuns'), cachedRuns: count('cachedRuns'), actualCostRuns: count('actualCostRuns'), estimatedCostRuns: count('estimatedCostRuns'), partialRuns: count('partialRuns'), inputTokens: total('inputTokens'), outputTokens: total('outputTokens'), cachedTokens: total('cachedTokens'), actualCostUsd: total('actualCostUsd'), estimatedCostUsd: total('estimatedCostUsd'), outcomes };
}
