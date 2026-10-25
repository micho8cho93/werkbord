// The new-team dialog's bookkeeping: what it is doing, and which new teams the person asked to connect their own runner to once
// an administrator has let them in. The wish is kept on this computer only; the connecting itself is asked of the app, which asks the
// person once more in a dialog a page cannot press (see Shell.connectRunner).

/** A team being created or joined, as the dialog shows it. */
export interface TeamSetupProgress {
  id: string;
  kind: 'create' | 'join';
  /** The person ticked "Connect this computer to the team". */
  connect: boolean;
  /** What the service is doing now, in words ("" when nothing is). */
  working: string;
  /** The join waits for an administrator to approve this computer. */
  waiting: boolean;
  /** The team is on this computer already: what failed is reaching it, and it is left from its own page. */
  exists: boolean;
  error: string;
  name: string;
}

const WANTED_KEY = 'werkbord.shell.runner-wanted';

/** The workspace ids that are to be asked about connecting a runner once their join is approved. */
export function readWanted(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(WANTED_KEY) ?? '[]');
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string' && /^team:[a-z0-9_]{1,24}$/.test(x)) : [];
  } catch {
    return [];
  }
}

export function writeWanted(ids: string[]): void {
  try {
    if (ids.length) localStorage.setItem(WANTED_KEY, JSON.stringify(ids));
    else localStorage.removeItem(WANTED_KEY);
  } catch {
    // Remembering is a convenience; the runner can be connected from the team's page.
  }
}
