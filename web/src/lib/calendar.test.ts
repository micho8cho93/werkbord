import { describe, expect, it } from 'vitest';
import { addDays, clockAt, lanes, minuteOf, rangeTitle, visibleDays, weekStart } from './calendar';

describe('calendar dates', () => {
  it('starts weeks on Monday', () => {
    expect(weekStart('2026-10-05')).toBe('2026-10-05');
    expect(weekStart('2026-10-11')).toBe('2026-10-05');
    expect(weekStart('2026-10-01')).toBe('2026-09-28');
    expect(visibleDays('2026-10-08', 'week')).toEqual(['2026-10-05', '2026-10-06', '2026-10-07', '2026-10-08', '2026-10-09', '2026-10-10', '2026-10-11']);
    expect(visibleDays('2026-10-08', 'day')).toEqual(['2026-10-08']);
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01');
  });

  it('titles a range as people write it', () => {
    expect(rangeTitle(visibleDays('2026-10-05', 'week'))).toBe('Oct 5 – 11, 2026');
    expect(rangeTitle(visibleDays('2026-10-01', 'week'))).toBe('Sep 28 – Oct 4, 2026');
    expect(rangeTitle(['2026-10-05'])).toBe('Mon, Oct 5, 2026');
  });

  it('reads and writes times of day', () => {
    expect(minuteOf('2026-10-05T09:30')).toBe(570);
    expect(clockAt(572)).toBe('09:30');
    expect(clockAt(-20)).toBe('00:00');
    expect(clockAt(24 * 60 + 5)).toBe('23:45');
  });
});

describe('lanes', () => {
  it('puts overlapping events side by side and lets the rest have the whole width', () => {
    const out = lanes(
      [
        { start: 540, item: 'a' },
        { start: 560, item: 'b' },
        { start: 700, item: 'c' },
      ],
      45,
    );
    expect(out).toEqual([
      { item: 'a', start: 540, lane: 0, of: 2 },
      { item: 'b', start: 560, lane: 1, of: 2 },
      { item: 'c', start: 700, lane: 0, of: 1 },
    ]);
  });
});
