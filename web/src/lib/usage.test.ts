import { describe, expect, it } from 'vitest';
import { budgetText, budgetsFrom, easesLabel, hasUsage, parseTokens, pressure, tokensLabel, windowLine, type UsageMeter, type UsageWindow } from './usage';

const win = (o: Partial<UsageWindow> = {}): UsageWindow => ({ tokens: 0, runs: 0, unreported: 0, budget: 0, ...o });

describe('usage meter wording', () => {
  it('abbreviates token counts without inventing precision', () => {
    expect([0, 999, 1500, 12000, 1234000, 2e9].map(tokensLabel)).toEqual(['0', '999', '1.5k', '12k', '1.2M', '2B']);
  });
  it('writes a saved budget back the way it was typed', () => {
    expect([2e6, 500e3, 1234567, 3e9, 0, undefined].map(budgetText)).toEqual(['2M', '500k', '1234567', '3B', '', '']);
    expect([2e6, 500e3, 1234567].every(n => parseTokens(budgetText(n)) === n)).toBe(true);
  });
  it('reads a typed budget', () => {
    expect(['2M', '500k', '1.5m', '2,000,000', ' 3 K ', ''].map(parseTokens)).toEqual([2e6, 5e5, 1.5e6, 2e6, 3e3, 0]);
    expect(['abc', '-5', '1e9', '99999999999999'].map(parseTokens)).toEqual([null, null, null, null]);
  });
  it('says only what was measured, and when runs reported nothing', () => {
    expect(windowLine(win({ tokens: 1200000 }))).toBe('1.2M used');
    expect(windowLine(win({ tokens: 900, budget: 1000, percent: 90 }))).toBe('900 used of 1k · 90%');
    expect(windowLine(win({ tokens: 5, unreported: 2 }))).toBe('5 used · 2 runs reported no tokens');
  });
  it('is calm below the threshold, near at it and over past the budget', () => {
    expect(pressure(win(), 80)).toBe('none');
    expect([50, 80, 99, 100, 140].map(p => pressure(win({ budget: 1, percent: p }), 80))).toEqual(['ok', 'near', 'near', 'over', 'over']);
  });
  it('says when a figure starts to fall, never a reset', () => {
    const now = Date.parse('2026-11-02T12:00:00Z');
    expect(easesLabel('2026-11-02T12:30:00Z', now)).toBe('eases in 30 min');
    expect(easesLabel('2026-11-02T15:00:00Z', now)).toBe('eases in about 3 h');
    expect(easesLabel('2026-11-05T12:00:00Z', now)).toBe('eases in about 3 days');
    expect(easesLabel('2026-11-02T11:00:00Z', now)).toBe('');
    expect(easesLabel(undefined, now)).toBe('');
  });
  it('shows a meter only when there is usage or a budget', () => {
    const m = (a: UsageMeter['agents']): UsageMeter => ({ at: '', mode: 'standard', saverAt: 80, agents: a });
    expect(hasUsage(undefined)).toBe(false);
    expect(hasUsage(m([]))).toBe(false);
    expect(hasUsage(m([{ agent: 'codex', fiveHour: win({ budget: 5 }), weekly: win() }]))).toBe(true);
    expect(hasUsage(m([{ agent: 'codex', fiveHour: win({ runs: 1 }), weekly: win() }]))).toBe(true);
  });
  it('turns typed fields into budgets, leaving empty ones out and reporting a bad one', () => {
    expect(budgetsFrom({ codex: { fiveHour: '2M', weekly: '' }, gemini: { fiveHour: '', weekly: '' } })).toEqual({ budgets: { codex: { fiveHour: 2e6 } }, problem: '' });
    expect(budgetsFrom({ codex: { fiveHour: 'lots', weekly: '' } }).problem).toContain('codex');
  });
});
