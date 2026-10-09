import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// A separate build: the Individual PWA never embeds the desktop workspace shell.
export default defineConfig({
  plugins: [svelte()],
  root: '.',
  base: './',
  publicDir: false,
  build: {
    outDir: '../desktop/frontend/dist/shell',
    emptyOutDir: true,
    target: 'es2022',
    rolldownOptions: { input: 'shell/index.html' },
  },
});
