import { mount } from 'svelte';
import Shell from './Shell.svelte';
import '@fontsource-variable/geist';
import '@fontsource-variable/geist-mono';
import '../app.css';

try {
  const choice = localStorage.getItem('werkbord.theme');
  if (choice === 'light' || choice === 'dark') document.documentElement.dataset.theme = choice;
  const style = localStorage.getItem('werkbord.style');
  if (style === 'graphite' || style === 'classic') document.documentElement.dataset.style = style;
} catch { /* The operating system supplies the default theme. */ }
// Salie once had a floating button and a choice of where to put it. It is in the sidebar only now; what they left here is of no use.
try { for (const key of ['werkbord.shell.salie-placement', 'werkbord.shell.salie']) localStorage.removeItem(key); } catch { /* Nothing to remove. */ }
mount(Shell, { target: document.getElementById('app')! });
