import { mount } from 'svelte';
import App from './App.svelte';
import { adoptTokenFromURL } from './lib/api';
import { enrollFromURL } from './lib/phones/enroll.svelte';
import { assistant } from './lib/assistant/sessions.svelte';
import { detectDesktop, openLinksInBrowser } from './lib/desktop.svelte';
import { embedded, followShell, surface } from './lib/embed';
import { onStyle, onTheme } from '../../internal/nativebridge/bridge.js';
import { look, theme } from './lib/theme.svelte';
import AssistantSurface from './lib/assistant/AssistantSurface.svelte';
import { app } from './lib/state.svelte';
import '@fontsource-variable/geist';
import '@fontsource-variable/geist-mono';
import './app.css';

adoptTokenFromURL();

// A phone that scanned its QR code is signed in before anything is asked of the controller.
async function boot() {
  await enrollFromURL();

  if (surface) {
    // The assistant's own frame in the desktop window: the assistant over the whole window and nothing else, started by the window's
    // sidebar button. It needs the sign-in, the theme the person chose, and to know it is in the app (for its file dialog); the board,
    // its live updates and the service worker are the workspace page's business.
    document.documentElement.classList.add('asst-surface');
    onTheme((choice) => theme.set(choice));
    onStyle((choice) => look.set(choice, true));
    void detectDesktop();
    app.start();
    assistant.init();
    mount(AssistantSurface, { target: document.getElementById('app')! });
  } else {
    app.start();
    // In the desktop app (and only there) a link that leaves Werkbord opens in the person's own browser.
    openLinksInBrowser();
    void detectDesktop();
    // When the desktop app's shell shows this page as one workspace among several, it can ask it to go to a place in it.
    followShell();
    // A conversation this tab already had (a reload, a return) is picked up where it was; nothing starts until the person opens it.
    // (In the desktop window the assistant is the shell's own frame, not this page's.)
    if (!embedded()) assistant.init();
    mount(App, { target: document.getElementById('app')! });
  }
}
void boot();

if (!surface && import.meta.env.PROD && 'serviceWorker' in navigator) {
  if (embedded()) {
    // The desktop window shows this page in a frame, where a worker only does harm (see public/sw.js) and nothing is
    // installed or used offline. Remove one that an earlier version registered there.
    void navigator.serviceWorker
      .getRegistrations()
      .then((registrations) => Promise.all(registrations.map((r) => r.unregister())))
      .catch(() => {});
  } else {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('/sw.js').catch(() => {
        // The app works without offline support.
      });
    });
  }
}
