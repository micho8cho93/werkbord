import { afterEach, describe, expect, it, vi } from 'vitest';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.resetModules();
});

async function freshApp() {
  vi.stubGlobal('window', { localStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });
  return (await import('./state.svelte')).app;
}

describe('the notice and what can be done about it', () => {
  it('carries one action while it shows, and drops it with the notice', async () => {
    const app = await freshApp();
    const run = vi.fn();
    app.notify('Archived “Fix login”.', 12000, { label: 'Undo', run });
    expect(app.notice).toBe('Archived “Fix login”.');
    expect(app.noticeAction?.label).toBe('Undo');
    // The toast takes the action before it dismisses the notice, because dismissing clears it.
    const action = app.noticeAction;
    app.dismissNotice();
    expect(app.noticeAction).toBeNull();
    action?.run();
    expect(run).toHaveBeenCalledOnce();
  });

  it('forgets the action when the next notice has none', async () => {
    const app = await freshApp();
    app.notify('Archived.', 9000, { label: 'Undo', run: () => {} });
    app.notify('Restored.');
    expect(app.noticeAction).toBeNull();
  });

  it('drops the action when the notice times out', async () => {
    vi.useFakeTimers();
    const app = await freshApp();
    app.notify('Archived.', 1000, { label: 'Undo', run: () => {} });
    vi.advanceTimersByTime(1001);
    expect(app.notice).toBe('');
    expect(app.noticeAction).toBeNull();
  });
});
