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
  days: ({ date: string; created: number; completed: number; attempts?: number } & Partial<Measurements>)[];
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

// ---- the pictures: pure helpers for the charts, so the figures drawn are the figures counted ----

/** Running totals of created and completed over the period, one entry per day. */
export function cumulative(days: { date: string; created: number; completed: number }[]): { date: string; created: number; completed: number }[] {
  let created = 0, completed = 0;
  return days.map((d) => ({ date: d.date, created: (created += d.created), completed: (completed += d.completed) }));
}

/** A whole-number percentage, or null when there is nothing to divide by (unknown, never 0%). */
export const percent = (part: number, whole: number): number | null => (whole > 0 ? Math.round((part / whole) * 100) : null);

/** The top of an axis and its ticks: a round number at or above the largest value, never zero. */
export function niceAxis(max: number, wanted = 4): { top: number; ticks: number[] } {
  const raw = Math.max(1, max);
  const unit = Math.pow(10, Math.floor(Math.log10(raw / wanted)));
  const step = Math.max(1, [1, 2, 5, 10].map((m) => m * unit).find((s) => Math.ceil(raw / s) <= wanted) ?? 10 * unit);
  const steps = Math.max(1, Math.ceil(raw / step));
  return { top: steps * step, ticks: Array.from({ length: steps + 1 }, (_, i) => i * step) };
}

/** Rows summed under one key (a provider, a runner, a project), largest first. Unknown values stay unknown, never zero. */
export function sumBy(rows: AnalyticsExecution[], key: 'provider' | 'runnerId' | 'projectId' | 'model', value: (row: AnalyticsExecution) => number | null): { key: string; value: number | null }[] {
  const sums = new Map<string, number | null>();
  for (const row of rows) {
    const v = value(row);
    const prior = sums.get(row[key]);
    sums.set(row[key], v == null ? (prior ?? null) : (prior ?? 0) + v);
  }
  return [...sums].map(([k, v]) => ({ key: k, value: v })).sort((a, b) => (b.value ?? -1) - (a.value ?? -1));
}

type Point = [number, number];
/** A smooth line through the points that never overshoots them (monotone cubic), so a running total never dips between days. */
export function smoothPath(points: Point[]): string {
  const n = points.length;
  if (n === 0) return '';
  const f = (v: number) => +v.toFixed(2);
  if (n === 1) return `M${f(points[0][0])},${f(points[0][1])}`;
  const dx: number[] = [], slope: number[] = [];
  for (let i = 0; i < n - 1; i++) {
    dx.push(points[i + 1][0] - points[i][0]);
    slope.push(dx[i] === 0 ? 0 : (points[i + 1][1] - points[i][1]) / dx[i]);
  }
  const tangent: number[] = [slope[0]];
  for (let i = 1; i < n - 1; i++) tangent.push(slope[i - 1] * slope[i] <= 0 ? 0 : (slope[i - 1] + slope[i]) / 2);
  tangent.push(slope[n - 2]);
  for (let i = 0; i < n - 1; i++) {
    if (slope[i] === 0) { tangent[i] = 0; tangent[i + 1] = 0; continue; }
    const a = tangent[i] / slope[i], b = tangent[i + 1] / slope[i], h = Math.hypot(a, b);
    if (h > 3) { tangent[i] = (3 * a * slope[i]) / h; tangent[i + 1] = (3 * b * slope[i]) / h; }
  }
  let d = `M${f(points[0][0])},${f(points[0][1])}`;
  for (let i = 0; i < n - 1; i++) {
    const w = dx[i] / 3;
    d += ` C${f(points[i][0] + w)},${f(points[i][1] + tangent[i] * w)} ${f(points[i + 1][0] - w)},${f(points[i + 1][1] - tangent[i + 1] * w)} ${f(points[i + 1][0])},${f(points[i + 1][1])}`;
  }
  return d;
}
