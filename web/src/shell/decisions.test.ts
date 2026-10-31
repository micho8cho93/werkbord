import { describe, expect, it } from 'vitest';
import { agentName, byArrival, current, pending, reconcile, type Decision } from './decisions';
import { decisions } from './decisions.svelte';
import { attentionGroups } from './aggregate';
import type { Attention, Entry, Overview, OverviewEntry, Summary } from './types';

const personal: Entry = { id: 'personal', kind: 'personal', name: 'Personal', state: 'ready' };
const team: Entry = { id: 'team:main', kind: 'team', name: 'Team', state: 'ready' };

const question = (id: string, askedAt: string, project = 'prj_a', extra: Partial<Attention> = {}): Attention => ({
  id: `question-${id}`, kind: 'needs_input', severity: 'warning', title: `An agent is asking: ${id}`, detail: `Task ${id}`, project: 'App',
  href: `#/p/${project}/task/t_${id}`, at: askedAt,
  question: { id, runId: `run_${id}`, projectId: project, taskId: `t_${id}`, kind: 'decision', prompt: `Prompt ${id}`, allowFreeText: true, askedAt, agent: 'claude-code' },
  ...extra,
});
const entry = (workspace: Entry, attention: Attention[], over: Partial<OverviewEntry> = {}): OverviewEntry => ({
  workspace, summary: { schema: 'workspace-v1', workspace, at: '', projects: [], work: [], schedule: [], attention } as Summary, ...over,
});
const overview = (...entries: OverviewEntry[]): Overview => ({ at: '', entries });
const keys = (list: Decision[]) => list.map((d) => d.key);

describe('which decisions there are', () => {
  it('takes only agent questions, not other things that need attention', () => {
    const o = overview(entry(personal, [question('a', '2026-10-01T10:00:00Z'), { id: 'blocked-1', kind: 'blocked', severity: 'warning', title: 'Blocked' }, { id: 'failed-1', kind: 'failed', severity: 'critical', title: 'Failed' }]));
    expect(keys(reconcile([], o))).toEqual(['personal|question-a']);
  });
  it('names the agent and the project from the record', () => {
    const [d] = reconcile([], overview(entry(personal, [question('a', '2026-10-01T10:00:00Z')])));
    expect(d).toMatchObject({ agent: 'Claude Code', task: 'Task a', project: 'App', projectId: 'prj_a', runId: 'run_a' });
    expect(agentName('')).toBe('');
  });
});

describe('first come, first served', () => {
  const o = overview(entry(personal, [question('late', '2026-10-01T12:00:00Z'), question('early', '2026-10-01T09:00:00Z')]), entry(team, [question('team', '2026-10-01T10:00:00Z', 'p1', { question: undefined, href: '?project=p1&ticket=7' })]));
  it('orders by the time each arrived, across workspaces, oldest first', () => {
    expect(keys(pending(reconcile([], o), null))).toEqual(['personal|question-early', 'team:main|question-team', 'personal|question-late']);
  });
  it('breaks a tie by the stable key, whatever order the sources listed them in', () => {
    const same = '2026-10-01T09:00:00Z';
    const forward = reconcile([], overview(entry(personal, [question('b', same), question('a', same)])));
    const reversed = reconcile([], overview(entry(personal, [question('a', same), question('b', same)])));
    expect(keys(pending(forward, null))).toEqual(['personal|question-a', 'personal|question-b']);
    expect(keys(pending(reversed, null))).toEqual(keys(pending(forward, null)));
  });
  it('puts one with no usable time last, and still in a fixed order', () => {
    const list = reconcile([], overview(entry(personal, [question('z', 'unknown', 'prj_a', { question: undefined, at: undefined }), question('y', '', 'prj_a', { question: undefined, at: undefined }), question('a', '2026-10-01T09:00:00Z')])));
    expect(keys(pending(list, null))).toEqual(['personal|question-a', 'personal|question-y', 'personal|question-z']);
    expect(byArrival(list[0], list[0])).toBe(0);
  });
  it('is not the order of failures and warnings, which stays by severity', () => {
    const old = question('old', '2026-09-01T00:00:00Z');
    const failure: Attention = { id: 'failed-1', kind: 'failed', severity: 'critical', title: 'Failed', at: '2026-10-02T00:00:00Z' };
    const groups = attentionGroups(overview(entry(personal, [old, failure])));
    expect(groups[0].rows[0].item.id).toBe('failed-1');
    expect(groups.map((g) => g.id)).toEqual(['urgent', 'actions']);
  });
});

describe('the project context', () => {
  const o = overview(entry(personal, [question('a1', '2026-10-01T09:00:00Z', 'prj_a'), question('b1', '2026-10-01T08:00:00Z', 'prj_b'), question('a2', '2026-10-01T10:00:00Z', 'prj_a')]), entry(team, [question('t', '2026-10-01T07:00:00Z', 'p1', { question: undefined, href: '?project=p1&ticket=1' })]));
  const all = reconcile([], o);
  it('All Projects takes every decision', () => expect(pending(all, null)).toHaveLength(4));
  it('one project takes only its own, still oldest first', () => {
    expect(keys(pending(all, { workspace: 'personal', project: 'prj_a' }))).toEqual(['personal|question-a1', 'personal|question-a2']);
    expect(keys(pending(all, { workspace: 'team:main', project: 'p1' }))).toEqual(['team:main|question-t']);
  });
  it('a project with the same id in another workspace is not the same project', () => {
    expect(pending(all, { workspace: 'team:main', project: 'prj_a' })).toEqual([]);
  });
});

describe('stale and disconnected sources', () => {
  const first = reconcile([], overview(entry(personal, [question('a', '2026-10-01T09:00:00Z')]), entry(team, [question('t', '2026-10-01T10:00:00Z', 'p1', { question: undefined, href: '?project=p1' })])));
  it('keeps a decision while its workspace does not answer, and says it is not current', () => {
    for (const silent of [{ error: 'Offline' }, { summary: undefined }, { workspace: { ...personal, state: 'offline' as const } }]) {
      const o = overview({ ...entry(personal, []), ...silent }, entry(team, []));
      const after = reconcile(first, o);
      expect(keys(after)).toContain('personal|question-a');
      expect(current(after.find((d) => d.key === 'personal|question-a')!, o)).toBe(false);
      // The workspace that did answer, and lists nothing, has resolved its own.
      expect(keys(after)).not.toContain('team:main|question-t');
    }
  });
  it('keeps the same decision through a failed refresh and back, without a duplicate or a lost arrival time', () => {
    const lost = overview({ ...entry(personal, []), error: 'Reconnect' }, entry(team, []));
    const back = overview(entry(personal, [question('a', '2026-10-01T09:00:00Z')]), entry(team, []));
    const after = reconcile(reconcile(first, lost), back);
    expect(keys(after)).toEqual(['personal|question-a']);
    expect(after[0].askedAt).toBe('2026-10-01T09:00:00Z');
  });
  it('drops a decision only when its workspace answers without it', () => {
    expect(reconcile(first, overview(entry(personal, []), entry(team, [])))).toEqual([]);
  });
  it('forgets a workspace the person has left, and keeps everything when there is no overview yet', () => {
    expect(keys(reconcile(first, overview(entry(team, []))))).toEqual([]);
    expect(reconcile(first, null)).toEqual(first);
  });
});

describe('resolving a decision', () => {
  it('advances to the next the moment one is answered, and confirms when the workspace stops listing it', () => {
    decisions.list = [];
    decisions.answered = {};
    decisions.sync(overview(entry(personal, [question('a', '2026-10-01T09:00:00Z'), question('b', '2026-10-01T10:00:00Z')])));
    const head = () => pending(decisions.list, null, new Set(Object.keys(decisions.answered)))[0]?.key;
    expect(head()).toBe('personal|question-a');
    decisions.settle('personal|question-a');
    expect(head()).toBe('personal|question-b');
    // The next refresh still lists it for a moment: it stays answered, not first again.
    decisions.sync(overview(entry(personal, [question('a', '2026-10-01T09:00:00Z'), question('b', '2026-10-01T10:00:00Z')])));
    expect(head()).toBe('personal|question-b');
    // Then the workspace confirms: the record of having answered it is no longer needed.
    decisions.sync(overview(entry(personal, [question('b', '2026-10-01T10:00:00Z')])));
    expect(decisions.answered).toEqual({});
    decisions.sync(overview(entry(personal, [])));
    expect(head()).toBeUndefined();
  });
  it('does not forget an answered decision while its workspace is silent', () => {
    decisions.list = [];
    decisions.answered = {};
    decisions.sync(overview(entry(personal, [question('a', '2026-10-01T09:00:00Z')])));
    decisions.settle('personal|question-a');
    decisions.sync(overview({ ...entry(personal, []), error: 'Offline' }));
    expect(decisions.answered).toEqual({ 'personal|question-a': true });
  });
});
