import { describe, expect, it } from 'vitest';
import { ageLabel, avatarTone, initials, resolveAgentState } from './identity';
import { AGENT_MARKS, agentName } from './agent-marks';

const NOW = Date.parse('2026-11-02T12:00:00Z');
const LIVE = ['queued', 'starting', 'running', 'needs_input', 'blocked'];
const ENDED = ['completed', 'failed', 'stopped'];

describe('agent state truth table', () => {
  // Only a confirmed, fresh running state may animate, and only when motion is allowed.
  for (const state of [...LIVE, ...ENDED, 'requested', 'nonsense', ''])
    for (const fresh of [true, false, null, undefined])
      for (const reducedMotion of [false, true])
        it(`${state || 'empty'} · fresh ${fresh} · reduced ${reducedMotion}`, () => {
          const v = resolveAgentState({ state, fresh, reducedMotion, agentName: 'Codex' });
          expect(v.animate).toBe(state === 'running' && fresh === true && !reducedMotion);
          expect(v.arc).toBe(state === 'running' && fresh === true && reducedMotion);
          if (LIVE.includes(state) && fresh !== true) expect(v.kind).toBe(fresh === false ? 'stale' : 'unknown');
          if (ENDED.includes(state) || state === 'requested') expect(v.kind).toBe(state);
          if (!LIVE.includes(state) && !ENDED.includes(state) && state !== 'requested') expect(v.kind).toBe('unknown');
          expect(v.ring).toBe(v.kind === 'needs_input' ? 'waiting' : v.kind === 'requested' ? 'queued' : v.kind);
          expect(v.label.startsWith('Codex, ')).toBe(true);
        });

  it('never turns an old or unknown report into done, offline or running', () => {
    for (const fresh of [false, undefined]) {
      const v = resolveAgentState({ state: 'running', fresh });
      expect(['stale', 'unknown']).toContain(v.kind);
      expect(v.label).not.toMatch(/running|completed|offline|done/);
    }
  });

  it('words the age of a stale report', () => {
    const v = resolveAgentState({ state: 'running', fresh: false, observedAt: NOW - 6 * 60_000, now: NOW, agentName: 'Codex' });
    expect(v.label).toBe('Codex, last reported 6m ago');
    expect(resolveAgentState({ state: 'running', fresh: false, agentName: 'Codex' }).label).toBe('Codex, last report is out of date');
  });

  it('accepts the aliases callers use for the same state', () => {
    expect(resolveAgentState({ state: 'waiting', fresh: true }).ring).toBe('waiting');
    expect(resolveAgentState({ state: 'done' }).kind).toBe('completed');
  });

  it('labels a running agent for assistive technology', () => {
    expect(resolveAgentState({ state: 'running', fresh: true, agentName: 'Codex' }).label).toBe('Codex, running');
  });
});

describe('ageLabel', () => {
  it('rounds down and ignores what it cannot read', () => {
    expect(ageLabel(NOW - 5_000, NOW)).toBe('just now');
    expect(ageLabel(NOW - 6 * 60_000, NOW)).toBe('6m');
    expect(ageLabel(new Date(NOW - 3 * 3_600_000).toISOString(), NOW)).toBe('3h');
    expect(ageLabel(NOW - 2 * 86_400_000, NOW)).toBe('2d');
    expect(ageLabel(NOW + 1000, NOW)).toBe('');
    expect(ageLabel('soon', NOW)).toBe('');
    expect(ageLabel(undefined, NOW)).toBe('');
  });
});

describe('avatars', () => {
  it('takes initials from a name and never returns nothing', () => {
    expect(initials('Maya Lindqvist')).toBe('ML');
    expect(initials('maya')).toBe('M');
    expect(initials('  jamie.o-neil ')).toBe('JN');
    expect(initials('')).toBe('?');
    expect(initials(undefined)).toBe('?');
  });
  it('gives one member one tone, within range', () => {
    expect(avatarTone('member-1')).toBe(avatarTone('member-1'));
    const tones = new Set(Array.from({ length: 60 }, (_, n) => avatarTone('member-' + n)));
    expect(tones.size).toBeGreaterThan(3);
    for (const t of tones) { expect(t).toBeGreaterThanOrEqual(0); expect(t).toBeLessThan(globalThis.WerkbordIdentity.TONES); }
  });
});

describe('agent marks', () => {
  it("holds a path and colour for each mark it claims, Codex's being OpenAI's", () => {
    for (const [id, m] of Object.entries(AGENT_MARKS)) {
      expect(m.path.length, id).toBeGreaterThan(20);
      expect(m.colour, id).toMatch(/^#[0-9a-f]{6}$/);
    }
    for (const id of ['claude-code', 'codex', 'gemini-cli', 'opencode']) expect(AGENT_MARKS[id]).toBeDefined();
  });
  it('names agents', () => {
    expect(agentName('codex')).toBe('Codex');
    expect(agentName('claude-code')).toBe('Claude Code');
    expect(agentName('qwen-code')).toBe('Qwen Code');
    expect(agentName('x', 'Given')).toBe('Given');
    expect(agentName(undefined)).toBe('Agent');
  });
});

describe('identity.css', () => {
  it('rotates only the running ring, under no-preference motion', async () => {
    // @ts-expect-error node's own types are not installed in this package; the test runs under node.
    const { readFileSync } = await import('node:fs');
    const css: string = readFileSync(new URL('../../../internal/uipresentation/static/identity.css', import.meta.url), 'utf8');
    const lines = css.split('\n').filter(l => /\banimation:/.test(l));
    expect(lines).toHaveLength(1);
    expect(lines[0]).toContain("[data-ring='running']::before");
    expect(css.indexOf('animation:')).toBeGreaterThan(css.indexOf('@media (prefers-reduced-motion: no-preference)'));
    expect(css).toContain('data-motion-paused');
  });
});
