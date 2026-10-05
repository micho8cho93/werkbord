import { describe, expect, it } from 'vitest';
import { attentionItems, segmentCount, SEGMENTS } from './attention';
import type { Overview, Question, Run, Task } from './types';

const run = (id: string, o: Partial<Run> = {}): Run => ({
  id,
  taskId: `t-${id}`,
  projectId: 'p1',
  agentId: 'claude',
  state: 'running',
  policy: { interaction: 'interactive' },
  version: 1,
  createdAt: '2026-10-05T09:00:00Z',
  updatedAt: '2026-10-05T09:00:00Z',
  ...o,
});
const task = (id: string, projectId = 'p1'): Task => ({
  id,
  projectId,
  title: `Task ${id}`,
  description: '',
  state: 'review',
  position: 1,
  execution: {},
  version: 1,
  createdAt: '2026-10-05T08:00:00Z',
  updatedAt: '2026-10-05T08:00:00Z',
});

const question: Question = {
  id: 'q1',
  projectId: 'p1',
  taskId: 't-r1',
  runId: 'r1',
  kind: 'approval',
  prompt: 'Run the tests?',
  state: 'pending',
  askedAt: '2026-10-05T09:30:00Z',
} as Question;

const overview: Overview = {
  projects: [
    { projectId: 'p1', name: 'my-app', needsInput: 1, blocked: 1, idle: 1, running: 0, failed: 1, review: 1, repoAttention: 0, repoRisk: 0 },
    { projectId: 'p2', name: 'api', needsInput: 0, blocked: 0, idle: 0, running: 0, failed: 0, review: 1, repoAttention: 0, repoRisk: 0 },
  ],
  questions: [{ question, projectName: 'my-app', taskTitle: 'Migrate to WAL', agentId: 'claude' }],
  runs: [
    { run: run('r1', { state: 'waiting_for_user', waiting: 'question' }), projectName: 'my-app', taskTitle: 'Migrate to WAL' },
    { run: run('r2', { state: 'blocked' }), projectName: 'my-app', taskTitle: 'Timezones' },
    { run: run('r3', { state: 'waiting_for_user', waiting: 'idle', runnerId: 'mbp' }), projectName: 'my-app', taskTitle: 'Auth test' },
  ],
  failed: [{ run: run('r4', { state: 'failed', endedAt: '2026-10-05T10:00:00Z' }), projectName: 'my-app', taskTitle: 'Bump Go' }],
  review: [
    { task: task('t5'), projectName: 'my-app' },
    { task: task('t6', 'p2'), projectName: 'api' },
  ],
  repository: [],
};

describe('attentionItems', () => {
  it('lists what needs a person, most pressing first', () => {
    const items = attentionItems(overview, [question]);
    expect(items.map((i) => i.kind)).toEqual(['question', 'blocked', 'failed', 'idle', 'review', 'review']);
    expect(items[0]).toMatchObject({ title: 'Migrate to WAL', projectName: 'my-app', question });
  });

  it('narrows to one project, runner or agent', () => {
    expect(attentionItems(overview, [question], { project: 'p2' }).map((i) => i.key)).toEqual(['review:t6']);
    expect(attentionItems(overview, [question], { runner: 'mbp' }, [{ id: 'mbp', kind: 'remote', projects: ['p1'] } as never]).map((i) => i.key)).toEqual([
      'idle:r3',
    ]);
  });

  it('counts by segment', () => {
    const items = attentionItems(overview, [question]);
    const counts = Object.fromEntries(SEGMENTS.map((s) => [s.id, segmentCount(items, s.kinds)]));
    expect(counts).toEqual({ all: 6, input: 2, blocked: 1, failed: 1, review: 2, risk: 0 });
  });

  it('is empty before the overview arrives', () => {
    expect(attentionItems(null, [question])).toEqual([]);
  });
});
