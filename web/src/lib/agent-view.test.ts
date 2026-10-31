import { beforeEach, expect, it, vi } from 'vitest';
import { readAgentView, writeAgentView } from './agent-view';

const store = new Map<string, string>();
beforeEach(() => {
  store.clear();
  vi.stubGlobal('sessionStorage', { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) });
});

it('keeps the list’s filter and scroll for the tab, and starts clean when there is nothing or the record is not one', () => {
  expect(readAgentView()).toEqual({ filter: 'all', project: '', scroll: 0 });
  writeAgentView({ filter: 'live', project: 'prj_a', scroll: 240 });
  expect(readAgentView()).toEqual({ filter: 'live', project: 'prj_a', scroll: 240 });
  for (const bad of ['not json', 'null', '5', '{"filter":"nope","project":7,"scroll":-4}']) {
    store.set('werkbord.agents.view', bad);
    expect(readAgentView(), bad).toEqual({ filter: 'all', project: '', scroll: 0 });
  }
});
