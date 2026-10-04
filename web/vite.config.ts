import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// In development the Vite server proxies the API to a running controller
// (`devboard serve`). In production the controller serves the built files.
const controller = process.env.DEVBOARD_URL ?? 'http://127.0.0.1:7420';

export default defineConfig({
  plugins: [svelte()],
  server: {
    proxy: {
      '/api': { target: controller, changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
  },
});
