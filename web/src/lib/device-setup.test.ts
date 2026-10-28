import { describe, expect, it } from 'vitest';
import { bindingLabel, connectProblem, connectionSummary, desktopCodeReady, normalizeDeviceStatus } from './device-setup';

const code = 'ab'.repeat(32);
const base = { controller: 'https://home.tailnet.ts.net:8443', name: 'Laptop', code, runner: false, runnerCode: '' };

describe('normalizeDeviceStatus', () => {
  it('turns what Go sends for nothing into empty values', () => {
    const s = normalizeDeviceStatus({ projects: null, bindings: null, job: undefined } as never);
    expect(s).toMatchObject({ mode: 'local', runner: false, projects: [], bindings: {}, job: { running: false } });
  });
  it('treats anything but remote as the local controller', () => {
    expect(normalizeDeviceStatus({ mode: 'nonsense' } as never).mode).toBe('local');
    expect(normalizeDeviceStatus(undefined).mode).toBe('local');
    expect(normalizeDeviceStatus({ mode: 'remote', controller: 'http://h' }).mode).toBe('remote');
  });
});

describe('connectionSummary', () => {
  it('says where the board lives', () => {
    expect(connectionSummary(normalizeDeviceStatus({}))).toContain('its own board');
    expect(connectionSummary(normalizeDeviceStatus({ mode: 'remote', controller: 'http://h', runner: true }))).toContain('runs agents');
    expect(connectionSummary(normalizeDeviceStatus({ mode: 'remote', controller: 'http://h' }))).toContain('agents run on its runners');
  });
});

describe('connectProblem', () => {
  it('accepts a complete request', () => expect(connectProblem(base, false)).toBe(''));
  it('asks for what is missing, in order', () => {
    expect(connectProblem({ ...base, controller: ' ' }, false)).toMatch(/controller address/);
    expect(connectProblem({ ...base, code: '' }, false)).toMatch(/desktop pairing code/);
    expect(connectProblem({ ...base, code: 'abc' }, false)).toMatch(/64/);
    expect(connectProblem({ ...base, runner: true }, false)).toMatch(/runner pairing code/);
  });
  it('allows a retry that reuses a saved pairing', () => expect(connectProblem({ ...base, code: '' }, true)).toBe(''));
  it('recognises a complete code', () => {
    expect(desktopCodeReady(` ${code} `)).toBe(true);
    expect(desktopCodeReady(code.slice(1))).toBe(false);
  });
});

describe('bindingLabel', () => {
  it('shows the chosen clone or says what is missing', () => {
    expect(bindingLabel('/code/app')).toBe('/code/app');
    expect(bindingLabel(undefined)).toMatch(/No local clone/);
  });
});
