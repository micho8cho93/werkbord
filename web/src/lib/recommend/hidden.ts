// Suggestions a person dismissed or found not useful, remembered on this device so that the same suggestion does not come back
// while it is unchanged. The controller also keeps these in the person's memory where it serves memory; a phone, which does not
// reach memory, keeps them here only. A suggestion's id changes when the facts under it change, so a hidden one returns when
// something real is different.

const KEY = 'werkbord.recommendations.hidden';
const LIFE_MS = 14 * 24 * 60 * 60 * 1000;
const MAX = 50;

type Hidden = Record<string, number>;

function read(storage: Storage | null, now: number): Hidden {
  if (!storage) return {};
  try {
    const raw = JSON.parse(storage.getItem(KEY) ?? '{}') as unknown;
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {};
    const out: Hidden = {};
    for (const [id, until] of Object.entries(raw as Record<string, unknown>)) {
      if (/^rec_[0-9a-f]{20}$/.test(id) && typeof until === 'number' && until > now) out[id] = until;
    }
    return out;
  } catch {
    return {};
  }
}

const local = (): Storage | null => {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
};

export function isHidden(id: string, now = Date.now(), storage: Storage | null = local()): boolean {
  return (read(storage, now)[id] ?? 0) > now;
}

/** Remembers the id for two weeks. Keeps the newest 50 and forgets the rest. */
export function hide(id: string, now = Date.now(), storage: Storage | null = local()): void {
  if (!storage || !/^rec_[0-9a-f]{20}$/.test(id)) return;
  const all = read(storage, now);
  all[id] = now + LIFE_MS;
  const newest = Object.entries(all).sort(([, a], [, b]) => b - a).slice(0, MAX);
  try {
    storage.setItem(KEY, JSON.stringify(Object.fromEntries(newest)));
  } catch {
    // The suggestion is hidden for this visit; it may show again after a reload.
  }
}
