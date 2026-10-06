import { afterEach, describe, expect, it, vi } from 'vitest';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
  vi.useRealTimers();
});

/** A WebKit window that records what the page posts to the app, as the desktop app's window is. */
function appWindow() {
  const posted: string[] = [];
  const win: Record<string, unknown> = { webkit: { messageHandlers: { external: { postMessage: (m: string) => posted.push(m) } } } };
  vi.stubGlobal('window', win);
  return { posted, win };
}

const call = (m: string) => JSON.parse(m.slice(1)) as { name: string; args: unknown[]; callbackID: string };

describe('the desktop bridge', () => {
  it('is not there in a browser, and nothing is attempted', async () => {
    vi.stubGlobal('window', {});
    const { callDesktop } = await import('./desktop');
    const { detectDesktop, desktop } = await import('./desktop.svelte');
    await expect(callDesktop('Info')).rejects.toThrow(/not the Werkbord desktop app/);
    await detectDesktop();
    expect(desktop.available).toBe(false);
  });

  it('speaks the Wails call protocol and resolves with the answer', async () => {
    const { posted, win } = appWindow();
    const { callDesktop } = await import('./desktop');
    const p = callDesktop<{ version: string }>('Info');
    expect(posted).toHaveLength(1);
    expect(posted[0][0]).toBe('C');
    const c = call(posted[0]);
    expect(c.name).toBe('main.App.Info');
    expect(c.args).toEqual([]);
    (win.wails as { Callback(m: string): void }).Callback(JSON.stringify({ callbackid: c.callbackID, result: { version: 'v1.1.0' } }));
    await expect(p).resolves.toEqual({ version: 'v1.1.0' });
  });

  it('rejects with the app’s own error', async () => {
    const { posted, win } = appWindow();
    const { callDesktop } = await import('./desktop');
    const p = callDesktop('RequestUpdate');
    (win.wails as { Callback(m: string): void }).Callback(JSON.stringify({ callbackid: call(posted[0]).callbackID, error: 'nope' }));
    await expect(p).rejects.toThrow('nope');
  });

  it('gives up on an app that never answers, unless it was told to wait', async () => {
    vi.useFakeTimers();
    const { posted } = appWindow();
    const { callDesktop } = await import('./desktop');
    const quick = callDesktop('Info', [], 3000);
    const patient = callDesktop('RequestUpdate', [], 0);
    let settled = false;
    patient.then(() => (settled = true), () => (settled = true));
    vi.advanceTimersByTime(60_000);
    await expect(quick).rejects.toThrow(/did not answer/);
    expect(settled).toBe(false);
    expect(posted).toHaveLength(2);
  });

  it('hands answers that are not its own to whoever listened before', async () => {
    const { win } = appWindow();
    const earlier = vi.fn();
    win.wails = { Callback: earlier };
    const { callDesktop } = await import('./desktop');
    void callDesktop('Info').catch(() => {});
    (win.wails as { Callback(m: string): void }).Callback('{"callbackid":"somebody-else","result":1}');
    (win.wails as { Callback(m: string): void }).Callback('not json');
    expect(earlier).toHaveBeenCalledTimes(2);
  });

  it('knows it is in the app once the app has answered Info', async () => {
    const { posted, win } = appWindow();
    const { detectDesktop, desktop } = await import('./desktop.svelte');
    const p = detectDesktop();
    (win.wails as { Callback(m: string): void }).Callback(
      JSON.stringify({ callbackid: call(posted[0]).callbackID, result: { version: 'v1.1.0', platform: 'darwin' } }),
    );
    await p;
    expect(desktop).toMatchObject({ available: true, version: 'v1.1.0', platform: 'darwin' });
  });

  it('stays an ordinary page when the app will not talk to it', async () => {
    vi.useFakeTimers();
    appWindow();
    const { detectDesktop, desktop } = await import('./desktop.svelte');
    const p = detectDesktop();
    vi.advanceTimersByTime(10_000);
    await p;
    expect(desktop.available).toBe(false);
  });
});

describe('links that leave Werkbord', () => {
  it('hands only other sites’ web addresses to the browser', async () => {
    const { externalLinkTarget } = await import('./desktop');
    const here = 'http://127.0.0.1:7420';
    expect(externalLinkTarget({ href: 'https://github.com/o/r/pull/1' }, here)).toBe('https://github.com/o/r/pull/1');
    expect(externalLinkTarget({ href: 'http://127.0.0.1:7421/' }, here)).toBe('http://127.0.0.1:7421/'); // another port is another site
    expect(externalLinkTarget({ href: 'http://127.0.0.1:7420/#/settings' }, here)).toBeNull(); // Werkbord's own pages
    for (const href of ['javascript:alert(1)', 'file:///etc/passwd', 'mailto:a@b.c', 'werkbord://update', 'data:text/html,x', '', 'not a url']) {
      expect(externalLinkTarget({ href }, here), href).toBeNull();
    }
    expect(externalLinkTarget(null, here)).toBeNull();
  });
});
