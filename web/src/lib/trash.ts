// The trash: a task or project goes Done/active, then archived, then deleted, and a deleted one is erased once its time is up. The
// controller erases (internal/domain/archive.go, TrashRetention); this only says how long is left, in words.

/** The days a deleted task or project can be restored for. The same number as the controller's TrashRetention. */
export const TRASH_DAYS = 30;

const DAY = 86_400_000;

/** Whole days left before something deleted at `deletedAt` is erased, never below 0. */
export function daysLeft(deletedAt: string, now: number): number {
  const erases = Date.parse(deletedAt) + TRASH_DAYS * DAY;
  if (!Number.isFinite(erases)) return 0;
  return Math.max(0, Math.ceil((erases - now) / DAY));
}

/** "Erased for good in 12 days", "Erased tomorrow", "Erased today". */
export function erasure(deletedAt: string, now: number): string {
  const n = daysLeft(deletedAt, now);
  return n <= 0 ? 'Erased today' : n === 1 ? 'Erased tomorrow' : `Erased for good in ${n} days`;
}

/** What a project says about finishing work by itself. */
export const ARCHIVE_AFTER: { days: number; label: string }[] = [
  { days: 0, label: 'Never' },
  { days: 7, label: 'After 7 days' },
  { days: 14, label: 'After 14 days' },
  { days: 30, label: 'After 30 days' },
  { days: 90, label: 'After 90 days' },
];
