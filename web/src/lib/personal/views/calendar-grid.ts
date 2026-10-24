// Month arithmetic for the calendar view, in the person's own time. Pure, so it can be tested without a screen.

const pad = (n: number) => String(n).padStart(2, '0');
export const isoDay = (date: Date): string => `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;

export interface Day { date: string; inMonth: boolean; today: boolean; weekend: boolean }

/** The first day of the week where the person lives: 0 is Sunday. Monday when the system does not say. */
export function weekStartsOn(locale = typeof navigator !== 'undefined' ? navigator.language : 'en'): number {
  try {
    const info = (new Intl.Locale(locale) as unknown as { weekInfo?: { firstDay: number }; getWeekInfo?: () => { firstDay: number } });
    const first = info.getWeekInfo?.().firstDay ?? info.weekInfo?.firstDay;
    return first === undefined ? 1 : first % 7;
  } catch {
    return 1;
  }
}

/** Whole weeks covering a month. `month` is 0-11. */
export function monthWeeks(year: number, month: number, weekStart: number, today = new Date()): Day[][] {
  const first = new Date(year, month, 1);
  const lead = (first.getDay() - weekStart + 7) % 7;
  const start = new Date(year, month, 1 - lead);
  const todayKey = isoDay(today);
  const weeks: Day[][] = [];
  for (let w = 0; w < 6; w++) {
    const days: Day[] = [];
    for (let d = 0; d < 7; d++) {
      const date = new Date(start.getFullYear(), start.getMonth(), start.getDate() + w * 7 + d);
      days.push({ date: isoDay(date), inMonth: date.getMonth() === month, today: isoDay(date) === todayKey, weekend: date.getDay() === 0 || date.getDay() === 6 });
    }
    // A last row that lies wholly in the next month is dropped.
    if (w >= 4 && !days.some((day) => day.inMonth)) break;
    weeks.push(days);
  }
  return weeks;
}

/** The calendar day a stored date or time falls on, in the person's time. */
export function dayOf(value: unknown, type: 'date' | 'datetime'): string {
  if (typeof value !== 'string') return '';
  if (type === 'date') return /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '' : isoDay(date);
}

/** The value that puts a record on another day, keeping the time of day it had. */
export function onDay(value: unknown, type: 'date' | 'datetime', day: string, fallbackHour = 9): string {
  if (type === 'date') return day;
  const [y, m, d] = day.split('-').map(Number);
  const old = typeof value === 'string' ? new Date(value) : new Date(NaN);
  const moved = Number.isNaN(old.getTime()) ? new Date(y, m - 1, d, fallbackHour, 0, 0) : new Date(y, m - 1, d, old.getHours(), old.getMinutes(), old.getSeconds());
  return moved.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

export function monthLabel(year: number, month: number, locale?: string): string {
  return new Date(year, month, 1).toLocaleDateString(locale, { month: 'long', year: 'numeric' });
}
export function weekdayLabels(weekStart: number, locale?: string): { short: string; long: string }[] {
  // 2023-01-01 was a Sunday.
  return Array.from({ length: 7 }, (_, i) => {
    const date = new Date(2023, 0, 1 + ((weekStart + i) % 7));
    return { short: date.toLocaleDateString(locale, { weekday: 'short' }), long: date.toLocaleDateString(locale, { weekday: 'long' }) };
  });
}
export function dayLabel(day: string, locale?: string): string {
  const [y, m, d] = day.split('-').map(Number);
  return new Date(y, m - 1, d).toLocaleDateString(locale, { weekday: 'long', month: 'long', day: 'numeric' });
}
