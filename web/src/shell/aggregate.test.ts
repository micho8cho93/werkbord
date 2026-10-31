import { expect, it } from 'vitest';
import { agenda, reportedActivity, attention, attentionAction, attentionGroups, counts, executionCounts, executionLabel, myWork, running } from './aggregate';
import type { Overview, Summary } from './types';

it('keeps workspace identity, permissions and failures in cross-workspace views', () => {
  const individual = { id: 'personal', kind: 'personal' as const, name: 'Universal', state: 'ready' as const };
  const team = { id: 'team:main', kind: 'team' as const, name: 'Team', state: 'ready' as const };
  const sum = (workspace: typeof individual | typeof team): Summary => ({ schema: 'workspace-v1', workspace, at: '', projects: [], work: [{ id: 'same-id', title: workspace.name, project: 'Project', status: 'doing', href: '#/task' }], attention: [{ id: 'notice', kind: 'blocked', severity: 'warning', title: workspace.name }], schedule: [{ id: 'schedule', title: workspace.name, project: 'Project', at: '2026-10-10T10:00:00Z', state: 'waiting', href: '#/calendar' }] });
  const o: Overview = { at: '', entries: [{ workspace: individual, summary: sum(individual) }, { workspace: team, summary: sum(team) }, { workspace: { ...team, id: 'team:offline', state: 'offline' }, error: 'Offline' }] };
  expect(myWork(o).map(g => g.workspace.id)).toEqual(['personal', 'team:main', 'team:offline']);
  expect(attention(o).map(a => a.workspace.id)).toEqual(['personal', 'team:main']);
  expect(counts(o)).toEqual({ needsYou: 2, critical: 0, unreachable: 1 });
  expect(agenda(o, new Date('2026-10-09'))[0].items.map(a => a.workspace.id)).toEqual(['personal', 'team:main']);
});

it('counts execution states separately and never calls a stale report running now', () => {
  const workspace = { id: 'personal', kind: 'personal' as const, name: 'Universal', state: 'ready' as const };
  const summary: Summary = { schema: 'workspace-v1', workspace, at: '2026-10-11T10:00:00Z', projects: [], attention: [], schedule: [], work: [
    { id: 'run', title: 'Running', project: 'App', status: 'doing', execution: 'running', href: '#/task' },
    { id: 'queue1', title: 'Queued', project: 'App', status: 'todo', execution: 'queued', href: '#/task' },
    { id: 'queue2', title: 'Queued', project: 'App', status: 'todo', execution: 'queued', href: '#/task' },
    { id: 'start', title: 'Starting', project: 'App', status: 'doing', execution: 'starting', href: '#/task' },
    { id: 'stale', title: 'Offline runner', project: 'App', status: 'doing', execution: 'running', href: '#/task', report: { stale: true, pendingRequests: ['stop'] } },
  ] };
  const overview: Overview = { at: '', entries: [{ workspace, summary }] };
  expect(running(overview)).toBe(1);
  expect(executionCounts(overview)).toEqual({ running: 1, queued: 2, starting: 1, unknown: 1 });
  overview.entries[0].error = 'Disconnected';
  expect(executionCounts(overview)).toEqual({ running: 0, queued: 0, starting: 0, unknown: 5 });
  expect(myWork(overview)[0].items[4].report?.pendingRequests).toEqual(['stop']);
  expect(executionLabel('idle')).not.toBe(executionLabel('needs_input'));
});

it('puts critical outages first, keeps idle turns and reviews actionable, and separates host advice', () => {
  const workspace = { id: 'personal', kind: 'personal' as const, name: 'Universal', state: 'ready' as const };
  const summary: Summary = { schema: 'workspace-v1', workspace, at: '', projects: [], work: [], schedule: [], attention: [
    { id: 'host', kind: 'host', severity: 'warning', title: 'Add another host' },
    { id: 'review', kind: 'review', severity: 'info', title: 'Review changes', href: '#/review' },
    { id: 'idle', kind: 'idle', severity: 'info', title: 'Review turn', href: '#/task' },
    { id: 'question', kind: 'needs_input', severity: 'warning', title: 'Choose a schema', href: '#/task' },
    { id: 'outage', kind: 'host', severity: 'critical', title: 'Workspace read-only' },
  ] };
  const overview: Overview = { at: '', entries: [{ workspace, summary }] };
  const groups = attentionGroups(overview);
  expect(groups.map(g => g.id)).toEqual(['urgent', 'actions', 'warnings']);
  expect(groups[0].rows.map(r => r.item.id)).toEqual(['outage']);
  expect(groups[1].rows.map(r => r.item.id)).toEqual(['question', 'review', 'idle']);
  expect(groups[2].rows.map(r => r.item.id)).toEqual(['host']);
  expect(counts(overview).needsYou).toBe(5);
  expect(attentionAction(summary.attention[3])).toBe('Answer');
});

it('lists reported execution once: the person\'s own is in the sessions list, Team\'s is shown nowhere else', () => {
  const individual = { id: 'personal', kind: 'personal' as const, name: 'Universal', state: 'ready' as const };
  const team = { id: 'team:main', kind: 'team' as const, name: 'Team', state: 'ready' as const };
  const sum = (workspace: typeof individual | typeof team): Summary => ({ schema: 'workspace-v1', workspace, at: '', projects: [], attention: [], schedule: [], work: [
    { id: 'run', title: 'Running', project: 'App', status: 'doing', execution: 'running', href: '#/task' },
    { id: 'idle', title: 'Not executing', project: 'App', status: 'todo', href: '#/task' },
  ] });
  const o: Overview = { at: '', entries: [{ workspace: individual, summary: sum(individual) }, { workspace: team, summary: sum(team) }] };
  expect(reportedActivity(o).map(g => g.workspace.id)).toEqual(['personal', 'team:main']);
  expect(reportedActivity(o, true).map(g => g.workspace.id)).toEqual(['team:main']);
  expect(reportedActivity(o, true)[0].items.map(w => w.id)).toEqual(['run']);
  expect(reportedActivity({ at: '', entries: [{ workspace: individual, summary: sum(individual) }] }, true)).toEqual([]);
});
