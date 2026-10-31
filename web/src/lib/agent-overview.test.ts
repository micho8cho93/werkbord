import { describe, expect, it } from 'vitest';
import { controlsFor, hasTaskContext, matches, outcomeOf, overviewOf, providersOf, reportedLine, sessionState, summaryOf } from './agent-overview';
import { isSessionKey, runKey, terminalKey, unique, type AgentSession } from './agent-sessions';
import { chainOf, pushed, withChain } from './origin';
import { originHref } from './origin-nav';
import { sessionsHref } from './location';

const caps = { feed: true, message: true, terminal: false, questions: true, stop: true };
const s = (o: Partial<AgentSession>): AgentSession => ({
  key: 'run:run_a', kind: 'run', id: 'run_a', projectId: 'p', taskId: 't1', agentId: 'codex', title: 'Fix login', state: 'running', live: true,
  capabilities: caps, createdAt: '2026-10-31T09:00:00Z', updatedAt: '2026-10-31T09:00:00Z', ...o,
});

describe('the overview separates what needs the person, what runs and what finished', () => {
  const all = [
    s({ key: 'run:a', id: 'a', waiting: true, updatedAt: '2026-10-31T10:00:00Z' }),
    s({ key: 'run:b', id: 'b' }),
    s({ key: 'run:c', id: 'c', live: false, state: 'completed', updatedAt: '2026-10-31T11:00:00Z' }),
    s({ key: 'run:d', id: 'd', live: false, state: 'failed', updatedAt: '2026-10-31T08:00:00Z' }),
    s({ key: 'terminal:e', kind: 'terminal', id: 'e', taskId: undefined, agentId: 'claude', state: 'interrupted', live: false, capabilities: { ...caps, terminal: true } }),
  ];

  it('groups live work by whether it waits, and puts failures and interruptions first among the finished', () => {
    const o = overviewOf(all);
    expect(o.needsYou.map(x => x.key)).toEqual(['run:a']);
    expect(o.running.map(x => x.key)).toEqual(['run:b']);
    expect(o.recent.map(x => x.key).slice(0, 2).sort()).toEqual(['run:d', 'terminal:e']);
    expect(o.recent.at(-1)?.key).toBe('run:c');
    expect(o.counts).toEqual({ needsYou: 1, running: 1, failed: 1, recent: 3 });
  });

  it('names how a session ended, and only says in the summary what is true', () => {
    expect(outcomeOf(all[2])).toBe('completed');
    expect(outcomeOf(all[3])).toBe('failed');
    expect(outcomeOf(all[4])).toBe('interrupted');
    expect(sessionState(all[0])).toBe('Needs you');
    expect(sessionState(all[2])).toBe('Completed');
    expect(summaryOf(overviewOf(all)).map(x => x.text)).toEqual(['1 needs you', '1 running', '1 failed']);
    expect(summaryOf(overviewOf([all[2]]))).toEqual([]);
  });

  it('filters by project, provider and kind, and a standalone session is one with no task', () => {
    expect(providersOf(all)).toEqual(['claude', 'codex']);
    expect(overviewOf(all, { provider: 'claude' }).recent.map(x => x.key)).toEqual(['terminal:e']);
    expect(overviewOf(all, { project: 'other' }).counts.recent).toBe(0);
    expect(overviewOf(all, { show: 'standalone' }).recent.map(x => x.key)).toEqual(['terminal:e']);
    expect(overviewOf(all, { show: 'live' }).counts).toMatchObject({ needsYou: 1, running: 1, recent: 0 });
    expect(matches(all[1], { show: 'task' })).toBe(true);
    expect(hasTaskContext(all[4])).toBe(false);
    expect(hasTaskContext(all[0])).toBe(true);
  });
});

describe('a session keeps one identity wherever it is opened', () => {
  it('lists an execution once and names it by the owner’s ID, from Work, Agents or Salie alike', () => {
    const a = s({});
    expect(unique([a, { ...a }, { ...a, title: 'copy' }])).toHaveLength(1);
    expect(runKey('run_a')).toBe(a.key);
    expect(isSessionKey(a.key) && isSessionKey(terminalKey('abc-1'))).toBe(true);
    expect(overviewOf(unique([a, { ...a }])).counts.running).toBe(1);
    // The address Agents opens a session at depends on the key alone, so every entry point leads to the same place.
    expect(sessionsHref(a.key)).toBe('#/sessions/run%3Arun_a');
  });

  it('switching to another session changes only the key in the address, never what runs', () => {
    expect(sessionsHref('run:run_b')).toBe('#/sessions/run%3Arun_b');
    const from = chainOf('#/sessions/run%3Arun_a?from=task%3Ap%3At1>work');
    expect(withChain(sessionsHref('run:run_b'), from)).toBe('#/sessions/run%3Arun_b?from=task%3Ap%3At1%3Ework');
    expect(chainOf(withChain(sessionsHref('run:run_b'), from))).toEqual(from);
  });
});

describe('a session leads back to its task and plan, and to where the person was', () => {
  it('Work → Plan → Task → Session and back, one step at a time', () => {
    const toSession = pushed({ kind: 'session', key: 'run:run_a' }, [{ kind: 'task', projectId: 'p', taskId: 't1' }, { kind: 'plan', projectId: 'p', planId: 'pl' }, { kind: 'work' }]);
    // Session → Task: the task is the nearest step, and the plan then Work remain behind it.
    const back = pushed({ kind: 'task', projectId: 'p', taskId: 't1' }, [{ kind: 'plan', projectId: 'p', planId: 'pl' }, { kind: 'work' }]);
    const href = originHref(back[0], back.slice(1))!;
    expect(href).toMatch(/^#\/p\/p\/task\/t1\?from=/);
    expect(chainOf(href)).toEqual([{ kind: 'plan', projectId: 'p', planId: 'pl' }, { kind: 'work' }]);
    expect(toSession.length).toBeLessThanOrEqual(3);
  });

  it('a session opened from the list goes back to the list, one from Salie to Salie', () => {
    expect(originHref({ kind: 'agents' }, [])).toBe('#/sessions');
    expect(originHref({ kind: 'salie', conversation: 'c1' }, [])).toBeNull();
  });
});

describe('only what the controller can operate is offered', () => {
  it('offers a live run its instructions and stop, and an ended one nothing', () => {
    expect(controlsFor(s({}))).toMatchObject({ message: true, stop: true, terminal: false, reason: '' });
    expect(controlsFor(s({ live: false, state: 'completed' }))).toMatchObject({ message: false, stop: false, terminal: false });
    expect(controlsFor(s({ live: false }))?.reason).toMatch(/ended/);
  });

  it('offers a terminal its keys only when it is a terminal the controller can drive', () => {
    const t = s({ kind: 'terminal', capabilities: { feed: false, message: false, terminal: true, questions: false, stop: true } });
    expect(controlsFor(t)).toMatchObject({ terminal: true, stop: true, message: false });
    expect(controlsFor({ ...t, capabilities: { ...t.capabilities, terminal: false, stop: false } })).toMatchObject({ terminal: false, stop: false });
  });

  it('shows but does not operate a session the controller can only read, or cannot reach', () => {
    const readOnly = controlsFor(s({ capabilities: { feed: true, message: false, terminal: false, questions: false, stop: false } }));
    expect(readOnly).toMatchObject({ message: false, stop: false, terminal: false });
    expect(readOnly.reason).toMatch(/not operate/);
    const offline = controlsFor(s({}), 'reconnecting');
    expect(offline).toMatchObject({ message: false, stop: false, terminal: false });
    expect(offline.reason).toMatch(/out of date/);
    expect(controlsFor(undefined)).toMatchObject({ message: false, stop: false, terminal: false });
  });
});

describe('remote activity says how fresh it is, and when it is not available', () => {
  const now = Date.parse('2026-10-31T12:00:00Z');
  it('is live when just reported, older when not, last known when stale', () => {
    expect(reportedLine({ recordedAt: '2026-10-31T11:59:40Z', stale: false }, now)).toEqual({ label: 'Reported 20s ago', freshness: 'live' });
    expect(reportedLine({ recordedAt: '2026-10-31T11:50:00Z', stale: false }, now)).toEqual({ label: 'Reported 10m ago', freshness: 'recent' });
    expect(reportedLine({ recordedAt: '2026-10-31T09:00:00Z', stale: true }, now)).toEqual({ label: 'Last reported 3h ago', freshness: 'stale' });
    expect(reportedLine({ stale: true }, now)).toEqual({ label: 'Last reported', freshness: 'stale' });
    expect(reportedLine(undefined, now).freshness).toBe('unknown');
  });
  it('is unavailable, not live, when the workspace cannot be reached', () => {
    expect(reportedLine({ recordedAt: '2026-10-31T11:59:50Z', stale: false }, now, false)).toEqual({ label: 'Unavailable · last known', freshness: 'unknown' });
  });
});
