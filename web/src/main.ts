import { mount } from 'svelte';
import App from './App.svelte';
import { adoptTokenFromURL } from './lib/api';
import { detectDesktop, openLinksInBrowser } from './lib/desktop.svelte';
import { app } from './lib/state.svelte';
import '@fontsource-variable/geist';
import '@fontsource-variable/geist-mono';
import './app.css';

adoptTokenFromURL();
app.start();
// In the desktop app (and only there) a link that leaves Werkbord opens in the person's own browser.
openLinksInBrowser();
void detectDesktop();

mount(App, { target: document.getElementById('app')! });

if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {
      // The app works without offline support.
    });
  });
}
