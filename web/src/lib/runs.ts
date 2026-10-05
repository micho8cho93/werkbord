// A project's runs, summarised: what the Runs page and the Overview draw. No framework code.

import { runStatus, type Tone } from './format';
import type { Run } from './types';

export interface HistoryCell {
  /** Start of the cell, in milliseconds. */
  start: number;
  /** The colour of the cell: what the runs in it came to, or are doing now. Empty: nothing ran. */
  tone: Tone | '';
  runs: number;
}

/** Tones of active runs, most pressing first: what a cell shows when several runs overlap it. */
const ACTIVE_ORDER: Tone[] = ['block', 'ask', 'idle', 'work'];
const ENDED_ORDER: Tone[] = ['bad', 'ok', 'neutral'];

/**
 * The last `hours` hours in cells of `cellMinutes`, oldest first. A cell is coloured by the runs
 * that were going during it: a run still going shows its state now; otherwise a failure shows
 * over a completion, and a completion over a stop. Nothing is invented: a cell with no run is empty.
 */
export function historyCells(runs: readonly Run[], now: number, hours = 24, cellMinutes = 30): HistoryCell[] {
  const size = cellMinutes * 60_000;
  const count = Math.round((hours * 60) / cellMinutes);
  const end = Math.ceil(now / size) * size;
  const first = end - count * size;
  const spans = runs.map((r) => ({ from: Date.parse(r.createdAt), to: r.endedAt ? Date.parse(r.endedAt) : now, tone: runStatus(r).tone, active: !r.endedAt && runStatus(r).active }));
  const cells: HistoryCell[] = [];
  for (let i = 0; i < count; i++) {
    const start = first + i * size;
    const stop = start + size;
    const inCell = spans.filter((s) => s.from < stop && s.to >= start);
    const active = inCell.filter((s) => s.active).map((s) => s.tone);
    const ended = inCell.filter((s) => !s.active).map((s) => s.tone);
    const tone = ACTIVE_ORDER.find((t) => active.includes(t)) ?? ENDED_ORDER.find((t) => ended.includes(t)) ?? '';
    cells.push({ start, tone, runs: inCell.length });
  }
  return cells;
}

/** A run's ID as people read it: its last characters, in mono. */
export function shortRunId(id: string): string {
  const bare = id.replace(/^run_/, '');
  return `run-${bare.slice(-4)}`;
}

/** Tokens a run reported, in a few characters ("12.4k"), or empty if it reported none. */
export function tokensShort(run: Run): string {
  const n = (run.usage?.inputTokens ?? 0) + (run.usage?.outputTokens ?? 0);
  if (!n) return '';
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}
