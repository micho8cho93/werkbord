import { mount } from 'svelte';
import Shell from './Shell.svelte';
import '@fontsource-variable/geist';
import '@fontsource-variable/geist-mono';
import '../app.css';

try {
  const choice = localStorage.getItem('werkbord.theme');
  if (choice === 'light' || choice === 'dark') document.documentElement.dataset.theme = choice;
} catch { /* The operating system supplies the default theme. */ }
mount(Shell, { target: document.getElementById('app')! });
