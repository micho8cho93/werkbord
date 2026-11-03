// The window is the app's own pages (this version) around the person's own Werkbord, which the controller serves (its version).
// When the controller is behind, some screens look old. This says so, in a few words, and what is in the way.

export interface UpdateState { behind: boolean; running?: string; carried?: string; reason?: string; waiting?: boolean }

export interface PartialUpdate { title: string; body: string }

/** What to tell the person, or null when the window is not partly updated. */
export function partialUpdate(s: UpdateState | null | undefined): PartialUpdate | null {
  if (!s?.behind) return null;
  const versions = s.running && s.carried ? `This app is ${s.carried}, but the part that runs your work is still ${s.running}, so some screens look old.` : 'Some screens look old.';
  const why = s.reason ? ` Waiting on: ${s.reason}.` : ' It is finishing now.';
  const how = s.reason && s.waiting ? ' It finishes by itself once that is clear.' : '';
  return { title: 'Werkbord is only partly updated.', body: versions + why + how };
}
