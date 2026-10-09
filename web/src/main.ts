import { mount } from 'svelte';
import App from './App.svelte';
import { adoptTokenFromURL } from './lib/api';
import { detectDesktop, openLinksInBrowser } from './lib/desktop.svelte';
import { embedded, followShell } from './lib/embed';
import { app } from './lib/state.svelte';
import '@fontsource-variable/geist';
import '@fontsource-variable/geist-mono';
import './app.css';

adoptTokenFromURL();
app.start();
// In the desktop app (and only there) a link that leaves Werkbord opens in the person's own browser.
openLinksInBrowser();
void detectDesktop();
// When the desktop app's shell shows this page as one workspace among several, it can ask it to go to a place in it.
followShell();

mount(App, { target: document.getElementById('app')! });

if (import.meta.env.PROD && 'serviceWorker' in navigator) {
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
