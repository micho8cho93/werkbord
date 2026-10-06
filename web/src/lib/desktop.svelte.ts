/**
 * Whether this page is inside the Werkbord desktop app, as far as the page can tell (see desktop.ts for
 * what the app offers and how a page asks). Reactive, so a banner can show "Update now" only where it works.
 */
import { callDesktop, externalLinkTarget, type DesktopInfo } from './desktop';

/** Whether this page is in the desktop app, which is known once it has answered. */
export const desktop = $state({ available: false, version: '', platform: '' });

/** Finds out whether the desktop app is here. Safe to call anywhere: in a browser it does nothing. */
export async function detectDesktop(): Promise<void> {
  try {
    const info = await callDesktop<DesktopInfo>('Info', [], 3000);
    desktop.available = true;
    desktop.version = info.version;
    desktop.platform = info.platform;
  } catch {
    // Not the app, or an app that will not talk to this page: it is an ordinary web page.
  }
}

/** In the desktop app, a link that leaves Werkbord opens in the person's own browser. */
export function openLinksInBrowser(): void {
  document.addEventListener(
    'click',
    (e) => {
      if (!desktop.available || e.defaultPrevented || e.button !== 0) return;
      const a = (e.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null;
      const url = externalLinkTarget(a, location.origin);
      if (!url) return;
      e.preventDefault();
      void callDesktop('OpenExternal', [url]).catch(() => {});
    },
    true,
  );
}
