import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1';

let values: Map<string, string>;
let replaceState: ReturnType<typeof vi.fn>;

function page(hash: string) {
  values = new Map();
  replaceState = vi.fn();
  vi.stubGlobal('window', {
    localStorage: {
      getItem: (k: string) => values.get(k) ?? null,
      setItem: (k: string, v: string) => values.set(k, v),
      removeItem: (k: string) => values.delete(k),
    },
  });
  vi.stubGlobal('location', { hash, pathname: '/' });
  vi.stubGlobal('history', { replaceState });
}

function answer(status: number, body: unknown) {
  const fetch = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }));
  vi.stubGlobal('fetch', fetch);
  return fetch;
}

beforeEach(() => vi.resetModules());
afterEach(() => vi.unstubAllGlobals());

describe('scanning the QR code', () => {
  it('signs the phone in without asking anything of the person, and removes the code from the address bar', async () => {
    page('#enroll=abc_DEF-123');
    const fetch = answer(201, { id: 'ph_1', name: 'iPhone (Safari)', createdAt: '2026-10-15T12:00:00Z', token: 'wbp_secret' });
    const { enrollFromURL, enrollment } = await import('./enroll.svelte');
    const { getToken } = await import('../api');

    await enrollFromURL(IPHONE);

    expect(replaceState).toHaveBeenCalledWith(null, '', '/#/');
    expect(fetch).toHaveBeenCalledTimes(1);
    const [url, init] = fetch.mock.calls[0]!;
    expect(url).toBe('/api/phones/enroll');
    expect(init!.method).toBe('POST');
    expect(JSON.parse(init!.body as string)).toEqual({ code: 'abc_DEF-123', name: 'iPhone (Safari)' });
    // the app's token is the phone's own, kept where the app keeps its token
    expect(getToken()).toBe('wbp_secret');
    expect(values.get('werkbord.token')).toBe('wbp_secret');
    expect(enrollment.state).toBe('done');
    expect(enrollment.name).toBe('iPhone (Safari)');
  });

  it('sends no Authorization header it does not have, and never puts the code or the token in the address', async () => {
    page('#enroll=code1');
    const fetch = answer(201, { id: 'ph_1', name: 'iPhone', createdAt: 'x', token: 'wbp_t' });
    const { enrollFromURL } = await import('./enroll.svelte');
    await enrollFromURL(IPHONE);
    const init = fetch.mock.calls[0]![1]!;
    expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
    for (const call of replaceState.mock.calls) expect(String(call[2])).not.toMatch(/code1|wbp_t/);
  });

  it('says the same thing for a wrong, expired or used code, and keeps the sign-in it had', async () => {
    page('#enroll=spent');
    answer(403, { error: { code: 'forbidden', message: 'that code does not work: make a new one on your computer' } });
    const { enrollFromURL, enrollment } = await import('./enroll.svelte');
    const { getToken, setToken } = await import('../api');
    setToken('existing');

    await enrollFromURL(IPHONE);

    expect(enrollment.state).toBe('failed');
    expect(enrollment.error).toMatch(/did not work/);
    expect(enrollment.error).toMatch(/Settings → Phones/);
    expect(getToken()).toBe('existing');
    expect(replaceState).toHaveBeenCalledWith(null, '', '/#/'); // the failed code is not left in the address bar either
  });

  it('tells a rate-limited phone to wait, and an unreachable controller to check the relay connection', async () => {
    page('#enroll=x');
    answer(429, { error: { code: 'rate_limited', message: 'too many' } });
    let m = await import('./enroll.svelte');
    await m.enrollFromURL(IPHONE);
    expect(m.enrollment.error).toMatch(/Wait a minute/);

    vi.resetModules();
    page('#enroll=x');
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('network'); }));
    m = await import('./enroll.svelte');
    await m.enrollFromURL(IPHONE);
    expect(m.enrollment.error).toMatch(/relay/);
  });

  it('does nothing without a code, and does not touch a page that has only a token link', async () => {
    page('#token=owner');
    const fetch = answer(500, {});
    const { enrollFromURL, enrollment } = await import('./enroll.svelte');
    await enrollFromURL(IPHONE);
    expect(fetch).not.toHaveBeenCalled();
    expect(replaceState).not.toHaveBeenCalled();
    expect(enrollment.state).toBe('idle');
  });

  it('refuses a link with a malformed code without asking the controller, and ignores one with no code', async () => {
    for (const [hash, state] of [['#enroll=%E0%A4%A', 'failed'], ['#enroll=', 'idle']] as const) {
      vi.resetModules();
      page(hash);
      const fetch = answer(201, {});
      const { enrollFromURL, enrollment } = await import('./enroll.svelte');
      await enrollFromURL(IPHONE);
      expect(fetch, hash).not.toHaveBeenCalled();
      expect(enrollment.state, hash).toBe(state);
    }
  });

  it('can be dismissed once seen', async () => {
    page('#enroll=c');
    answer(201, { id: 'ph_1', name: 'iPhone', createdAt: 'x', token: 'wbp_t' });
    const { enrollFromURL, dismissEnrollment, enrollment } = await import('./enroll.svelte');
    await enrollFromURL(IPHONE);
    expect(enrollment.state).toBe('done');
    dismissEnrollment();
    expect(enrollment.state).toBe('idle');
  });
});

describe('the other ways of signing in are unchanged', () => {
  it('still adopts a #token= link, for a desktop browser or this computer', async () => {
    page('#token=owner-token');
    const { adoptTokenFromURL, getToken } = await import('../api');
    adoptTokenFromURL();
    expect(getToken()).toBe('owner-token');
    expect(replaceState).toHaveBeenCalledWith(null, '', '/#/');
  });
});
