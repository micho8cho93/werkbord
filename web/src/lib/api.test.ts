import { afterEach, describe, expect, it, vi } from 'vitest';

afterEach(() => { vi.unstubAllGlobals(); vi.resetModules(); });

describe('browser credentials', () => {
  it.each(['unavailable', 'write denied'])('keeps a page-only token when storage is %s', async (failure) => {
    const localStorage = {
      getItem: () => null,
      setItem: () => { throw new Error('storage denied'); },
      removeItem: () => { throw new Error('storage denied'); },
    };
    vi.stubGlobal('window', failure === 'unavailable' ? { get localStorage() { throw new Error('storage denied'); } } : { localStorage });
    const { getToken, setToken } = await import('./api');
    setToken('disposable-test-token');
    expect(getToken()).toBe('disposable-test-token');
    setToken('');
    expect(getToken()).toBe('');
  });

  it('uses authoritative storage across tabs when storage works', async () => {
    const values = new Map<string, string>();
    vi.stubGlobal('window', { localStorage: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
    } });
    const { getToken, setToken } = await import('./api');
    setToken('fixture');
    expect(getToken()).toBe('fixture');
    values.clear();
    expect(getToken()).toBe('');
  });

  it('removes a malformed pairing fragment without crashing startup or losing a credential', async () => {
    vi.stubGlobal('window', { localStorage: null });
    vi.stubGlobal('location', { hash: '#token=%E0%A4%A', pathname: '/' });
    const replaceState = vi.fn();
    vi.stubGlobal('history', { replaceState });
    const { adoptTokenFromURL, getToken, setToken } = await import('./api');
    setToken('existing-fixture');
    expect(adoptTokenFromURL).not.toThrow();
    expect(getToken()).toBe('existing-fixture');
    expect(replaceState).toHaveBeenCalledWith(null, '', '/#/');
  });
});
