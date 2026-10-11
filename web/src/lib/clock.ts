// Times of day on a 12-hour or 24-hour clock. Pure: every function says which clock it writes, so a test can pin either.
// The app reads them through lib/clock.svelte.ts, which supplies the clock the person chose.

const cycle = (hour12: boolean): Intl.DateTimeFormatOptions => ({ hourCycle: hour12 ? 'h12' : 'h23', hour: hour12 ? 'numeric' : '2-digit', minute: '2-digit' });

/** Whether the device's locale writes times on a 12-hour clock. */
export function deviceHour12(): boolean {
  try {
    const hc = new Intl.DateTimeFormat(undefined, { hour: 'numeric' }).resolvedOptions().hourCycle;
    return hc === 'h11' || hc === 'h12';
  } catch {
    return false;
  }
}

/** "15:20" or "3:20 PM": the time of day of an instant. */
export function formatTime(at: Date | number | string, hour12: boolean): string {
  return new Intl.DateTimeFormat(undefined, cycle(hour12)).format(new Date(at));
}

/** "Mon 15:20": a weekday and the time, for something a few days old. */
export function formatWeekdayTime(at: Date | number | string, hour12: boolean): string {
  return new Intl.DateTimeFormat(undefined, { weekday: 'short', ...cycle(hour12) }).format(new Date(at));
}

/** "10/11/2026, 3:20 PM": a date and the time of day, for when something was last seen. */
export function formatDateTime(at: Date | number | string, hour12: boolean): string {
  return new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'numeric', day: 'numeric', ...cycle(hour12) }).format(new Date(at));
}

/** A wall time that has no date or zone of its own ("09:30" as the Calendar stores it) written on the chosen clock. */
export function formatWall(hhmm: string, hour12: boolean): string {
  const [h, m] = hhmm.slice(0, 5).split(':').map(Number);
  if (!Number.isFinite(h) || !Number.isFinite(m)) return hhmm;
  return new Intl.DateTimeFormat(undefined, { ...cycle(hour12), timeZone: 'UTC' }).format(new Date(Date.UTC(2000, 0, 1, h, m)));
}

/** The label of an hour on the Calendar's side: "09:00" or "9 AM". */
export function hourLabel(hour: number, hour12: boolean): string {
  if (!hour12) return `${String(hour).padStart(2, '0')}:00`;
  return new Intl.DateTimeFormat(undefined, { hourCycle: 'h12', hour: 'numeric', timeZone: 'UTC' }).format(new Date(Date.UTC(2000, 0, 1, hour)));
}
