// The calendar's arithmetic, on wall dates ("2026-10-05") so a time zone is applied only once,
// where a time is read or written. No framework code, so it can be tested on its own.

const DAY = 86_400_000;
const at = (d: string) => Date.parse(`${d}T12:00:00Z`);
const iso = (ms: number) => new Date(ms).toISOString().slice(0, 10);

/** Adds days to a wall date. */
export function addDays(day: string, n: number): string {
  return iso(at(day) + n * DAY);
}

/** The Monday of the week a day is in. */
export function weekStart(day: string): string {
  const dow = (new Date(at(day)).getUTCDay() + 6) % 7; // Monday 0 … Sunday 6
  return addDays(day, -dow);
}

/** The days a view shows: the day itself, or its week from Monday. */
export function visibleDays(day: string, view: 'day' | 'week'): string[] {
  if (!Number.isFinite(at(day))) return [];
  if (view === 'day') return [day];
  const first = weekStart(day);
  return Array.from({ length: 7 }, (_, i) => addDays(first, i));
}

const MONTH = (d: string) => new Intl.DateTimeFormat('en', { month: 'short', timeZone: 'UTC' }).format(new Date(at(d)));

/** "Oct 5 – 11, 2026", "Sep 28 – Oct 4, 2026", "Mon, Oct 5, 2026". */
export function rangeTitle(days: readonly string[]): string {
  if (!days.length) return '';
  const a = days[0];
  const b = days[days.length - 1];
  const year = b.slice(0, 4);
  if (a === b) return new Intl.DateTimeFormat('en', { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' }).format(new Date(at(a)));
  const da = Number(a.slice(8));
  const db = Number(b.slice(8));
  return MONTH(a) === MONTH(b) ? `${MONTH(a)} ${da} – ${db}, ${year}` : `${MONTH(a)} ${da} – ${MONTH(b)} ${db}, ${year}`;
}

/** Minutes after midnight of a wall time ("2026-10-05T09:30" → 570). */
export function minuteOf(wall: string): number {
  const [h, m] = wall.slice(11, 16).split(':').map(Number);
  return h * 60 + m;
}

/** "HH:MM" for a minute of the day, snapped to `step` minutes and kept inside the day. */
export function clockAt(minute: number, step = 15): string {
  const m = Math.max(0, Math.min(24 * 60 - step, Math.round(minute / step) * step));
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

/**
 * Side-by-side lanes for events that would overlap. Each event occupies `length` minutes from its
 * start; events that touch a cluster share its width.
 */
export function lanes<T>(events: readonly { start: number; item: T }[], length: number): { item: T; start: number; lane: number; of: number }[] {
  const sorted = [...events].sort((a, b) => a.start - b.start);
  const out: { item: T; start: number; lane: number; of: number }[] = [];
  let cluster: { item: T; start: number; lane: number; of: number }[] = [];
  let ends: number[] = [];
  let clusterEnd = -1;
  const flush = () => {
    for (const e of cluster) e.of = Math.max(1, ends.length);
    out.push(...cluster);
    cluster = [];
    ends = [];
  };
  for (const e of sorted) {
    if (cluster.length && e.start >= clusterEnd) flush();
    let lane = ends.findIndex((end) => end <= e.start);
    if (lane === -1) lane = ends.length;
    ends[lane] = e.start + length;
    clusterEnd = Math.max(clusterEnd, e.start + length);
    cluster.push({ item: e.item, start: e.start, lane, of: 1 });
  }
  flush();
  return out;
}
