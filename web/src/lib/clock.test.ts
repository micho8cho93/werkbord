import { describe, expect, it } from 'vitest';
import { formatDateTime, formatTime, formatWall, formatWeekdayTime, hourLabel } from './clock';

// 15:20 in the zone of the machine running the test.
const at = new Date(2026, 9, 11, 15, 20);

describe('the clock', () => {
  it('writes the same moment on either clock', () => {
    expect(formatTime(at, false)).toBe('15:20');
    expect(formatTime(at, true)).toMatch(/^3:20\s?PM$/i);
  });

  it('pads the 24-hour clock and never writes 24:00', () => {
    expect(formatTime(new Date(2026, 9, 11, 0, 5), false)).toBe('00:05');
    expect(formatTime(new Date(2026, 9, 11, 9, 5), false)).toBe('09:05');
    expect(formatTime(new Date(2026, 9, 11, 0, 5), true)).toMatch(/^12:05\s?AM$/i);
  });

  it('writes a wall time the Calendar stores, without moving it by a zone', () => {
    expect(formatWall('09:30', false)).toBe('09:30');
    expect(formatWall('09:30', true)).toMatch(/^9:30\s?AM$/i);
    expect(formatWall('21:00', true)).toMatch(/^9:00\s?PM$/i);
    expect(formatWall('not a time', true)).toBe('not a time');
  });

  it('labels the hour grid for either clock', () => {
    expect(hourLabel(0, false)).toBe('00:00');
    expect(hourLabel(15, false)).toBe('15:00');
    expect(hourLabel(0, true)).toMatch(/^12\s?AM$/i);
    expect(hourLabel(15, true)).toMatch(/^3\s?PM$/i);
  });

  it('carries the weekday and the date alongside the time', () => {
    expect(formatWeekdayTime(at, false)).toMatch(/Sun.*15:20|15:20.*Sun/);
    expect(formatDateTime(at, false)).toContain('15:20');
    expect(formatDateTime(at, true)).toMatch(/3:20\s?PM/i);
  });
});
