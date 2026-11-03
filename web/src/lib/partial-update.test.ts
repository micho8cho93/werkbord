import { describe, expect, it } from 'vitest';
import { partialUpdate } from './partial-update';

describe('the window says when it is only partly updated', () => {
  it('says nothing when the controller is up to date, or the app has nothing to say', () => {
    expect(partialUpdate({ behind: false })).toBeNull();
    expect(partialUpdate(null)).toBeNull();
    expect(partialUpdate(undefined)).toBeNull();
  });

  it('names both versions and what is in the way, and that it finishes by itself', () => {
    const say = partialUpdate({ behind: true, running: '5.5.2-preview.1', carried: '5.6.0-preview.24', reason: 'Codex in test-repo has been running since Oct 27 and is still open', waiting: true });
    expect(say?.title).toBe('Werkbord is only partly updated.');
    expect(say?.body).toBe('This app is 5.6.0-preview.24, but the part that runs your work is still 5.5.2-preview.1, so some screens look old. Waiting on: Codex in test-repo has been running since Oct 27 and is still open. It finishes by itself once that is clear.');
  });

  it('does not promise a retry that is not scheduled, and does not invent a reason', () => {
    expect(partialUpdate({ behind: true, running: '1.8.0', carried: '1.9.0', reason: 'device operations are pending' })?.body).not.toContain('by itself');
    expect(partialUpdate({ behind: true, running: '1.8.0', carried: '1.9.0' })?.body).toContain('It is finishing now.');
  });

  it('still says something when the versions are not known', () => {
    expect(partialUpdate({ behind: true })?.body).toBe('Some screens look old. It is finishing now.');
  });
});
