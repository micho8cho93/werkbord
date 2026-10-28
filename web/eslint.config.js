import { defineConfig } from 'eslint/config';
import globals from 'globals';
import js from '@eslint/js';
import ts from 'typescript-eslint';
import svelte from 'eslint-plugin-svelte';
import svelteConfig from './svelte.config.js';

export default defineConfig(
  { ignores: ['dist/', 'node_modules/'] },
  js.configs.recommended,
  ts.configs.recommended,
  svelte.configs.recommended,
  {
    languageOptions: { globals: { ...globals.browser } },
  },
  {
    // The desktop app's web view answers "Cancel" to every native dialog, so what sat behind one silently never ran. Ask in the
    // page instead (src/lib/confirm.svelte.ts).
    files: ['src/**/*.ts', 'src/**/*.svelte'],
    ignores: ['**/*.test.ts'],
    rules: {
      'no-restricted-globals': ['error',
        { name: 'confirm', message: 'Native dialogs do not work in the desktop app. Use confirmAction() from lib/confirm.svelte.' },
        { name: 'alert', message: 'Native dialogs do not work in the desktop app. Show the message in the page.' },
        { name: 'prompt', message: 'Native dialogs do not work in the desktop app. Ask in the page.' },
      ],
      'no-restricted-properties': ['error',
        { object: 'window', property: 'confirm', message: 'Use confirmAction() from lib/confirm.svelte.' },
        { object: 'window', property: 'alert', message: 'Show the message in the page.' },
        { object: 'window', property: 'prompt', message: 'Ask in the page.' },
      ],
    },
  },
  {
    files: ['public/sw.js'],
    languageOptions: { globals: { ...globals.serviceworker } },
  },
  {
    files: ['**/*.svelte', '**/*.svelte.ts'],
    languageOptions: {
      parserOptions: {
        projectService: true,
        extraFileExtensions: ['.svelte'],
        parser: ts.parser,
        svelteConfig,
      },
    },
  },
);
