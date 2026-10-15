import { describe, expect, it } from 'vitest';
import { matchesRunner, runnerCard } from './runners';
import type { Runner } from '../types';

const GB = 1024 ** 3;
const runner = (over: Partial<Runner> = {}) =>
  ({ id: 'r', name: 'Mac mini', kind: 'remote', os: 'darwin', arch: 'arm64', version: '4.32.0', online: true, disabled: false, currentRuns: 2, capacity: 4, capabilities: { cpu: 8, ramBytes: 24 * GB, availableRamBytes: 12 * GB, storageBytes: 500 * GB, agents: [], options: [], repositories: [] }, ...over }) as Runner;

describe('runnerCard', () => {
  it('says what a working runner reports of itself', () => {
    expect(runnerCard(runner())).toMatchObject({ name: 'Mac mini', spec: 'darwin/arm64 · Werkbord 4.32.0', state: 'Working', tone: 'work', online: true, slots: '2 of 4', cpu: '8 cores', ram: '12 GB free', storage: '500 GB' });
  });

  it('names this computer, and says nothing it was not told', () => {
    const c = runnerCard(runner({ kind: 'local', capabilities: { cpu: 1, agents: [], options: [], repositories: [] } }));
    expect(c.name).toBe('Mac mini (this computer)');
    expect(c).toMatchObject({ cpu: '1 core', ram: '', storage: '' });
  });

  it('shows no usage for a runner that is offline or switched off', () => {
    expect(runnerCard(runner({ online: false }))).toMatchObject({ state: 'Offline', tone: 'neutral', online: false, slots: '', cpu: '' });
    expect(runnerCard(runner({ disabled: true }))).toMatchObject({ state: 'Disabled', online: false });
  });

  it('is idle and ok when connected with nothing running', () => {
    expect(runnerCard(runner({ currentRuns: 0 }))).toMatchObject({ state: 'Idle', tone: 'ok' });
  });
});

describe('matchesRunner', () => {
  it('counts reachable and switched-on runners as online, everything else as offline', () => {
    const on = { online: true, disabled: false };
    const off = { online: false, disabled: false };
    const disabled = { online: true, disabled: true };
    expect([on, off, disabled].map((r) => matchesRunner(r, 'online'))).toEqual([true, false, false]);
    expect([on, off, disabled].map((r) => matchesRunner(r, 'offline'))).toEqual([false, true, true]);
    expect([on, off, disabled].every((r) => matchesRunner(r, 'all'))).toBe(true);
  });
});
