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

import { createNativeBridge } from '../../../internal/nativebridge/bridge.js';

const call = createNativeBridge('main.App', {
  unavailable: 'this is not the Werkbord desktop app',
  timeout: 'the desktop app did not answer',
});

export function callDesktop<T>(method: string, args: unknown[] = [], timeoutMs = 5000): Promise<T> {
  return call(method, args, timeoutMs) as Promise<T>;
}

export interface DesktopInfo {
  version: string;
  platform: string;
  /** This app can replace itself (a build that carries its updater). Optional: an app from before it existed does not send it. */
  updater?: boolean;
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
