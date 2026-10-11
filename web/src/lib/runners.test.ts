import { describe, expect, it } from 'vitest';
import { eligibleRunners, executionAgents, onlyBusy } from './execution';
import { normalizeRunner, normalizeRunners } from './runners';
import type { Runner } from './types';

// What the Go controller sends for a runner that has reported nothing: nil slices are `null`.
const bare = {
  id: 'r1', name: 'Bare', kind: 'remote', hostname: 'h', os: 'linux', arch: 'amd64', version: '', createdAt: '', lastSeenAt: '',
  online: true, disabled: false, automatic: true, capacity: 1, currentRuns: 0, allowClone: false,
  projects: null,
  capabilities: { cpu: 2, agents: null, options: null, repositories: null },
} as unknown as Runner;

describe('normalizeRunner', () => {
  it('turns null lists into empty ones', () => {
    const r = normalizeRunner(bare);
    expect(r.projects).toEqual([]);
    expect(r.capabilities).toMatchObject({ cpu: 2, agents: [], options: [], repositories: [] });
  });

  it('copes with a runner that has no capabilities yet', () => {
    const r = normalizeRunner({ ...bare, capabilities: undefined } as unknown as Runner);
    expect(r.capabilities.agents).toEqual([]);
  });

  it('keeps lists that are there, and leaves the input alone', () => {
    const full = { ...bare, projects: ['p'], capabilities: { ...bare.capabilities, repositories: ['p'] } } as unknown as Runner;
    expect(normalizeRunner(full).projects).toEqual(['p']);
    expect(bare.projects).toBeNull();
  });

  it('accepts no list at all', () => {
    expect(normalizeRunners(null)).toEqual([]);
    expect(normalizeRunners(undefined)).toEqual([]);
  });
});

describe('runner helpers given null lists', () => {
  it('no longer throw before normalising, and treat a runner with nothing reported as unable to take the task', () => {
    expect(() => eligibleRunners([bare], 'p')).not.toThrow();
    expect(eligibleRunners([bare], 'p')).toEqual([]);
    expect(executionAgents([], [bare], 'p')).toEqual([]);
    expect(onlyBusy([], [bare], 'p')).toBe(false);
  });
});
