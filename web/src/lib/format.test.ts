import { describe, expect, it } from 'vitest';
import { agentName, cardActivity, formatDuration, oneLine, runElapsed, runStatus, timeAgo } from './format';
import type { Run } from './types';

const run = (over: Partial<Run> = {}): Run => ({
  id: 'run_1', taskId: 'tsk_1', projectId: 'prj_1', agentId: 'claude-code', state: 'running', version: 1,
  policy: { interaction: 'interactive' }, createdAt: '2026-10-04T10:00:00Z', updatedAt: '2026-10-04T10:00:00Z', ...over,
});

describe('runStatus', () => {
  it('says whether the agent needs the user, and for what', () => {
    expect(runStatus(run({ state: 'running' }))).toMatchObject({ label: 'Running', tone: 'work', needsInput: false, active: true });
    expect(runStatus(run({ state: 'starting' }))).toMatchObject({ label: 'Starting', needsInput: false, active: true });
    expect(runStatus(run({ state: 'waiting_for_user', waiting: 'question' }))).toMatchObject({ label: 'Needs input', tone: 'ask', needsInput: true });
    expect(runStatus(run({ state: 'waiting_for_user', waiting: 'idle' }))).toMatchObject({ label: 'Waiting for you', tone: 'idle', needsInput: true });
  });

  it('describes how a finished run ended, with the reason', () => {
    expect(runStatus(run({ state: 'completed' }))).toMatchObject({ label: 'Completed', tone: 'ok', active: false, needsInput: false });
    expect(runStatus(run({ state: 'failed', reason: 'exited with status 2' }))).toMatchObject({ label: 'Failed', tone: 'bad', active: false, hint: 'exited with status 2' });
    expect(runStatus(run({ state: 'stopped', reason: 'stopped by user' }))).toMatchObject({ label: 'Stopped', tone: 'neutral', active: false });
  });
});

// A task in Doing is, at a glance, Running, Needs input, Blocked or Failed: four states of the run,
// each told apart by label and by tone, and none of them a column.
describe('the four states of a task in Doing', () => {
  const states = {
    running: runStatus(run({ state: 'running' })),
    needsInput: runStatus(run({ state: 'waiting_for_user', waiting: 'question' })),
    blocked: runStatus(run({ state: 'blocked', blocker: { summary: 'Which provider?', source: 'question', raisedAt: '2026-10-04T10:01:00Z' } })),
    failed: runStatus(run({ state: 'failed', reason: 'exit 1' })),
  };

  it('are told apart by wording and by tone', () => {
    expect(Object.values(states).map((s) => s.label)).toEqual(['Running', 'Needs input', 'Blocked', 'Failed']);
    expect(new Set(Object.values(states).map((s) => s.tone)).size).toBe(4);
  });

  it('say whether the user is wanted', () => {
    expect(states.running.needsInput).toBe(false);
    expect(states.needsInput.needsInput).toBe(true);
    expect(states.blocked).toMatchObject({ tone: 'block', needsInput: true, active: true });
    expect(states.failed).toMatchObject({ needsInput: false, active: false });
  });

  it('puts the blocker, not the activity, on a blocked card', () => {
    const blocked = run({ state: 'blocked', activity: 'editing main.go', blocker: { summary: 'Which provider?', source: 'report', raisedAt: '2026-10-04T10:01:00Z' } });
    expect(cardActivity(blocked)).toBe('Which provider?');
    expect(cardActivity(run({ state: 'blocked' }))).toBe('');
    expect(cardActivity(run({ state: 'running', activity: 'editing main.go' }))).toBe('editing main.go');
  });
});

describe('durations', () => {
  it('formats them compactly', () => {
    expect(formatDuration(0)).toBe('0s');
    expect(formatDuration(59_999)).toBe('59s');
    expect(formatDuration(60_000)).toBe('1m 00s');
    expect(formatDuration(192_000)).toBe('3m 12s');
    expect(formatDuration(3_840_000)).toBe('1h 04m');
    expect(formatDuration(-5000)).toBe('0s');
  });

  it('counts up while a run is going and stops when it ends', () => {
    const start = Date.parse('2026-10-04T10:00:00Z');
    expect(runElapsed(run(), start + 192_000)).toBe('3m 12s');
    const ended = run({ state: 'completed', endedAt: '2026-10-04T10:05:00Z' });
    expect(runElapsed(ended, start + 99_999_999)).toBe('5m 00s');
  });

  it('says how long ago something happened', () => {
    const now = Date.parse('2026-10-04T12:00:00Z');
    expect(timeAgo('2026-10-04T11:59:55Z', now)).toBe('just now');
    expect(timeAgo('2026-10-04T11:59:15Z', now)).toBe('45s ago');
    expect(timeAgo('2026-10-04T11:55:00Z', now)).toBe('5m ago');
    expect(timeAgo('2026-10-04T09:00:00Z', now)).toBe('3h ago');
    expect(timeAgo('2026-10-02T12:00:00Z', now)).toBe('2d ago');
  });
});

describe('card text', () => {
  it('shows what the agent is doing while it works, and why it failed after', () => {
    expect(cardActivity(run({ activity: 'Edit src/a.ts' }))).toBe('Edit src/a.ts');
    expect(cardActivity(run({ state: 'waiting_for_user', waiting: 'idle', activity: 'Done with the parser.' }))).toBe('Done with the parser.');
    expect(cardActivity(run({ state: 'failed', reason: 'could not start codex: not signed in', activity: 'old' }))).toBe('could not start codex: not signed in');
    expect(cardActivity(run({ state: 'completed', activity: 'old' }))).toBe('');
    expect(cardActivity(run())).toBe('');
  });

  it('reduces text to one line', () => {
    expect(oneLine('  a\n\n  b\tc ')).toBe('a b c');
    expect(oneLine('x'.repeat(200), 10)).toBe('xxxxxxxxx…');
  });

  it('names agents, falling back to the id', () => {
    const agents = [{ id: 'codex', name: 'Codex', available: true }];
    expect(agentName(agents, 'codex')).toBe('Codex');
    expect(agentName(agents, 'unknown')).toBe('unknown');
  });
});
