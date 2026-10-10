import { describe, expect, it } from 'vitest';
import { filterActive, inkFor, labelsOf, matches, modeOf, noFilter, pruneFilter, sortedLabels, toggleLabel } from './labels';
import type { Label } from './types';

const label = (id: string, name: string, color = '#336699'): Label => ({ id, name, color, version: 1 });

describe('labels', () => {
  it('reads an absent mode as agent work, as every task was', () => {
    expect(modeOf({})).toBe('agent');
    expect(modeOf({ workMode: 'human' })).toBe('human');
  });

  it('keeps a task’s labels in the order they were put on and skips unknown ones', () => {
    const all = [label('a', 'Alpha'), label('b', 'Bravo'), label('c', 'Charlie')];
    expect(labelsOf({ labelIds: ['c', 'gone', 'a'] }, all).map((l) => l.id)).toEqual(['c', 'a']);
    expect(labelsOf({}, all)).toEqual([]);
  });

  it('sorts by name ignoring case', () => {
    expect(sortedLabels([label('1', 'bravo'), label('2', 'Alpha'), label('3', 'Charlie')]).map((l) => l.name)).toEqual(['Alpha', 'bravo', 'Charlie']);
  });

  it('picks the text colour with the better contrast', () => {
    expect(inkFor('#000000')).toBe('#ffffff');
    expect(inkFor('#ffffff')).toBe('#14130f');
    expect(inkFor('#d0a215')).toBe('#14130f'); // yellow
    expect(inkFor('#4385be')).toBe('#14130f'); // mid blue still reads better dark
    expect(inkFor('#1a237e')).toBe('#ffffff');
    expect(inkFor('not a colour')).toBe('#14130f');
  });
});

describe('filtering', () => {
  const tasks = [
    { id: 'x', labelIds: ['a'], workMode: 'agent' as const },
    { id: 'y', labelIds: ['a', 'b'], workMode: 'human' as const },
    { id: 'z', labelIds: ['b'], workMode: 'hybrid' as const },
    { id: 'n' },
  ];
  const ids = (f: ReturnType<typeof noFilter>) => tasks.filter((t) => matches(t, f)).map((t) => t.id);

  it('keeps everything when nothing is set', () => {
    expect(filterActive(noFilter())).toBe(false);
    expect(ids(noFilter())).toEqual(['x', 'y', 'z', 'n']);
  });

  it('matches any of the labels, or all of them', () => {
    expect(ids({ labels: ['a'], match: 'any', mode: '' })).toEqual(['x', 'y']);
    expect(ids({ labels: ['a', 'b'], match: 'any', mode: '' })).toEqual(['x', 'y', 'z']);
    expect(ids({ labels: ['a', 'b'], match: 'all', mode: '' })).toEqual(['y']);
  });

  it('filters by work mode, alone or with labels, and reads a missing mode as agent', () => {
    expect(ids({ labels: [], match: 'any', mode: 'human' })).toEqual(['y']);
    expect(ids({ labels: [], match: 'any', mode: 'agent' })).toEqual(['x', 'n']);
    expect(ids({ labels: ['b'], match: 'any', mode: 'hybrid' })).toEqual(['z']);
  });

  it('toggles a label in and out and forgets deleted ones', () => {
    let f = noFilter();
    f = toggleLabel(f, 'a');
    expect(f.labels).toEqual(['a']);
    f = toggleLabel(toggleLabel(f, 'b'), 'a');
    expect(f.labels).toEqual(['b']);
    expect(pruneFilter(f, [label('a', 'A')]).labels).toEqual([]);
    expect(pruneFilter(f, [label('b', 'B')])).toBe(f);
  });
});
