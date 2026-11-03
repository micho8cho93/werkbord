import { describe, expect, it } from 'vitest';
import { activeAgents, agentOf, nextAction, planBranches, planOfTask, planSteps, progressOf, reworkBefore, stepOf, stepPosition } from './work-steps';
import { chainOf, pushed, withChain } from './origin';
import { originHref } from './origin-nav';
import type { Run, Task } from './types';
import type { WorkPlan } from './work-queue';

const task = (id: string, o: Partial<Task> = {}): Task => ({ id, projectId: 'p', title: `Task ${id}`, description: '', state: 'backlog', position: 1, execution: {}, version: 1, createdAt: '', updatedAt: '', ...o });
const after = (...deps: string[]) => ({ orchestration: { enabled: false, dependencies: deps, expectedPaths: [] } });
const run = (taskId: string, o: Partial<Run> = {}): Run => ({ id: `r-${taskId}`, taskId, projectId: 'p', agentId: 'a', state: 'running', policy: {}, version: 1, createdAt: '', updatedAt: '', ...o }) as Run;
const latestOf = (...rs: Run[]) => Object.fromEntries(rs.map(r => [r.taskId, r]));

describe('a task says whether it is complete, running, ready, blocked or waiting', () => {
  it('names each state and gives a reason where work cannot go on', () => {
    const tasks = [task('a', { state: 'done' }), task('b', { state: 'doing' }), task('c'), task('d', after('b')), task('e', { state: 'review' }), task('f')];
    const latest = latestOf(run('b'), run('f', { state: 'failed', reason: 'tests failed' }));
    expect(stepOf(tasks[0], tasks, latest)).toMatchObject({ kind: 'done', label: 'Complete' });
    expect(stepOf(tasks[1], tasks, latest)).toMatchObject({ kind: 'running', label: 'Running', next: '' });
    expect(stepOf(tasks[2], tasks, latest)).toMatchObject({ kind: 'ready', label: 'Ready' });
    expect(stepOf(tasks[3], tasks, latest)).toMatchObject({ kind: 'waiting', blockedBy: [{ id: 'b', title: 'Task b' }] });
    expect(stepOf(tasks[3], tasks, latest).reason).toContain('Task b');
    expect(stepOf(tasks[4], tasks, latest)).toMatchObject({ kind: 'review', next: 'Review' });
    expect(stepOf(tasks[5], tasks, latest)).toMatchObject({ kind: 'blocked', label: 'Failed', reason: expect.stringContaining('tests failed') });
  });

  it('holds every task after a failed one, however far along the chain, and lets go once it is reworked', () => {
    const tasks = [task('a'), task('b', after('a')), task('c', after('b'))];
    const failed = latestOf(run('a', { state: 'failed' }));
    expect(stepOf(tasks[2], tasks, failed)).toMatchObject({ label: 'Held', blockedBy: [{ id: 'b', title: 'Task b' }] });
    expect(stepOf(tasks[2], tasks, failed).reason).toContain('Task a');
    expect(reworkBefore(tasks[2], tasks, failed).map(t => t.id)).toEqual(['a']);
    expect(stepOf(tasks[2], tasks, latestOf(run('a', { state: 'running' }))).label).toBe('Waiting');
  });

  it('treats a dependency that failed, was stopped or is gone as a block, not a wait', () => {
    const tasks = [task('a', { state: 'doing' }), task('b', after('a')), task('c', after('missing'))];
    const failed = stepOf(tasks[1], tasks, latestOf(run('a', { state: 'failed' })));
    expect(failed).toMatchObject({ kind: 'blocked', label: 'Held', next: 'Open the task that needs rework' });
    expect(failed.reason).toContain('Task a');
    expect(stepOf(tasks[2], tasks, {})).toMatchObject({ kind: 'blocked', blockedBy: [{ id: 'missing', title: 'Unavailable task' }] });
  });

  it('asks for the person when the agent does', () => {
    const t = task('a', { state: 'doing' });
    expect(stepOf(t, [t], latestOf(run('a', { state: 'waiting_for_user', waiting: 'question' })))).toMatchObject({ kind: 'attention', next: 'Answer' });
    expect(stepOf(t, [t], latestOf(run('a', { state: 'waiting_for_user', waiting: 'idle' })))).toMatchObject({ kind: 'attention', next: 'Reply' });
    expect(stepOf(t, [t], latestOf(run('a', { state: 'blocked', blocker: { summary: 'Needs a migration decision' } as Run['blocker'] })))).toMatchObject({ kind: 'attention', reason: 'Needs a migration decision' });
  });

  it('is unavailable for a task that cannot be found', () => {
    expect(stepOf(undefined, [], {})).toMatchObject({ kind: 'missing', label: 'Unavailable' });
  });
});

describe('a plan shows its progress and where it stands', () => {
  const plan = (...taskIds: string[]): WorkPlan => ({ id: 'pl', projectId: 'p', title: 'Plan', description: '', taskIds, decisions: '', questions: '', version: 1 });

  it('counts complete tasks and names the first one still to do', () => {
    const tasks = [task('a', { state: 'done' }), task('b'), task('c', after('b'))];
    const p = progressOf(plan('a', 'b', 'c'), tasks, {});
    expect(p).toMatchObject({ total: 3, done: 1, standing: 'ready', label: 'In progress', upNext: { id: 'b' } });
  });

  it('puts a task that can move before one that waits, whatever their order', () => {
    const tasks = [task('c', after('b')), task('b')];
    expect(progressOf(plan('c', 'b'), tasks, {}).upNext?.id).toBe('b');
    expect(progressOf(plan('c'), [task('c', after('missing'))], {}).upNext?.id).toBe('c');
  });

  it('is complete only when every task is, and empty with none', () => {
    expect(progressOf(plan('a'), [task('a', { state: 'done' })], {})).toMatchObject({ standing: 'complete', label: 'Complete', upNext: undefined });
    expect(progressOf(plan(), [], {})).toMatchObject({ standing: 'empty', total: 0 });
  });

  it('puts what needs the person before what is blocked', () => {
    const tasks = [task('a', { state: 'doing' }), task('b', { state: 'review' }), task('c', after('a'))];
    expect(progressOf(plan('a', 'b', 'c'), tasks, latestOf(run('a')))).toMatchObject({ standing: 'attention', label: 'Needs you' });
    const stuck = [task('a'), task('b', after('a'))];
    expect(progressOf(plan('a', 'b'), stuck, latestOf(run('a', { state: 'failed' })))).toMatchObject({ standing: 'blocked', label: '1 needs rework, 1 held', rework: 1 });
  });

  it('finds the plan of a task, and a task with no plan has none', () => {
    const plans = [plan('a', 'b')];
    expect(planOfTask('b', plans)?.id).toBe('pl');
    expect(planOfTask('standalone', plans)).toBeUndefined();
  });
});

describe('a task has one clear next action when one is possible', () => {
  const opts = { agentWork: true, canStart: true, hasRun: false };
  const next = (t: Task, tasks: Task[], latest: Record<string, Run>, o = opts) => nextAction(t, stepOf(t, tasks, latest), o);

  it('starts an agent on ready work, or runs it again after a failure', () => {
    const t = task('a');
    expect(next(t, [t], {})).toEqual({ kind: 'start', label: 'Start an agent' });
    expect(next(t, [t], latestOf(run('a', { state: 'stopped' })), { ...opts, hasRun: true })).toMatchObject({ kind: 'again' });
  });

  it('moves human work along, since an agent is never started on it', () => {
    const t = task('a');
    expect(next(t, [t], {}, { agentWork: false, canStart: false, hasRun: false })).toEqual({ kind: 'move', label: 'Move to In progress' });
    expect(next(task('a', { state: 'doing' }), [], {}, { agentWork: false, canStart: false, hasRun: false })).toEqual({ kind: 'move', label: 'Submit for review' });
  });

  it('answers a question, marks reviewed work done, reopens finished work', () => {
    const t = task('a', { state: 'doing' });
    expect(next(t, [t], latestOf(run('a', { state: 'waiting_for_user', waiting: 'question' })))).toEqual({ kind: 'answer', label: 'Answer' });
    expect(next(task('a', { state: 'review' }), [], {})).toEqual({ kind: 'review', label: 'Mark done' });
    expect(next(task('a', { state: 'done' }), [], {})).toEqual({ kind: 'reopen', label: 'Reopen task' });
  });

  it('offers nothing while the agent works, while waiting, or while a blocker must be cleared first', () => {
    const a = task('a', { state: 'doing' });
    const b = task('b', after('a'));
    expect(next(a, [a], latestOf(run('a')))).toMatchObject({ kind: 'none' });
    expect(next(b, [a, b], latestOf(run('a')))).toMatchObject({ kind: 'none' });
    expect(next(b, [a, b], latestOf(run('a', { state: 'failed' })))).toMatchObject({ kind: 'none' });
    expect(next(task('x', { archivedAt: 'now' }), [], {})).toMatchObject({ kind: 'none' });
  });
});

describe('Work → Plan → Task → Session, Session → Task → Plan, Task → Calendar → Task', () => {
  it('a task opened from its plan comes back to the plan, then Work', () => {
    const fromWork = [{ kind: 'work' as const }];
    const here = pushed({ kind: 'plan', projectId: 'p', planId: 'pl' }, fromWork);
    const href = withChain('#/p/p/task/t1?plan=pl', here);
    const chain = chainOf(href);
    expect(chain).toEqual([{ kind: 'plan', projectId: 'p', planId: 'pl' }, { kind: 'work' }]);
    expect(originHref(chain[0], chain.slice(1))).toBe('#/p/p/work?plan=pl&from=work');
  });

  it('a session opened from the task goes back to the task, which still goes back to the plan', () => {
    const task = pushed({ kind: 'task', projectId: 'p', taskId: 't1' }, [{ kind: 'plan', projectId: 'p', planId: 'pl' }, { kind: 'work' }]);
    const session = chainOf('#/sessions/run%3Ar1?from=' + encodeURIComponent(task.map(o => o.kind === 'task' ? 'task:p:t1' : o.kind === 'plan' ? 'plan:p:pl' : 'work').join('>')));
    expect(session[0]).toEqual({ kind: 'task', projectId: 'p', taskId: 't1' });
    const back = originHref(session[0], session.slice(1))!;
    expect(back).toContain('#/p/p/task/t1');
    expect(chainOf(back)[0]).toEqual({ kind: 'plan', projectId: 'p', planId: 'pl' });
  });

  it('the Calendar opened from a task goes back to that task, and the task opened from it goes back to the Calendar', () => {
    const toCalendar = withChain('#/p/p/calendar?task=t1', pushed({ kind: 'task', projectId: 'p', taskId: 't1' }, [{ kind: 'work' }]));
    expect(new URLSearchParams(toCalendar.split('?')[1]).get('task')).toBe('t1');
    const chain = chainOf(toCalendar);
    expect(originHref(chain[0], chain.slice(1))).toContain('#/p/p/task/t1');
    const fromCalendar = withChain('#/p/p/task/t1', [{ kind: 'calendar', projectId: 'p' }]);
    const back = chainOf(fromCalendar);
    expect(originHref(back[0], back.slice(1))).toBe('#/p/p/calendar');
  });
});

describe('Back from a task to a project Calendar names the task, so the Calendar opens on its entry', () => {
  it('adds the task to the Calendar address only when the Calendar is a project’s own', () => {
    expect(originHref({ kind: 'calendar', projectId: 'p' }, [], 't1')).toBe('#/p/p/calendar?task=t1');
    expect(originHref({ kind: 'calendar', projectId: 'p' }, [{ kind: 'work' }], 't1')).toBe('#/p/p/calendar?task=t1&from=work');
    expect(originHref({ kind: 'calendar', projectId: 'p' }, [])).toBe('#/p/p/calendar');
    expect(originHref({ kind: 'board', projectId: 'p' }, [], 't1')).toBe('#/p/p/work?section=queue');
  });
});

describe('a plan lists its steps in order, with the agent on each', () => {
  const tasks = [task('a', { state: 'done' }), task('b', { state: 'doing', ...after('a') }), task('c', after('b')), task('d', { state: 'doing' })];
  const ids = { taskIds: ['a', 'b', 'c', 'd'] };

  it('numbers each step and says what it comes after', () => {
    const steps = planSteps(ids, tasks, latestOf(run('b')));
    expect(steps.map(s => stepPosition(s))).toEqual(['Step 1 of 4', 'Step 2 of 4 · After step 1', 'Step 3 of 4 · After step 2', 'Step 4 of 4']);
  });

  it('names the agent only for a task that has a run, and takes freshness from the caller', () => {
    const latest = latestOf(run('b', { agentId: 'codex' }));
    expect(planSteps(ids, tasks, latest)[1].agent).toMatchObject({ agentId: 'codex', state: 'running', fresh: true });
    expect(planSteps(ids, tasks, latest, () => true)[1].agent).toMatchObject({ state: 'running', fresh: false });
    expect(planSteps(ids, tasks, latest)[2].agent).toBeUndefined();
  });

  it('maps a run waiting for the person to needs_input, never to running', () => {
    expect(agentOf(run('b', { state: 'waiting_for_user' }), false)?.state).toBe('needs_input');
  });

  it('calls two running steps parallel only when no dependency path joins them', () => {
    const latest = latestOf(run('b'), run('d', { agentId: 'claude-code' }));
    const steps = planSteps(ids, tasks, latest);
    expect(steps[1].parallel && steps[3].parallel).toBe(true);
    const chained = [task('a', { state: 'doing' }), task('b', { state: 'doing', ...after('a') })];
    expect(planSteps({ taskIds: ['a', 'b'] }, chained, latestOf(run('a'), run('b'))).some(s => s.parallel)).toBe(false);
    expect(planSteps(ids, tasks, latestOf(run('b'))).some(s => s.parallel)).toBe(false);
  });

  it('counts only live agents, at most three, with the rest counted', () => {
    const many = ['a', 'b', 'c', 'd', 'e'].map(id => task(id, { state: 'doing' }));
    const steps = planSteps({ taskIds: many.map(t => t.id) }, many, latestOf(...many.map(t => run(t.id))));
    expect(activeAgents(steps)).toMatchObject({ more: 2 });
    expect(activeAgents(steps).agents).toHaveLength(3);
    expect(activeAgents(planSteps(ids, tasks, latestOf(run('b', { state: 'completed' }))))).toEqual({ agents: [], more: 0 });
  });

  it('lists task branches once each and invents no plan branch', () => {
    const latest = latestOf(run('b', { branch: 'werkbord/b', headCommit: 'abcdef0123', updatedAt: '2026-11-02T10:00:00Z' }), run('d', { branch: 'werkbord/b', updatedAt: '2026-11-02T11:00:00Z' }));
    const branches = planBranches(planSteps(ids, tasks, latest), latest);
    expect(branches).toEqual([{ branch: 'werkbord/b', commit: 'abcdef0', taskId: 'b', title: 'Task b', at: '2026-11-02T10:00:00Z' }]);
  });
});
