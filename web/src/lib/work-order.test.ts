import { describe, expect, it } from 'vitest';
import '../../../internal/uipresentation/static/work-queue.js';
import '../../../internal/uipresentation/static/work-order.js';

const { between, spread } = WerkbordWork.order;

describe('Work ordering positions', () => {
  it('places an item between two neighbours, or past an edge, on the same scale as the rest of the list', () => {
    expect(between(1, 3)).toBe(2);
    expect(between(1, 2)).toBe(1.5);
    expect(between(undefined, 4)).toBe(3);
    expect(between(4, undefined)).toBe(5);
    expect(between(undefined, undefined)).toBe(1);
  });
  it('reports when two neighbours have no room left, so the list is spread instead of guessing', () => {
    expect(between(1, 1)).toBeNull();
    expect(between(1, 1 + 1e-12)).toBeNull();
    expect(between(Number.MIN_VALUE, 0)).toBeNull();
  });
  it('keeps moving to the top without running out of room', () => {
    let first = 1;
    for (let i = 0; i < 200; i++) first = between(undefined, first) as number;
    expect(first).toBe(-199);
  });
  it('spreads n items evenly and in order', () => {
    expect(spread(0)).toEqual([]);
    expect(spread(3)).toEqual([1, 2, 3]);
  });
  it('survives repeated halving until the room truly runs out, then asks for a spread', () => {
    const before = 1;
    let after = 2, steps = 0;
    for (;;) {
      const next = between(before, after);
      if (next === null) break;
      expect(next).toBeGreaterThan(before);
      expect(next).toBeLessThan(after);
      after = next; steps++;
    }
    expect(steps).toBeGreaterThan(20);
    expect(steps).toBeLessThan(60);
  });
  it('merges with the queue grouping instead of replacing it, whichever file loads first', () => {
    expect(typeof WerkbordWork.group).toBe('function');
    expect(typeof WerkbordWork.order.between).toBe('function');
  });
});
