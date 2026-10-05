import { describe, expect, it } from 'vitest';
import { historyCells, shortRunId, tokensShort } from './runs';
import type { Run } from './types';

const now = Date.parse('2026-10-05T12:00:00Z');
const run = (o: Partial<Run>): Run => ({
  id: 'run_abcdef123',
  taskId: 't',
  projectId: 'p',
  agentId: 'claude',
  state: 'completed',
  policy: { interaction: 'interactive' },
  version: 1,
  createdAt: '2026-10-05T10:05:00Z',
  updatedAt: '2026-10-05T10:05:00Z',
  ...o,
});

describe('historyCells', () => {
  it('covers the last day in half hours, oldest first, ending at the current half hour', () => {
    const cells = historyCells([], now);
    expect(cells).toHaveLength(48);
    expect(cells[47].start).toBe(Date.parse('2026-10-05T11:30:00Z'));
    expect(cells.every((c) => c.tone === '' && c.runs === 0)).toBe(true);
  });

  it('colours the cells a run spanned by how it ended', () => {
    const cells = historyCells([run({ endedAt: '2026-10-05T10:40:00Z' })], now);
    const lit = cells.filter((c) => c.tone);
    expect(lit.map((c) => new Date(c.start).toISOString().slice(11, 16))).toEqual(['10:00', '10:30']);
    expect(lit.every((c) => c.tone === 'ok')).toBe(true);
  });

  it('shows a failure over a completion, and what is going on now over both', () => {
    const cells = historyCells(
      [
        run({ id: 'a', endedAt: '2026-10-05T11:40:00Z' }),
        run({ id: 'b', state: 'failed', createdAt: '2026-10-05T11:31:00Z', endedAt: '2026-10-05T11:35:00Z' }),
        run({ id: 'c', state: 'blocked', createdAt: '2026-10-05T11:50:00Z' }),
      ],
      now,
    );
    expect(cells[47]).toMatchObject({ tone: 'block', runs: 3 });
    expect(cells[46].tone).toBe('ok');
  });
});

describe('labels', () => {
  it('shortens IDs and token counts', () => {
    expect(shortRunId('run_abcdef123')).toBe('run-f123');
    expect(tokensShort(run({}))).toBe('');
    expect(tokensShort(run({ usage: { inputTokens: 9000, outputTokens: 2400, costKind: 'usage_only' } }))).toBe('11k');
    expect(tokensShort(run({ usage: { inputTokens: 900, outputTokens: 40, costKind: 'usage_only' } }))).toBe('940');
  });
});
