import { mount } from 'svelte';
import App from './App.svelte';
import { adoptTokenFromURL } from './lib/api';
import { app } from './lib/state.svelte';
import './app.css';

adoptTokenFromURL();
app.start();

mount(App, { target: document.getElementById('app')! });

if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {
      // The app works without offline support.
    });
  });
}
