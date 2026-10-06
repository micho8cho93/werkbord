/**
 * The Werkbord desktop app shows this same web app in a native window (docs/DESKTOP.md): there is one
 * interface, served by the controller, and it is the same in a browser, on a phone and here. What a web
 * page cannot do it asks the app for, through the few things the app offers (Info, OpenExternal,
 * UpdateStatus, RequestUpdate), and only when it finds the app there. Nothing in a browser changes.
 *
 * The app is a WebKit window, which gives a page one way to talk to it: window.webkit.messageHandlers.external.
 * The messages are the Wails runtime's calls ("C" + JSON), and the answer comes back to window.wails.Callback.
 * The app only answers a page the controller served (Wails checks the origin), and none of what it offers
 * returns anything the page does not already hold.
 */

interface Handler {
  postMessage(message: string): void;
}
interface Bridged {
  webkit?: { messageHandlers?: { external?: Handler } };
  wails?: { Callback?: (message: string) => void };
}

interface Pending {
  resolve(value: unknown): void;
  reject(error: Error): void;
  timer?: ReturnType<typeof setTimeout>;
}
const pending = new Map<string, Pending>();
let sequence = 0;
let installed = false;

function handler(): Handler | null {
  const h = (globalThis as unknown as { window?: Bridged }).window?.webkit?.messageHandlers?.external;
  return h && typeof h.postMessage === 'function' ? h : null;
}

/** Listens for the app's answers. Anything that is not one of ours goes on to whoever was listening before. */
function listen(): void {
  if (installed) return;
  installed = true;
  const w = (globalThis as unknown as { window: Bridged }).window;
  w.wails = w.wails ?? {};
  const previous = w.wails.Callback;
  w.wails.Callback = (message: string) => {
    let answer: { callbackid?: string; result?: unknown; error?: unknown } | undefined;
    try {
      answer = JSON.parse(message);
    } catch {
      // Not ours.
    }
    const entry = answer?.callbackid ? pending.get(answer.callbackid) : undefined;
    if (!answer || !entry) {
      previous?.(message);
      return;
    }
    pending.delete(answer.callbackid!);
    clearTimeout(entry.timer);
    if (answer.error) entry.reject(new Error(String(answer.error)));
    else entry.resolve(answer.result);
  };
}

/**
 * Calls something the desktop app offers. It rejects when the app is not there, and when it does not
 * answer in time (timeoutMs 0 waits as long as it takes: an update does).
 */
export function callDesktop<T>(method: string, args: unknown[] = [], timeoutMs = 5000): Promise<T> {
  const h = handler();
  if (!h) return Promise.reject(new Error('this is not the Werkbord desktop app'));
  listen();
  return new Promise<T>((resolve, reject) => {
    const id = `wb${++sequence}`;
    const entry: Pending = { resolve: resolve as (v: unknown) => void, reject };
    if (timeoutMs > 0) {
      entry.timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error('the desktop app did not answer'));
      }, timeoutMs);
    }
    pending.set(id, entry);
    h.postMessage('C' + JSON.stringify({ name: `main.App.${method}`, args, callbackID: id }));
  });
}

export interface DesktopInfo {
  version: string;
  platform: string;
}

/**
 * Where a click on this link should go instead of into the window: the address of a link that leaves
 * Werkbord (GitHub, Tailscale, a pull request), which the window itself cannot open and must not
 * navigate to. Null for everything else.
 */
export function externalLinkTarget(a: { href?: string } | null | undefined, origin: string): string | null {
  if (!a?.href) return null;
  let url: URL;
  try {
    url = new URL(a.href);
  } catch {
    return null;
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
  return url.origin === origin ? null : url.href;
}
