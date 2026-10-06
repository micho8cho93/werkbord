import { afterEach, describe, expect, it, vi } from 'vitest';
import type { UpdateStatus } from './types';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

const status = (over: Partial<UpdateStatus> = {}): UpdateStatus => ({ current: 'v1.0.0', latest: 'v1.1.0', available: true, release: true, ...over });

async function appWith(update: UpdateStatus | null, dismissed = '') {
  const store = new Map<string, string>(dismissed ? [['werkbord.update.dismissed', dismissed]] : []);
  vi.stubGlobal('window', {
    localStorage: { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => store.set(k, v), removeItem: (k: string) => store.delete(k) },
  });
  const { app } = await import('./state.svelte');
  app.update = update;
  return { app, store };
}

describe('offering an update', () => {
  it('offers a newer release, once, until it is put off', async () => {
    const { app, store } = await appWith(status());
    expect(app.updateOffer?.latest).toBe('v1.1.0');
    app.dismissUpdate('v1.1.0');
    expect(app.updateOffer).toBeNull();
    expect(store.get('werkbord.update.dismissed')).toBe('v1.1.0');
    // A later release is a new offer.
    app.update = status({ latest: 'v1.2.0' });
    expect(app.updateOffer?.latest).toBe('v1.2.0');
  });

  it('remembers what was put off', async () => {
    const { app } = await appWith(status(), 'v1.1.0');
    expect(app.updateOffer).toBeNull();
  });

  it.each([
    ['up to date', status({ available: false, latest: 'v1.0.0' })],
    ['a build from source', status({ release: false, available: false, latest: undefined })],
    ['checking turned off', status({ disabled: true, available: false })],
    ['offline', status({ available: false, latest: undefined, error: 'dial tcp: no route' })],
    ['an older controller that cannot say', null],
  ])('offers nothing for %s', async (_name, update) => {
    const { app } = await appWith(update);
    expect(app.updateOffer).toBeNull();
  });

  it('asks the controller, and not about updates it cannot tell of', async () => {
    const { app } = await appWith(null);
    const { api } = await import('./api');
    const ask = vi.spyOn(api, 'updateStatus').mockResolvedValueOnce(status());
    await app.checkUpdate();
    expect(app.update?.latest).toBe('v1.1.0');
    // Not again within the hour, unless it was asked for.
    await app.checkUpdate();
    expect(ask).toHaveBeenCalledTimes(1);
    await app.checkUpdate(true);
    expect(ask).toHaveBeenLastCalledWith(true);
    // A controller that has no such endpoint is not an error anyone sees.
    ask.mockRejectedValueOnce(new Error('404'));
    await expect(app.checkUpdate(true)).resolves.toBeUndefined();
  });
});
