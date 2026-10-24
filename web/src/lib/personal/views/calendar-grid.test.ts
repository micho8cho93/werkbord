import { describe, expect, it } from 'vitest';
import { dayOf, isoDay, monthWeeks, onDay, weekdayLabels } from './calendar-grid';

describe('calendar month grid', () => {
  it('covers a month in whole weeks starting on the chosen weekday', () => {
    const weeks = monthWeeks(2026, 9, 1, new Date(2026, 9, 24)); // October 2026 starts on a Thursday
    expect(weeks.every((w) => w.length === 7)).toBe(true);
    expect(weeks[0][0].date).toBe('2026-09-28');
    expect(weeks[0][3]).toMatchObject({ date: '2026-10-01', inMonth: true });
    expect(weeks.at(-1)!.some((d) => d.date === '2026-10-31')).toBe(true);
    expect(weeks.flat().filter((d) => d.today).map((d) => d.date)).toEqual(['2026-10-24']);
    expect(weeks.flat().filter((d) => d.inMonth)).toHaveLength(31);
  });
  it('starts on Sunday when asked', () => {
    expect(monthWeeks(2026, 9, 0)[0][0].date).toBe('2026-09-27');
    expect(weekdayLabels(0, 'en-US')[0].long).toBe('Sunday');
    expect(weekdayLabels(1, 'en-US')[0].long).toBe('Monday');
  });
  it('drops a trailing week that belongs to the next month', () => {
    expect(monthWeeks(2026, 1, 0)).toHaveLength(4); // February 2026 is exactly four weeks from Sunday
    expect(monthWeeks(2026, 1, 1)).toHaveLength(5);
    expect(monthWeeks(2026, 9, 1)).toHaveLength(5);
  });
  it('places a stored date or time on a day in local time', () => {
    expect(dayOf('2026-10-24', 'date')).toBe('2026-10-24');
    expect(dayOf('2026-10-24T09:30:00Z', 'datetime')).toBe(isoDay(new Date('2026-10-24T09:30:00Z')));
    expect(dayOf('nonsense', 'datetime')).toBe('');
    expect(dayOf(4, 'date')).toBe('');
    expect(dayOf('2026-10-24T09:30:00Z', 'date')).toBe('');
  });
  it('moves a record to another day and keeps its time of day', () => {
    expect(onDay('2026-10-24', 'date', '2026-11-02')).toBe('2026-11-02');
    const moved = onDay('2026-10-24T09:30:00Z', 'datetime', '2026-11-02');
    const before = new Date('2026-10-24T09:30:00Z'), after = new Date(moved);
    expect([after.getFullYear(), after.getMonth(), after.getDate()]).toEqual([2026, 10, 2]);
    expect([after.getHours(), after.getMinutes()]).toEqual([before.getHours(), before.getMinutes()]);
    expect(onDay(undefined, 'datetime', '2026-11-02')).toMatch(/^2026-11-0[12]T\d\d:00:00Z$/);
  });
});
