import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ControllerEvent, Overview, Project, Question, Run, Task } from './types';

// The API is replaced by a fake controller that answers each project-scoped call for that project
// alone, as the real one does. What these tests check is what the interface does with the answers.
const db = vi.hoisted(() => ({
  projects: [] as unknown[],
  tasks: {} as Record<string, unknown[]>,
  latest: {} as Record<string, unknown[]>,
  history: {} as Record<string, unknown[]>,
  questions: {} as Record<string, unknown[]>,
  overview: { projects: [], questions: [], runs: [], failed: [], review: [], repository: [] } as unknown,
  calls: [] as string[],
}));

vi.mock('./api', () => {
  const call = <T>(what: string, projectId: string, rows: Record<string, unknown[]>) => {
    db.calls.push(`${what}:${projectId}`);
    return Promise.resolve((rows[projectId] ?? []) as T);
  };
  return {
    ApiError: class extends Error {},
    eventsURL: () => '/api/events',
    api: {
      listProjects: () => Promise.resolve(db.projects),
      listAgents: () => Promise.resolve([]),
      controlCenter: () => Promise.resolve(db.overview),
      getSettings: () => Promise.resolve({}),
      onboarding: () => Promise.resolve({ completedAt: '2026-10-04T10:00:00Z' }),
      listTasks: (p: string) => call('tasks', p, db.tasks),
      listProjectRuns: (p: string) => call('runs', p, db.latest),
      listActivity: (p: string) => call('activity', p, db.history),
      listPendingQuestions: (p: string) => call('questions', p, db.questions),
    },
  };
});

import { ProjectScope, newer } from './scope.svelte';
import { app } from './state.svelte';

const T0 = '2026-10-04T10:00:00Z';
const policy = { interaction: 'interactive' as const };
const task = (id: string, projectId: string, over: Partial<Task> = {}): Task => ({
  id, projectId, title: `task ${id}`, description: '', state: 'backlog', position: 1, execution: {}, version: 1, createdAt: T0, updatedAt: T0, ...over,
});
const run = (id: string, taskId: string, projectId: string, over: Partial<Run> = {}): Run => ({
  id, taskId, projectId, agentId: 'fake', state: 'running', policy, version: 1, createdAt: T0, updatedAt: T0, ...over,
});
const question = (id: string, runId: string, taskId: string, projectId: string, over: Partial<Question> = {}): Question => ({
  id, runId, taskId, projectId, kind: 'clarification', prompt: `q ${id}`, allowFreeText: true, state: 'pending', askedAt: T0, ...over,
});
const project = (id: string, name: string): Project => ({ id, name, repoPath: `/code/${name}`, execution: {}, createdAt: T0, updatedAt: T0 });
let seq = 0;
const event = (type: string, projectId: string, payload: unknown, extra: Partial<ControllerEvent> = {}): ControllerEvent => ({
  seq: ++seq, type, projectId, payload, createdAt: T0, ...extra,
});

const settle = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  db.projects = [project('prj_a', 'Alpha'), project('prj_b', 'Beta')];
  db.tasks = { prj_a: [task('tsk_a1', 'prj_a'), task('tsk_a2', 'prj_a')], prj_b: [task('tsk_b1', 'prj_b')] };
  db.latest = { prj_a: [run('run_a1', 'tsk_a1', 'prj_a')], prj_b: [run('run_b1', 'tsk_b1', 'prj_b', { state: 'blocked' })] };
  db.history = { prj_a: [run('run_a1', 'tsk_a1', 'prj_a')], prj_b: [run('run_b1', 'tsk_b1', 'prj_b', { state: 'blocked' })] };
  db.questions = { prj_a: [question('qst_a', 'run_a1', 'tsk_a1', 'prj_a')], prj_b: [] };
  db.overview = { projects: [], questions: [], runs: [], failed: [], review: [], repository: [] };
  db.calls = [];
  // `app` is one object for the whole page; each test starts it afresh.
  app.scopes.clear();
  app.overview = null;
  app.lastProjectId = '';
});

describe('a project scope', () => {
  it('is filled from the project-scoped endpoints and holds only that project', async () => {
    const a = new ProjectScope('prj_a');
    await a.load();
    expect(a.loaded).toBe(true);
    expect(a.tasks.map((t) => t.id)).toEqual(['tsk_a1', 'tsk_a2']);
    expect(Object.keys(a.latestRun)).toEqual(['tsk_a1']);
    expect(a.history.map((r) => r.id)).toEqual(['run_a1']);
    expect(a.questions.map((q) => q.id)).toEqual(['qst_a']);
    expect(db.calls.sort()).toEqual(['activity:prj_a', 'questions:prj_a', 'runs:prj_a', 'tasks:prj_a']);
  });

  it('discards anything of another project even if one were to arrive', async () => {
    // A confused or hostile answer: project B's rows in project A's response.
    db.tasks.prj_a = [task('tsk_a1', 'prj_a'), task('tsk_b1', 'prj_b')];
    db.latest.prj_a = [run('run_a1', 'tsk_a1', 'prj_a'), run('run_b1', 'tsk_b1', 'prj_b')];
    db.history.prj_a = [run('run_b1', 'tsk_b1', 'prj_b')];
    db.questions.prj_a = [question('qst_b', 'run_b1', 'tsk_b1', 'prj_b')];
    const a = new ProjectScope('prj_a');
    await a.load();
    expect(a.tasks.map((t) => t.id)).toEqual(['tsk_a1']);
    expect(Object.keys(a.latestRun)).toEqual(['tsk_a1']);
    expect(a.history).toEqual([]);
    expect(a.questions).toEqual([]);
  });

  it('ignores events of other projects', async () => {
    const a = new ProjectScope('prj_a');
    await a.load();
    const before = JSON.stringify([a.tasks, a.latestRun, a.history, a.questions]);

    expect(a.apply(event('task.created', 'prj_b', task('tsk_b9', 'prj_b')))).toBe(false);
    expect(a.apply(event('task.updated', 'prj_b', task('tsk_a1', 'prj_b', { title: 'hijacked', version: 9 })))).toBe(false);
    expect(a.apply(event('run.state_changed', 'prj_b', { run: run('run_b9', 'tsk_b9', 'prj_b') }))).toBe(false);
    expect(a.apply(event('agent.question', 'prj_b', { question: question('qst_b9', 'run_b9', 'tsk_b9', 'prj_b') }))).toBe(false);
    expect(a.apply(event('agent.output', 'prj_b', { stream: 'assistant', text: 'hello' }, { taskId: 'tsk_a1', runId: 'run_a1' }))).toBe(false);
    expect(JSON.stringify([a.tasks, a.latestRun, a.history, a.questions])).toBe(before);
  });

  it('refuses a task or run of another project handed to it directly', async () => {
    const a = new ProjectScope('prj_a');
    await a.load();
    a.upsertTask(task('tsk_b1', 'prj_b'));
    a.upsertRun(run('run_b1', 'tsk_b1', 'prj_b'));
    a.resolveQuestion({ id: 'qst_a', projectId: 'prj_b' }); // B's say-so cannot close A's question
    expect(a.tasks.map((t) => t.id)).toEqual(['tsk_a1', 'tsk_a2']);
    expect(a.latestRun.tsk_b1).toBeUndefined();
    expect(a.history.map((r) => r.id)).toEqual(['run_a1']);
    expect(a.questions.map((q) => q.id)).toEqual(['qst_a']);
  });

  it('keeps itself current from its own project\'s events', async () => {
    const a = new ProjectScope('prj_a');
    await a.load();

    expect(a.apply(event('task.created', 'prj_a', task('tsk_a3', 'prj_a')))).toBe(true);
    expect(a.tasks.map((t) => t.id)).toContain('tsk_a3');
    a.apply(event('task.updated', 'prj_a', task('tsk_a3', 'prj_a', { state: 'doing', version: 2 })));
    a.apply(event('task.updated', 'prj_a', task('tsk_a3', 'prj_a', { state: 'backlog', version: 1 }))); // stale: ignored
    expect(a.tasks.find((t) => t.id === 'tsk_a3')?.state).toBe('doing');

    // A new run is the board's latest for its task, and the newest entry of the history.
    const second = run('run_a2', 'tsk_a1', 'prj_a', { createdAt: '2026-10-04T11:00:00Z', state: 'blocked' });
    a.apply(event('run.state_changed', 'prj_a', { run: second }));
    expect(a.latestRun.tsk_a1.id).toBe('run_a2');
    expect(a.history.map((r) => r.id)).toEqual(['run_a2', 'run_a1']);
    a.apply(event('run.state_changed', 'prj_a', { run: { ...second, state: 'running', version: 2 } }));
    expect(a.latestRun.tsk_a1.state).toBe('running');
    expect(a.history).toHaveLength(2);

    a.apply(event('agent.output', 'prj_a', { stream: 'tool', text: 'Edit  main.go' }, { taskId: 'tsk_a1', runId: 'run_a2' }));
    expect(a.latestRun.tsk_a1.activity).toBe('Edit main.go');

    a.apply(event('agent.question', 'prj_a', { question: question('qst_a2', 'run_a2', 'tsk_a1', 'prj_a') }));
    expect(a.pendingFor('run_a2').map((q) => q.id)).toEqual(['qst_a2']);
    a.apply(event('question.answered', 'prj_a', { question: question('qst_a2', 'run_a2', 'tsk_a1', 'prj_a', { state: 'answered', answer: 'x' }) }));
    expect(a.pendingFor('run_a2')).toEqual([]);
    expect(a.pendingFor('run_a1').map((q) => q.id)).toEqual(['qst_a']);
  });

  it('does not show a question the controller answered for the user as waiting for them', async () => {
    const a = new ProjectScope('prj_a');
    await a.load();
    const auto = question('qst_auto', 'run_a1', 'tsk_a1', 'prj_a', { state: 'answered', answer: 'decide it yourself', answeredBy: 'policy' });
    a.apply(event('agent.question', 'prj_a', { question: auto }));
    a.apply(event('question.answered', 'prj_a', { question: auto }));
    expect(a.questions.map((q) => q.id)).toEqual(['qst_a']);
  });

  it('counts tasks by what their latest run needs', async () => {
    const b = new ProjectScope('prj_b');
    await b.load();
    expect(b.taskCount((r) => r.state === 'blocked')).toBe(1);
    expect(b.taskCount((r) => r.state === 'running')).toBe(0);
  });

  it('keeps the newer of two runs of one task', () => {
    const old = run('r1', 't', 'p', { createdAt: '2026-10-04T10:00:00Z' });
    const young = run('r2', 't', 'p', { createdAt: '2026-10-04T11:00:00Z' });
    expect(newer(undefined, old)).toBe(old);
    expect(newer(old, young)).toBe(young);
    expect(newer(young, old)).toBe(young);
    expect(newer(old, { ...old, version: 2 }).version).toBe(2);
    expect(newer({ ...old, version: 3 }, old).version).toBe(3);
  });
});

describe('switching projects', () => {
  it('shows a project\'s own data at once, with nothing of the one before', async () => {
    const a = app.enter('prj_a')!;
    const b = app.enter('prj_b')!;
    await settle();
    // Two scopes, each holding only its own project: switching is looking at the other.
    expect(a).not.toBe(b);
    expect(a.tasks.map((t) => t.id)).toEqual(['tsk_a1', 'tsk_a2']);
    expect(b.tasks.map((t) => t.id)).toEqual(['tsk_b1']);
    expect(app.scope('prj_a')).toBe(a);
    expect(app.scope('prj_b')).toBe(b);
    expect(a.tasks.every((t) => t.projectId === 'prj_a')).toBe(true);
    expect(b.tasks.every((t) => t.projectId === 'prj_b')).toBe(true);
    expect(b.latestRun.tsk_a1).toBeUndefined();
  });

  it('needs no fetching to go back to a project already visited', async () => {
    app.enter('prj_a');
    app.enter('prj_b');
    await settle();
    db.calls = [];
    expect(app.enter('prj_a')?.tasks).toHaveLength(2);
    expect(app.enter('prj_b')?.tasks).toHaveLength(1);
    await settle();
    expect(db.calls).toEqual([]);
  });

  it('can have a project ready without making it the current one', async () => {
    app.enter('prj_a');
    app.warm('prj_b');
    app.warm('prj_b'); // asking twice fetches once
    await settle();
    expect(app.lastProjectId).toBe('prj_a');
    expect(db.calls.filter((c) => c === 'tasks:prj_b')).toHaveLength(1);
    db.calls = [];
    expect(app.enter('prj_b')?.tasks.map((t) => t.id)).toEqual(['tsk_b1']); // already there
    await settle();
    expect(db.calls).toEqual([]);
    app.warm('');
  });

  it('remembers the project last used, and enters nothing for no project', () => {
    app.enter('prj_b');
    expect(app.lastProjectId).toBe('prj_b');
    expect(app.enter('')).toBeUndefined();
    expect(app.lastProjectId).toBe('prj_b');
  });

  it('routes each event to its project only', async () => {
    const a = app.enter('prj_a')!;
    const b = app.enter('prj_b')!;
    await settle();
    const apply = (ev: ControllerEvent) => (app as unknown as { apply(e: ControllerEvent): void }).apply(ev);
    apply(event('task.created', 'prj_a', task('tsk_a7', 'prj_a')));
    apply(event('task.created', 'prj_b', task('tsk_b7', 'prj_b')));
    expect(a.tasks.map((t) => t.id)).toContain('tsk_a7');
    expect(a.tasks.map((t) => t.id)).not.toContain('tsk_b7');
    expect(b.tasks.map((t) => t.id)).toContain('tsk_b7');
    expect(b.tasks.map((t) => t.id)).not.toContain('tsk_a7');
  });

  it('closes a question in its own project\'s scope and in the global list', async () => {
    const a = app.enter('prj_a')!;
    const b = app.enter('prj_b')!;
    await settle();
    expect(a.questions).toHaveLength(1);
    app.resolveQuestion({ id: 'qst_a', projectId: 'prj_a' });
    expect(a.questions).toHaveLength(0);
    expect(b.questions).toHaveLength(0);
  });
});

describe('the Control Center stays across projects', () => {
  const overview = (): Overview => ({
    projects: [
      { projectId: 'prj_a', name: 'Alpha', needsInput: 1, blocked: 0, idle: 0, running: 0, failed: 0, review: 0, repoAttention: 0, repoRisk: 0 },
      { projectId: 'prj_b', name: 'Beta', needsInput: 0, blocked: 1, idle: 1, running: 0, failed: 0, review: 0, repoAttention: 0, repoRisk: 0 },
    ],
    questions: [{ question: question('qst_ov', 'run_a1', 'tsk_a1', 'prj_a'), projectName: 'Alpha', taskTitle: 'Alpha task', agentId: 'fake' }],
    runs: [
      { run: run('run_a1', 'tsk_a1', 'prj_a', { state: 'waiting_for_user', waiting: 'question' }), projectName: 'Alpha', taskTitle: 'Alpha task' },
      { run: run('run_b1', 'tsk_b1', 'prj_b', { state: 'blocked' }), projectName: 'Beta', taskTitle: 'Beta task' },
      { run: run('run_b2', 'tsk_b2', 'prj_b', { state: 'waiting_for_user', waiting: 'idle' }), projectName: 'Beta', taskTitle: 'Beta second' },
    ],
    failed: [],
    review: [],
    repository: [],
  });

  it('counts what needs the user in every project, whichever one is open', async () => {
    db.overview = overview();
    await app.refresh();
    app.enter('prj_a'); // being in Alpha does not hide Beta's blocked run
    expect(app.questions.map((q) => q.id)).toEqual(['qst_ov']);
    expect(app.blockedCount).toBe(1);
    expect(app.idleCount).toBe(1);
    expect(app.needsInput).toBe(1);
    expect(app.needsYou).toBe(3);
  });

  it('counts failures and repositories at risk too, but not finished work or ordinary findings', async () => {
    const o = overview();
    o.failed = [{ run: run('run_f', 'tsk_f', 'prj_a', { state: 'failed' }), projectName: 'Alpha', taskTitle: 'Failed task' }];
    o.repository = [{ id: 'hf_1', projectId: 'prj_b', type: 'unresolved_conflicts', severity: 'critical' } as never];
    o.review = [{ task: { id: 'tsk_r', projectId: 'prj_a', title: 'Ready' } as never, projectName: 'Alpha' }];
    db.overview = o;
    await app.refresh();
    expect(app.failedCount).toBe(1);
    expect(app.riskCount).toBe(1);
    expect(app.needsYou).toBe(5); // 1 question + 1 blocked + 1 idle + 1 failed + 1 repository at risk; the task in Review is not counted
  });

  it('names the project and task of anything, even in a project that was never opened', async () => {
    db.overview = overview();
    await app.refresh();
    expect(app.scope('prj_b')).toBeUndefined(); // never entered
    expect(app.taskInfo('tsk_b1', 'prj_b')).toEqual({ title: 'Beta task', projectId: 'prj_b', projectName: 'Beta' });
    expect(app.taskInfo('tsk_a1', 'prj_a')).toEqual({ title: 'Alpha task', projectId: 'prj_a', projectName: 'Alpha' });
    expect(app.taskInfo('tsk_nope', 'prj_a')).toBeUndefined();
  });
});
