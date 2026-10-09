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

/** A page the desktop shell shows in a frame: it has a parent window, and talks to it with postMessage. */
function framedWindow() {
  const sent: { msg: Record<string, unknown>; target: string }[] = [];
  const listeners: ((e: { source: unknown; data: unknown }) => void)[] = [];
  const parent = { postMessage: (msg: Record<string, unknown>, target: string) => sent.push({ msg, target }) };
  const win: Record<string, unknown> = {
    parent,
    // The runtime exists in every frame in WebKit, but only the window's own page gets its answers.
    webkit: { messageHandlers: { external: { postMessage: () => { throw new Error('a frame must not use the runtime directly'); } } } },
    addEventListener: (_: string, l: (e: { source: unknown; data: unknown }) => void) => listeners.push(l),
    removeEventListener: () => {},
  };
  vi.stubGlobal('window', win);
  const deliver = (source: unknown, data: unknown) => listeners.forEach((l) => l({ source, data }));
  return { sent, parent, deliver };
}

describe('the desktop bridge in a frame of the shell', () => {
  it('asks the page that framed it, never the runtime, and resolves with its answer', async () => {
    const { sent, parent, deliver } = framedWindow();
    const { callDesktop } = await import('./desktop');
    const p = callDesktop<{ version: string }>('Info');
    expect(sent).toHaveLength(1);
    expect(sent[0].msg).toMatchObject({ type: 'werkbord.native.request', method: 'Info', args: [] });
    const id = sent[0].msg.id as string;
    deliver({}, { type: 'werkbord.native.result', id, ok: true, result: { version: 'x' } }); // not the parent: ignored
    deliver(parent, { type: 'werkbord.native.result', id, ok: true, result: { version: 'v1.8.0' } });
    await expect(p).resolves.toEqual({ version: 'v1.8.0' });
  });

  it('carries the shell’s refusal as an error', async () => {
    const { sent, parent, deliver } = framedWindow();
    const { callDesktop } = await import('./desktop');
    const p = callDesktop('ChooseDirectory');
    deliver(parent, { type: 'werkbord.native.result', id: sent[0].msg.id, ok: false, error: 'not for this workspace' });
    await expect(p).rejects.toThrow('not for this workspace');
  });

  it('follows the shell to a place inside the page and nowhere else', async () => {
    const { parent, deliver } = framedWindow();
    const loc = { hash: '' };
    vi.stubGlobal('location', loc);
    const { followShell } = await import('./embed');
    followShell();
    deliver({}, { type: 'werkbord.navigate', href: '#/control' }); // not the parent
    expect(loc.hash).toBe('');
    for (const bad of ['https://evil.example/', '//evil.example', '/p/x', 'javascript:alert(1)', '#/../x', '?tab=board']) {
      deliver(parent, { type: 'werkbord.navigate', href: bad });
    }
    expect(loc.hash).toBe('');
    deliver(parent, { type: 'werkbord.navigate', href: '#/p/prj_1/task/tsk_1' });
    expect(loc.hash).toBe('#/p/prj_1/task/tsk_1');
  });

  it('does not follow anyone in a page that has no parent', async () => {
    vi.stubGlobal('window', { addEventListener: () => { throw new Error('nothing to listen to'); } });
    const { followShell } = await import('./embed');
    expect(() => followShell()).not.toThrow();
  });
});
