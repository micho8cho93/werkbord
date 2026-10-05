// What needs a person, as one list: the Control Center shows it for every project, a project's
// Overview for that project. No framework code, so it can be tested on its own.

import type { Tone } from './format';
import type { AttentionReview, AttentionRun, AttentionSchedule, HealthFinding, Overview, Question, Run, Runner } from './types';

export type AttentionKind = 'question' | 'blocked' | 'schedule' | 'failed' | 'idle' | 'risk' | 'review';

export interface AttentionItem {
  key: string;
  kind: AttentionKind;
  projectId: string;
  projectName: string;
  taskId?: string;
  title: string;
  /** When it started waiting. */
  at: string;
  run?: Run;
  question?: Question;
  schedule?: AttentionSchedule;
  finding?: HealthFinding;
  review?: AttentionReview;
}

export const KIND_LABEL: Record<AttentionKind, string> = {
  question: 'Needs input',
  blocked: 'Blocked',
  schedule: 'Cannot start',
  failed: 'Failed',
  idle: 'Your turn',
  risk: 'Repository risk',
  review: 'Ready for review',
};

export const KIND_TONE: Record<AttentionKind, Tone> = {
  question: 'ask',
  blocked: 'block',
  schedule: 'block',
  failed: 'bad',
  idle: 'idle',
  risk: 'block',
  review: 'ok',
};

/** The Control Center's segments: each a few kinds that are dealt with the same way. */
export const SEGMENTS: readonly { id: string; label: string; kinds: readonly AttentionKind[] }[] = [
  { id: 'all', label: 'All', kinds: ['question', 'blocked', 'schedule', 'failed', 'idle', 'risk', 'review'] },
  { id: 'input', label: 'Needs input', kinds: ['question', 'idle'] },
  { id: 'blocked', label: 'Blocked', kinds: ['blocked', 'schedule'] },
  { id: 'failed', label: 'Failed', kinds: ['failed'] },
  { id: 'review', label: 'Review', kinds: ['review'] },
  { id: 'risk', label: 'Risk', kinds: ['risk'] },
];

export interface AttentionFilter {
  project?: string;
  runner?: string;
  agent?: string;
}

/** The order things are dealt with in: what blocks an agent first, finished work last. */
const ORDER: AttentionKind[] = ['question', 'blocked', 'schedule', 'failed', 'idle', 'risk', 'review'];

/**
 * Everything that needs a person, most pressing first. Within a kind, what has waited longest
 * comes first, except failures and risks, newest first, as the controller lists them.
 */
export function attentionItems(
  overview: Overview | null,
  questions: readonly Question[],
  filter: AttentionFilter = {},
  runners: readonly Runner[] = [],
): AttentionItem[] {
  if (!overview) return [];
  const { project = '', runner = '', agent = '' } = filter;
  const matchRun = (r: Run) => (!project || r.projectId === project) && (!runner || r.runnerId === runner) && (!agent || r.agentId === agent);
  const matchProject = (id: string) =>
    (!project || id === project) && (!runner || runners.some((r) => r.id === runner && (r.kind === 'local' || r.projects?.includes(id))));
  const runs = overview.runs.filter((x) => matchRun(x.run));
  const projectName = (id: string) => overview.projects.find((p) => p.projectId === id)?.name ?? '';
  const fromRun = (kind: AttentionKind, x: AttentionRun, at = x.run.updatedAt): AttentionItem => ({
    key: `${kind}:${x.run.id}`,
    kind,
    projectId: x.run.projectId,
    projectName: x.projectName,
    taskId: x.run.taskId,
    title: x.taskTitle,
    at,
    run: x.run,
  });

  const items: AttentionItem[] = [];
  for (const q of questions) {
    if (!matchProject(q.projectId)) continue;
    const r = runs.find((x) => x.run.id === q.runId);
    if ((runner || agent) && !r) continue;
    const named = overview.questions.find((x) => x.question.id === q.id);
    items.push({
      key: `question:${q.id}`,
      kind: 'question',
      projectId: q.projectId,
      projectName: named?.projectName ?? projectName(q.projectId),
      taskId: q.taskId,
      title: named?.taskTitle ?? r?.taskTitle ?? 'Task',
      at: q.askedAt,
      run: r?.run,
      question: q,
    });
  }
  for (const x of runs) {
    if (x.run.state === 'blocked') items.push(fromRun('blocked', x, x.run.blocker?.raisedAt ?? x.run.updatedAt));
    else if (x.run.state === 'waiting_for_user' && x.run.waiting === 'idle') items.push(fromRun('idle', x));
  }
  for (const s of overview.orchestration ?? []) {
    if (!matchProject(s.task.projectId)) continue;
    if (s.decision.state !== 'blocked' && s.decision.state !== 'potentially_conflicting') continue;
    items.push({ key: `schedule:${s.task.id}`, kind: 'schedule', projectId: s.task.projectId, projectName: s.projectName, taskId: s.task.id, title: s.task.title, at: s.task.updatedAt, schedule: s });
  }
  for (const x of overview.failed) if (matchRun(x.run)) items.push(fromRun('failed', x, x.run.endedAt ?? x.run.updatedAt));
  for (const f of overview.repository) {
    if (!matchProject(f.projectId)) continue;
    items.push({ key: `risk:${f.id}`, kind: 'risk', projectId: f.projectId, projectName: f.projectName ?? projectName(f.projectId), title: f.title, at: f.detectedAt, finding: f });
  }
  for (const r of overview.review) {
    if (!matchProject(r.task.projectId)) continue;
    if ((runner || agent) && !(r.lastRun && matchRun(r.lastRun))) continue;
    items.push({ key: `review:${r.task.id}`, kind: 'review', projectId: r.task.projectId, projectName: r.projectName, taskId: r.task.id, title: r.task.title, at: r.task.updatedAt, run: r.lastRun, review: r });
  }

  const newestFirst = new Set<AttentionKind>(['failed', 'risk']);
  return items.sort((a, b) => {
    const k = ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind);
    if (k) return k;
    return newestFirst.has(a.kind) ? b.at.localeCompare(a.at) : a.at.localeCompare(b.at);
  });
}

/** How many items a segment holds. */
export function segmentCount(items: readonly AttentionItem[], kinds: readonly AttentionKind[]): number {
  return items.filter((i) => kinds.includes(i.kind)).length;
}
