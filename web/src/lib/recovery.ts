import { ApiError } from './api';

/** A mutation may have committed before its response was lost, including malformed success bodies. */
export function uncertainMutation(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 0 || error.status >= 500;
}

/** Recognize only the controller's explicit disabled contract; a generic 503 remains unknown. */
export function healthDisabled(error: unknown): boolean {
  return error instanceof ApiError && error.status === 503 && error.code === 'unavailable' && error.message === 'repository health is not enabled';
}

/** Tab-local, origin-scoped drafts survive reload without retaining briefs after the tab session. */
export function readDraft<T>(key: string): T | null {
  try { return JSON.parse(window.sessionStorage.getItem(`werkbord.draft.${key}`) ?? 'null') as T | null; }
  catch { return null; }
}

export function writeDraft(key: string, value: unknown): boolean {
  try {
    if (value === null) window.sessionStorage.removeItem(`werkbord.draft.${key}`);
    else window.sessionStorage.setItem(`werkbord.draft.${key}`, JSON.stringify(value));
    return true;
  } catch { return false; }
}
