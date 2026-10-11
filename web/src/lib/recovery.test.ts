import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from './api';
import { healthDisabled, readDraft, uncertainMutation, writeDraft } from './recovery';
afterEach(() => vi.unstubAllGlobals());
describe('recovery contracts', () => {
  it('recognizes explicit disabling, never infers it from a generic 503', () => {
    expect(healthDisabled(new ApiError(503, 'unavailable', 'repository health is not enabled'))).toBe(true);
    expect(healthDisabled(new ApiError(503, 'unavailable', 'Service unavailable'))).toBe(false);
    expect(healthDisabled(new ApiError(503, 'http_error', 'repository health is not enabled'))).toBe(false);
    expect(healthDisabled(new Error('repository health is not enabled'))).toBe(false);
  });
  it('guards lost and malformed responses, allows validation refusals to be corrected', () => {
    expect(uncertainMutation(new ApiError(0, 'offline', 'offline'))).toBe(true);
    expect(uncertainMutation(new ApiError(500, 'internal', 'unknown'))).toBe(true);
    expect(uncertainMutation(new SyntaxError('invalid response'))).toBe(true);
    expect(uncertainMutation(new ApiError(400, 'invalid', 'title required'))).toBe(false);
    expect(uncertainMutation(new ApiError(409, 'conflict', 'refused'))).toBe(false);
  });
  it('restores the draft and uncertainty together, isolates projects and clears acknowledged work', () => {
    const values = new Map<string, string>();
    vi.stubGlobal('window', { sessionStorage: { getItem: (k: string) => values.get(k) ?? null, setItem: (k: string, v: string) => values.set(k, v), removeItem: (k: string) => values.delete(k) } });
    const value = { title: 'Brief', description: 'Do not lose this', uncertainSave: true };
    expect(writeDraft('new-task.a', value)).toBe(true);
    expect(readDraft('new-task.a')).toEqual(value);
    expect(readDraft('new-task.b')).toBeNull();
    writeDraft('new-task.a', null);
    expect(readDraft('new-task.a')).toBeNull();
  });
  it('reports denied storage instead of promising reload recovery', () => {
    vi.stubGlobal('window', { get sessionStorage() { throw Error('denied'); } });
    expect(readDraft('answer')).toBeNull();
    expect(writeDraft('answer', { text: 'Keep this' })).toBe(false);
  });
});
