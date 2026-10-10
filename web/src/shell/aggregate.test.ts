import { expect, it } from 'vitest';
import { agenda, attention, counts, myWork } from './aggregate';
import type { Overview, Summary } from './types';

it('keeps workspace identity, permissions and failures in cross-workspace views', () => {
  const individual = { id: 'personal', kind: 'personal' as const, name: 'Individual', state: 'ready' as const };
  const team = { id: 'team:main', kind: 'team' as const, name: 'Team', state: 'ready' as const };
  const sum = (workspace: typeof individual | typeof team): Summary => ({ schema: 'workspace-v1', workspace, at: '', projects: [], work: [{ id: 'same-id', title: workspace.name, project: 'Project', status: 'doing', href: '#/task' }], attention: [{ id: 'notice', kind: 'blocked', severity: 'warning', title: workspace.name }], schedule: [{ id: 'schedule', title: workspace.name, project: 'Project', at: '2026-10-10T10:00:00Z', state: 'waiting', href: '#/calendar' }] });
  const o: Overview = { at: '', entries: [{ workspace: individual, summary: sum(individual) }, { workspace: team, summary: sum(team) }, { workspace: { ...team, id: 'team:offline', state: 'offline' }, error: 'Offline' }] };
  expect(myWork(o).map(g => g.workspace.id)).toEqual(['personal', 'team:main', 'team:offline']);
  expect(attention(o).map(a => a.workspace.id)).toEqual(['personal', 'team:main']);
  expect(counts(o)).toEqual({ needsYou: 2, critical: 0, unreachable: 1 });
  expect(agenda(o, new Date('2026-10-09'))[0].items.map(a => a.workspace.id)).toEqual(['personal', 'team:main']);
});
